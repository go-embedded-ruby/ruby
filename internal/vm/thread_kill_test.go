// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestThreadKill covers Thread#kill / #exit / #terminate (and the Thread.exit /
// Thread.kill class forms): a killed thread unwinds at its next yield point,
// running its ensure blocks but bypassing rescue, and finishes with a nil value,
// against MRI 3.4.
func TestThreadKill(t *testing.T) {
	cases := []struct{ src, want string }{
		// Kills a sleeping thread; the statement after sleep never runs.
		{`r = nil; t = Thread.new { sleep; r = :after }
		  Thread.pass until t.stop?; t.kill; t.join; p r`, "nil\n"},
		// Kills the current thread; the statement after kill never runs.
		{`r = nil; Thread.new { Thread.current.kill; r = :after }.join; p r`, "nil\n"},
		// Runs the ensure clause on the way out.
		{`log = []; t = Thread.new { begin; Thread.current.kill; ensure; log << :ens; end }
		  t.join; p log`, "[:ens]\n"},
		// A kill cannot be rescued: neither the rescue body nor the rest of the block runs.
		{`log = []; t = Thread.new { begin; Thread.current.kill; rescue Exception; log << :rescued; end; log << :after }
		  t.join; p log`, "[]\n"},
		// Nested ensure clauses all run.
		{`sp = []
		  outer = Thread.new do
		    begin
		      inner = Thread.new { begin; sleep; ensure; sp << :inner; end }
		      sleep
		    ensure
		      sp << :outer; inner.kill; inner.join
		    end
		  end
		  Thread.pass while outer.status && outer.status != "sleep"
		  sleep 0.02; outer.kill; outer.join; p sp.sort`, "[:inner, :outer]\n"},
		// exit and terminate are aliases of kill (same UnboundMethod).
		{`p Thread.instance_method(:exit) == Thread.instance_method(:kill)`, "true\n"},
		{`p Thread.instance_method(:terminate) == Thread.instance_method(:kill)`, "true\n"},
		// #value of a killed thread is nil; #status is false; #alive? is false.
		{`t = Thread.new { sleep }; Thread.pass until t.stop?; t.kill; p [t.value, t.status, t.alive?]`,
			"[nil, false, false]\n"},
		// A kill does not reset $! seen by ensure clauses.
		{`sp = []; exc = RuntimeError.new("foo")
		  t = Thread.new do
		    begin; raise exc
		    ensure
		      sp << $!
		      begin; Thread.current.kill; ensure; sp << $!; end
		    end
		  end
		  t.join; p sp.map { |e| e.equal?(exc) }`, "[true, true]\n"},
		// Killing a dead thread is a no-op that returns the thread.
		{`t = Thread.new { 1 }; t.join; p t.kill.equal?(t)`, "true\n"},
		// #kill returns the (live) target thread.
		{`t = Thread.new { sleep }; Thread.pass until t.stop?; r = t.kill; p r.equal?(t); t.join`, "true\n"},
		// #terminate and #exit also terminate a sleeping thread.
		{`r = nil; t = Thread.new { sleep; r = :after }; Thread.pass until t.stop?; t.terminate; t.join; p r`, "nil\n"},
		{`r = nil; t = Thread.new { sleep; r = :after }; Thread.pass until t.stop?; t.exit; t.join; p r`, "nil\n"},
		// Thread.exit ends the current thread; Thread.kill(t) ends t.
		{`t = Thread.new { Thread.exit; sleep }; t.join; p t.status`, "false\n"},
		{`r = nil; t = Thread.new { sleep; r = :after }; Thread.pass until t.stop?; Thread.kill(t); t.join; p r`, "nil\n"},
		// Killing a running thread that yields via Thread.pass.
		{`q = Queue.new; t = Thread.new { q.push(:go); loop { Thread.pass } }
		  q.pop; t.kill; t.join; p t.status`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// Killing the main thread ends the program cleanly: statements before the kill
	// run, the kill is not an error, and nothing after it runs.
	if got := eval(t, `puts "before"; Thread.current.kill; puts "after"`); got != "before\n" {
		t.Errorf("main self-kill: got %q want %q", got, "before\n")
	}
	if got := eval(t, `puts "before"; Thread.exit; puts "after"`); got != "before\n" {
		t.Errorf("main Thread.exit: got %q want %q", got, "before\n")
	}
	// Another thread killing the main thread ends the program cleanly.
	if got := eval(t, `Thread.new { sleep 0.02; Thread.main.kill }; sleep 5; puts "reached"`); got != "" {
		t.Errorf("cross-thread main kill: got %q want empty", got)
	}

	// Thread.kill argument errors.
	if err := runErr(t, `Thread.kill`); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
		t.Errorf("Thread.kill (no args): got %v want ArgumentError", err)
	}
	if err := runErr(t, `Thread.kill(42)`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("Thread.kill(42): got %v want TypeError", err)
	}
}
