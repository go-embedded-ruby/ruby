package vm

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
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
	tvars  map[object.Value]object.Value // Thread#thread_variable_get/set (thread-local)
	abort  bool                          // abort_on_exception

	// Fiber bookkeeping (Thread#[] is fiber-local in MRI, so it lives per fiber,
	// not per thread). rootFiber is the fiber the thread body runs in and the one
	// Fiber.current returns at the top level; cur is the fiber currently executing
	// in this thread (kept in sync by setCurFiber); curResumed is the top of this
	// thread's resume chain — the fiber Fiber.yield and a finishing transfer chain
	// hand control back to (see fiber.go).
	rootFiber  *Fiber
	cur        *Fiber
	curResumed *Fiber

	// scheduler is this thread's Fiber scheduler (Fiber.set_scheduler), MRI's
	// thread->scheduler; nil when none is installed.
	scheduler object.Value

	// priority is Thread#priority, inherited from the creating thread and clamped
	// to MRI's [-3, 3]. rbgo's cooperative scheduler does not act on it, but the
	// value round-trips because programs read it back.
	priority int

	// interruptMasks is the stack of Thread.handle_interrupt configurations in
	// effect for this thread, innermost last. An empty stack means every
	// asynchronous exception is delivered as soon as the thread reaches a
	// safepoint, which is the behaviour of a program that never calls
	// handle_interrupt.
	interruptMasks []*object.Hash

	// Eager-start handshake: a freshly spawned thread runs immediately (as in
	// MRI) until its first blocking point or completion, at which moment it hands
	// control back to its spawner over handback. parked guards that one-shot
	// handoff (the main thread starts parked, so it never hands back).
	handback chan struct{}
	parked   bool

	// Execution context parked here while this thread does not hold the GVL. The
	// current fiber travels with the thread via cur (kept current by setCurFiber),
	// so it needs no separate saved slot.
	savedLastMatch object.Value
	savedCurExc    object.Value
	savedReqDirs   []string

	// wake is a fresh channel installed under the GVL while this thread is parked
	// in a sleep (Kernel#sleep with no/positive duration, Thread.stop, Mutex#sleep)
	// and nil otherwise. Thread#wakeup/#run wakes it by CLOSING the channel — a
	// permanent signal, so a wakeup racing the park's release→block window is never
	// lost — and clearing the field, so a second wakeup is a no-op.
	wake chan struct{}

	// pendingRaise is an exception object queued by Thread#raise targeting this
	// thread; nil means none. The target picks it up at its next interpreter
	// safepoint (see VM.serviceSafepoint), where the raise fires in the target's
	// own execution context so the exception's backtrace is the target's. Read and
	// written only under the GVL, so no atomics are needed — the raiser publishes
	// it while holding the lock and the target reads it while holding the lock.
	pendingRaise object.Value

	// killed is set by Thread#kill / #exit / #terminate targeting this thread; the
	// target picks it up at its next yield point (see VM.serviceSafepoint) and
	// unwinds via killSignal, running ensure blocks and terminating with a nil
	// result. Written and read only under the GVL, like pendingRaise.
	killed bool

	// reportOnException mirrors Thread#report_on_exception (default true in MRI):
	// whether a thread terminating with an unhandled exception prints a warning.
	// rbgo does not print the warning, but the accessor is honoured for programs
	// (and specs) that toggle it. Set on spawn so the zero value never masquerades
	// as an explicit false.
	reportOnException bool
}

// serviceSafepoint delivers a thread's queued asynchronous event, called by t
// itself right after it resumes from a cooperative yield point (Kernel#sleep,
// Thread.stop, Mutex#sleep, Thread.pass, Thread#run) while holding the GVL. A
// Thread#raise fires the queued exception in t's own context, so captureBacktrace
// records t's frames (MRI uses the interrupted thread's backtrace). It is enough
// to check here — not on every interpreter instruction — because a cross-thread
// raise can only be queued while t holds no GVL, i.e. while t is parked in exactly
// one of these yield points; the raiser has no other window to run. Returns
// normally (a no-op) when nothing is queued; panics to unwind when an event fires.
func (vm *VM) serviceSafepoint(t *RThread) { vm.serviceSafepointAt(t, false) }

