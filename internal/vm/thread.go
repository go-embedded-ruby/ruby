package vm

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"strconv"
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

	// ntid is Thread#native_thread_id: a small distinct number handed out at
	// creation. The main thread is built before any spawn and keeps the zero
	// value, which reads back as 1.
	ntid int

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

	// intr is the wakeup edge an asynchronous interrupt (Thread#kill, Thread#raise)
	// sends so this thread leaves whatever blocking wait it is in. It is the
	// counterpart of MRI's registered unblocking function: thread.c's
	// threadptr_interrupt_locked sets the interrupt flag AND calls
	// th->unblock.func, because the flag alone cannot reach a thread that is
	// asleep. The flag here is killed / pendingRaise; this channel is only the
	// edge. Buffered so an interrupter never blocks, and drained under the GVL
	// before each wait so a leftover token can never wake the wrong wait.
	intr chan struct{}

	// shielded counts the nested uninterruptible regions this thread is inside.
	// While it is non-zero an interrupt stays QUEUED rather than being delivered:
	// MRI's do_mutex_lock with interruptible_p == 0 saves the pending interrupts,
	// finishes acquiring the mutex, and restores them afterwards
	// (mutex_lock_uninterruptible, thread_sync.c). Leaving the killed /
	// pendingRaise flags set is that save, and not consuming them is that restore.
	shielded int

	// held is every Mutex this thread currently owns, in MRI's keeping_mutexes
	// order (thread_sync.c thread_mutex_insert / thread_mutex_remove). A thread
	// that dies hands all of them on, which is what makes a Mutex survive its
	// owner's death (rb_threadptr_unlock_all_locking_mutexes, thread.c). Touched
	// only under the GVL.
	held []*RMutex

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
	if t.shielded > 0 {
		// Inside an uninterruptible region: the interrupt stays queued and fires at
		// the first safepoint after the region ends. MRI does the same with
		// interruptible_p == 0 (do_mutex_lock), which is what lets rb_mutex_sleep's
		// ensure clause reacquire the mutex for a thread that has been killed.
		return
	}
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

// interruptCh returns this thread's interrupt channel, allocating it on first
// use so a thread built by any construction path has one. Caller holds the GVL.
func (t *RThread) interruptCh() chan struct{} {
	if t.intr == nil {
		t.intr = make(chan struct{}, 1)
	}
	return t.intr
}

// interrupt delivers the wakeup edge for an interrupt already queued on t
// (killed or pendingRaise): every blocking wait selects on this channel, so the
// wait returns and its loop reaches a safepoint. This is MRI's unblocking
// function (thread.c threadptr_interrupt_locked), and like MRI it is separate
// from the interrupt flag itself. A parked sleep is also woken, which is how a
// Kernel#sleep or Thread.stop learns of the interrupt. Caller holds the GVL.
func (t *RThread) interrupt() {
	select {
	case t.interruptCh() <- struct{}{}:
	default: // a token is already queued; one edge is enough
	}
	t.wakeParked()
}

// drainInterrupt hands back the interrupt channel with any stale token cleared,
// ready for one wait. Both this drain and every send happen under the GVL, so a
// wait can never be woken by an edge meant for an earlier one. MRI tolerates
// spurious wakeups (SLEEP_ALLOW_SPURIOUS) and its loops re-check their own
// predicate; rbgo keeps the loops and removes the spurious edge, so a witness
// for an interruption bug is deterministic rather than timing-dependent.
func (t *RThread) drainInterrupt() <-chan struct{} {
	ch := t.interruptCh()
	select {
	case <-ch:
	default:
	}
	return ch
}

