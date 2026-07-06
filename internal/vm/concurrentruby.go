// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	concurrent "github.com/go-ruby-concurrent-ruby/concurrent-ruby"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerConcurrentRuby installs the core of Ruby's concurrent-ruby gem onto the
// already-created Concurrent module (see concurrent.go), backed by the pure-Go
// (CGO=0) github.com/go-ruby-concurrent-ruby/concurrent-ruby library: the atomics
// (AtomicReference / AtomicFixnum / AtomicBoolean), the thread-safe
// Concurrent::Map, the Future / Promise asynchronous values, the
// FixedThreadPool / ThreadPoolExecutor, and the CountDownLatch / Semaphore /
// CyclicBarrier synchronization primitives, plus the Concurrent error tree.
//
// Every Ruby block a primitive takes (a Future body, a Promise continuation, a
// Map#compute_if_absent computation, a barrier action) is threaded to the library
// as a Go func and run through vmExecutor — inline on the VM goroutine under the
// emulated GVL — so it is serialized onto the single-threaded VM with no worker
// goroutine and fully deterministic resolution (see concurrentruby_bind.go for
// the seam and threading rationale).
func (vm *VM) registerConcurrentRuby(mod *RClass) {
	// mk registers a class both scoped under Concurrent and flat in vm.consts (by
	// its qualified name), so raise / const lookup find it either way.
	mk := func(name string, super *RClass) *RClass {
		full := "Concurrent::" + name
		cls := newClass(full, super)
		mod.consts[name] = cls
		vm.consts[full] = cls
		return cls
	}

	vm.registerConcurrentErrors(mod, mk)
	vm.registerConcurrentAtomics(mk)
	vm.registerConcurrentMap(mk("Map", vm.cObject))
	vm.registerConcurrentFuture(mk("Future", vm.cObject))
	vm.registerConcurrentPromise(mk("Promise", vm.cObject))
	vm.registerConcurrentPools(mk)
	vm.registerConcurrentSync(mk)
}

// registerConcurrentErrors installs the Concurrent error tree: Concurrent::Error
// < StandardError, and MultipleAssignmentError / RejectedExecutionError /
// TimeoutError under it (the classes the library's sentinels map to).
func (vm *VM) registerConcurrentErrors(_ *RClass, mk func(string, *RClass) *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := mk("Error", std)
	mk("MultipleAssignmentError", base)
	mk("RejectedExecutionError", base)
	mk("TimeoutError", base)
}

// smeth installs a class ("singleton") method.
func csMethod(cls *RClass, name string, fn NativeFn) {
	cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
}

// ---- atomics ----------------------------------------------------------------

func (vm *VM) registerConcurrentAtomics(mk func(string, *RClass) *RClass) {
	vm.registerAtomicReference(mk("AtomicReference", vm.cObject))
	vm.registerAtomicFixnum(mk("AtomicFixnum", vm.cObject))
	vm.registerAtomicBoolean(mk("AtomicBoolean", vm.cObject))
}

func (vm *VM) registerAtomicReference(cls *RClass) {
	csMethod(cls, "new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var init object.Value = object.NilV
		if len(args) > 0 {
			init = args[0]
		}
		return &ConcurrentAtomicRef{r: concurrent.NewAtomicReference(init), cls: cls}
	})
	self := func(v object.Value) *ConcurrentAtomicRef { return v.(*ConcurrentAtomicRef) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("get", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return concurrentBack(self(v).r.Get())
	})
	cls.methods["value"] = cls.methods["get"]
	set := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).r.Set(args[0])
		return args[0]
	}
	d("set", set)
	d("value=", set)
	d("get_and_set", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return concurrentBack(self(v).r.GetAndSet(args[0]))
	})
	cls.methods["swap"] = cls.methods["get_and_set"]
	d("compare_and_set", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).r.CompareAndSet(args[0], args[1]))
	})
	cls.methods["compare_and_swap"] = cls.methods["compare_and_set"]
	d("update", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		blk = concurrentBlock(blk)
		return concurrentBack(self(v).r.Update(func(old any) any {
			return vm.callBlock(blk, []object.Value{concurrentBack(old)})
		}))
	})
}