// serviceSafepointAt is serviceSafepoint told whether the yield point it was
// called from is a blocking one (a genuine wait, not a bare Thread.pass), which
// is what Thread.handle_interrupt's :on_blocking timing keys on.
func (vm *VM) serviceSafepointAt(t *RThread, blocking bool) {
	if t.killed {
		// Clear the flag before unwinding so ensure blocks that themselves reach a
		// yield point are not re-killed mid-run; the killSignal carries the unwind.
		t.killed = false
		// MRI reports "aborting" for a thread that is unwinding a kill — the status
		// its own ensure blocks observe, and what a peer sees until it is dead.
		t.status = "aborting"
		panic(killSignal{})
	}
	exc := t.pendingRaise
	if exc == nil {
		return
	}
	switch vm.interruptTiming(t, exc) {
	case "never":
		return // deferred until the handle_interrupt block that masked it exits
	case "on_blocking":
		if !blocking {
			return
		}
	}
	t.pendingRaise = nil
	panic(vm.excError(vm.captureBacktrace(exc)))
}

// interruptTiming reports how an asynchronous exception must be handled right
// now — "immediate", "never" or "on_blocking" — following MRI's
// rb_threadptr_pending_interrupt_check_mask (thread.c): the mask stack is
// scanned from the innermost frame outwards and the first entry whose key is an
// ancestor of the exception's class decides. With no match the interrupt is
// immediate, which is the whole behaviour of a program that never masks.
func (vm *VM) interruptTiming(t *RThread, exc object.Value) string {
	if len(t.interruptMasks) == 0 {
		return "immediate"
	}
	cls := vm.classOf(exc)
	for i := len(t.interruptMasks) - 1; i >= 0; i-- {
		h := t.interruptMasks[i]
		for _, k := range h.Keys {
			kc, ok := k.(*RClass)
			if !ok || !classIsA(cls, kc) {
				continue
			}
			if v, _ := h.Get(k); v != nil {
				if sym, isSym := v.(object.Symbol); isSym {
					return string(sym)
				}
			}
		}
	}
	return "immediate"
}

// parkWake installs a fresh wakeup channel and returns it; the caller (holding
// the GVL) passes it to the blocking wait and calls unpark when the wait ends.
func (t *RThread) parkWake() chan struct{} {
	t.wake = make(chan struct{})
	return t.wake
}

// unpark clears the wakeup channel once a sleep has ended (caller holds the GVL),
// so a later wakeup on the now-running thread is a no-op.
func (t *RThread) unpark() { t.wake = nil }

// wakeParked wakes a thread parked in a sleep by closing its wake channel; nil
// means it is not sleeping, so this is a no-op. Caller holds the GVL.
func (t *RThread) wakeParked() {
	if t.wake != nil {
		close(t.wake)
		t.wake = nil
	}
}

// initFibers gives the thread its root fiber and points the current/resume-chain
// slots at it, so the thread body runs in a real (root) fiber from the outset.
func (t *RThread) initFibers() {
	t.rootFiber = newRootFiber(t)
	t.cur = t.rootFiber
	t.curResumed = t.rootFiber
}

// fiberLocals returns the fiber-local storage of the fiber currently executing
// in this thread, lazily allocating it. Thread#[] and friends read/write here,
// so a value set in one fiber is not visible in another (MRI semantics).
func (t *RThread) fiberLocals() map[object.Value]object.Value {
	f := t.cur
	if f.locals == nil {
		f.locals = map[object.Value]object.Value{}
	}
	return f.locals
}

func (t *RThread) ToS() string     { return t.describe() }
func (t *RThread) Inspect() string { return t.describe() }
func (t *RThread) Truthy() bool    { return true }

// describe renders MRI's Thread#to_s / #inspect (thread.c rb_thread_inspect):
// "#<Thread:0xADDR[@name] [file:line ]status>". The file:line field is the
// thread block's source location and is absent for a thread with no block (the
// main thread); the status word is the live status, or "dead" once finished.
func (t *RThread) describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "#<Thread:%p", t)
	if n, ok := t.name.(*object.String); ok {
		b.WriteString("@" + n.Str())
	}
	if t.blk != nil && t.blk.iseq != nil && t.blk.iseq.File != "" {
		// rbgo tracks no per-instruction line, so the line field is 0 — the same
		// value __LINE__ reports, so the two stay consistent.
		fmt.Fprintf(&b, " %s:0", t.blk.iseq.File)
	}
	b.WriteString(" " + t.statusWord() + ">")
	return b.String()
}

