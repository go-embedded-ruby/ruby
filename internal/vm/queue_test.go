// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestQueue covers Thread::Queue and Thread::SizedQueue, asserted against MRI
// Ruby 3.4. Threads run on the emulated GVL with eager start (a new thread runs
// until its first block), which makes every interleaving here deterministic: a
// producer/consumer handoff completes exactly at the blocking points.
func TestQueue(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction and the Enumerable seed (Ruby 3.4).
		{`q = Queue.new; p [q.size, q.empty?]`, "[0, true]\n"},
		{`q = Queue.new([1, 2, 3]); p [q.size, q.pop, q.pop, q.pop, q.empty?]`, "[3, 1, 2, 3, true]\n"},
		{`p Queue.private_instance_methods.include?(:initialize)`, "true\n"},

		// FIFO, size/length, empty?, clear.
		{`q = Queue.new; q << 1; q.enq(2); q.push(3); p [q.deq, q.shift, q.pop, q.length, q.size]`, "[1, 2, 3, 0, 0]\n"},
		{`q = Queue.new; q.push(1); q.clear; p q.empty?`, "true\n"},

		// Aliases share the underlying method (UnboundMethod identity), as in MRI.
		{`p Queue.instance_method(:deq) == Queue.instance_method(:pop)`, "true\n"},
		{`p Queue.instance_method(:shift) == Queue.instance_method(:pop)`, "true\n"},
		{`p Queue.instance_method(:enq) == Queue.instance_method(:<<)`, "true\n"},
		{`p Queue.instance_method(:push) == Queue.instance_method(:<<)`, "true\n"},
		{`p Queue.instance_method(:length) == Queue.instance_method(:size)`, "true\n"},

		// close / closed?, and a closed empty queue yields nil.
		{`q = Queue.new; r = [q.closed?]; r << q.close.closed?; p r`, "[false, true]\n"},
		{`q = Queue.new; q.close; q.close; p q.closed?`, "true\n"}, // idempotent
		{`q = Queue.new; q.close; p q.pop`, "nil\n"},

		// Blocking pop woken by a push / by close.
		{`q = Queue.new; t = Thread.new { q.pop }; q.push(42); p t.value`, "42\n"},
		{`q = Queue.new; t = Thread.new { q.pop }; q.close; p t.value`, "nil\n"},

		// num_waiting counts blocked poppers.
		{`q = Queue.new; t = Thread.new { q.pop }; n = q.num_waiting; q.push(1); t.join; p n`, "1\n"},

		// Non-blocking pop of an empty queue; false-ish non_block coerces to boolean.
		{`q = Queue.new; q << 1 << 2; p [q.pop(false), q.pop(nil)]`, "[1, 2]\n"},

		// Timeouts (kwarg). A nil timeout means "no timer".
		{`q = Queue.new; p q.pop(timeout: 0.01)`, "nil\n"},
		{`q = Queue.new; q << 7; p q.pop(timeout: 5)`, "7\n"},

		// to_s reflects the class (used by the freeze message).
		{`p Queue.new.to_s`, "\"#<Thread::Queue>\"\n"},
		{`p SizedQueue.new(3).to_s`, "\"#<Thread::SizedQueue>\"\n"},

		// SizedQueue construction + max / max=.
		{`p [SizedQueue.new(5).max, SizedQueue.new(12.9).max]`, "[5, 12]\n"},
		{`q = SizedQueue.new(2); p q.class == Thread::SizedQueue`, "true\n"},
		{`q = SizedQueue.new(5); q.max = 10; p q.max`, "10\n"},
		// max= smaller than the current fill never drops items.
		{`q = SizedQueue.new(5); q.enq(1); q.enq(2); q.enq(3); q.max = 2; p [q.size > q.max, q.deq, q.deq, q.deq]`, "[true, 1, 2, 3]\n"},

		// Backpressure: a producer blocks on a full SizedQueue, then proceeds once
		// a consumer frees a slot.
		{`q = SizedQueue.new(1); q << 1; t = Thread.new { q << 2 }; first = q.pop; t.join; p [first, q.pop]`, "[1, 2]\n"},
		// Thread#stop? is false while running, true once dead or parked at a block.
		{`p Thread.current.stop?`, "false\n"},
		{`t = Thread.new { 1 }; t.join; p t.stop?`, "true\n"},
		{`q = Queue.new; t = Thread.new { q.pop }; r = t.stop?; q.push(1); t.join; p r`, "true\n"},

		// SizedQueue num_waiting counts blocked producers.
		{`q = SizedQueue.new(1); q << 1; t = Thread.new { q << 2 }; n = q.num_waiting; q.pop; t.join; p n`, "1\n"},
		// A larger max wakes a blocked producer.
		{`q = SizedQueue.new(1); q << 1; t = Thread.new { q << 2 }; q.max = 5; t.join; p [q.pop, q.pop]`, "[1, 2]\n"},
		// clear frees the whole capacity, unblocking a producer.
		{`q = SizedQueue.new(1); q << 1; t = Thread.new { q << 2 }; q.clear; t.join; p q.pop`, "2\n"},
		// close interrupts a blocked producer with ClosedQueueError.
		{`q = SizedQueue.new(1); q << 1; t = Thread.new { begin; q << 2; rescue ClosedQueueError; :closed; end }; q.close; p t.value`, ":closed\n"},
		// Non-blocking push of a full SizedQueue; push timeout returns nil.
		{`q = SizedQueue.new(1); q << 1; p q.push(2, timeout: 0.01)`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// Queue.new seed coercion errors.
		{`Queue.new(42)`, "can't convert Integer into Array"},
		{`Queue.new(Object.new)`, "can't convert Object into Array"},
		// #to_a exists but returns a non-Array.
		{`class Bad; def to_a; "str"; end; end; Queue.new(Bad.new)`, "can't convert Bad into Array (Bad#to_a gives String)"},
		// Enqueue / dequeue on a closed queue, and non-blocking empty pop.
		{`q = Queue.new; q.close; q.push(1)`, "queue closed"},
		{`q = Queue.new; q.pop(true)`, "queue empty"},
		// freeze always raises, with the receiver's to_s in the message.
		{`Queue.new.freeze`, "cannot freeze #<Thread::Queue>"},
		{`SizedQueue.new(1).freeze`, "cannot freeze #<Thread::SizedQueue>"},
		// SizedQueue argument validation.
		{`SizedQueue.new`, "wrong number of arguments"},
		{`SizedQueue.new(0)`, "queue size must be positive"},
		{`SizedQueue.new(-1)`, "queue size must be positive"},
		{`SizedQueue.new("12")`, "no implicit conversion of String into Integer"},
		{`q = SizedQueue.new(5); q.max = 0`, "queue size must be positive"},
		{`q = SizedQueue.new(5); q.max = "foo"`, "no implicit conversion of String into Integer"},
		// Full non-blocking push.
		{`q = SizedQueue.new(1); q << 1; q.push(2, true)`, "queue full"},
		// timeout option validation.
		{`q = Queue.new; q.pop(true, timeout: 1)`, "can't set a timeout if non_block is enabled"},
		{`q = Queue.new; q.pop(timeout: "1")`, "no implicit conversion of String into Float"},
		{`q = Queue.new; q.pop(timeout: false)`, "no implicit conversion of false into Float"},
		{`q = Queue.new; q.pop(timeout: true)`, "no implicit conversion of true into Float"},
		// Closed-queue push interrupt on a bounded queue is a ClosedQueueError.
		{`q = SizedQueue.new(1); q.close; q.push(1)`, "queue closed"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
