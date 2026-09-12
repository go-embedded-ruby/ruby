// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Fiber is a cooperative coroutine backed by a goroutine. Control is handed
// between fibers over per-fiber inbox channels: to switch to another fiber the
// running one sends it a message and then blocks on its own inbox until control
// returns, so exactly one fiber runs at a time within a Ruby thread (a strict
// handoff — no real concurrency, and the channel operations give the race
// detector the happens-before it needs).
//
// Two switching disciplines coexist, matching MRI:
//   - resume / Fiber.yield: ASYMMETRIC. resume pushes the target onto the
//     thread's resume chain; Fiber.yield (or the block finishing) returns control
//     to the resumer.
//   - transfer: SYMMETRIC. any fiber may transfer to any other without touching
//     the resume chain. When a fiber reached only by transfer finishes, control
//     returns to the current top of the resume chain (the root fiber if nothing
//     was resumed).
//
// Every Ruby thread owns a root fiber (RThread.rootFiber), which is the fiber
// Fiber.current returns at the top level and the fiber a bare transfer chain
// unwinds back to. Fiber-local storage (Thread#[]) lives on each Fiber, so it is
// not shared across a thread's fibers.
type Fiber struct {
	blk    *Proc
	inbox  chan fiberMsg
	state  fiberState
	thread *RThread

	// resumer is the fiber to hand control back to on Fiber.yield or termination
	// while this fiber sits on the resume chain; nil when this fiber is not a
	// resume target (reached by transfer, or the root fiber).
	resumer *Fiber

	// resuming is MRI's fiber->resuming_fiber (cont.c): the fiber this one handed
	// control to with #resume and is now suspended waiting on. It is what
	// Fiber#raise walks so that raising on a fiber that is itself resuming another
	// delivers the exception to the fiber actually running, and it is cleared the
	// moment control comes back. Non-nil exactly while state == fibResuming.
	resuming *Fiber

	// locals backs Thread#[] / #[]= (fiber-local storage), lazily allocated.
	locals map[object.Value]object.Value

	// storage backs Fiber#storage / Fiber.[] / Fiber.[]= — MRI's per-execution
	// context ec->storage (cont.c fiber_storage_get/set). nil means "not allocated
	// yet": Fiber[] on it reads nil and Fiber[]= allocates it lazily, exactly as
	// fiber_storage_get(fiber, allocate) does. A fiber created from another
	// inherits a shallow copy (rb_obj_dup) taken at creation, so later writes on
	// either side are private.
	storage *object.Hash

	// blocking is MRI's fiber->blocking. A root fiber is blocking; Fiber.new
	// creates a non-blocking fiber unless `blocking: true` is passed. It drives
	// Fiber#blocking? (a boolean) and Fiber.blocking? (1 or false).
	blocking bool

	// killed records that Fiber#kill has been asked for. Like MRI's fiber->killed
	// it is checked at every switch-in (fiber_check_killed), so a fiber marked
	// while it was suspended unwinds the moment it next gains control — including
	// a fiber killed by one of its own descendants.
	killed bool

	// curExc parks this fiber's $! while it is suspended. MRI keeps errinfo in the
	// execution context, which is per fiber, so a fiber rescuing an exception must
	// not leave it visible as the caller's $! when control returns — Fiber#raise's
	// automatic cause chaining reads the *calling* context's $!.
	curExc object.Value

	// label is the non-empty middle field of #inspect (the block's source file);
	// empty only for a root fiber, which has no block.
	label string
}

// fiberState is how a fiber is currently suspended (or running); it drives the
// resume/transfer mixing rules MRI enforces with FiberError.
type fiberState uint8

const (
	fibCreated     fiberState = iota // never switched into yet
	fibRunning                       // currently executing (== vm.currentFiber)
	fibResuming                      // suspended, having resumed a child fiber
	fibYielded                       // suspended by Fiber.yield
	fibTransferred                   // suspended by Fiber#transfer
	fibDead                          // block finished
)

// fiberMsg is the payload handed across a fiber switch: args carries the
// resume/transfer arguments (and, on termination, the single final value),
// err carries an exception to re-raise in the fiber that receives control, exc
// carries a Fiber#raise exception object (raised — and so backtraced — in the
// receiving fiber, as MRI does). A Fiber#kill carries no payload: it sets the
// target's killed flag and the switch-in check (fiber_check_killed, mirrored in
// fiberSwitch) does the unwinding.
type fiberMsg struct {
	args []object.Value
	err  *RubyError
	exc  object.Value
}

