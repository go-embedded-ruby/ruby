// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"time"

	async "github.com/go-ruby-async/async"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires Kernel#Async, the Async::Task node surface and the Async::
// synchronization primitives over github.com/go-ruby-async/async. A task body is
// a captured Ruby block run against an AsyncTask (running the block is the rbgo
// seam; the reactor/scheduling is the library). Every blocking operation takes
// the currently-running task as its caller — the invariant is that while a task
// body's Ruby code runs, vm.curAsyncTask is that task (set in runAsyncBody and
// restored after every suspending call by asyncBlocking).

// registerAsyncKernel installs Kernel#Async — Async { |task| … } runs the async
// reactor with the block as the root task and returns the root's result, mapping
// a task failure onto the corresponding Ruby exception.
func (vm *VM) registerAsyncKernel() {
	vm.cObject.define("Async", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "Async requires a block")
		}
		prev := vm.curAsyncTask
		res, err := async.Run(vm.asyncBody(blk))
		vm.curAsyncTask = prev
		if prev == nil {
			vm.asyncTasks = nil // the root reactor finished; drop its wrapper cache
		}
		vm.asyncRaise(err)
		return asyncResultValue(res)
	})
}

// asyncWrap returns the one Async::Task wrapper for t, creating it on first use
// and caching it so the same underlying task always yields the same Ruby object
// (Async::Task.current, a block's task param and #parent/#children then compare
// #equal? as in MRI). A nil task maps to nil.
func (vm *VM) asyncWrap(t *async.Task) object.Value {
	if t == nil {
		return object.NilV
	}
	if vm.asyncTasks == nil {
		vm.asyncTasks = map[*async.Task]*AsyncTask{}
	}
	if w, ok := vm.asyncTasks[t]; ok {
		return w
	}
	w := &AsyncTask{t: t, cls: vm.cAsyncTask}
	vm.asyncTasks[t] = w
	return w
}

// asyncBody wraps a Ruby task block as an async.Body.
func (vm *VM) asyncBody(blk *Proc) async.Body {
	return func(t *async.Task) (any, error) {
		return vm.runAsyncBody(blk, t)
	}
}

// runAsyncBody runs a Ruby task block as a task's body: it makes the task current
// while the block runs and wraps it for the block param. A Ruby exception raised
// by the block is caught and returned as a Go error the library propagates
// through the task tree (re-raised at the boundary that surfaces the failure);
// anything else — notably async's private cancellation panic (Async::Stop /
// TimeoutError), which the library recovers in Task.run — is re-raised so the
// library sees it.
func (vm *VM) runAsyncBody(blk *Proc, t *async.Task) (result any, err error) {
	prev := vm.curAsyncTask
	vm.curAsyncTask = t
	defer func() {
		vm.curAsyncTask = prev
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok {
				result, err = nil, &asyncRubyError{e: re}
				return
			}
			panic(r)
		}
	}()
	return vm.callBlock(blk, []object.Value{vm.asyncWrap(t)}), nil
}

// asyncCaller returns the currently-running task, raising if called outside an
// Async reactor (no task is current).
func (vm *VM) asyncCaller() *async.Task {
	if vm.curAsyncTask == nil {
		raise("RuntimeError", "no Async task is currently running (call inside an Async{} block)")
	}
	return vm.curAsyncTask
}

// asyncBlocking runs a suspending async op on behalf of the current task: it
// resolves the caller, runs fn (which may suspend and let other tasks make
// themselves current), then restores curAsyncTask so the rest of this task's
// block still sees itself as current.
func (vm *VM) asyncBlocking(fn func(caller *async.Task)) {
	caller := vm.asyncCaller()
	defer func() { vm.curAsyncTask = caller }()
	fn(caller)
}

// asyncRaise re-raises a task failure into the VM: a Ruby exception raised inside
// a body is re-raised verbatim; a stop/timeout maps to Async::Stop /
// Async::TimeoutError; any other Go error becomes a RuntimeError.
func (vm *VM) asyncRaise(err error) {
	if err == nil {
		return
	}
	var re *asyncRubyError
	if errors.As(err, &re) {
		panic(re.e)
	}
	switch {
	case errors.Is(err, async.ErrStop):
		raise("Async::Stop", "task was stopped")
	case errors.Is(err, async.ErrTimeout):
		raise("Async::TimeoutError", "execution expired")
	}
	raise("RuntimeError", "%s", err.Error())
}

// asyncResultValue maps a task/queue/condition value (stored as any, always a
// Ruby object.Value) back into the object graph, nil-safe.
func asyncResultValue(v any) object.Value {
	if ov, ok := v.(object.Value); ok && ov != nil {
		return ov
	}
	return object.NilV
}

// asyncDuration converts a Ruby number of seconds into a time.Duration.
func asyncDuration(v object.Value) time.Duration {
	switch n := v.(type) {
	case object.Integer:
		return time.Duration(int64(n)) * time.Second
	case object.Float:
		return time.Duration(float64(n) * float64(time.Second))
	}
	return 0
}