// statusWord is the word Thread#status, #to_s and #inspect print: a finished
// thread is "dead" however it finished, otherwise the live status ("run",
// "sleep", or "aborting" while a kill unwinds).
func (t *RThread) statusWord() string {
	if t.isDone() {
		return "dead"
	}
	return t.status
}

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
	t.savedLastMatch = vm.lastMatch
	t.savedCurExc = vm.curExc
	t.savedReqDirs = vm.requireDirs
}

func (t *RThread) restoreCtx(vm *VM) {
	vm.currentFiber = t.cur
	vm.lastMatch = t.savedLastMatch
	vm.curExc = t.savedCurExc
	vm.requireDirs = t.savedReqDirs
	vm.currentThread = t
}

// threadBlock releases the GVL, runs the blocking wait fn while other threads
// run, then re-acquires the GVL and restores this thread's context. The thread
// counts as asleep for the duration — that is what Thread#status and #stop?
// report to a peer while it waits. The caller must currently hold the GVL.
func (vm *VM) threadBlock(fn func()) { vm.threadRelease(fn, "sleep") }

// threadPass releases the GVL around a bare scheduler yield (Thread.pass,
// Thread#run). Unlike a blocking wait it leaves the status alone: MRI's
// Thread.pass only offers the scheduler a switch, so the thread stays runnable —
// a thread spinning in `loop { Thread.pass }` reports "run", not "sleep", and
// #stop? stays false. Peers routinely spin on exactly that ("Thread.pass while
// t.status != 'run'"), which never ends if a passing thread looks asleep.
func (vm *VM) threadPass(fn func()) { vm.threadRelease(fn, "") }