// threadInterrupted reports whether t has an asynchronous interrupt that a
// blocking wait must act on now: MRI's RUBY_VM_INTERRUPTED together with the
// pending-interrupt mask (rb_threadptr_pending_interrupt_check_mask, thread.c).
// A raise the current Thread.handle_interrupt frame defers is NOT one — the wait
// must go back to sleep, which is what do_mutex_lock's loop does when its
// interrupt check does not raise. Caller holds the GVL.
func (vm *VM) threadInterrupted(t *RThread) bool {
	if t.shielded > 0 {
		return false
	}
	if t.killed {
		return true
	}
	if t.pendingRaise == nil {
		return false
	}
	// blocking=true: a wait is a blocking yield point, so :on_blocking counts.
	return vm.interruptTiming(t, t.pendingRaise) != "never"
}

// interruptibleWait releases the GVL and waits until ch fires or this thread is
// interrupted, reporting whether ch fired. It is the Go counterpart of MRI's
// native_sleep with an unblocking function registered (thread.c sleep_forever,
// thread_join_sleep; thread_sync.c do_mutex_lock and queue_sleep all reach the
// same primitive): the wait ends on an interrupt and the CALLER's loop then
// reaches a safepoint, which is MRI's RUBY_VM_CHECK_INTS_BLOCKING.
//
// Go's own blocking primitives cannot do this. A `sync.Mutex` acquisition and a
// bare channel receive are not interruptible and expose no unblocking hook, so a
// Ruby-level Mutex cannot BE a sync.Mutex if Thread#kill must interrupt it; it
// has to be a wait queue whose waits go through here, which is also how MRI
// builds it (a waitq plus the general thread sleep).
func (vm *VM) interruptibleWait(ch <-chan struct{}) (fired bool) {
	intr := vm.currentThread.drainInterrupt()
	vm.threadBlock(func() {
		select {
		case <-ch:
			fired = true
		case <-intr:
		}
	})
	return fired
}