// asyncInt coerces a Ruby number to an int, defaulting when absent.
func asyncInt(args []object.Value, def int) int {
	if len(args) == 0 {
		return def
	}
	switch n := args[0].(type) {
	case object.Integer:
		return int(int64(n))
	case object.Float:
		return int(float64(n))
	}
	return def
}

// asyncSMethod installs a class ("singleton") method on a class.
func asyncSMethod(cls *RClass, name string, fn NativeFn) {
	cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
}

// registerAsyncTask installs the Async::Task surface (self = AsyncTask).
func (vm *VM) registerAsyncTask(cls *RClass) {
	self := func(v object.Value) *async.Task { return v.(*AsyncTask).t }

	// Async::Task.current — the task whose body is currently running (nil if none).
	asyncSMethod(cls, "current", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.asyncWrap(vm.curAsyncTask)
	})

	cls.define("async", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "Async::Task#async requires a block")
		}
		return vm.asyncWrap(self(v).Async(vm.asyncBody(blk)))
	})
	cls.define("wait", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		var res any
		var err error
		vm.asyncBlocking(func(caller *async.Task) { res, err = self(v).Wait(caller) })
		vm.asyncRaise(err)
		return asyncResultValue(res)
	})
	cls.define("stop", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).Stop()
		return v
	})
	cls.define("sleep", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		d := time.Duration(0)
		if len(args) > 0 {
			d = asyncDuration(args[0])
		}
		vm.asyncBlocking(func(_ *async.Task) { self(v).Sleep(d) })
		return object.NilV
	})
	cls.define("yield", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.asyncBlocking(func(_ *async.Task) { self(v).Yield() })
		return object.NilV
	})
	cls.define("with_timeout", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if blk == nil {
			raise("ArgumentError", "Async::Task#with_timeout requires a block")
		}
		d := asyncDuration(args[0])
		var res any
		var err error
		vm.asyncBlocking(func(_ *async.Task) { res, err = self(v).WithTimeout(d, vm.asyncBody(blk)) })
		vm.asyncRaise(err)
		return asyncResultValue(res)
	})
	cls.define("result", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return asyncResultValue(self(v).Result())
	})
	cls.define("state", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(string(self(v).State()))
	})
	cls.define("running?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).RunningQ())
	})
	completed := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).CompleteQ())
	}
	cls.define("complete?", completed)
	cls.define("completed?", completed)
	cls.define("stopped?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).StoppedQ())
	})
	cls.define("failed?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).FailedQ())
	})
	cls.define("parent", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.asyncWrap(self(v).Parent())
	})
	cls.define("children", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		cs := self(v).Children()
		out := make([]object.Value, len(cs))
		for i, c := range cs {
			out[i] = vm.asyncWrap(c)
		}
		return object.NewArrayFromSlice(out)
	})
}

// registerAsyncBarrier installs Async::Barrier (self = AsyncBarrier).
func (vm *VM) registerAsyncBarrier(cls *RClass) {
	asyncSMethod(cls, "new", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &AsyncBarrier{b: async.NewBarrier(), cls: cls}
	})
	self := func(v object.Value) *async.Barrier { return v.(*AsyncBarrier).b }

	cls.define("async", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "Async::Barrier#async requires a block")
		}
		var child *async.Task
		vm.asyncBlocking(func(caller *async.Task) { child = self(v).Async(caller, vm.asyncBody(blk)) })
		return vm.asyncWrap(child)
	})
	cls.define("wait", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		var err error
		vm.asyncBlocking(func(caller *async.Task) { err = self(v).Wait(caller) })
		vm.asyncRaise(err)
		return object.NilV
	})
	cls.define("stop", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).Stop()
		return v
	})
	cls.define("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Size()))
	})
	cls.define("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Empty())
	})
}

// registerAsyncSemaphore installs Async::Semaphore (self = AsyncSemaphore).
func (vm *VM) registerAsyncSemaphore(cls *RClass) {
	asyncSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return &AsyncSemaphore{s: async.NewSemaphore(asyncInt(args, 1)), cls: cls}
	})
	self := func(v object.Value) *async.Semaphore { return v.(*AsyncSemaphore).s }

	cls.define("acquire", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk != nil {
			var res any
			vm.asyncBlocking(func(caller *async.Task) {
				res, _ = self(v).AcquireDo(caller, func() (any, error) {
					return vm.callBlock(blk, nil), nil
				})
			})
			return asyncResultValue(res)
		}
		vm.asyncBlocking(func(caller *async.Task) { self(v).Acquire(caller) })
		return object.NilV
	})
	cls.define("release", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).Release()
		return object.NilV
	})
	cls.define("count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Count()))
	})
	cls.define("limit", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Limit()))
	})
	cls.define("limit=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		self(v).SetLimit(asyncInt(args, 1))
		return args[0]
	})
	cls.define("blocking?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Blocking())
	})
	cls.define("waiting", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Waiting()))
	})
}