// threadRelease is the body shared by threadBlock and threadPass: save this
// thread's context, optionally publish a status for the window in which it does
// not hold the GVL, release it, run fn, then take the GVL back and restore both.
// An empty status leaves the published one untouched.
func (vm *VM) threadRelease(fn func(), status string) {
	t := vm.currentThread
	t.saveCtx(vm)
	prev := t.status
	if status != "" {
		t.status = status
	}
	vm.gvl.Unlock()
	t.firstPark() // hand control back to the spawner on this thread's first block
	fn()
	vm.gvl.Lock()
	t.restoreCtx(vm)
	t.status = prev
	if status == "sleep" && len(t.interruptMasks) > 0 {
		// A genuine wait is where an :on_blocking interrupt becomes deliverable, and
		// the only place a Queue#pop or Mutex#lock learns of one. This is gated on a
		// mask being in effect so a program that never calls Thread.handle_interrupt
		// keeps exactly the delivery points it had.
		vm.serviceSafepointAt(t, true)
	}
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

func (vm *VM) registerThread() {
	std := vm.consts["StandardError"].(*RClass)
	if _, ok := vm.consts["ThreadError"]; !ok {
		vm.consts["ThreadError"] = newClass("ThreadError", std)
	}
	// StopIteration is in place from the Phase-3 exception hierarchy (built before
	// the stdlib), so ClosedQueueError < StopIteration as in MRI.
	if _, ok := vm.consts["ClosedQueueError"]; !ok {
		vm.consts["ClosedQueueError"] = newClass("ClosedQueueError", vm.consts["StopIteration"].(*RClass))
	}

	vm.registerThreadClass()
	vm.registerMutex()
	vm.registerQueue()
	vm.registerConditionVariable()
	vm.registerSleep()
}

func (vm *VM) registerThreadClass() {
	cThread := newClass("Thread", vm.cObject)
	vm.consts["Thread"] = cThread
	sdef := func(name string, fn NativeFn) {
		cThread.smethods[name] = &Method{name: name, owner: cThread, native: fn}
	}

	// Class-level defaults MRI keeps as VM globals: the value a freshly created
	// thread starts its #report_on_exception / #abort_on_exception with, and the
	// deadlock-detector switch. They live here (one set per VM) because they are
	// read only through these accessors and by spawn.
	reportDefault, abortDefault, ignoreDeadlock := true, false, false

	spawn := func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ThreadError", "must be called with a block")
		}
		t := &RThread{
			blk: blk, args: append([]object.Value{}, args...),
			done: make(chan struct{}), status: "run", handback: make(chan struct{}),
			reportOnException: reportDefault, abort: abortDefault,
		}
		t.initFibers()
		// A new thread's root fiber starts from a copy of the CREATING fiber's
		// storage, the way MRI seeds the new thread's execution context from the
		// current one (thread.c thread_create_core via rb_fiber_inherit_storage).
		t.rootFiber.storage = dupFiberStorage(vm.currentFiber.storage)
		t.priority = vm.currentThread.priority // MRI: a new thread inherits its creator's priority
		vm.threads = append(vm.threads, t)
		go func() {
			vm.gvl.Lock()
			t.restoreCtx(vm)
			func() {
				defer func() {
					if r := recover(); r != nil {
						// A Thread#kill terminates the thread cleanly: ensure blocks have
						// already run during the unwind, the result is nil, and nothing is
						// re-raised on join (distinct from an unhandled exception).
						if _, ok := r.(killSignal); ok {
							t.result = object.NilV
							return
						}
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
		return t
	}
	sdef("new", spawn)
	// Thread.start / Thread.fork bypass #initialize, so MRI reports the missing
	// block from rb_block_proc ("tried to create Proc object without a block")
	// rather than Thread#initialize's ThreadError. They share one Method record,
	// so Thread.method(:fork) == Thread.method(:start), as MRI aliases them.
	sdef("start", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "tried to create Proc object without a block")
		}
		return spawn(vm, self, args, blk)
	})
	cThread.smethods["fork"] = cThread.smethods["start"]
	sdef("current", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return vm.currentThread })
	sdef("main", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return vm.mainThread })
	sdef("list", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		var live []object.Value
		for _, t := range vm.threads {
			if !t.isDone() {
				live = append(live, t)
			}
		}
		return object.NewArrayFromSlice(live)
	})
	sdef("pass", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.threadPass(runtime.Gosched)
		vm.serviceSafepoint(vm.currentThread) // interrupt a running thread that yields via Thread.pass
		return object.NilV
	})
	// Thread.stop puts the current thread to sleep until another thread wakes it
	// with Thread#wakeup or #run, then returns nil.
	sdef("stop", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		t := vm.currentThread
		ch := t.parkWake()
		vm.threadBlock(func() { <-ch })
		t.unpark()
		vm.serviceSafepoint(t) // a Thread#raise that woke the park fires here
		return object.NilV
	})

	// join(limit = nil) waits for the thread to finish and returns it; with a
	// timeout it returns nil instead if the thread is still running when the
	// timeout expires (thread.c thread_join_m). Either way an unhandled exception
	// from the thread body is re-raised in the joiner.
	cThread.define("join", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		if len(args) > 0 && !object.IsNil(args[0]) {
			if !vm.threadJoinLimit(t, threadTimeInterval(args[0])) {
				return object.NilV
			}
			return t
		}
		vm.threadJoin(t)
		return t
	})
	cThread.define("value", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		vm.threadJoin(t)
		return t.result
	})
	cThread.define("alive?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(!self.(*RThread).isDone())
	})
	// stop? is true when the thread is not running: either finished (dead) or
	// parked at a blocking point ("sleep"), matching MRI.
	cThread.define("stop?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		return object.Bool(t.isDone() || t.status == "sleep")
	})
	cThread.define("status", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		if t.isDone() {
			if t.err != nil {
				return object.NilV // terminated by an exception
			}
			return object.Bool(false) // terminated normally
		}
		return object.NewString(t.status)
	})
	// Thread#to_s / #inspect. MRI builds the description in ASCII-8BIT and lets a
	// non-ASCII thread name widen it, so a name outside ASCII yields a UTF-8
	// string and everything else a BINARY one. They share one Method record, as
	// MRI aliases them.
	cThread.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		d := self.(*RThread).describe()
		for i := 0; i < len(d); i++ {
			if d[i] >= 0x80 {
				return object.NewString(d)
			}
		}
		return object.NewStringViewEnc(d, "ASCII-8BIT")
	})
	cThread.methods["inspect"] = cThread.methods["to_s"]
	// Thread#priority / #priority=. MRI clamps an assignment to [-3, 3] and
	// requires an Integer; a new thread inherits the creating thread's value.
	cThread.define("priority", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self.(*RThread).priority)
	})
	cThread.define("priority=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		n, ok := args[0].(object.Integer)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(args[0]))
		}
		p := int(n)
		if p > 3 {
			p = 3
		}
		if p < -3 {
			p = -3
		}
		self.(*RThread).priority = p
		return args[0]
	})
	// wakeup marks a sleeping thread runnable, delivering to it if it is parked in
	// a sleep; on a dead thread it raises ThreadError, as in MRI. Returns self.
	cThread.define("wakeup", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		if t.isDone() {
			raise("ThreadError", "killed thread")
		}
		t.wakeParked()
		return t
	})
	// run wakes the thread like wakeup and additionally yields so the scheduler
	// can pick it up; cooperatively that is a wakeup followed by Thread.pass.
	cThread.define("run", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		if t.isDone() {
			raise("ThreadError", "killed thread")
		}
		t.wakeParked()
		vm.threadPass(runtime.Gosched)
		vm.serviceSafepoint(vm.currentThread) // a raise queued against the caller fires here
		return t
	})
	cThread.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if n := self.(*RThread).name; !object.IsNil(n) {
			return n
		}
		return object.NilV
	})
	cThread.define("name=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if !object.IsNil(args[0]) {
			s, ok := args[0].(*object.String)
			if !ok {
				// MRI runs the argument through rb_check_string_type, i.e. #to_str.
				if vm.respondsToDynamic(args[0], "to_str") {
					s, ok = vm.send(args[0], "to_str", nil, nil).(*object.String)
				}
				if !ok {
					raise("TypeError", "no implicit conversion of %s into String", classNameOf(args[0]))
				}
				args = append([]object.Value{s}, args[1:]...)
			}
			if strings.ContainsRune(s.Str(), 0) {
				raise("ArgumentError", "string contains null byte")
			}
		}
		self.(*RThread).name = args[0]
		return args[0]
	})
	cThread.define("[]", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if v, ok := self.(*RThread).fiberLocals()[vm.threadLocalKey(args[0])]; ok {
			return v
		}
		return object.NilV
	})
	cThread.define("[]=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*RThread).fiberLocals()[vm.threadLocalKey(args[0])] = args[1]
		return args[1]
	})
	cThread.define("key?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := self.(*RThread).fiberLocals()[vm.threadLocalKey(args[0])]
		return object.Bool(ok)
	})
	// fetch(key[, default]) { |key| ... }: read a fiber-local like Hash#fetch —
	// the value if set, else the block's result (which takes precedence over a
	// default), else the default, else a KeyError. Zero or more-than-two args is
	// an ArgumentError.
	cThread.define("fetch", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) < 1 || len(args) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
		}
		t := self.(*RThread)
		if v, ok := t.fiberLocals()[vm.threadLocalKey(args[0])]; ok {
			return v
		}
		if blk != nil {
			return vm.callBlock(blk, []object.Value{args[0]})
		}
		if len(args) == 2 {
			return args[1]
		}
		raise("KeyError", "key not found: %s", args[0].Inspect())
		return object.NilV
	})
	// keys: the fiber-local names of this thread, as Symbols, in a deterministic
	// order (the storage map iterates randomly).
	cThread.define("keys", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		locals := t.fiberLocals()
		keys := make([]object.Value, 0, len(locals))
		for k := range locals {
			keys = append(keys, k)
		}
		sort.SliceStable(keys, func(i, j int) bool {
			return string(keys[i].(object.Symbol)) < string(keys[j].(object.Symbol))
		})
		return object.NewArrayFromSlice(keys)
	})
	// thread_variable_get/set/? and thread_variables: thread-local storage that is
	// distinct from Thread#[] (which is fiber-local in MRI). Keys are coerced to
	// Symbols like the fiber-local accessors.
	cThread.define("thread_variable_get", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		if v, ok := t.tvars[vm.threadLocalKey(args[0])]; ok {
			return v
		}
		return object.NilV
	})
	cThread.define("thread_variable_set", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		k := vm.threadLocalKey(args[0])
		// MRI deletes the variable when the value is nil (thread.c
		// rb_thread_variable_set), so #thread_variable? then reports false.
		if object.IsNil(args[1]) {
			delete(t.tvars, k)
			return args[1]
		}
		if t.tvars == nil {
			t.tvars = map[object.Value]object.Value{}
		}
		t.tvars[k] = args[1]
		return args[1]
	})
	cThread.define("thread_variable?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := self.(*RThread).tvars[vm.threadLocalKey(args[0])]
		return object.Bool(ok)
	})
	cThread.define("thread_variables", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		keys := make([]object.Value, 0, len(t.tvars))
		for k := range t.tvars {
			keys = append(keys, k)
		}
		// Deterministic order (map iteration is randomised): sort the Symbol keys.
		sort.SliceStable(keys, func(i, j int) bool {
			return string(keys[i].(object.Symbol)) < string(keys[j].(object.Symbol))
		})
		return object.NewArrayFromSlice(keys)
	})
	// raise([exc[, msg]]) delivers an exception to the thread asynchronously: it is
	// picked up at the target's next interpreter safepoint (a backward jump or a
	// method/block entry) and raised in the target's own context. With no arguments
	// it raises a RuntimeError with an empty message (MRI); otherwise the arguments
	// are coerced exactly as Kernel#raise (String, exception class, instance, or a
	// class+message pair). A dead thread ignores the raise and returns nil; raising
	// the current thread raises immediately. The exception object is built in the
	// caller's context, so its own #exception/constructor runs here, but its
	// backtrace is captured in the target — matching MRI.
	cThread.define("raise", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		if t.isDone() {
			return object.NilV
		}
		var exc object.Value
		if len(args) == 0 {
			exc = vm.send(vm.consts["RuntimeError"].(*RClass), "new", []object.Value{object.NewString("")}, nil)
		} else {
			exc = vm.raiseExceptionObject(args)
		}
		if t == vm.currentThread {
			panic(vm.excError(vm.captureBacktrace(exc)))
		}
		t.pendingRaise = exc
		// Wake a target parked in a sleep so it leaves its blocking wait and reaches
		// the next safepoint; a target blocked elsewhere (join/mutex) services the
		// raise when that wait returns.
		if t.wake != nil {
			t.wakeParked()
		}
		return t
	})
	cThread.define("report_on_exception", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*RThread).reportOnException)
	})
	cThread.define("report_on_exception=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*RThread).reportOnException = args[0].Truthy()
		return args[0]
	})
	// kill / exit / terminate ask the thread to terminate: it unwinds at its next
	// yield point via killSignal, running its ensure blocks (but bypassing rescue),
	// and finishes with a nil value. Killing the current thread unwinds it
	// immediately; killing the main thread ends the program. A dead thread is a
	// no-op. Returns the thread. The three names share one Method record so
	// Thread.instance_method(:exit) == Thread.instance_method(:kill), as in MRI.
	kill := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		if t.isDone() {
			return t
		}
		if t == vm.currentThread {
			t.status = "aborting"
			panic(killSignal{})
		}
		t.killed = true
		if t.wake != nil {
			t.wakeParked() // a sleeping target leaves its wait and reaches its next yield point
		}
		return t
	}
	cThread.define("kill", kill)
	cThread.methods["exit"] = cThread.methods["kill"]
	cThread.methods["terminate"] = cThread.methods["kill"]
	// Thread.exit / Thread.kill(thread): the class-level forms — Thread.exit ends
	// the current thread; Thread.kill(t) ends t (MRI's deprecated spelling).
	sdef("exit", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return kill(vm, vm.currentThread, nil, nil)
	})
	sdef("kill", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		t, ok := args[0].(*RThread)
		if !ok {
			raise("TypeError", "wrong argument type %s (expected Thread)", classNameOf(args[0]))
		}
		return kill(vm, t, nil, nil)
	})
	cThread.define("abort_on_exception", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*RThread).abort)
	})
	cThread.define("abort_on_exception=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*RThread).abort = args[0].Truthy()
		return args[0]
	})
	// The class-level forms of the two per-thread flags set the default a new
	// thread starts with (MRI's rb_thread_s_abort_exc_set /
	// rb_thread_s_report_exc_set); they do not change threads already running.
	sdef("abort_on_exception", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(abortDefault)
	})
	sdef("abort_on_exception=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		abortDefault = args[0].Truthy()
		return args[0]
	})
	sdef("report_on_exception", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(reportDefault)
	})
	sdef("report_on_exception=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		reportDefault = args[0].Truthy()
		return args[0]
	})
	// Thread.ignore_deadlock switches off MRI's deadlock detector. rbgo has no
	// detector to switch off, so the flag only round-trips.
	sdef("ignore_deadlock", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(ignoreDeadlock)
	})
	sdef("ignore_deadlock=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ignoreDeadlock = args[0].Truthy()
		return args[0]
	})
	// Thread.allocate has no allocator in MRI: a Thread only exists with a block.
	sdef("allocate", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return raise("TypeError", "allocator undefined for Thread")
	})
	// Thread#native_thread_id: MRI returns the OS thread id of a running thread
	// and nil once it is dead. Goroutines have no stable OS thread, so rbgo hands
	// out a small distinct integer per thread, which is what the specs pin (an
	// Integer, different per thread, nil when not running).
	cThread.define("native_thread_id", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		if t.isDone() {
			return object.NilV
		}
		for i, o := range vm.threads {
			if o == t {
				return object.Integer(i + 2)
			}
		}
		return object.Integer(1) // the main thread is not in vm.threads
	})
	// Thread.handle_interrupt(config) { ... } masks asynchronous exceptions for
	// the duration of the block (rb_thread_s_handle_interrupt, thread.c). The
	// configuration is pushed before the block so an interrupt this frame makes
	// deliverable fires immediately — before the block — and popped afterwards,
	// where any interrupt deferred by the frame is delivered, including on the way
	// out of an exception raised inside the block.
	sdef("handle_interrupt", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		h, ok := args[0].(*object.Hash)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Hash", classNameOf(args[0]))
		}
		if blk == nil {
			raise("ArgumentError", "block is needed.")
		}
		t := vm.currentThread
		t.interruptMasks = append(t.interruptMasks, h)
		defer func() {
			t.interruptMasks = t.interruptMasks[:len(t.interruptMasks)-1]
			// Leaving the frame can make a deferred interrupt deliverable; raising
			// here during an unwind replaces the exception on its way out, as MRI does.
			vm.serviceSafepointAt(t, true)
		}()
		vm.serviceSafepointAt(t, true)
		return vm.callBlock(blk, nil)
	})
	// Thread.pending_interrupt? / Thread#pending_interrupt? report whether an
	// asynchronous exception is queued against the thread and has not been
	// delivered yet.
	sdef("pending_interrupt?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.currentThread.pendingRaise != nil)
	})
	cThread.define("pending_interrupt?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*RThread).pendingRaise != nil)
	})
}