func (f *Fiber) ToS() string { return f.Inspect() }
func (f *Fiber) Inspect() string {
	var status string
	switch {
	case f.state == fibCreated:
		status = "created"
	case f.state == fibDead:
		status = "terminated"
	case f == f.thread.vmCurrentFiber():
		status = "resumed"
	default:
		status = "suspended"
	}
	// MRI renders #<Fiber:0x... LABEL (status)>. The root fiber has no block, so
	// its middle field is empty (the spec allows it); a child fiber shows its
	// block's source file so the field is always non-empty.
	if f.label == "" {
		return fmt.Sprintf("#<Fiber:%p (%s)>", f, status)
	}
	return fmt.Sprintf("#<Fiber:%p %s (%s)>", f, f.label, status)
}
func (f *Fiber) Truthy() bool { return true }

// vmCurrentFiber returns the fiber currently running in this fiber's thread. It
// exists only so Inspect (which has no *VM) can ask "am I the running fiber?".
func (t *RThread) vmCurrentFiber() *Fiber { return t.cur }

// newFiber builds a not-yet-started fiber for thread t running blk.
func newFiber(t *RThread, blk *Proc) *Fiber {
	label := "-"
	if blk != nil && blk.iseq != nil && blk.iseq.File != "" {
		label = blk.iseq.File
	}
	return &Fiber{blk: blk, inbox: make(chan fiberMsg), state: fibCreated, thread: t, label: label, curExc: object.NilV}
}

// newRootFiber builds a thread's root fiber, which is running from the outset
// (it is the fiber executing the thread body at the top level). A root fiber is
// blocking, as in MRI: only Fiber.new / Fiber.schedule make a non-blocking one.
func newRootFiber(t *RThread) *Fiber {
	return &Fiber{inbox: make(chan fiberMsg), state: fibRunning, thread: t, blocking: true, curExc: object.NilV}
}