// registerAsyncCondition installs Async::Condition (self = AsyncCondition).
func (vm *VM) registerAsyncCondition(cls *RClass) {
	asyncSMethod(cls, "new", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &AsyncCondition{c: async.NewCondition(), cls: cls}
	})
	self := func(v object.Value) *async.Condition { return v.(*AsyncCondition).c }

	cls.define("wait", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		var val any
		vm.asyncBlocking(func(caller *async.Task) { val = self(v).Wait(caller) })
		return asyncResultValue(val)
	})
	cls.define("signal", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		var val any
		if len(args) > 0 {
			val = args[0]
		}
		self(v).Signal(val)
		return object.NilV
	})
	cls.define("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Empty())
	})
	cls.define("wait_count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).WaitCount()))
	})
}

// registerAsyncNotification installs Async::Notification (self = AsyncNotification).
func (vm *VM) registerAsyncNotification(cls *RClass) {
	asyncSMethod(cls, "new", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &AsyncNotification{n: async.NewNotification(), cls: cls}
	})
	self := func(v object.Value) *async.Notification { return v.(*AsyncNotification).n }

	cls.define("wait", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.asyncBlocking(func(caller *async.Task) { self(v).Wait(caller) })
		return object.NilV
	})
	cls.define("signal", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).Signal()
		return object.NilV
	})
	cls.define("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).WaitCount() == 0)
	})
	cls.define("wait_count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).WaitCount()))
	})
}

// registerAsyncQueue installs Async::Queue (self = AsyncQueue).
func (vm *VM) registerAsyncQueue(cls *RClass) {
	asyncSMethod(cls, "new", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &AsyncQueue{q: async.NewQueue(), cls: cls}
	})
	self := func(v object.Value) *async.Queue { return v.(*AsyncQueue).q }

	enqueue := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		self(v).Enqueue(args[0])
		return v
	}
	cls.define("enqueue", enqueue)
	cls.define("push", enqueue)
	cls.define("<<", enqueue)

	dequeue := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		var item any
		vm.asyncBlocking(func(caller *async.Task) { item = self(v).Dequeue(caller) })
		return asyncResultValue(item)
	}
	cls.define("dequeue", dequeue)
	cls.define("pop", dequeue)

	cls.define("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Empty())
	})
	cls.define("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Size()))
	})
}

// registerAsyncLimitedQueue installs Async::LimitedQueue (self = AsyncLimitedQueue).
func (vm *VM) registerAsyncLimitedQueue(cls *RClass) {
	asyncSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return &AsyncLimitedQueue{q: async.NewLimitedQueue(asyncInt(args, 1)), cls: cls}
	})
	self := func(v object.Value) *async.LimitedQueue { return v.(*AsyncLimitedQueue).q }

	enqueue := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		vm.asyncBlocking(func(caller *async.Task) { self(v).Enqueue(caller, args[0]) })
		return v
	}
	cls.define("enqueue", enqueue)
	cls.define("push", enqueue)
	cls.define("<<", enqueue)

	dequeue := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		var item any
		vm.asyncBlocking(func(caller *async.Task) { item = self(v).Dequeue(caller) })
		return asyncResultValue(item)
	}
	cls.define("dequeue", dequeue)
	cls.define("pop", dequeue)

	cls.define("limit", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Limit()))
	})
	cls.define("limited?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).LimitedQ())
	})
	cls.define("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Empty())
	})
	cls.define("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Size()))
	})
}

// registerAsyncWaiter installs Async::Waiter (self = AsyncWaiter). Waiter.new
// spawns its tasks as children of the current task (or an explicit parent task).
func (vm *VM) registerAsyncWaiter(cls *RClass) {
	asyncSMethod(cls, "new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		parent := vm.asyncCaller()
		if len(args) > 0 {
			if at, ok := args[0].(*AsyncTask); ok {
				parent = at.t
			}
		}
		return &AsyncWaiter{w: async.NewWaiter(parent), cls: cls}
	})
	self := func(v object.Value) *async.Waiter { return v.(*AsyncWaiter).w }

	cls.define("async", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "Async::Waiter#async requires a block")
		}
		return vm.asyncWrap(self(v).Async(vm.asyncBody(blk)))
	})
	cls.define("wait", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		count := asyncInt(args, -1)
		var results []any
		var err error
		vm.asyncBlocking(func(caller *async.Task) { results, err = self(v).Wait(caller, count) })
		vm.asyncRaise(err)
		out := make([]object.Value, len(results))
		for i, r := range results {
			out[i] = asyncResultValue(r)
		}
		return object.NewArrayFromSlice(out)
	})
	cls.define("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Size()))
	})
}
