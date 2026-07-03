package vm

import (
	"fmt"
	"runtime"
	"sort"
	"time"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file implements Ruby threads on top of an emulated Global VM Lock (GVL):
// exactly one Ruby thread executes VM bytecode at a time, matching MRI's memory
// model. Each Thread.new runs a goroutine that must hold vm.gvl to run; the lock
// is released only inside the blocking native methods here (Thread#join,
// Mutex#lock, Queue#pop, Kernel#sleep, Thread.pass). On each release the thread's
// execution context (its current fiber, $~, the rescued-exception slot, the
// require stack) is saved and the next runnable thread's is restored, so the
// shared VM fields never carry one thread's state into another.
//
// Scheduling is cooperative: a thread yields only at those blocking points (no
// time-slice preemption), which is sufficient for the deterministic concurrency
// patterns Ruby programs rely on — Queue producer/consumer, Mutex sections, and
// join/value — while keeping the whole design race-free under `go test -race`.

// RThread backs a Ruby Thread.
type RThread struct {
	blk    *Proc
	args   []object.Value
	result object.Value
	err    *RubyError // unhandled exception, re-raised on join/value
	done   chan struct{}
	status string                        // "run" | "sleep" | "dead"
	name   object.Value                  // Thread#name (nil or a String)
	locals map[object.Value]object.Value // Thread#[] / #[]= (fiber-local)
	tvars  map[object.Value]object.Value // Thread#thread_variable_get/set (thread-local)
	abort  bool                          // abort_on_exception

	// Eager-start handshake: a freshly spawned thread runs immediately (as in
	// MRI) until its first blocking point or completion, at which moment it hands
	// control back to its spawner over handback. parked guards that one-shot
	// handoff (the main thread starts parked, so it never hands back).
	handback chan struct{}
	parked   bool

	// Execution context parked here while this thread does not hold the GVL.
	savedFiber     *Fiber
	savedLastMatch object.Value
	savedCurExc    object.Value
	savedReqDirs   []string
}

func (t *RThread) ToS() string     { return "#<Thread>" }
func (t *RThread) Inspect() string { return "#<Thread:" + t.status + ">" }
func (t *RThread) Truthy() bool    { return true }

// isDone reports whether the thread has finished (its done channel is closed).
func (t *RThread) isDone() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *RThread) saveCtx(vm *VM) {
	t.savedFiber = vm.currentFiber
	t.savedLastMatch = vm.lastMatch
	t.savedCurExc = vm.curExc
	t.savedReqDirs = vm.requireDirs
}

func (t *RThread) restoreCtx(vm *VM) {
	vm.currentFiber = t.savedFiber
	vm.lastMatch = t.savedLastMatch
	vm.curExc = t.savedCurExc
	vm.requireDirs = t.savedReqDirs
	vm.currentThread = t
}

// threadBlock releases the GVL, runs the blocking wait fn while other threads
// run, then re-acquires the GVL and restores this thread's context. The caller
// must currently hold the GVL.
func (vm *VM) threadBlock(fn func()) {
	t := vm.currentThread
	t.saveCtx(vm)
	prev := t.status
	t.status = "sleep"
	vm.gvl.Unlock()
	t.firstPark() // hand control back to the spawner on this thread's first block
	fn()
	vm.gvl.Lock()
	t.restoreCtx(vm)
	t.status = prev
}

// firstPark performs the one-shot eager-start handoff: the first time a spawned
// thread releases the GVL (by blocking or finishing) it signals its spawner,
// which is parked in eagerStart. The main thread starts parked, so this no-ops
// for it and for any thread past its first yield.
func (t *RThread) firstPark() {
	if !t.parked {
		t.parked = true
		t.handback <- struct{}{}
	}
}

// eagerStart hands the GVL to a freshly spawned thread and waits until it first
// blocks or finishes, so a new thread runs immediately as in MRI. The caller
// (the spawning thread) must hold the GVL.
func (vm *VM) eagerStart(t *RThread) {
	cur := vm.currentThread
	cur.saveCtx(vm)
	vm.gvl.Unlock()
	<-t.handback
	vm.gvl.Lock()
	cur.restoreCtx(vm)
}

// threadCaptureErr turns a panic recovered in a thread's goroutine into the
// RubyError to re-raise on join: a Ruby exception is preserved as-is; any other
// panic (a Go-level failure) is wrapped as a RuntimeError rather than crashing
// the process.
func threadCaptureErr(r any) *RubyError {
	if re, ok := r.(RubyError); ok {
		return &re
	}
	e := RubyError{Class: "RuntimeError", Message: fmt.Sprint(r)}
	return &e
}