// threadJoin blocks the current thread until t finishes, then re-raises t's
// unhandled exception (if any) in the joining thread, as MRI does.
func (vm *VM) threadJoin(t *RThread) {
	if !t.isDone() {
		vm.threadBlock(func() { <-t.done })
	}
	vm.serviceMaskedSafepoint()
	if t.err != nil {
		panic(*t.err)
	}
}

// serviceMaskedSafepoint is the safepoint a join reaches even when it never had
// to wait (the joined thread had already finished). MRI checks for interrupts at
// every such checkpoint; rbgo only needs to while a Thread.handle_interrupt mask
// is in effect, which is where the timing of a delivery is observable — so a
// program that never masks keeps exactly the delivery points it had.
func (vm *VM) serviceMaskedSafepoint() {
	if t := vm.currentThread; len(t.interruptMasks) > 0 {
		vm.serviceSafepointAt(t, true)
	}
}

// threadJoinLimit waits at most secs for t to finish and reports whether it did.
// A thread that finished with an unhandled exception re-raises it in the joiner,
// exactly as an untimed join does; a timeout leaves the thread running and the
// caller returns nil.
func (vm *VM) threadJoinLimit(t *RThread, secs float64) bool {
	if !t.isDone() {
		vm.threadBlock(func() {
			select {
			case <-t.done:
			case <-time.After(time.Duration(secs * float64(time.Second))):
			}
		})
	}
	if !t.isDone() {
		return false
	}
	vm.serviceMaskedSafepoint()
	if t.err != nil {
		panic(*t.err)
	}
	return true
}