func (vm *VM) registerFiber() {
	cFiber := newClass("Fiber", vm.cObject)
	vm.consts["Fiber"] = cFiber
	if _, ok := vm.consts["FiberError"]; !ok {
		fe := newClass("FiberError", vm.consts["StandardError"].(*RClass))
		vm.consts["FiberError"] = fe
	}
	sdef := func(name string, fn NativeFn) {
		cFiber.smethods[name] = &Method{name: name, owner: cFiber, native: fn}
	}

	// Fiber.new(blocking: false, storage: nil) { ... }. MRI reads three keywords
	// (blocking:, pool:, storage:) in rb_fiber_initialize_kw; rbgo has no fiber
	// pool, so pool: is accepted and ignored. With no storage: keyword the new
	// fiber inherits a shallow copy of the creating fiber's storage
	// (inherit_fiber_storage == rb_obj_dup of the current one), which is what
	// makes Fiber[] visible to a nested fiber without sharing writes.
	sdef("new", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "tried to create a Fiber without a block")
		}
		f := newFiber(vm.currentThread, blk)
		f.storage = dupFiberStorage(vm.currentFiber.storage)
		if kw := fiberKwargs(args); kw != nil {
			if v, ok := kw.Get(object.Symbol("blocking")); ok {
				f.blocking = v.Truthy()
			}
			if v, ok := kw.Get(object.Symbol("storage")); ok {
				fiberStorageValidate(v)
				h, _ := v.(*object.Hash)
				f.storage = dupFiberStorage(h)
			}
		}
		return f
	})
	sdef("yield", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.fiberYield(args)
	})
	sdef("current", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.currentFiber
	})
	// Fiber.blocking? reports MRI's thread->blocking counter: false when the
	// running fiber is non-blocking, otherwise the count (1 in practice, since
	// only the current fiber contributes and Fiber.blocking is a no-op when
	// already blocking). Fiber#blocking? is the plain per-fiber boolean.
	sdef("blocking?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.currentFiber.blocking {
			return object.Integer(1)
		}
		return object.Bool(false)
	})
	// Fiber.blocking { |fiber| ... } forces the running fiber to be blocking for
	// the duration of the block (rb_fiber_blocking, cont.c): already-blocking is a
	// bare yield, otherwise the flag is set and restored under an ensure.
	sdef("blocking", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		f := vm.currentFiber
		if f.blocking {
			return vm.callBlock(blk, []object.Value{f})
		}
		f.blocking = true
		defer func() { f.blocking = false }()
		return vm.callBlock(blk, []object.Value{f})
	})
	// Fiber[] / Fiber[]= address the running fiber's storage (never another
	// fiber's), coercing the key with rb_to_symbol — a Symbol, a String, or
	// anything with #to_str; #to_sym is deliberately not consulted. Assigning nil
	// deletes the key, and reading from a fiber with no storage yet is nil without
	// allocating one.
	sdef("[]", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		key := vm.fiberStorageKey(args[0])
		st := vm.currentFiber.storage
		if st == nil {
			return object.NilV
		}
		if v, ok := st.Get(key); ok {
			return v
		}
		return object.NilV
	})
	sdef("[]=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		key := vm.fiberStorageKey(args[0])
		f := vm.currentFiber
		if object.IsNil(args[1]) {
			if f.storage != nil {
				f.storage.Delete(key)
			}
			return object.NilV
		}
		if f.storage == nil {
			f.storage = object.NewHash()
		}
		f.storage.Set(key, args[1])
		return args[1]
	})
	cFiber.define("resume", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.fiberResume(self.(*Fiber), args)
	})
	cFiber.define("transfer", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.fiberTransfer(self.(*Fiber), args)
	})
	cFiber.define("alive?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*Fiber).state != fibDead)
	})
	cFiber.define("blocking?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*Fiber).blocking)
	})
	// Fiber#raise raises in the fiber at the point it last suspended
	// (rb_fiber_raise, cont.c). The exception object is built in the CALLER's
	// context — so its constructor and its automatic cause chaining ($!) are the
	// caller's — while the backtrace is stamped in the target, where it is
	// actually raised.
	cFiber.define("raise", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.fiberRaise(self.(*Fiber), vm.fiberRaiseException(args))
	})
	// Fiber#kill terminates the fiber with an uncatchable unwind: ensure blocks
	// run, rescue clauses do not (rb_fiber_m_kill, cont.c). An unborn fiber goes
	// straight to terminated; a second kill is a no-op returning false; killing
	// the current fiber unwinds here and now.
	cFiber.define("kill", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		f := self.(*Fiber)
		if f.killed {
			return object.Bool(false)
		}
		f.killed = true
		switch {
		case f.state == fibCreated:
			f.state = fibDead
		case f.state == fibDead:
			// already terminated: nothing to unwind
		case f == vm.currentFiber:
			panic(killSignal{})
		default:
			vm.fiberRaise(f, nil)
		}
		return f
	})
	cFiber.define("storage", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		f := self.(*Fiber)
		vm.fiberStorageOwnFiber(f)
		if f.storage == nil {
			return object.NilV
		}
		return dupFiberStorage(f.storage)
	})
	// Fiber.scheduler / Fiber.set_scheduler hold the current thread's fiber
	// scheduler (MRI keeps it in thread->scheduler, rb_fiber_scheduler_set). rbgo
	// runs every fiber blocking, so nothing consults the scheduler yet; setting
	// one is validated and stored so the hook is observable, as ruby/spec pins.
	sdef("scheduler", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if s := vm.currentThread.scheduler; s != nil {
			return s
		}
		return object.NilV
	})
	sdef("set_scheduler", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		if object.IsNil(args[0]) {
			vm.currentThread.scheduler = nil
			return object.NilV
		}
		// rb_fiber_scheduler_set verifies the whole interface up front, naming the
		// first method the object is missing.
		for _, m := range []string{"block", "unblock", "kernel_sleep", "io_wait"} {
			if !vm.respondsTo(args[0], m) {
				raise("ArgumentError", "Scheduler must implement #%s", m)
			}
		}
		vm.currentThread.scheduler = args[0]
		return args[0]
	})
	cFiber.define("storage=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		f := self.(*Fiber)
		vm.fiberStorageOwnFiber(f)
		fiberStorageValidate(args[0])
		h, _ := args[0].(*object.Hash)
		f.storage = dupFiberStorage(h)
		return args[0]
	})
}

