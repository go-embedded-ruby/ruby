// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	concurrent "github.com/go-ruby-concurrent-ruby/concurrent-ruby"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the value-conversion and callback seam between rbgo's Ruby object
// graph and the interpreter-independent
// github.com/go-ruby-concurrent-ruby/concurrent-ruby library (see
// concurrentruby.go for the class/method registration). The library owns the
// atomics, the thread-safe Map, the Future/Promise state machines and the
// synchronization primitives, and expresses every point that takes a Ruby block
// as a Go func parameter and every asynchronous primitive as taking an Executor.
//
// Threading model — how Ruby callbacks stay VM-safe with no leaked goroutines:
// rbgo runs bytecode under an emulated GVL, one Ruby thread at a time (see
// thread.go). A Ruby block handed to Concurrent::Future.execute /
// ThreadPoolExecutor#post / Promise#then / Map#compute_if_absent is only ever
// invoked from Ruby code that is *itself* running on the VM goroutine holding the
// GVL. We therefore run every such block through vmExecutor, an Executor whose
// Post executes the task inline on that same goroutine (concurrent-ruby's
// ImmediateExecutor discipline). The block is thereby serialized onto the
// single-threaded VM — the same guarantee puma/grpc obtain by *acquiring* the GVL
// from a worker goroutine — but achieved here without spawning any worker
// goroutine at all, so nothing is ever leaked and resolution is fully
// deterministic (a Future is already settled when #execute returns). Because the
// call is already on the GVL-holding goroutine, it must NOT re-lock vm.gvl (the
// mutex is not reentrant); the puma/grpc save/restore-context dance is
// unnecessary precisely because control never left the VM goroutine.

// vmExecutor is the concurrent.Executor the binding threads through every
// asynchronous primitive. Post runs the task inline on the VM goroutine (under
// the GVL the caller already holds), so a Ruby block posted to a Future / thread
// pool / promise continuation is executed serialized onto the single-threaded VM
// with no worker goroutine — deterministic and leak-free.
type vmExecutor struct{ vm *VM }

// Post runs task inline and always accepts it (ImmediateExecutor semantics).
func (vmExecutor) Post(task func()) bool {
	task()
	return true
}

// rubyRejection is the Go error a rejected Future/Promise carries: it wraps the
// Ruby exception object a block raised (or a Ruby value passed to Promise#reject),
// so #reason can hand the original object back and #value! can re-raise it.
type rubyRejection struct{ obj object.Value }

func (e *rubyRejection) Error() string { return e.obj.ToS() }

// runConcurrentBlock invokes a Ruby block on the VM (inline, on the VM goroutine
// under the GVL) and reports either its value or the exception it raised. A Ruby
// `raise` inside the block is recovered and returned as the exception object, so
// a Future/Promise becomes :rejected rather than unwinding the whole VM; any
// non-Ruby Go panic propagates unchanged.
func (vm *VM) runConcurrentBlock(blk *Proc, args []object.Value) (val object.Value, exc object.Value) {
	defer func() {
		if r := recover(); r != nil {
			re, ok := r.(RubyError)
			if !ok {
				panic(r) // a real Go panic, not a Ruby raise: propagate.
			}
			val, exc = object.NilV, vm.exceptionObject(re)
		}
	}()
	return vm.callBlock(blk, args), object.NilV
}

// concurrentTaskFn wraps a Ruby block as the (value, error) Go func a Future
// body / Promise continuation expects: a normal return fulfils it, a Ruby raise
// rejects it with the raised exception object.
func (vm *VM) concurrentTaskFn(blk *Proc, args ...object.Value) func() (any, error) {
	return func() (any, error) {
		val, exc := vm.runConcurrentBlock(blk, args)
		if !object.IsNil(exc) {
			return object.NilV, &rubyRejection{obj: exc}
		}
		return val, nil
	}
}

// concurrentReason maps the Go error a rejected primitive carries back to the
// Ruby exception object #reason returns: a rubyRejection yields the original
// object, a library sentinel (e.g. a broken barrier) yields a Concurrent error,
// and nil (not rejected) yields nil.
func (vm *VM) concurrentReason(err error) object.Value {
	if err == nil {
		return object.NilV
	}
	if rr, ok := err.(*rubyRejection); ok {
		return rr.obj
	}
	return vm.exceptionObject(RubyError{Class: "Concurrent::Error", Message: err.Error()})
}

