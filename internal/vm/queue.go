// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// RQueue backs both Thread::Queue (an unbounded thread-safe FIFO) and
// Thread::SizedQueue (a bounded FIFO with producer backpressure). max == 0 marks
// an unbounded Queue; max > 0 marks a SizedQueue whose capacity is max.
//
// Every field is mutated only while the current thread holds the emulated GVL
// (all the native methods below run under it, and the blocking helpers release
// the GVL only around a bare channel/timer wait that never touches q), so the
// structure is race-free under `go test -race` without any Go-level locking.
type RQueue struct {
	items  []object.Value
	waitq  []chan struct{} // poppers blocked on an empty queue, FIFO
	pushq  []chan struct{} // pushers blocked on a full SizedQueue, FIFO
	closed bool
	max    int     // 0 = unbounded Queue; >0 = SizedQueue capacity
	class  *RClass // the Ruby class this instance reports (Queue/SizedQueue/subclass)
}

func (q *RQueue) ToS() string {
	if q.max > 0 {
		return "#<Thread::SizedQueue>"
	}
	return "#<Thread::Queue>"
}
func (q *RQueue) Inspect() string { return q.ToS() }
func (q *RQueue) Truthy() bool    { return true }

func (vm *VM) registerQueue() {
	cQueue := newClass("Queue", vm.cObject)
	vm.consts["Queue"] = cQueue
	vm.consts["Thread"].(*RClass).consts["Queue"] = cQueue
	cQueue.smethods["new"] = &Method{name: "new", owner: cQueue, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		q := &RQueue{class: classOfReceiver(self, cQueue)}
		vm.send(q, "initialize", args, blk)
		return q
	}}
	// Queue#initialize(enum = nil): with an argument, the elements of the given
	// Enumerable (coerced via #to_a) seed the queue (Ruby 3.4). It is a private
	// method, as in MRI.
	cQueue.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		q := self.(*RQueue)
		if len(args) > 0 {
			q.items = append(q.items, vm.queueSeed(args[0])...)
		}
		return object.NilV
	})
	cQueue.methods["initialize"].vis = visPrivate

	push := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.queuePush(self.(*RQueue), args)
	}
	cQueue.define("push", push)
	aliasBuiltin(cQueue, "<<", "push")
	aliasBuiltin(cQueue, "enq", "push")

	pop := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		nonBlock, timeout, hasTimeout := parseBlockOpts(args, 0)
		return vm.queuePop(self.(*RQueue), nonBlock, timeout, hasTimeout)
	}
	cQueue.define("pop", pop)
	aliasBuiltin(cQueue, "shift", "pop")
	aliasBuiltin(cQueue, "deq", "pop")

	cQueue.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(self.(*RQueue).items)))
	})
	aliasBuiltin(cQueue, "length", "size")

	cQueue.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(len(self.(*RQueue).items) == 0)
	})
	cQueue.define("num_waiting", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		q := self.(*RQueue)
		return object.IntValue(int64(len(q.waitq) + len(q.pushq)))
	})
	cQueue.define("clear", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		q := self.(*RQueue)
		q.items = nil
		// Dropping every item frees the whole capacity, so any blocked pushers can
		// now proceed.
		q.wakePushers()
		return self
	})
	cQueue.define("close", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		q := self.(*RQueue)
		q.closed = true
		// Wake every blocked popper (they return nil on an empty closed queue) and
		// every blocked pusher (they raise ClosedQueueError).
		for _, ch := range q.waitq {
			close(ch)
		}
		q.waitq = nil
		for _, ch := range q.pushq {
			close(ch)
		}
		q.pushq = nil
		return self
	})
	cQueue.define("closed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*RQueue).closed)
	})
	cQueue.define("freeze", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		q := self.(*RQueue)
		raise("TypeError", "cannot freeze %s", q.ToS())
		return self
	})

	vm.registerSizedQueue(cQueue)
}

func (vm *VM) registerSizedQueue(cQueue *RClass) {
	cSized := newClass("SizedQueue", cQueue)
	vm.consts["SizedQueue"] = cSized
	vm.consts["Thread"].(*RClass).consts["SizedQueue"] = cSized
	cSized.smethods["new"] = &Method{name: "new", owner: cSized, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		n := vm.toIntCoerce(args[0])
		if n <= 0 {
			raise("ArgumentError", "queue size must be positive")
		}
		return &RQueue{max: int(n), class: classOfReceiver(self, cSized)}
	}}
	cSized.define("max", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*RQueue).max))
	})
	cSized.define("max=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		n := vm.toIntCoerce(args[0])
		if n <= 0 {
			raise("ArgumentError", "queue size must be positive")
		}
		q := self.(*RQueue)
		grew := int(n) > q.max
		q.max = int(n)
		// A larger capacity may unblock waiting pushers; a smaller one never drops
		// items already enqueued beyond the new maximum.
		if grew {
			q.wakePushers()
		}
		return args[0]
	})
}

// classOfReceiver returns the class a native `new` should stamp on its instance:
// the receiver class when `new` is called on Queue/SizedQueue (or a Ruby
// subclass of them), falling back to def when the receiver is not a class.
func classOfReceiver(self object.Value, def *RClass) *RClass {
	if c, ok := self.(*RClass); ok {
		return c
	}
	return def
}

// wakePushers wakes every currently blocked pusher; each re-checks capacity under
// the GVL when it runs, so waking them all is safe (only those that find room
// enqueue, the rest block again).
func (q *RQueue) wakePushers() {
	for _, ch := range q.pushq {
		close(ch)
	}
	q.pushq = nil
}