// interruptibleWaitFor is interruptibleWait bounded by d, reporting whether ch
// fired and whether the bound elapsed. MRI's bounded waits pass a limit to
// native_sleep the same way (thread_join_sleep, queue_sleep).
func (vm *VM) interruptibleWaitFor(ch <-chan struct{}, d time.Duration) (fired, timedOut bool) {
	intr := vm.currentThread.drainInterrupt()
	vm.threadBlock(func() {
		tm := time.NewTimer(d)
		defer tm.Stop()
		select {
		case <-ch:
			fired = true
		case <-tm.C:
			timedOut = true
		case <-intr:
		}
	})
	return fired, timedOut
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

// abortMainThread raises a dead thread's unhandled exception in the main thread,
// which is what Thread#abort_on_exception= and Thread.abort_on_exception= promise:
// thread_start_func_2 (thread.c) ends with
//
//	if (RB_TYPE_P(errinfo, T_OBJECT)) rb_threadptr_raise(ractor_main_th, 1, &errinfo);
//
// and rb_threadptr_raise enqueues the exception and then interrupts, which is why
// the main thread's `sleep` ends instead of running to completion. Until now the
// flag was written by its setter and read by its getter and by nothing else, so it
// was a stored value equal to its own default and no program could observe it.
//
// The guard mirrors MRI's: only a real exception object propagates. A Go-level
// failure wrapped as a RuntimeError has no Ruby object, and MRI has no such case,
// so it stays what it already is -- re-raised in whoever joins the thread.
func (vm *VM) abortMainThread(t *RThread) {
	main := vm.mainThread
	exc := t.err.Obj
	if main == nil || main == t || exc == nil || main.isDone() {
		return
	}
	if main.pendingRaise != nil {
		// MRI queues pending interrupts; rbgo holds one, so the first abort to reach
		// the main thread is the one it raises, rather than the last.
		return
	}
	main.pendingRaise = exc
	main.interrupt()
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
	// reportDefault is Thread.report_on_exception, which a new thread INHERITS
	// (thread.c thread_create_core copies th->vm->thread_report_on_exception).
	// abortDefault is Thread.abort_on_exception, which it does NOT: nothing in
	// thread_create_core touches th->abort_on_exception, and termination tests the
	// two independently ("th->vm->thread_abort_on_exception || th->abort_on_exception",
	// thread.c). Seeding the per-thread flag from the class-level one made
	// `Thread.abort_on_exception = true; Thread.new {}.abort_on_exception` answer
	// true where MRI 4.0.5 answers false.
	reportDefault, abortDefault, ignoreDeadlock := true, false, false

	spawn := func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ThreadError", "must be called with a block")
		}
		t := &RThread{
			blk: blk, args: append([]object.Value{}, args...),
			done: make(chan struct{}), status: "run", handback: make(chan struct{}),
			reportOnException: reportDefault,
		}
		t.initFibers()
		// A new thread's root fiber starts from a copy of the CREATING fiber's
		// storage, the way MRI seeds the new thread's execution context from the
		// current one (thread.c thread_create_core via rb_fiber_inherit_storage).
		t.rootFiber.storage = dupFiberStorage(vm.currentFiber.storage)
		t.priority = vm.currentThread.priority // MRI: a new thread inherits its creator's priority
		t.ntid = len(vm.threads) + 1
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
			close(t.done) // MRI: rb_threadptr_join_list_wakeup, before the two below
			// A dying thread gives up the mutexes it still holds, then -- if its
			// unhandled exception is one abort_on_exception asked to be fatal -- raises
			// that exception in the MAIN thread. Both are what MRI's
			// thread_start_func_2 (thread.c) does at exactly this point.
			vm.releaseHeldMutexes(t)
			if t.err != nil && (t.abort || abortDefault) {
				vm.abortMainThread(t)
			}
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
	// Thread.each_caller_location(start = 1, length = nil) { |loc| ... } -> nil:
	// yields each frame of the CURRENT execution stack as a
	// Thread::Backtrace::Location, nearest-first, and answers nil.
	//
	// vm_backtrace.c v3_4_0:1401 each_caller_location selects the range with
	// ec_backtrace_range(ec, argc, argv, 1, 1, &n) — the SAME lev_default and
	// lev_plus that rb_f_caller_locations passes to ec_backtrace_to_ary — so the
	// argument handling is Kernel#caller_locations' exactly, down to the Range
	// form and the nil-for-overshoot case. That is why callerSlice serves both:
	// the two must not drift, and one of them owning the rule is how they cannot.
	//
	// It was unimplementable before the line map: every Location it yields is
	// compared to caller_locations' by #to_s, which is "path:lineno:in 'label'".
	sdef("each_caller_location", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		// rb_scan_args(argc, argv, "02:") followed by rb_get_kwargs(opts, (ID[]){0},
		// 0, 0, NULL): the keyword table is EMPTY, so every keyword is unknown.
		if kw := trailingKwHash(args); kw != nil {
			args = args[:len(args)-1]
			if len(kw.Keys) > 0 {
				word, names := "keyword", make([]string, 0, len(kw.Keys))
				if len(kw.Keys) > 1 {
					word = "keywords"
				}
				for _, k := range kw.Keys {
					names = append(names, k.Inspect())
				}
				raise("ArgumentError", "unknown %s: %s", word, strings.Join(names, ", "))
			}
		}
		if len(args) > 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0..2)", len(args))
		}
		frames, present := vm.callerSlice(args)
		if !present {
			return object.NilV
		}
		// The LocalJumpError comes from the rb_yield inside the iteration, not from
		// a guard before it: a range that selects NO frame never yields, so a
		// block-less call over an empty selection is not an error. Hence the check
		// is on "about to yield at least once", not on the block alone.
		if blk == nil && len(frames) > 0 {
			raise("LocalJumpError", "no block given")
		}
		for _, f := range frames {
			vm.callBlock(blk, []object.Value{vm.backtraceLocation(f.ToS())})
		}
		return object.NilV
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
		// Deliver the unblocking edge so the target leaves whatever blocking wait it
		// is in — a parked sleep, but equally a Mutex#lock, Queue#pop or Thread#join
		// — and reaches its next safepoint. MRI does exactly this in
		// rb_threadptr_raise: enqueue the exception, then rb_threadptr_interrupt.
		t.interrupt()
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
		// rb_thread_kill (thread.c) enqueues the kill and then calls
		// rb_threadptr_interrupt, whose unblocking function pulls the target out of
		// ANY blocking wait. Waking only a parked sleep left a target blocked in
		// Mutex#lock, Queue#pop or Thread#join waiting for an event that its killer
		// had just made impossible, so the kill never landed and a join on it never
		// returned.
		t.interrupt()
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
	cThread.define("native_thread_id", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		t := self.(*RThread)
		if t.isDone() {
			return object.NilV
		}
		if t.ntid == 0 {
			return object.Integer(1) // the main thread, created before any spawn
		}
		return object.Integer(t.ntid)
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
	self := vm.currentThread
	// MRI's thread_join_sleep (thread.c) is a loop: sleep, then
	// RUBY_VM_CHECK_INTS_BLOCKING, then re-test whether the target finished. The
	// check is what lets a Thread#kill or Thread#raise aimed at the JOINER end the
	// join; without it a join waits on a target that may never finish.
	for !t.isDone() {
		vm.interruptibleWait(t.done)
		vm.serviceSafepointAt(self, true)
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
	self := vm.currentThread
	// An ABSOLUTE deadline, so a wait cut short by an interrupt that turns out to
	// be masked does not restart the clock: MRI's thread_join_sleep computes `end`
	// once and calls hrtime_update_expire on every turn of the loop.
	end := time.Now().Add(time.Duration(secs * float64(time.Second)))
	for !t.isDone() {
		d := time.Until(end)
		if d <= 0 {
			return false
		}
		vm.interruptibleWaitFor(t.done, d)
		vm.serviceSafepointAt(self, true)
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
			secs = vm.timeInterval(args[0])
			hasDur = true
		}
		vm.mutexUnlock(m) // ownership check + release (ThreadError if not held)
		t := vm.currentThread
		ch := t.parkWake()
		start := time.Now()
		// rb_mutex_sleep wraps the sleep in rb_ensure whose ensure clause is
		// mutex_lock_uninterruptible, so the mutex comes back however the sleep ends
		// -- including through a raise delivered inside it.
		relocked := false
		relock := func() {
			if !relocked {
				relocked = true
				vm.mutexLockUninterruptible(m)
			}
		}
		defer relock()
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
		relock()               // MRI re-acquires the mutex before the raise propagates
		vm.serviceSafepoint(t) // a Thread#raise that woke the park fires here (mutex held)
		return object.IntValue(int64(time.Since(start).Seconds() + 0.5))
	})
}