// threadTimeInterval coerces a Thread#join timeout to seconds the way MRI's
// thread_join_m does — through rb_num2dbl, so the rejection names Float: an
// Integer or Float is the number of seconds, a String has its own message
// ("no implicit conversion to float from string"), and anything else is
// "can't convert CLASS into Float".
func threadTimeInterval(v object.Value) float64 {
	switch n := v.(type) {
	case object.Integer:
		return float64(n)
	case object.Float:
		return float64(n)
	case *object.String:
		raise("TypeError", "no implicit conversion to float from string")
	}
	raise("TypeError", "can't convert %s into Float", classNameOf(v))
	return 0
}

// threadLocalKey normalises a Thread#[] / #[]= / #key? / #fetch key to a Symbol,
// so thread[:k] and thread["k"] address the same slot, as in MRI. A key that is
// neither a Symbol nor a String is coerced through #to_str; anything that does
// not yield a String (e.g. nil or an Integer) raises TypeError, as in MRI.
func (vm *VM) threadLocalKey(k object.Value) object.Value {
	switch v := k.(type) {
	case object.Symbol:
		return v
	case *object.String:
		return object.Symbol(v.Str())
	}
	if vm.respondsToDynamic(k, "to_str") {
		if s, ok := vm.send(k, "to_str", nil, nil).(*object.String); ok {
			return object.Symbol(s.Str())
		}
	}
	// MRI formats the offending key with its own #inspect, so an object that
	// defines one is named the way the program would print it.
	raise("TypeError", "%s is not a symbol nor a string", vm.send(k, "inspect", nil, nil).ToS())
	return object.NilVal()
}