func (vm *VM) registerAtomicFixnum(cls *RClass) {
	csMethod(cls, "new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return &ConcurrentAtomicFixnum{f: concurrent.NewAtomicFixnum(int64(concurrentIntArg(args, 0, 0))), cls: cls}
	})
	self := func(v object.Value) *ConcurrentAtomicFixnum { return v.(*ConcurrentAtomicFixnum) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("value", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).f.Value())
	})
	d("value=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).f.SetValue(int64(concurrentInt(args[0])))
		return args[0]
	})
	d("increment", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).f.Increment(int64(concurrentIntArg(args, 0, 1))))
	})
	cls.methods["up"] = cls.methods["increment"]
	d("decrement", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).f.Decrement(int64(concurrentIntArg(args, 0, 1))))
	})
	cls.methods["down"] = cls.methods["decrement"]
	d("compare_and_set", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).f.CompareAndSet(int64(concurrentInt(args[0])), int64(concurrentInt(args[1]))))
	})
	d("update", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		blk = concurrentBlock(blk)
		return object.IntValue(self(v).f.Update(func(old int64) int64 {
			return int64(concurrentInt(vm.callBlock(blk, []object.Value{object.IntValue(old)})))
		}))
	})
}

func (vm *VM) registerAtomicBoolean(cls *RClass) {
	csMethod(cls, "new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		init := len(args) > 0 && args[0].Truthy()
		return &ConcurrentAtomicBool{b: concurrent.NewAtomicBoolean(init), cls: cls}
	})
	self := func(v object.Value) *ConcurrentAtomicBool { return v.(*ConcurrentAtomicBool) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("value", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).b.Value())
	})
	d("value=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).b.SetValue(args[0].Truthy())
		return args[0]
	})
	d("true?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).b.TrueQ())
	})
	d("false?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).b.FalseQ())
	})
	d("make_true", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).b.MakeTrue())
	})
	d("make_false", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).b.MakeFalse())
	})
	d("compare_and_set", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).b.CompareAndSet(args[0].Truthy(), args[1].Truthy()))
	})
}

// ---- Concurrent::Map --------------------------------------------------------

func (vm *VM) registerConcurrentMap(cls *RClass) {
	csMethod(cls, "new", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ConcurrentMap{m: concurrent.NewMap(), cls: cls, vm: vm}
	})
	self := func(v object.Value) *ConcurrentMap { return v.(*ConcurrentMap) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		e, ok := self(v).m.GetPair(setKey(args[0]))
		if !ok {
			return object.NilV
		}
		return e.(cmapEntry).val
	})
	d("[]=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).m.Set(setKey(args[0]), cmapEntry{key: args[0], val: args[1]})
		return args[1]
	})
	cls.methods["store"] = cls.methods["[]="]
	d("compute_if_absent", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		blk = concurrentBlock(blk)
		e := self(v).m.ComputeIfAbsent(setKey(args[0]), func() any {
			return cmapEntry{key: args[0], val: vm.callBlock(blk, nil)}
		})
		return e.(cmapEntry).val
	})
	d("put_if_absent", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		prev := self(v).m.PutIfAbsent(setKey(args[0]), cmapEntry{key: args[0], val: args[1]})
		if prev == nil {
			return object.NilV
		}
		return prev.(cmapEntry).val
	})
	d("delete", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		prev := self(v).m.Delete(setKey(args[0]))
		if prev == nil {
			return object.NilV
		}
		return prev.(cmapEntry).val
	})
	d("key?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).m.KeyQ(setKey(args[0])))
	})
	cls.methods["has_key?"] = cls.methods["key?"]
	d("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).m.Size()))
	})
	cls.methods["length"] = cls.methods["size"]
	d("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).m.Empty())
	})
	d("clear", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).m.Clear()
		return v
	})
	d("keys", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		vals := self(v).m.Values()
		out := make([]object.Value, len(vals))
		for i, e := range vals {
			out[i] = e.(cmapEntry).key
		}
		return object.NewArrayFromSlice(out)
	})
	d("values", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		vals := self(v).m.Values()
		out := make([]object.Value, len(vals))
		for i, e := range vals {
			out[i] = e.(cmapEntry).val
		}
		return object.NewArrayFromSlice(out)
	})
	d("each_pair", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		blk = concurrentBlock(blk)
		_ = self(v).m.EachPair(func(_, value any) error {
			e := value.(cmapEntry)
			vm.callBlock(blk, []object.Value{e.key, e.val})
			return nil
		})
		return v
	})
	cls.methods["each"] = cls.methods["each_pair"]
}

// ---- Concurrent::Future -----------------------------------------------------