// mutexSleepDur coerces a duration to seconds without a *VM in hand. It is the
// numeric part of (*VM).timeInterval and nothing more: with no VM there is no
// way to send #divmod, so an object that would convert that way is refused here.
// IO.select's timeout (spawn.go) is the one caller that arrives this way.
func mutexSleepDur(v object.Value) float64 { return (*VM)(nil).timeInterval(v) }

// timeInterval is MRI's rb_time_interval — time.c's time_timespec(num, TRUE),
// the coercion Kernel#sleep, Mutex#sleep and IO.select put a duration through.
//
//   - an Integer or a Float is a number of seconds, and a negative one is an
//     ArgumentError ("time interval must not be negative");
//   - a Float whose integral part does not fit a time_t — which covers NaN and
//     both infinities — is a RangeError, MRI's `"%f out of Time range"`;
//   - anything else is offered #divmod(1), and a two-element Array answer is
//     read as [whole seconds, fraction]: that is how a Rational, and any object
//     that defines #divmod, becomes an interval. Only the whole-seconds half is
//     range-checked, exactly as arg_range_check is placed in time_timespec.
//   - anything that does not convert is a TypeError naming its class.
func (vm *VM) timeInterval(v object.Value) float64 {
	switch n := v.(type) {
	case object.Integer:
		if n < 0 {
			raise("ArgumentError", "time interval must not be negative")
		}
		return float64(n)
	case object.Float:
		f := float64(n)
		if f < 0 {
			raise("ArgumentError", "time interval must not be negative")
		}
		timeIntervalRangeCheck(f)
		return f
	case *object.Bignum:
		// time_timespec sends a Bignum through NUM2TIMET as well; rbgo only ever
		// builds a Bignum for a value that does not fit an int64, so every one of
		// them overflows the time_t the interval is kept in.
		raise("RangeError", "bignum too big to convert into 'long'")
	}
	if vm != nil && vm.respondsToDynamic(v, "divmod") {
		d := vm.send(v, "divmod", []object.Value{object.IntValue(1)}, nil)
		if a, ok := vm.checkArrayType(d); ok && len(a.Elems) >= 2 {
			return vm.timeIntervalFromDivmod(a.Elems[0], a.Elems[1])
		}
	}
	raise("TypeError", "can't convert %s into time interval", vm.timeIntervalClassName(v))
	return 0
}

