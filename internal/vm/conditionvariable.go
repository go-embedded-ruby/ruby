// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// RConditionVariable backs Thread::ConditionVariable (also available top-level).
// It layers on the thread primitives: #wait registers the current thread and
// releases-and-sleeps by delegating to mutex.sleep (which parks on the thread's
// wake channel), and #signal/#broadcast wake waiters through the same mechanism
// Thread#wakeup uses. The waiter list is mutated only while the current thread
// holds the emulated GVL, so it needs no Go-level lock and is race-free under
// `go test -race`.
type RConditionVariable struct {
	waiters []*RThread // threads in #wait, oldest first (FIFO wake order)
}

func (c *RConditionVariable) ToS() string     { return "#<Thread::ConditionVariable>" }
func (c *RConditionVariable) Inspect() string { return c.ToS() }
func (c *RConditionVariable) Truthy() bool    { return true }

// removeWaiter drops t from the FIFO list if present (idempotent: a thread woken
// by #signal/#broadcast is already removed, and its #wait still self-removes).
func (c *RConditionVariable) removeWaiter(t *RThread) {
	out := c.waiters[:0]
	for _, w := range c.waiters {
		if w != t {
			out = append(out, w)
		}
	}
	c.waiters = out
}

func (vm *VM) registerConditionVariable() {
	cCV := newClass("ConditionVariable", vm.cObject)
	vm.consts["ConditionVariable"] = cCV
	vm.consts["Thread"].(*RClass).consts["ConditionVariable"] = cCV

	cCV.smethods["new"] = &Method{name: "new", owner: cCV, native: func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RConditionVariable{}
	}}

	// wait(mutex, timeout = nil): register as a waiter, then release the mutex and
	// sleep by delegating to mutex.sleep(timeout) exactly as MRI does — so any
	// object responding to #sleep works (the spec passes a mock). On wake (a
	// #signal/#broadcast, a direct Thread#wakeup/#run, or the timeout) mutex.sleep
	// re-acquires the mutex and returns. Returns self.
	cCV.define("wait", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cv := self.(*RConditionVariable)
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
		}
		mutex := args[0]
		t := vm.currentThread
		cv.waiters = append(cv.waiters, t)
		defer cv.removeWaiter(t)
		if len(args) >= 2 && !object.IsNil(args[1]) {
			vm.send(mutex, "sleep", []object.Value{args[1]}, nil)
		} else {
			vm.send(mutex, "sleep", nil, nil)
		}
		return cv
	})

	// signal: wake the thread that has been waiting longest (FIFO). No-op with no
	// waiters. Returns self.
	cCV.define("signal", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		cv := self.(*RConditionVariable)
		if len(cv.waiters) > 0 {
			t := cv.waiters[0]
			cv.waiters = cv.waiters[1:]
			t.wakeParked()
		}
		return cv
	})

	// broadcast: wake every waiting thread. Returns self.
	cCV.define("broadcast", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		cv := self.(*RConditionVariable)
		ws := cv.waiters
		cv.waiters = nil
		for _, t := range ws {
			t.wakeParked()
		}
		return cv
	})

	// marshal_dump: a ConditionVariable cannot be dumped, matching MRI.
	cCV.define("marshal_dump", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		raise("TypeError", "can't dump ConditionVariable")
		return object.NilV
	})
}