func (vm *VM) registerConcurrentFuture(cls *RClass) {
	// Concurrent::Future.execute { block } posts the block to the vmExecutor,
	// which runs it inline on the VM goroutine, so the returned Future is already
	// settled (fulfilled with the block's value, or rejected with its raise).
	csMethod(cls, "execute", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		blk = concurrentBlock(blk)
		f := concurrent.FutureExecute(vmExecutor{vm}, vm.concurrentTaskFn(blk))
		return &ConcurrentFuture{f: f, cls: cls, vm: vm}
	})
	self := func(v object.Value) *ConcurrentFuture { return v.(*ConcurrentFuture) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("value", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return concurrentBack(self(v).f.Value(concurrentTimeout(args, 0)))
	})
	d("value!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		res, err := self(v).f.ValueBang(concurrentTimeout(args, 0))
		if err != nil {
			vm.concurrentReRaise(err)
		}
		return concurrentBack(res)
	})
	d("wait", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).f.Wait(concurrentTimeout(args, 0))
		return v
	})
	d("state", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return concurrentState(self(v).f.State())
	})
	d("reason", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.concurrentReason(self(v).f.Reason())
	})
	d("pending?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).f.PendingQ())
	})
	d("fulfilled?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).f.FulfilledQ())
	})
	cls.methods["realized?"] = cls.methods["fulfilled?"]
	d("rejected?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).f.RejectedQ())
	})
	d("complete?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).f.CompleteQ())
	})
}

// ---- Concurrent::Promise ----------------------------------------------------

func (vm *VM) registerConcurrentPromise(cls *RClass) {
	csMethod(cls, "new", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ConcurrentPromise{p: concurrent.NewPromise(), cls: cls, vm: vm}
	})
	self := func(v object.Value) *ConcurrentPromise { return v.(*ConcurrentPromise) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("fulfill", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self(v).p.Fulfill(concurrentPromiseVal(args)); err != nil {
			raise("Concurrent::MultipleAssignmentError", "%s", err.Error())
		}
		return v
	})
	d("reject", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self(v).p.Reject(vm.concurrentRejectErr(concurrentPromiseVal(args))); err != nil {
			raise("Concurrent::MultipleAssignmentError", "%s", err.Error())
		}
		return v
	})
	d("then", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		blk = concurrentBlock(blk)
		next := self(v).p.Then(func(val any) (any, error) {
			r, exc := vm.runConcurrentBlock(blk, []object.Value{concurrentBack(val)})
			if !object.IsNil(exc) {
				return object.NilV, &rubyRejection{obj: exc}
			}
			return r, nil
		})
		return &ConcurrentPromise{p: next, cls: cls, vm: vm}
	})
	d("value", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return concurrentBack(self(v).p.Value(concurrentTimeout(args, 0)))
	})
	d("reason", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.concurrentReason(self(v).p.Reason())
	})
	d("state", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return concurrentState(self(v).p.State())
	})
	d("fulfilled?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).p.State() == concurrent.Fulfilled)
	})
	d("rejected?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).p.State() == concurrent.Rejected)
	})
	d("pending?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).p.State() == concurrent.Pending)
	})
}

// concurrentPromiseVal returns the value a Promise#fulfill / #reject received, or
// nil when the call passed no argument.
func concurrentPromiseVal(args []object.Value) object.Value {
	if len(args) == 0 {
		return object.NilV
	}
	return args[0]
}

// ---- thread pools -----------------------------------------------------------

func (vm *VM) registerConcurrentPools(mk func(string, *RClass) *RClass) {
	fixed := mk("FixedThreadPool", vm.cObject)
	pool := mk("ThreadPoolExecutor", vm.cObject)

	csMethod(fixed, "new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		n := concurrentIntArg(args, 0, 1)
		return &ConcurrentPool{exec: vmExecutor{vm}, cls: fixed, vm: vm, minLen: n, maxLen: n}
	})
	csMethod(pool, "new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		minT, maxT := concurrentPoolOpts(args)
		return &ConcurrentPool{exec: vmExecutor{vm}, cls: pool, vm: vm, minLen: minT, maxLen: maxT}
	})

	for _, cls := range []*RClass{fixed, pool} {
		vm.definePoolMethods(cls)
	}
}