// fiberKwargs returns the trailing Hash of a Fiber.new call — its keyword
// bundle. Fiber.new takes no positional arguments, so a trailing Hash is always
// the keywords.
func fiberKwargs(args []object.Value) *object.Hash {
	if len(args) == 0 {
		return nil
	}
	h, _ := args[len(args)-1].(*object.Hash)
	return h
}

// dupFiberStorage takes the shallow copy MRI makes with rb_obj_dup whenever a
// storage hash crosses into a fiber (inheritance at creation, Fiber#storage=,
// and the copy Fiber#storage hands back). A nil storage stays nil — "not
// allocated yet" is distinct from an empty hash.
func dupFiberStorage(h *object.Hash) *object.Hash {
	if h == nil {
		return nil
	}
	d := object.NewHashCap(h.Len())
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		d.Set(k, v)
	}
	return d
}

// fiberStorageValidate mirrors fiber_storage_validate (cont.c): nil is allowed
// and means "lazily initialised", anything that is not a Hash is a TypeError, a
// frozen Hash is a FrozenError, and every key must already be a Symbol (the
// storage hash is never key-coerced wholesale, only per-key through Fiber[]=).
func fiberStorageValidate(v object.Value) {
	if object.IsNil(v) {
		return
	}
	h, ok := v.(*object.Hash)
	if !ok {
		raise("TypeError", "storage must be a hash")
	}
	if h.Frozen {
		raise("FrozenError", "storage must not be frozen")
	}
	for _, k := range h.Keys {
		if _, isSym := k.(object.Symbol); !isSym {
			raise("TypeError", "wrong argument type %s (expected Symbol)", classNameOf(k))
		}
	}
}

// fiberStorageOwnFiber enforces storage_access_must_be_from_same_fiber (cont.c):
// Fiber#storage and Fiber#storage= only work on Fiber.current.
func (vm *VM) fiberStorageOwnFiber(f *Fiber) {
	if f != vm.currentFiber {
		raise("ArgumentError", "Fiber storage can only be accessed from the Fiber it belongs to")
	}
}

// fiberStorageKey coerces a Fiber[] / Fiber[]= key the way MRI's rb_to_symbol
// does: a Symbol is itself, a String becomes the Symbol of its content, and any
// other object is asked for #to_str (NOT #to_sym — ruby/spec pins that). Nothing
// else is a key.
func (vm *VM) fiberStorageKey(v object.Value) object.Value {
	switch k := v.(type) {
	case object.Symbol:
		return k
	case *object.String:
		return object.Symbol(k.Str())
	}
	if vm.respondsTo(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return object.Symbol(s.Str())
		}
	}
	return raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
}

// fiberRaiseException builds the exception object for Fiber#raise from its
// arguments, exactly as Kernel#raise does (rb_make_exception plus, since Ruby
// 4.0, the cause: keyword): a bare call is a RuntimeError with an empty message,
// a third positional argument is the backtrace, and cause: overrides the
// automatic chaining from the caller's $!. It does not panic — the exception
// travels to the target fiber, which raises it.
func (vm *VM) fiberRaiseException(args []object.Value) object.Value {
	args, causeGiven, causeVal := popCauseKwarg(args)
	if len(args) == 0 {
		if causeGiven {
			raise("ArgumentError", "only cause is given with no arguments")
		}
		exc := vm.send(vm.consts["RuntimeError"].(*RClass), "new", []object.Value{object.NewString("")}, nil)
		vm.applyRaiseCause(exc, false, object.NilV)
		return exc
	}
	exc := vm.raiseExceptionObject(args)
	if len(args) >= 3 {
		vm.applyRaiseBacktrace(exc, args[2])
	}
	// A cause: that already reaches this exception through its own chain is
	// circular; MRI rejects it before linking. cause == exc is simply not set.
	if causeGiven && !object.IsNil(causeVal) && causeVal != exc {
		for c := getIvar(causeVal, causeIvar); !object.IsNil(c); c = getIvar(c, causeIvar) {
			if c == exc {
				raise("ArgumentError", "circular causes")
			}
		}
	}
	// A cause: that IS the exception being raised is not a cause at all — MRI
	// leaves the link unset rather than pointing the exception at itself.
	if causeGiven && causeVal == exc {
		return exc
	}
	vm.applyRaiseCause(exc, causeGiven, causeVal)
	return exc
}