// RMutex backs a Ruby Mutex (Thread::Mutex).
type RMutex struct {
	owner *RThread
	waitq []mutexWaiter
}

type mutexWaiter struct {
	t  *RThread
	ch chan struct{}
}

func (m *RMutex) ToS() string     { return "#<Thread::Mutex>" }
func (m *RMutex) Inspect() string { return m.ToS() }
func (m *RMutex) Truthy() bool    { return true }

// RQueue backs a Ruby Queue (Thread::Queue): an unbounded thread-safe FIFO.
type RQueue struct {
	items  []object.Value
	waitq  []chan struct{}
	closed bool
}

func (q *RQueue) ToS() string     { return "#<Thread::Queue>" }
func (q *RQueue) Inspect() string { return q.ToS() }
func (q *RQueue) Truthy() bool    { return true }

func (vm *VM) registerThread() {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	if _, ok := vm.consts["ThreadError"]; !ok {
		vm.consts["ThreadError"] = object.Wrap(newClass("ThreadError", std))
	}
	// StopIteration is in place from the Phase-3 exception hierarchy (built before
	// the stdlib), so ClosedQueueError < StopIteration as in MRI.
	if _, ok := vm.consts["ClosedQueueError"]; !ok {
		vm.consts["ClosedQueueError"] = object.Wrap(newClass("ClosedQueueError", object.Kind[*RClass](vm.consts["StopIteration"])))
	}

	vm.registerThreadClass()
	vm.registerMutex()
	vm.registerQueue()
	vm.registerSleep()
}

func (vm *VM) registerThreadClass() {
	cThread := newClass("Thread", vm.cObject)
	vm.consts["Thread"] = object.Wrap(cThread)
	sdef := func(name string, fn NativeFn) {
		cThread.smethods[name] = &Method{name: name, owner: cThread, native: fn}
	}

	spawn := func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ThreadError", "must be called with a block")
		}
		t := &RThread{
			blk: blk, args: append([]object.Value{}, args...),
			done: make(chan struct{}), status: "run", handback: make(chan struct{}),
			locals: map[object.Value]object.Value{},
		}
		vm.threads = append(vm.threads, t)
		go func() {
			vm.gvl.Lock()
			t.restoreCtx(vm)
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.err = threadCaptureErr(r)
					}
				}()
				t.result = vm.callBlock(t.blk, t.args)
			}()
			t.status = "dead"
			close(t.done)
			t.firstPark() // release the spawner if the thread never blocked
			vm.gvl.Unlock()
		}()
		vm.eagerStart(t)
		return object.Wrap(t)
	}
	sdef("new", spawn)
	sdef("start", spawn)
	sdef("fork", spawn)
	sdef("current", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(vm.currentThread)
	})
	sdef("main", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(vm.mainThread)
	})
	sdef("list", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		var live []object.Value
		for _, t := range vm.threads {
			if !t.isDone() {
				live = append(live, object.Wrap(t))
			}
		}
		return object.Wrap(object.NewArrayFromSlice(live))
	})
	sdef("pass", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.threadBlock(runtime.Gosched)
		return object.NilVal()
	})

	cThread.define("join", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := object.Kind[*RThread](self)
		vm.threadJoin(t)
		return object.Wrap(t)
	})
	cThread.define("value", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := object.Kind[*RThread](self)
		vm.threadJoin(t)
		return t.result
	})
	cThread.define("alive?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(!object.Kind[*RThread](self).isDone())))
	})
	cThread.define("status", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := object.Kind[*RThread](self)
		if t.isDone() {
			if t.err != nil {
				return object.NilVal() // terminated by an exception
			}
			return object.BoolValue(bool(object.Bool(false))) // terminated normally
		}
		return object.Wrap(object.NewString(t.status))
	})
	cThread.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if n := object.Kind[*RThread](self).name; !object.IsNil(n) {
			return n
		}
		return object.NilVal()
	})
	cThread.define("name=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		object.Kind[*RThread](self).name = args[0]
		return args[0]
	})
	cThread.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if v, ok := object.Kind[*RThread](self).locals[threadLocalKey(args[0])]; ok {
			return v
		}
		return object.NilVal()
	})
	cThread.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		object.Kind[*RThread](self).locals[threadLocalKey(args[0])] = args[1]
		return args[1]
	})
	cThread.define("key?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := object.Kind[*RThread](self).locals[threadLocalKey(args[0])]
		return object.BoolValue(bool(object.Bool(ok)))
	})
	// thread_variable_get/set/? and thread_variables: thread-local storage that is
	// distinct from Thread#[] (which is fiber-local in MRI). Keys are coerced to
	// Symbols like the fiber-local accessors.
	cThread.define("thread_variable_get", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		t := object.Kind[*RThread](self)
		if v, ok := t.tvars[threadVarKey(args[0])]; ok {
			return v
		}
		return object.NilVal()
	})
	cThread.define("thread_variable_set", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		t := object.Kind[*RThread](self)
		if t.tvars == nil {
			t.tvars = map[object.Value]object.Value{}
		}
		t.tvars[threadVarKey(args[0])] = args[1]
		return args[1]
	})
	cThread.define("thread_variable?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := object.Kind[*RThread](self).tvars[threadVarKey(args[0])]
		return object.BoolValue(bool(object.Bool(ok)))
	})
	cThread.define("thread_variables", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := object.Kind[*RThread](self)
		keys := make([]object.Value, 0, len(t.tvars))
		for k := range t.tvars {
			keys = append(keys, k)
		}
		// Deterministic order (map iteration is randomised): sort the Symbol keys.
		sort.SliceStable(keys, func(i, j int) bool {
			return string(object.Kind[object.Symbol](keys[i])) < string(object.Kind[object.Symbol](keys[j]))
		})
		return object.Wrap(object.NewArrayFromSlice(keys))
	})
	cThread.define("abort_on_exception", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(object.Kind[*RThread](self).abort)))
	})
	cThread.define("abort_on_exception=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		object.Kind[*RThread](self).abort = args[0].Truthy()
		return args[0]
	})
}