// concurrentReRaise re-raises a rejected primitive's reason as a Ruby exception,
// preserving the original object identity when the rejection carried one (so
// #value! surfaces exactly the exception the block raised).
func (vm *VM) concurrentReRaise(err error) {
	if rr, ok := err.(*rubyRejection); ok {
		panic(vm.excError(rr.obj))
	}
	raise("Concurrent::Error", "%s", err.Error())
}

// concurrentState maps a library State to the Ruby lifecycle Symbol #state
// returns (:pending / :fulfilled / :rejected).
func concurrentState(s concurrent.State) object.Value { return object.Symbol(string(s)) }

// concurrentBack converts a library result (always an object.Value the binding
// stored, or nil for an unset/rejected slot) back to a Ruby value.
func concurrentBack(v any) object.Value {
	if ov, ok := v.(object.Value); ok {
		return ov
	}
	return object.NilV
}

// ---- value wrappers ---------------------------------------------------------

// ConcurrentAtomicRef wraps a Concurrent::AtomicReference.
type ConcurrentAtomicRef struct {
	r   *concurrent.AtomicReference
	cls *RClass
}

func (o *ConcurrentAtomicRef) ToS() string     { return "#<Concurrent::AtomicReference>" }
func (o *ConcurrentAtomicRef) Inspect() string { return o.ToS() }
func (o *ConcurrentAtomicRef) Truthy() bool    { return true }

// ConcurrentAtomicFixnum wraps a Concurrent::AtomicFixnum.
type ConcurrentAtomicFixnum struct {
	f   *concurrent.AtomicFixnum
	cls *RClass
}

func (o *ConcurrentAtomicFixnum) ToS() string     { return "#<Concurrent::AtomicFixnum>" }
func (o *ConcurrentAtomicFixnum) Inspect() string { return o.ToS() }
func (o *ConcurrentAtomicFixnum) Truthy() bool    { return true }

// ConcurrentAtomicBool wraps a Concurrent::AtomicBoolean.
type ConcurrentAtomicBool struct {
	b   *concurrent.AtomicBoolean
	cls *RClass
}

func (o *ConcurrentAtomicBool) ToS() string     { return "#<Concurrent::AtomicBoolean>" }
func (o *ConcurrentAtomicBool) Inspect() string { return o.ToS() }
func (o *ConcurrentAtomicBool) Truthy() bool    { return true }

// cmapEntry is what a ConcurrentMap stores as each value: the original Ruby key
// (so #keys / #each_pair recover it, since the library keys by a canonical Go
// key) alongside the Ruby value.
type cmapEntry struct{ key, val object.Value }

// ConcurrentMap wraps a Concurrent::Map. Keys are canonicalized through setKey
// (the very hash/eql? mapping the Set binding uses), so two equal Ruby strings
// address the same slot; the original key is recovered from the stored entry.
type ConcurrentMap struct {
	m   *concurrent.Map
	cls *RClass
	vm  *VM
}

func (o *ConcurrentMap) ToS() string     { return "#<Concurrent::Map>" }
func (o *ConcurrentMap) Inspect() string { return o.ToS() }
func (o *ConcurrentMap) Truthy() bool    { return true }

// ConcurrentFuture wraps a Concurrent::Future.
type ConcurrentFuture struct {
	f   *concurrent.Future
	cls *RClass
	vm  *VM
}

func (o *ConcurrentFuture) ToS() string     { return "#<Concurrent::Future>" }
func (o *ConcurrentFuture) Inspect() string { return o.ToS() }
func (o *ConcurrentFuture) Truthy() bool    { return true }

// ConcurrentPromise wraps a Concurrent::Promise.
type ConcurrentPromise struct {
	p   *concurrent.Promise
	cls *RClass
	vm  *VM
}

func (o *ConcurrentPromise) ToS() string     { return "#<Concurrent::Promise>" }
func (o *ConcurrentPromise) Inspect() string { return o.ToS() }
func (o *ConcurrentPromise) Truthy() bool    { return true }