// fiberRaise delivers exc to f, following MRI's fiber_raise (cont.c):
//   - raising on the running fiber raises here and now;
//   - a fiber that is itself resuming another is not where execution is, so the
//     exception walks down to the fiber actually running;
//   - a fiber suspended in #transfer is transferred into, one suspended in
//     Fiber.yield (or never started) is resumed into.
//
// A nil exc is Fiber#kill's uncatchable unwind, which travels the same way.
func (vm *VM) fiberRaise(f *Fiber, exc object.Value) object.Value {
	if f == vm.currentFiber {
		if exc == nil {
			panic(killSignal{})
		}
		panic(vm.excError(vm.captureBacktrace(exc)))
	}
	if f.resuming != nil {
		return vm.fiberRaise(f.resuming, exc)
	}
	msg := fiberMsg{exc: exc}
	if f.state == fibTransferred {
		return vm.fiberTransferMsg(f, msg)
	}
	return vm.fiberResumeMsg(f, msg)
}

// setCurFiber records f as the fiber now executing in the current thread. It is
// called by whichever goroutine has just gained control, so vm.currentFiber and
// the thread's cur pointer always name the running fiber (never nil). It also
// restores f's own $!, which MRI keeps in the (per fiber) execution context.
func (vm *VM) setCurFiber(f *Fiber) {
	vm.currentFiber = f
	vm.currentThread.cur = f
	vm.curExc = f.curExc
}

// fiberSwitch hands control from the running fiber to target, delivering msg,
// then blocks the (now suspended) source fiber on its own inbox until control
// returns to it and returns the message that resumed it. The receiving side is
// responsible for calling setCurFiber, so this never publishes target as current
// before target actually runs. On regaining control the source runs MRI's
// fiber_check_killed: a fiber marked by Fiber#kill while it was suspended
// unwinds at exactly this point.
func (vm *VM) fiberSwitch(target *Fiber, msg fiberMsg) fiberMsg {
	src := vm.currentFiber
	src.curExc = vm.curExc
	target.inbox <- msg
	got := <-src.inbox
	vm.setCurFiber(src)
	if src.killed {
		panic(killSignal{})
	}
	return got
}

// fiberDeliver unpacks a message that has just switched control into the running
// fiber: a Fiber#raise exception is raised here (so the backtrace is this
// fiber's), an exception propagated from a finished fiber is re-raised, and
// otherwise the arguments become the value of the switch. A Fiber#kill never
// reaches here — fiberSwitch's killed check fires first.
func (vm *VM) fiberDeliver(msg fiberMsg) object.Value {
	if msg.exc != nil {
		panic(vm.excError(vm.captureBacktrace(msg.exc)))
	}
	if msg.err != nil {
		panic(*msg.err)
	}
	return yieldValue(msg.args)
}

// fiberBegin spawns target's goroutine, which waits for the first switch-in,
// runs the block, and on completion (or a panic) hands control on via
// fiberTerminate. Called once, the first time a created fiber is switched into.
func (vm *VM) fiberBegin(f *Fiber) {
	go func() {
		msg := <-f.inbox
		vm.setCurFiber(f)
		var result object.Value = object.NilV
		var rerr *RubyError
		func() {
			defer func() {
				if r := recover(); r != nil {
					if _, ok := r.(killSignal); ok {
						// Fiber#kill: ensure blocks have already run during the unwind and
						// nothing is re-raised in the fiber that regains control.
						result = object.NilV
						return
					}
					if re, ok := r.(RubyError); ok {
						rerr = &re
					} else {
						e := RubyError{Class: "FiberError", Message: "fiber terminated abnormally"}
						rerr = &e
					}
				}
			}()
			result = vm.callBlock(f.blk, msg.args)
		}()
		vm.fiberTerminate(f, result, rerr)
	}()
}

// fiberResume transfers control into f with args, the shape Fiber#resume and the
// Enumerator/Lazy fiber pumps call. It is fiberResumeMsg with a plain argument
// payload.
func (vm *VM) fiberResume(f *Fiber, args []object.Value) object.Value {
	return vm.fiberResumeMsg(f, fiberMsg{args: args})
}

// fiberTransfer switches control to target with args, the shape Fiber#transfer
// calls. It is fiberTransferMsg with a plain argument payload.
func (vm *VM) fiberTransfer(target *Fiber, args []object.Value) object.Value {
	return vm.fiberTransferMsg(target, fiberMsg{args: args})
}