// threadJoin blocks the current thread until t finishes, then re-raises t's
// unhandled exception (if any) in the joining thread, as MRI does.
func (vm *VM) threadJoin(t *RThread) {
	if !t.isDone() {
		vm.threadBlock(func() { <-t.done })
	}
	if t.err != nil {
		panic(*t.err)
	}
}

// threadLocalKey normalises a Thread#[] key (Symbol or String) to a Symbol, so
// thread[:k] and thread["k"] address the same slot, as in MRI.
func threadLocalKey(k object.Value) object.Value {
	if s, ok := object.KindOK[*object.String](k); ok {
		return object.SymVal(string(object.Symbol(s.Str())))
	}
	return k
}

// threadVarKey coerces a thread-variable key to a Symbol, requiring a Symbol or
// String as MRI does (anything else raises TypeError). Used by the
// thread_variable_* accessors, which — unlike Thread#[] — do not accept other
// key types.
func threadVarKey(k object.Value) object.Value {
	{
		__sw173 := k
		switch {
		case object.IsKind[object.Symbol](__sw173):
			v := object.Kind[object.Symbol](__sw173)
			_ = v
			return object.SymVal(string(v))
		case object.IsKind[*object.String](__sw173):
			v := object.Kind[*object.String](__sw173)
			_ = v
			return object.SymVal(string(object.Symbol(v.Str())))
		}
	}
	raise("TypeError", "%s is not a symbol nor a string", k.Inspect())
	return object.NilVal()
}

func (vm *VM) registerMutex() {
	cMutex := newClass("Mutex", vm.cObject)
	vm.consts["Mutex"] = object.Wrap(cMutex)
	object.Kind[*RClass](vm.consts["Thread"]).consts["Mutex"] = object.Wrap(cMutex)
	cMutex.smethods["new"] = &Method{name: "new", owner: cMutex, native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&RMutex{})
	}}
	cMutex.define("lock", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.mutexLock(object.Kind[*RMutex](self))
		return self
	})
	cMutex.define("unlock", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.mutexUnlock(object.Kind[*RMutex](self))
		return self
	})
	cMutex.define("try_lock", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := object.Kind[*RMutex](self)
		if m.owner != nil {
			return object.BoolValue(bool(object.Bool(false)))
		}
		m.owner = vm.currentThread
		return object.BoolValue(bool(object.Bool(true)))
	})
	cMutex.define("locked?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(object.Kind[*RMutex](self).owner != nil)))
	})
	cMutex.define("owned?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(object.Kind[*RMutex](self).owner == vm.currentThread)))
	})
	cMutex.define("synchronize", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ThreadError", "must be called with a block")
		}
		m := object.Kind[*RMutex](self)
		vm.mutexLock(m)
		defer vm.mutexUnlock(m)
		return vm.callBlock(blk, nil)
	})
}