func (vm *VM) definePoolMethods(cls *RClass) {
	self := func(v object.Value) *ConcurrentPool { return v.(*ConcurrentPool) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// #post { block } runs the block through the executor seam — inline on the VM
	// goroutine — counting the completion; a shut-down pool rejects the task
	// (returns false), matching the gem's :abort fallback policy. A task's raise
	// is swallowed (the gem discards an executor task's exception).
	d("post", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		blk = concurrentBlock(blk)
		p := self(v)
		if p.shutdown {
			return object.False
		}
		p.exec.Post(func() {
			_, _ = vm.runConcurrentBlock(blk, nil)
			p.completed++
		})
		return object.True
	})
	d("shutdown", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).shutdown = true
		return v
	})
	cls.methods["kill"] = cls.methods["shutdown"]
	d("wait_for_termination", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		// Tasks run inline on #post, so a shut-down pool is already terminated.
		return object.Bool(self(v).shutdown)
	})
	d("running?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(!self(v).shutdown)
	})
	d("shutdown?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).shutdown)
	})
	d("queue_length", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		_ = self(v)
		return object.IntValue(0)
	})
	d("completed_task_count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(self(v).completed)
	})
	d("length", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).minLen))
	})
	d("max_length", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).maxLen))
	})
}

// concurrentPoolOpts reads ThreadPoolExecutor.new's option Hash
// ({min_threads:, max_threads:}), defaulting both to 1.
func concurrentPoolOpts(args []object.Value) (minT, maxT int) {
	minT, maxT = 1, 1
	if len(args) == 0 {
		return
	}
	h, ok := args[0].(*object.Hash)
	if !ok {
		return
	}
	if v, ok := h.Get(object.Symbol("min_threads")); ok {
		minT = concurrentInt(v)
	}
	if v, ok := h.Get(object.Symbol("max_threads")); ok {
		maxT = concurrentInt(v)
	}
	return
}

// ---- synchronization primitives ---------------------------------------------

func (vm *VM) registerConcurrentSync(mk func(string, *RClass) *RClass) {
	vm.registerCountDownLatch(mk("CountDownLatch", vm.cObject))
	vm.registerSemaphore(mk("Semaphore", vm.cObject))
	vm.registerCyclicBarrier(mk("CyclicBarrier", vm.cObject))
}

func (vm *VM) registerCountDownLatch(cls *RClass) {
	csMethod(cls, "new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return &ConcurrentLatch{l: concurrent.NewCountDownLatch(concurrentIntArg(args, 0, 1)), cls: cls}
	})
	self := func(v object.Value) *ConcurrentLatch { return v.(*ConcurrentLatch) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).l.Count()))
	})
	d("count_down", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).l.CountDown()
		return v
	})
	d("wait", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).l.Wait(concurrentTimeout(args, 0)))
	})
}

func (vm *VM) registerSemaphore(cls *RClass) {
	csMethod(cls, "new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return &ConcurrentSemaphore{s: concurrent.NewSemaphore(concurrentIntArg(args, 0, 0)), cls: cls}
	})
	self := func(v object.Value) *ConcurrentSemaphore { return v.(*ConcurrentSemaphore) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("acquire", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).s.Acquire(concurrentIntArg(args, 0, 1))
		return object.NilV
	})
	d("release", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).s.Release(concurrentIntArg(args, 0, 1))
		return object.NilV
	})
	d("try_acquire", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).s.TryAcquire(concurrentIntArg(args, 0, 1)))
	})
	d("available_permits", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).s.AvailablePermits()))
	})
	d("drain_permits", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).s.Drain()))
	})
	d("reduce_permits", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).s.ReducePermits(concurrentInt(args[0]))
		return object.NilV
	})
}

func (vm *VM) registerCyclicBarrier(cls *RClass) {
	csMethod(cls, "new", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		parties := concurrentIntArg(args, 0, 1)
		if blk != nil {
			return &ConcurrentBarrier{b: concurrent.NewCyclicBarrierWithAction(parties, func() {
				_, _ = vm.runConcurrentBlock(blk, nil)
			}), cls: cls}
		}
		return &ConcurrentBarrier{b: concurrent.NewCyclicBarrier(parties), cls: cls}
	})
	self := func(v object.Value) *ConcurrentBarrier { return v.(*ConcurrentBarrier) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("parties", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).b.Parties()))
	})
	d("number_waiting", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).b.NumberWaiting()))
	})
	d("wait", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).b.Wait(concurrentTimeout(args, 0)))
	})
	d("reset", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).b.Reset()
		return v
	})
	d("broken?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).b.BrokenQ())
	})
}