func (vm *VM) registerMutex() {
	cMutex := newClass("Mutex", vm.cObject)
	vm.consts["Mutex"] = cMutex
	vm.consts["Thread"].(*RClass).consts["Mutex"] = cMutex
	cMutex.smethods["new"] = &Method{name: "new", owner: cMutex, native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RMutex{}
	}}
	cMutex.define("lock", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.mutexLock(self.(*RMutex))
		return self
	})
	cMutex.define("unlock", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.mutexUnlock(self.(*RMutex))
		return self
	})
	cMutex.define("try_lock", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := self.(*RMutex)
		if m.owner != nil {
			return object.Bool(false)
		}
		m.owner = vm.currentThread
		return object.Bool(true)
	})
	cMutex.define("locked?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*RMutex).owner != nil)
	})
	cMutex.define("owned?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*RMutex).owner == vm.currentThread)
	})
	cMutex.define("synchronize", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ThreadError", "must be called with a block")
		}
		m := self.(*RMutex)
		vm.mutexLock(m)
		defer vm.mutexUnlock(m)
		return vm.callBlock(blk, nil)
	})
	// sleep(duration = nil): release the mutex, sleep, then re-acquire it, returning
	// the rounded number of seconds slept. The duration is validated first (a
	// negative one is an ArgumentError even on an unowned mutex); releasing the
	// mutex raises ThreadError when the current thread does not hold it. A nil
	// duration sleeps until woken (Thread#wakeup/#run), which this cooperative
	// model does not provide, so it parks — matching MRI's blocking behaviour.
	cMutex.define("sleep", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := self.(*RMutex)
		var secs float64
		hasDur := false
		if len(args) > 0 && !object.IsNil(args[0]) {
			secs = mutexSleepDur(args[0])
			hasDur = true
		}
		vm.mutexUnlock(m) // ownership check + release (ThreadError if not held)
		t := vm.currentThread
		ch := t.parkWake()
		start := time.Now()
		if hasDur {
			vm.threadBlock(func() {
				select {
				case <-ch:
				case <-time.After(time.Duration(secs * float64(time.Second))):
				}
			})
		} else {
			vm.threadBlock(func() { <-ch }) // park until Thread#wakeup/#run
		}
		t.unpark()
		vm.mutexLock(m)        // MRI re-acquires the mutex before the raise propagates
		vm.serviceSafepoint(t) // a Thread#raise that woke the park fires here (mutex held)
		return object.IntValue(int64(time.Since(start).Seconds() + 0.5))
	})
}