func (vm *VM) mutexLock(m *RMutex) {
	t := vm.currentThread
	if m.owner == nil {
		m.owner = t
		return
	}
	if m.owner == t {
		raise("ThreadError", "deadlock; recursive locking")
	}
	w := mutexWaiter{t: t, ch: make(chan struct{})}
	m.waitq = append(m.waitq, w)
	vm.threadBlock(func() { <-w.ch })
	// On wake, mutexUnlock has already transferred ownership to t.
}

func (vm *VM) mutexUnlock(m *RMutex) {
	if m.owner != vm.currentThread {
		raise("ThreadError", "Attempt to unlock a mutex which is not locked")
	}
	if len(m.waitq) > 0 {
		w := m.waitq[0]
		m.waitq = m.waitq[1:]
		m.owner = w.t // hand the lock straight to the next waiter
		close(w.ch)
		return
	}
	m.owner = nil
}

func (vm *VM) registerQueue() {
	cQueue := newClass("Queue", vm.cObject)
	vm.consts["Queue"] = object.Wrap(cQueue)
	object.Kind[*RClass](vm.consts["Thread"]).consts["Queue"] = object.Wrap(cQueue)
	cQueue.smethods["new"] = &Method{name: "new", owner: cQueue, native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&RQueue{})
	}}
	push := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		vm.queuePush(object.Kind[*RQueue](self), args[0])
		return self
	}
	cQueue.define("push", push)
	cQueue.define("<<", push)
	cQueue.define("enq", push)
	pop := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.queuePop(object.Kind[*RQueue](self), len(args) > 0 && args[0].Truthy())
	}
	cQueue.define("pop", pop)
	cQueue.define("shift", pop)
	cQueue.define("deq", pop)
	cQueue.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(object.Kind[*RQueue](self).items)))
	})
	cQueue.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(object.Kind[*RQueue](self).items)))
	})
	cQueue.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(len(object.Kind[*RQueue](self).items) == 0)))
	})
	cQueue.define("num_waiting", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(object.Kind[*RQueue](self).waitq)))
	})
	cQueue.define("clear", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		object.Kind[*RQueue](self).items = nil
		return self
	})
	cQueue.define("close", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		q := object.Kind[*RQueue](self)
		q.closed = true
		for _, ch := range q.waitq {
			close(ch)
		}
		q.waitq = nil
		return self
	})
	cQueue.define("closed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(object.Kind[*RQueue](self).closed)))
	})
}

func (vm *VM) queuePush(q *RQueue, v object.Value) {
	if q.closed {
		raise("ClosedQueueError", "queue closed")
	}
	q.items = append(q.items, v)
	if len(q.waitq) > 0 {
		ch := q.waitq[0]
		q.waitq = q.waitq[1:]
		close(ch) // wake exactly one waiting popper
	}
}

func (vm *VM) queuePop(q *RQueue, nonBlock bool) object.Value {
	for len(q.items) == 0 {
		if q.closed {
			return object.NilVal()
		}
		if nonBlock {
			raise("ThreadError", "queue empty")
		}
		ch := make(chan struct{})
		q.waitq = append(q.waitq, ch)
		vm.threadBlock(func() { <-ch })
	}
	v := q.items[0]
	q.items = q.items[1:]
	return v
}

// registerSleep adds a GVL-aware Kernel#sleep that releases the lock while
// sleeping, so other threads run. With no argument it would sleep forever in
// MRI; here it requires a duration.
func (vm *VM) registerSleep() {
	vm.cObject.define("sleep", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		secs := 0.0
		if len(args) > 0 {
			{
				__sw174 := args[0]
				switch {
				case object.IsInt(__sw174):
					n := object.AsInteger(__sw174)
					_ = n
					secs = float64(n)
				case object.IsFloat(__sw174):
					n := object.AsFloatV(__sw174)
					_ = n
					secs = float64(n)
				default:
					n := __sw174
					_ = n
					raise("TypeError", "can't convert %s into time interval", classNameOf(args[0]))
				}
			}
		}
		vm.threadBlock(func() { time.Sleep(time.Duration(secs * float64(time.Second))) })
		return object.IntValue(int64(secs))
	})
}