// timeIntervalFromDivmod turns the [quotient, remainder] pair #divmod(1) gives
// back into seconds. MRI reads the quotient with NUM2TIMET (so a non-integral
// quotient is a TypeError) and the remainder as `rem * 1_000_000_000` narrowed
// with NUM2LONG, i.e. truncated to whole nanoseconds — which is why a Rational
// interval loses everything below a nanosecond rather than rounding.
func (vm *VM) timeIntervalFromDivmod(q, rem object.Value) float64 {
	// NUM2TIMET is rb_num2long, which TRUNCATES a Float rather than refusing it —
	// so #divmod may answer with one and keep the fraction in the remainder, as
	// `[0.5, 0]` does — and which names nil differently from everything else it
	// cannot convert.
	if object.IsNil(q) {
		raise("TypeError", "no implicit conversion from nil to integer")
	}
	sec := vm.repeatLong(q)
	if sec < 0 {
		raise("ArgumentError", "time interval must not be negative")
	}
	nsec := vm.send(vm.send(rem, "*", []object.Value{object.IntValue(1000000000)}, nil), "to_i", nil, nil)
	n, _ := nsec.(object.Integer)
	return float64(sec) + float64(n)/1e9
}

// timeIntervalRangeCheck is time_timespec's `if (f != t.tv_sec)` guard: the
// integral part of the duration has to survive the trip through a time_t (an
// int64 here), so NaN and Infinity land in the same RangeError as a finite value
// that is simply too large. The number is formatted the way MRI's own vsnprintf
// renders `%f` — "NaN", "Inf", and six decimals otherwise. Negative infinity
// never reaches here: arg_range_check refuses a negative interval first.
func timeIntervalRangeCheck(x float64) {
	i, _ := math.Modf(x)
	if !math.IsNaN(i) && i >= -9223372036854775808.0 && i < 9223372036854775808.0 {
		return
	}
	raise("RangeError", "%s out of Time range", timeIntervalFormat(x))
}

// timeIntervalFormat renders a duration for the RangeError above with C's `%f`
// as Ruby's vsnprintf spells the non-finite cases.
func timeIntervalFormat(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case math.IsInf(x, 1):
		return "Inf"
	}
	return strconv.FormatFloat(x, 'f', 6, 64)
}

// timeIntervalClassName names a value's class for the TypeError above. MRI uses
// rb_obj_class, which knows Rational and Complex apart from a plain Object; the
// VM-less caller falls back to the static table.
func (vm *VM) timeIntervalClassName(v object.Value) string {
	if vm != nil {
		if c := vm.classOf(v); c != nil {
			return c.name
		}
	}
	return classNameOf(v)
}