// ConcurrentPool wraps a Concurrent::FixedThreadPool / ThreadPoolExecutor. Posted
// blocks run inline through the vmExecutor seam (serialized on the VM goroutine),
// so the pool spawns no worker goroutine; it tracks the shutdown flag and the
// completed-task count itself.
type ConcurrentPool struct {
	exec      concurrent.Executor
	cls       *RClass
	vm        *VM
	minLen    int
	maxLen    int
	shutdown  bool
	completed int64
}

func (o *ConcurrentPool) ToS() string     { return "#<Concurrent::ThreadPoolExecutor>" }
func (o *ConcurrentPool) Inspect() string { return o.ToS() }
func (o *ConcurrentPool) Truthy() bool    { return true }

// ConcurrentLatch wraps a Concurrent::CountDownLatch.
type ConcurrentLatch struct {
	l   *concurrent.CountDownLatch
	cls *RClass
}

func (o *ConcurrentLatch) ToS() string     { return "#<Concurrent::CountDownLatch>" }
func (o *ConcurrentLatch) Inspect() string { return o.ToS() }
func (o *ConcurrentLatch) Truthy() bool    { return true }

// ConcurrentSemaphore wraps a Concurrent::Semaphore.
type ConcurrentSemaphore struct {
	s   *concurrent.Semaphore
	cls *RClass
}

func (o *ConcurrentSemaphore) ToS() string     { return "#<Concurrent::Semaphore>" }
func (o *ConcurrentSemaphore) Inspect() string { return o.ToS() }
func (o *ConcurrentSemaphore) Truthy() bool    { return true }

// ConcurrentBarrier wraps a Concurrent::CyclicBarrier.
type ConcurrentBarrier struct {
	b   *concurrent.CyclicBarrier
	cls *RClass
}

func (o *ConcurrentBarrier) ToS() string     { return "#<Concurrent::CyclicBarrier>" }
func (o *ConcurrentBarrier) Inspect() string { return o.ToS() }
func (o *ConcurrentBarrier) Truthy() bool    { return true }

// ---- argument coercions -----------------------------------------------------

// concurrentInt coerces a Ruby Integer argument to an int, raising TypeError for
// a non-Integer (a count / permit / delta must be an Integer).
func concurrentInt(v object.Value) int {
	switch n := v.(type) {
	case object.Integer:
		return int(n)
	case *object.Bignum:
		return int(n.I.Int64())
	}
	raise("TypeError", "expected an Integer, got %s", v.Inspect())
	panic("unreachable")
}

// concurrentIntArg returns the i-th argument as an int, or def when it was
// omitted (an optional delta / permit count).
func concurrentIntArg(args []object.Value, i, def int) int {
	if i >= len(args) {
		return def
	}
	return concurrentInt(args[i])
}

// concurrentTimeout maps an optional Ruby timeout argument (nil / omitted =>
// wait forever, a numeric seconds value => that duration) to a Go duration.
func concurrentTimeout(args []object.Value, i int) time.Duration {
	if i >= len(args) || object.IsNil(args[i]) {
		return concurrent.NoTimeout
	}
	switch n := args[i].(type) {
	case object.Integer:
		return time.Duration(float64(n) * float64(time.Second))
	case object.Float:
		return time.Duration(float64(n) * float64(time.Second))
	}
	raise("TypeError", "expected a numeric timeout, got %s", args[i].Inspect())
	panic("unreachable")
}

// concurrentBlock returns the block a method requires, raising ArgumentError (as
// the gem does: "no block given") when it was omitted.
func concurrentBlock(blk *Proc) *Proc {
	if blk == nil {
		raise("ArgumentError", "no block given")
	}
	return blk
}

// concurrentRejectErr converts a Ruby argument passed to Promise#reject into the
// Go error the promise stores: an exception object is carried as-is, and any
// other value (typically a String) becomes a RuntimeError-flavoured rejection.
func (vm *VM) concurrentRejectErr(v object.Value) error {
	if s, ok := v.(*object.String); ok {
		return &rubyRejection{obj: vm.exceptionObject(RubyError{Class: "RuntimeError", Message: s.Str()})}
	}
	return &rubyRejection{obj: v}
}
