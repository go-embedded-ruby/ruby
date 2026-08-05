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

	// locals backs Thread#[] / #[]= (fiber-local storage), lazily allocated.
	locals map[object.Value]object.Value

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
// err carries an exception to re-raise in the fiber that receives control.
type fiberMsg struct {
	args []object.Value
	err  *RubyError
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
	return &Fiber{blk: blk, inbox: make(chan fiberMsg), state: fibCreated, thread: t, label: label}
}

// newRootFiber builds a thread's root fiber, which is running from the outset
// (it is the fiber executing the thread body at the top level).
func newRootFiber(t *RThread) *Fiber {
	return &Fiber{inbox: make(chan fiberMsg), state: fibRunning, thread: t}
}

func (vm *VM) registerFiber() {
	cFiber := newClass("Fiber", vm.cObject)
	vm.consts["Fiber"] = cFiber
	if _, ok := vm.consts["FiberError"]; !ok {
		fe := newClass("FiberError", vm.consts["StandardError"].(*RClass))
		vm.consts["FiberError"] = fe
	}

	cFiber.smethods["new"] = &Method{name: "new", owner: cFiber, native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "tried to create a Fiber without a block")
		}
		return newFiber(vm.currentThread, blk)
	}}
	cFiber.smethods["yield"] = &Method{name: "yield", owner: cFiber, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.fiberYield(args)
	}}
	cFiber.smethods["current"] = &Method{name: "current", owner: cFiber, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.currentFiber
	}}
	cFiber.define("resume", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.fiberResume(self.(*Fiber), args)
	})
	cFiber.define("transfer", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.fiberTransfer(self.(*Fiber), args)
	})
	cFiber.define("alive?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*Fiber).state != fibDead)
	})
}

// setCurFiber records f as the fiber now executing in the current thread. It is
// called by whichever goroutine has just gained control, so vm.currentFiber and
// the thread's cur pointer always name the running fiber (never nil).
func (vm *VM) setCurFiber(f *Fiber) {
	vm.currentFiber = f
	vm.currentThread.cur = f
}

// fiberSwitch hands control from the running fiber to target, delivering msg,
// then blocks the (now suspended) source fiber on its own inbox until control
// returns to it and returns the message that resumed it. The receiving side is
// responsible for calling setCurFiber, so this never publishes target as current
// before target actually runs.
func (vm *VM) fiberSwitch(target *Fiber, msg fiberMsg) fiberMsg {
	src := vm.currentFiber
	target.inbox <- msg
	got := <-src.inbox
	vm.setCurFiber(src)
	return got
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

// fiberResume transfers control into f with args (the asymmetric discipline) and
// returns the value f yields or finally produces. It enforces MRI's resume rules.
func (vm *VM) fiberResume(f *Fiber, args []object.Value) object.Value {
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
	created := f.state == fibCreated
	if created {
		vm.fiberBegin(f)
	}
	t := vm.currentThread
	c := vm.currentFiber
	c.state = fibResuming
	f.resumer = t.curResumed
	t.curResumed = f
	f.state = fibRunning
	msg := vm.fiberSwitch(f, fiberMsg{args: args})
	c.state = fibRunning
	if msg.err != nil {
		panic(*msg.err)
	}
	return yieldValue(msg.args)
}

// fiberTransfer switches control to target with args (the symmetric discipline)
// without touching the resume chain, and returns the value delivered when control
// next returns to the calling fiber. Transferring to the running fiber is a
// no-op that returns its argument.
func (vm *VM) fiberTransfer(target *Fiber, args []object.Value) object.Value {
	if target.thread != vm.currentThread {
		raise("FiberError", "fiber called across threads")
	}
	if target == vm.currentFiber {
		return yieldValue(args) // transfer to the running fiber: continue immediately
	}
	switch target.state {
	case fibDead:
		raise("FiberError", "dead fiber called")
	case fibYielded:
		raise("FiberError", "cannot transfer to a fiber that has suspended by Fiber.yield")
	case fibResuming, fibRunning:
		raise("FiberError", "cannot transfer to a resuming fiber")
	}
	created := target.state == fibCreated
	if created {
		vm.fiberBegin(target)
	}
	c := vm.currentFiber
	c.state = fibTransferred
	target.state = fibRunning
	msg := vm.fiberSwitch(target, fiberMsg{args: args})
	c.state = fibRunning
	if msg.err != nil {
		panic(*msg.err)
	}
	return yieldValue(msg.args)
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
	msg := vm.fiberSwitch(resumer, fiberMsg{args: args})
	f.state = fibRunning
	return yieldValue(msg.args)
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