func (vm *VM) mutexLock(m *RMutex) {
	t := vm.currentThread
	if m.owner == nil {
		m.owner = t
		t.held = append(t.held, m)
		return
	}
	if m.owner == t {
		raise("ThreadError", "deadlock; recursive locking")
	}
	w := m.enqueue(t)
	// MRI wraps the wait in the equivalent of an ensure clause so a waiter leaving
	// through an interrupt unlinks itself from the queue (do_mutex_lock's
	// ccan_list_del, and the comment there that an rb_ensure would be needed).
	// Without it an unlock hands the mutex to a thread that is gone and the mutex
	// stays locked for the rest of the program.
	locked := false
	defer func() {
		if !locked {
			m.leave(t)
		}
	}()
	// do_mutex_lock (thread_sync.c) loops `while (mutex->fiber != fiber)`: sleep,
	// check interrupts, and only then decide whether the lock is held. rbgo needs
	// the same loop for the same reason — the wait can end without the lock.
	for m.owner != t {
		vm.interruptibleWait(w.ch)
		if !vm.threadInterrupted(t) {
			continue
		}
		// "release mutex before checking for interrupts...as interrupt checking
		// code might call rb_raise()" (do_mutex_lock). Whether the unlock handed us
		// the mutex a moment ago or we are still queued, leave cleanly first.
		m.leave(t)
		vm.serviceSafepointAt(t, true)
		// The interrupt turned out to be deferred by a Thread.handle_interrupt
		// frame, so go back to waiting — MRI's loop does the same.
		w = m.enqueue(t)
	}
	locked = true
	// On wake, mutexUnlock has already transferred ownership to t.
	t.held = append(t.held, m) // MRI: mutex_locked -> thread_mutex_insert
}

// mutexLockUninterruptible acquires m and defers any interrupt that arrives while
// it waits until the mutex is held. It is MRI's mutex_lock_uninterruptible --
// do_mutex_lock(self, 0) -- and it has one caller, for one reason: rb_mutex_sleep
// uses it as its ensure clause, so a thread killed after being signalled still
// REACQUIRES the lock before it unwinds. core/conditionvariable/wait_spec.rb names
// that outright ("reacquires the lock even if the thread is killed after being
// signaled"), and with the interruptible form the kill landed on the relock, the
// thread unwound without the mutex, and the Ruby ensure clause's unlock raised
// "Attempt to unlock a mutex which is not locked".
func (vm *VM) mutexLockUninterruptible(m *RMutex) {
	t := vm.currentThread
	t.shielded++
	defer func() { t.shielded-- }()
	vm.mutexLock(m)
}

// enqueue registers t as a waiter on m and returns its wait slot. Caller holds
// the GVL.
func (m *RMutex) enqueue(t *RThread) mutexWaiter {
	w := mutexWaiter{t: t, ch: make(chan struct{})}
	m.waitq = append(m.waitq, w)
	return w
}

// leave takes t off m: if t had already been handed ownership it is passed on to
// the next waiter, otherwise t's queue entries are dropped. It is idempotent, so
// the deferred cleanup and the in-loop interrupt path can both call it.
func (m *RMutex) leave(t *RThread) {
	if m.owner == t {
		m.handOn()
		return
	}
	kept := m.waitq[:0:0]
	for _, w := range m.waitq {
		if w.t != t {
			kept = append(kept, w)
		}
	}
	m.waitq = kept
}

// forget drops m from the thread's owned-mutex list (MRI's thread_mutex_remove,
// thread_sync.c). Caller holds the GVL.
func (t *RThread) forget(m *RMutex) {
	for i, h := range t.held {
		if h == m {
			t.held = append(t.held[:i:i], t.held[i+1:]...)
			return
		}
	}
}