// fiberResumeMsg transfers control into f with msg (the asymmetric discipline) and
// returns the value f yields or finally produces. It enforces MRI's resume rules;
// a msg carrying a Fiber#raise exception additionally refuses an unborn fiber,
// which has no suspension point to raise at (fiber_resume_kw, cont.c).
func (vm *VM) fiberResumeMsg(f *Fiber, msg fiberMsg) object.Value {
	if msg.exc != nil && f.state == fibCreated {
		raise("FiberError", "cannot raise exception on unborn fiber")
	}
	if f.thread != vm.currentThread {
		raise("FiberError", "fiber called across threads")
	}
	switch f.state {
	case fibDead:
		raise("FiberError", "dead fiber called")
	case fibRunning:
		raise("FiberError", "attempt to resume the current fiber")
	case fibResuming:
		raise("FiberError", "attempt to resume a resuming fiber")
	case fibTransferred:
		raise("FiberError", "cannot resume a fiber that has been transferred")
	}
	if f.state == fibCreated {
		vm.fiberBegin(f)
	}
	t := vm.currentThread
	c := vm.currentFiber
	c.state = fibResuming
	c.resuming = f
	f.resumer = t.curResumed
	t.curResumed = f
	f.state = fibRunning
	got := vm.fiberSwitch(f, msg)
	c.state = fibRunning
	c.resuming = nil
	return vm.fiberDeliver(got)
}

// fiberTransferMsg switches control to target with msg (the symmetric discipline)
// without touching the resume chain, and returns the value delivered when control
// next returns to the calling fiber. Transferring to the running fiber is a
// no-op that returns its argument.
func (vm *VM) fiberTransferMsg(target *Fiber, msg fiberMsg) object.Value {
	if target.thread != vm.currentThread {
		raise("FiberError", "fiber called across threads")
	}
	if target == vm.currentFiber {
		return vm.fiberDeliver(msg) // transfer to the running fiber: continue immediately
	}
	switch target.state {
	case fibDead:
		raise("FiberError", "dead fiber called")
	case fibYielded:
		raise("FiberError", "cannot transfer to a fiber that has suspended by Fiber.yield")
	case fibResuming, fibRunning:
		raise("FiberError", "cannot transfer to a resuming fiber")
	}
	if target.state == fibCreated {
		vm.fiberBegin(target)
	}
	c := vm.currentFiber
	c.state = fibTransferred
	target.state = fibRunning
	got := vm.fiberSwitch(target, msg)
	c.state = fibRunning
	return vm.fiberDeliver(got)
}

// fiberYield suspends the running fiber, handing val back to its resumer; it
// returns the arguments of the next resume. Only a fiber on top of the resume
// chain may yield — the root fiber, or a fiber reached only by transfer, raises.
func (vm *VM) fiberYield(args []object.Value) object.Value {
	f := vm.currentFiber
	t := vm.currentThread
	if f != t.curResumed || f.resumer == nil {
		raise("FiberError", "can't yield from root fiber")
	}
	resumer := f.resumer
	f.state = fibYielded
	t.curResumed = resumer
	got := vm.fiberSwitch(resumer, fiberMsg{args: args})
	f.state = fibRunning
	return vm.fiberDeliver(got)
}

// fiberTerminate runs in a finished fiber's goroutine to hand control on. A fiber
// that sits on top of the resume chain returns to its resumer; one reached only
// by transfer returns to the current top of the resume chain (the root fiber if
// nothing was resumed). The value (or exception) is delivered to that fiber and
// this goroutine ends.
func (vm *VM) fiberTerminate(f *Fiber, result object.Value, rerr *RubyError) {
	f.state = fibDead
	t := f.thread
	var target *Fiber
	if f == t.curResumed {
		target = f.resumer
		t.curResumed = f.resumer
		f.resumer = nil
	} else {
		target = t.curResumed
	}
	target.inbox <- fiberMsg{args: []object.Value{result}, err: rerr}
}

// yieldValue packs resume/yield/transfer arguments into the single value Ruby
// exposes: the bare value for one argument, nil for none, an array for several.
func yieldValue(args []object.Value) object.Value {
	switch len(args) {
	case 0:
		return object.NilV
	case 1:
		return args[0]
	default:
		return object.NewArrayFromSlice(append([]object.Value{}, args...))
	}
}