// queueSeed coerces a Queue.new argument to the slice of elements to seed with,
// via #to_a, matching MRI's messages when the value is not Array-convertible.
func (vm *VM) queueSeed(v object.Value) []object.Value {
	name := vm.classOf(v).name
	if !vm.respondsToDynamic(v, "to_a") {
		raise("TypeError", "can't convert %s into Array", name)
	}
	res := vm.send(v, "to_a", nil, nil)
	arr, ok := res.(*object.Array)
	if !ok {
		raise("TypeError", "can't convert %s into Array (%s#to_a gives %s)",
			name, name, vm.classOf(res).name)
	}
	return append([]object.Value{}, arr.Elems...)
}

// parseBlockOpts decodes the shared (non_block, timeout:) options of Queue#pop
// and SizedQueue#push. positional is the index of the non_block flag among args
// (0 for pop, 1 for push, after the value). A trailing Hash carrying a :timeout
// key is the keyword bundle; a nil timeout means "wait indefinitely" (no timer).
// Passing both non_block and a timeout is an ArgumentError, as in MRI.
func parseBlockOpts(args []object.Value, positional int) (nonBlock bool, timeout float64, hasTimeout bool) {
	if h, ok := trailingHash(args); ok {
		if tv, found := h.Get(object.Symbol("timeout")); found {
			args = args[:len(args)-1]
			if !object.IsNil(tv) {
				timeout = num2dblQ(tv)
				hasTimeout = true
			}
		}
	}
	if len(args) > positional {
		nonBlock = args[positional].Truthy()
	}
	if nonBlock && hasTimeout {
		raise("ArgumentError", "can't set a timeout if non_block is enabled")
	}
	return nonBlock, timeout, hasTimeout
}

// num2dblQ converts a timeout value to seconds, accepting only real numerics and
// raising MRI's rb_num2dbl TypeError otherwise.
func num2dblQ(v object.Value) float64 {
	switch n := v.(type) {
	case object.Integer:
		return float64(n)
	case object.Float:
		return float64(n)
	}
	raise("TypeError", "no implicit conversion of %s into Float", num2dblName(v))
	return 0
}

// num2dblName is the name MRI's rb_num2dbl TypeError uses for v: the literal
// true/false for those singletons, otherwise the class name. A nil timeout never
// reaches here (parseBlockOpts treats it as "no timeout"), so it is not handled.
func num2dblName(v object.Value) string {
	if b, ok := v.(object.Bool); ok {
		if bool(b) {
			return "true"
		}
		return "false"
	}
	return classNameOf(v)
}

// queueBlock releases the GVL and waits on ch, optionally bounded by a timer. It
// reports whether the wait ended on the timeout rather than a wake. fn runs in
// the calling goroutine, so timedOut is written and read without a data race.
func (vm *VM) queueBlock(ch chan struct{}, timeout float64, hasTimeout bool) (timedOut bool) {
	if !hasTimeout {
		vm.threadBlock(func() { <-ch })
		return false
	}
	vm.threadBlock(func() {
		t := time.NewTimer(time.Duration(timeout * float64(time.Second)))
		defer t.Stop()
		select {
		case <-ch:
		case <-t.C:
			timedOut = true
		}
	})
	return timedOut
}

// queuePush enqueues args[0]. On a full SizedQueue it blocks the caller until
// room frees (or the queue closes, or the timeout elapses); on an unbounded Queue
// it never blocks. Returns self, or nil when a bounded push timed out.
func (vm *VM) queuePush(q *RQueue, args []object.Value) object.Value {
	nonBlock, timeout, hasTimeout := parseBlockOpts(args, 1)
	if q.closed {
		raise("ClosedQueueError", "queue closed")
	}
	for q.max > 0 && len(q.items) >= q.max {
		if nonBlock {
			raise("ThreadError", "queue full")
		}
		ch := make(chan struct{})
		q.pushq = append(q.pushq, ch)
		timedOut := vm.queueBlock(ch, timeout, hasTimeout)
		q.pushq = removeChan(q.pushq, ch)
		if q.closed {
			raise("ClosedQueueError", "queue closed")
		}
		if timedOut {
			return object.NilV
		}
	}
	q.items = append(q.items, args[0])
	q.wakeOnePopper()
	return q
}

// queuePop dequeues the head, blocking on an empty queue until an item arrives
// (or the queue closes, or the timeout elapses). A closed empty queue yields nil;
// a non-blocking pop of an empty queue raises ThreadError.
func (vm *VM) queuePop(q *RQueue, nonBlock bool, timeout float64, hasTimeout bool) object.Value {
	for len(q.items) == 0 {
		if q.closed {
			return object.NilV
		}
		if nonBlock {
			raise("ThreadError", "queue empty")
		}
		ch := make(chan struct{})
		q.waitq = append(q.waitq, ch)
		timedOut := vm.queueBlock(ch, timeout, hasTimeout)
		q.waitq = removeChan(q.waitq, ch)
		if timedOut {
			return object.NilV
		}
	}
	v := q.items[0]
	q.items = q.items[1:]
	// A slot just freed: let one blocked pusher (SizedQueue) proceed.
	q.wakeOnePusher()
	return v
}

func (q *RQueue) wakeOnePopper() {
	if len(q.waitq) > 0 {
		ch := q.waitq[0]
		q.waitq = q.waitq[1:]
		close(ch)
	}
}

func (q *RQueue) wakeOnePusher() {
	if len(q.pushq) > 0 {
		ch := q.pushq[0]
		q.pushq = q.pushq[1:]
		close(ch)
	}
}

// removeChan drops ch from a waiter slice if present (a woken waiter is already
// gone; a timed-out one removes itself). Order among the survivors is preserved.
func removeChan(waiters []chan struct{}, ch chan struct{}) []chan struct{} {
	for i, w := range waiters {
		if w == ch {
			return append(waiters[:i:i], waiters[i+1:]...)
		}
	}
	return waiters
}
