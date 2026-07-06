// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	async "github.com/go-ruby-async/async"
)

// This file (with async_bind.go) binds github.com/go-ruby-async/async — a
// pure-Go (CGO=0) model of the structured-concurrency core of Ruby's async gem
// (socketry/async) — into rbgo (require "async"):
//
//	Async do |task|
//	  child = task.async { |t| 41 + 1 }
//	  child.wait                       # => 42
//	  barrier = Async::Barrier.new
//	  3.times { |i| barrier.async { |t| i } }
//	  barrier.wait
//	end
//
// The library owns the whole task tree — spawning, cancellation, failure
// propagation — and the Async:: synchronization primitives (Barrier, Semaphore,
// Condition, Notification, Queue, LimitedQueue, Waiter). Where a fiber suspends
// and is later resumed, async leaves an injectable Scheduler seam; it ships a
// deterministic in-memory CoScheduler (goroutine + channel handoff, exactly one
// fiber runnable at a time — the same strict-handoff model as rbgo's own Fiber,
// see fiber.go), which async.Run uses to drive the reactor. rbgo supplies the
// other seam: a task Body is a Ruby *Proc, run cooperatively on the scheduler's
// fiber (running the block is the rbgo seam; the scheduling is the library).
//
// Because the scheduler runs exactly one fiber at a time with a channel handoff
// between each, running Ruby via callBlock inside a task body is race-free under
// `go test -race`, just as Fiber#resume is. A task body's Ruby exception is
// caught in runAsyncBody and converted to a Go error the library propagates
// through the task tree; async's own cancellation (Async::Stop / TimeoutError)
// is a private panic recovered by the library, so runAsyncBody deliberately
// re-raises anything that is not a plain Ruby exception.

// AsyncTask wraps a *async.Task — one node of the structured-concurrency tree
// (Async::Task). It carries its own class so classOf reports Async::Task.
type AsyncTask struct {
	t   *async.Task
	cls *RClass
}

func (t *AsyncTask) ToS() string     { return "#<Async::Task>" }
func (t *AsyncTask) Inspect() string { return t.ToS() }
func (t *AsyncTask) Truthy() bool    { return true }

// AsyncBarrier wraps a *async.Barrier (Async::Barrier).
type AsyncBarrier struct {
	b   *async.Barrier
	cls *RClass
}

func (b *AsyncBarrier) ToS() string     { return "#<Async::Barrier>" }
func (b *AsyncBarrier) Inspect() string { return b.ToS() }
func (b *AsyncBarrier) Truthy() bool    { return true }

// AsyncSemaphore wraps a *async.Semaphore (Async::Semaphore).
type AsyncSemaphore struct {
	s   *async.Semaphore
	cls *RClass
}

func (s *AsyncSemaphore) ToS() string     { return "#<Async::Semaphore>" }
func (s *AsyncSemaphore) Inspect() string { return s.ToS() }
func (s *AsyncSemaphore) Truthy() bool    { return true }

// AsyncCondition wraps a *async.Condition (Async::Condition).
type AsyncCondition struct {
	c   *async.Condition
	cls *RClass
}

func (c *AsyncCondition) ToS() string     { return "#<Async::Condition>" }
func (c *AsyncCondition) Inspect() string { return c.ToS() }
func (c *AsyncCondition) Truthy() bool    { return true }

// AsyncNotification wraps a *async.Notification (Async::Notification).
type AsyncNotification struct {
	n   *async.Notification
	cls *RClass
}

func (n *AsyncNotification) ToS() string     { return "#<Async::Notification>" }
func (n *AsyncNotification) Inspect() string { return n.ToS() }
func (n *AsyncNotification) Truthy() bool    { return true }

// AsyncQueue wraps a *async.Queue (Async::Queue).
type AsyncQueue struct {
	q   *async.Queue
	cls *RClass
}

func (q *AsyncQueue) ToS() string     { return "#<Async::Queue>" }
func (q *AsyncQueue) Inspect() string { return q.ToS() }
func (q *AsyncQueue) Truthy() bool    { return true }

// AsyncLimitedQueue wraps a *async.LimitedQueue (Async::LimitedQueue).
type AsyncLimitedQueue struct {
	q   *async.LimitedQueue
	cls *RClass
}

func (q *AsyncLimitedQueue) ToS() string     { return "#<Async::LimitedQueue>" }
func (q *AsyncLimitedQueue) Inspect() string { return q.ToS() }
func (q *AsyncLimitedQueue) Truthy() bool    { return true }

// AsyncWaiter wraps a *async.Waiter (Async::Waiter).
type AsyncWaiter struct {
	w   *async.Waiter
	cls *RClass
}

func (w *AsyncWaiter) ToS() string     { return "#<Async::Waiter>" }
func (w *AsyncWaiter) Inspect() string { return w.ToS() }
func (w *AsyncWaiter) Truthy() bool    { return true }

// asyncRubyError carries a Ruby exception raised inside a task body across the
// async library's Go-error task-tree propagation, so it can be re-raised
// verbatim at the boundary that surfaces the failure (Async{} result, #wait, a
// barrier/waiter/semaphore join). A stop/timeout uses async's own sentinel
// errors instead (see asyncRaise).
type asyncRubyError struct{ e RubyError }

func (a *asyncRubyError) Error() string { return a.e.Error() }

// registerAsync installs the Async module and Kernel#Async plus the Async::Task
// node class and the synchronization primitives (require "async"). The method
// surface and the Kernel#Async entry point are wired in async_bind.go.
func (vm *VM) registerAsync() {
	mod := newClass("Async", nil)
	mod.isModule = true
	vm.consts["Async"] = mod

	mk := func(name string, super *RClass) *RClass {
		full := "Async::" + name
		cls := newClass(full, super)
		mod.consts[name] = cls
		vm.consts[full] = cls
		return cls
	}

	// Async::Stop and Async::TimeoutError — the exceptions a stopped / timed-out
	// task surfaces when waited on. StandardError-rooted so `rescue` catches them.
	std := vm.consts["StandardError"].(*RClass)
	stop := mk("Stop", std)
	vm.consts["Async::Stop"] = stop
	mk("TimeoutError", std)

	vm.cAsyncTask = mk("Task", vm.cObject)
	vm.registerAsyncTask(vm.cAsyncTask)
	vm.registerAsyncBarrier(mk("Barrier", vm.cObject))
	vm.registerAsyncSemaphore(mk("Semaphore", vm.cObject))
	vm.registerAsyncCondition(mk("Condition", vm.cObject))
	vm.registerAsyncNotification(mk("Notification", vm.cObject))
	vm.registerAsyncQueue(mk("Queue", vm.cObject))
	vm.registerAsyncLimitedQueue(mk("LimitedQueue", vm.cObject))
	vm.registerAsyncWaiter(mk("Waiter", vm.cObject))

	vm.registerAsyncKernel()
}