// mutexSleepDur coerces a Mutex#sleep / Kernel#sleep duration to seconds,
// rejecting a negative interval with ArgumentError as MRI does.
func mutexSleepDur(v object.Value) float64 {
	var f float64
	switch n := v.(type) {
	case object.Integer:
		f = float64(n)
	case object.Float:
		f = float64(n)
	default:
		raise("TypeError", "can't convert %s into time interval", classNameOf(v))
	}
	if f < 0 {
		raise("ArgumentError", "time interval must not be negative")
	}
	return f
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

// registerSleep adds a GVL-aware Kernel#sleep that releases the lock while
// sleeping, so other threads run. With no argument it would sleep forever in
// MRI; here it requires a duration.
func (vm *VM) registerSleep() {
	vm.cObject.define("sleep", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		hasDur := len(args) > 0 && !object.IsNil(args[0])
		var secs float64
		if hasDur {
			switch n := args[0].(type) {
			case object.Integer:
				secs = float64(n)
			case object.Float:
				secs = float64(n)
			default:
				raise("TypeError", "can't convert %s into time interval", classNameOf(args[0]))
			}
			if secs < 0 {
				raise("ArgumentError", "time interval must not be negative")
			}
		}
		t := vm.currentThread
		ch := t.parkWake()
		start := time.Now()
		if hasDur {
			vm.threadBlock(func() {
				select {
				case <-ch:
				case <-time.After(time.Duration(secs * float64(time.Second))):
				}
			})
		} else {
			// No argument: sleep until woken by Thread#wakeup/#run, as in MRI.
			vm.threadBlock(func() { <-ch })
		}
		t.unpark()
		vm.serviceSafepoint(t) // a Thread#raise that woke the park fires here
		return object.IntValue(int64(time.Since(start).Seconds() + 0.5))
	})
}