// releaseHeldMutexes gives up every Mutex this thread still owns as it dies:
// MRI's rb_threadptr_unlock_all_locking_mutexes (thread.c), which walks the
// keeping_mutexes list and unlocks each one, waking its waiters. Without it a
// Mutex whose owner was killed, or died on an unhandled exception, stayed locked
// for the rest of the program, so the next Mutex#lock waited on a thread that no
// longer existed -- a hang that survived making the wait interruptible, because
// nothing was ever going to interrupt it.
func (vm *VM) releaseHeldMutexes(t *RThread) {
	for len(t.held) > 0 {
		m := t.held[0]
		if m.owner == t {
			m.handOn() // also removes m from t.held
			continue
		}
		t.forget(m)
	}
}

// handOn passes ownership to the head of the wait queue, or clears it when
// nobody is waiting. It is the tail of mutexUnlock without the ownership check,
// shared with the interrupt path that must give the mutex back.
func (m *RMutex) handOn() {
	if m.owner != nil {
		m.owner.forget(m) // MRI: thread_mutex_remove, inside rb_mutex_unlock_th
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

func (vm *VM) mutexUnlock(m *RMutex) {
	if m.owner != vm.currentThread {
		raise("ThreadError", "Attempt to unlock a mutex which is not locked")
	}
	m.handOn()
}

// threadWaitFor is rb_thread_wait_for (thread.c): park the current thread for
// d, releasing the GVL so other threads run, and deliver whatever asynchronous
// event woke it. It is the wait a blocking operation polls on — File#flock's
// 0.1 s retry, which MRI spells the same way — so the thread reports "sleep"
// while it waits and a Thread#kill or Thread#raise reaches it at the safepoint
// rather than after the whole operation.
func (vm *VM) threadWaitFor(d time.Duration) {
	t := vm.currentThread
	ch := t.parkWake()
	vm.threadBlock(func() {
		select {
		case <-ch:
		case <-time.After(d):
		}
	})
	t.unpark()
	vm.serviceSafepoint(t)
}

// registerSleep adds a GVL-aware Kernel#sleep that releases the lock while
// sleeping, so other threads run. It follows rb_f_sleep (process.c): a fiber
// scheduler, when one is installed and the running fiber is non-blocking, takes
// the whole operation over through #kernel_sleep; otherwise no argument (or an
// explicit nil) sleeps until woken and anything else is a duration.
func (vm *VM) registerSleep() {
	vm.cObject.define("sleep", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		// rb_f_sleep asks rb_fiber_scheduler_current first and hands the arguments
		// on verbatim with rb_fiber_scheduler_kernel_sleepv — so `sleep` with no
		// argument calls #kernel_sleep with no argument, not with nil. The
		// scheduler owns the waiting from there (the usual implementation yields
		// the fiber), and none of the argument checks below run: MRI does not look
		// at the duration at all on this path.
		//
		// The result on every path is `time(0) - beg`: rb_f_sleep brackets the wait
		// with a clock of whole seconds, so what it counts is the number of second
		// boundaries crossed, not the duration rounded. sleep(1.5) therefore
		// answers 1 or 2 depending on where the call falls inside a second.
		beg := time.Now().Unix()
		if sched := vm.fiberSchedulerCurrent(); sched != nil {
			vm.send(sched, "kernel_sleep", args, nil)
			return object.IntValue(time.Now().Unix() - beg)
		}
		hasDur := len(args) > 0 && !object.IsNil(args[0])
		var secs float64
		if hasDur {
			// rb_check_arity(argc, 0, 1) guards the duration branch only, so a
			// second argument is refused here but tolerated by a scheduler above.
			if len(args) > 1 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 0..1)", len(args))
			}
			secs = vm.timeInterval(args[0])
		} else if len(args) > 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0..1)", len(args))
		}
		t := vm.currentThread
		ch := t.parkWake()
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
		return object.IntValue(time.Now().Unix() - beg)
	})
}
