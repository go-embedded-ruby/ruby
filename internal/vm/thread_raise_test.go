// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestThreadRaise covers Thread#raise: an exception delivered to another thread
// is picked up at that thread's next interpreter safepoint (a woken sleep, a
// backward jump, or a method/block entry) and raised in its own context, so it
// surfaces through Thread#join / #value, against MRI 3.4.
func TestThreadRaise(t *testing.T) {
	cases := []struct{ src, want string }{
		// A class + message raises that class with that message on the sleeper.
		{`t = Thread.new { begin; sleep; rescue Exception => e; e; end }
		  Thread.pass until t.stop?; t.raise(ArgumentError, "boom")
		  p [t.value.class, t.value.message]`, `[ArgumentError, "boom"]` + "\n"},
		// No arguments raises a RuntimeError with an empty message (MRI).
		{`t = Thread.new { begin; sleep; rescue Exception => e; e; end }
		  Thread.pass until t.stop?; t.raise
		  p [t.value.class, t.value.message]`, `[RuntimeError, ""]` + "\n"},
		// A lone String raises RuntimeError with that message.
		{`t = Thread.new { begin; sleep; rescue Exception => e; e; end }
		  Thread.pass until t.stop?; t.raise("just a message")
		  p [t.value.class, t.value.message]`, `[RuntimeError, "just a message"]` + "\n"},
		// A bare exception class instantiates it with no message.
		{`t = Thread.new { begin; sleep; rescue Exception => e; e; end }
		  Thread.pass until t.stop?; t.raise(TypeError)
		  p t.value.class`, "TypeError\n"},
		// The exact exception instance passed is the one raised (identity).
		{`e = RuntimeError.new("x")
		  t = Thread.new { begin; sleep; rescue Exception => ex; ex; end }
		  Thread.pass until t.stop?; t.raise(e)
		  p t.value.equal?(e)`, "true\n"},
		// A CPU-bound thread that yields (loop { Thread.pass }) is interrupted at a
		// safepoint on the next block re-entry.
		{`q = Queue.new
		  t = Thread.new { begin; q.push(:go); loop { Thread.pass }; rescue Exception => e; e.message; end }
		  q.pop; t.raise(StandardError, "spin")
		  p t.value`, `"spin"` + "\n"},
		// raise returns the thread (self) for a live target.
		{`t = Thread.new { begin; sleep; rescue Exception; end }
		  Thread.pass until t.stop?; r = t.raise("m"); p r.equal?(t); t.join`, "true\n"},
		// A dead thread ignores the raise and returns nil.
		{`t = Thread.new { 1 }; t.join; p t.raise("ignored")`, "nil\n"},
		// Raising the current thread raises immediately (synchronously).
		{`begin; Thread.current.raise(TypeError, "self"); rescue => e; p [e.class, e.message]; end`,
			`[TypeError, "self"]` + "\n"},
		// Mutex#sleep is interruptible; the mutex is re-acquired before the raise
		// propagates (MRI), so the rescue sees it owned.
		{`m = Mutex.new; owned = nil
		  t = Thread.new { m.lock; begin; m.sleep; rescue Exception; owned = m.owned?; end }
		  Thread.pass until t.stop?; t.raise(RuntimeError, "wake"); t.join; p owned`, "true\n"},
		// report_on_exception defaults to true and is settable.
		{`p Thread.current.report_on_exception`, "true\n"},
		{`t = Thread.new { Thread.current.report_on_exception = false; Thread.current.report_on_exception }
		  p t.value`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// A non-exception argument is a TypeError, matching Kernel#raise; the current
	// thread raises it synchronously so the caller observes it directly.
	if err := runErr(t, `Thread.current.raise(42)`); err == nil ||
		!strings.Contains(err.Error(), "exception class/object expected") {
		t.Errorf("raise(non-exception): got %v want TypeError", err)
	}

	// A non-exception class or object (shared with Kernel#raise coercion) is a
	// TypeError, not a bogus wrapped exception.
	for _, src := range []string{`raise(String)`, `raise(Object.new)`, `raise("msg", {})`} {
		if err := runErr(t, src); err == nil ||
			!strings.Contains(err.Error(), "exception class/object expected") {
			t.Errorf("%s: got %v want TypeError exception class/object expected", src, err)
		}
	}

	// An unhandled Thread#raise surfaces through Thread#value in the joining thread.
	if err := runErr(t, `t = Thread.new { Thread.current.report_on_exception = false; sleep }
		Thread.pass until t.stop?; t.raise(RuntimeError, "unhandled"); t.value`); err == nil ||
		!strings.Contains(err.Error(), "unhandled") {
		t.Errorf("unhandled Thread#raise: got %v", err)
	}
}
