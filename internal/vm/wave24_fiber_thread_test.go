// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"runtime"
	"strings"
	"testing"
)

// TestWave24FiberThread pins the wave-24 Fiber and Thread surface: Fiber#raise,
// Fiber storage, Fiber.blocking?/#blocking?/Fiber.blocking, Fiber#kill,
// Fiber.set_scheduler, and on the Thread side #to_s/#inspect, #priority,
// #join(limit), #native_thread_id, the class-level default accessors,
// Thread.handle_interrupt and Thread.pending_interrupt?.
//
// Every expectation in the table was produced by running the same source under
// MRI ruby 4.0.5 and comparing the raw bytes of stdout, so the want strings are
// MRI's output, not a reading of it.
func TestWave24FiberThread(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Fiber.new(blocking: true) { Fiber.blocking? }.resume`, "1\n"},
		{`p Fiber.new(blocking: false) { Fiber.blocking? }.resume`, "false\n"},
		{`p Fiber.new { Fiber.current.blocking? }.resume`, "false\n"},
		{`p Fiber.current.blocking?`, "true\n"},
		{`p Fiber.blocking?`, "1\n"},
		{`p Fiber.new { Fiber.blocking { |f| f.blocking? } }.resume`, "true\n"},
		{`p Fiber.blocking { |f| f.blocking? }`, "true\n"},
		{`p Fiber.new(storage: {a: 1}) { Fiber[:a] }.resume`, "1\n"},
		{`p Fiber.new(storage: {a: 1}) { Fiber.current.storage }.resume`, "{a: 1}\n"},
		{`p Fiber.new(storage: nil) { Fiber.current.storage }.resume`, "nil\n"},
		{`p Fiber.new { Fiber["k"] = 7; Fiber[:k] }.resume`, "7\n"},
		{`p Fiber.new { Fiber[:k] = 7; Fiber["k"] }.resume`, "7\n"},
		{`p Fiber.new(storage: {a: 1}) { Fiber[:a] = nil; Fiber.current.storage }.resume`, "{}\n"},
		{`p Fiber.new { Fiber[:zz] }.resume`, "nil\n"},
		{`k = Object.new; def k.to_str; "sk"; end; p Fiber.new { Fiber[k] = 3; Fiber[:sk] }.resume`, "3\n"},
		{`begin; Fiber[12]; rescue => e; p [e.class, e.message]; end`, "[TypeError, \"12 is not a symbol nor a string\"]\n"},
		{`begin; Fiber.current.storage = 42; rescue => e; p [e.class, e.message]; end`, "[TypeError, \"storage must be a hash\"]\n"},
		{`begin; Fiber.current.storage = {a: 1}.freeze; rescue => e; p [e.class, e.message]; end`, "[FrozenError, \"storage must not be frozen\"]\n"},
		{`begin; Fiber.current.storage = {1 => 2}; rescue => e; p [e.class, e.message]; end`, "[TypeError, \"wrong argument type Integer (expected Symbol)\"]\n"},
		{`begin; Fiber.new(storage: 42) {}; rescue => e; p [e.class, e.message]; end`, "[TypeError, \"storage must be a hash\"]\n"},
		{`f = Fiber.new(storage: {a: 1}) { nil }; begin; f.storage; rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"Fiber storage can only be accessed from the Fiber it belongs to\"]\n"},
		{`p Fiber.current.storage`, "nil\n"},
		{`p Fiber.new { Fiber.current.storage = {b: 2}; Fiber.current.storage }.resume`, "{b: 2}\n"},
		{`f = Fiber.new { true }; begin; f.raise; rescue => e; p [e.class, e.message]; end`, "[FiberError, \"cannot raise exception on unborn fiber\"]\n"},
		{`f = Fiber.new { true }; f.resume; begin; f.raise; rescue => e; p e.class; end`, "FiberError\n"},
		{`begin; Fiber.current.raise(ArgumentError, "z"); rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"z\"]\n"},
		{`f = Fiber.new { Fiber.yield }; f.resume; begin; f.raise; rescue => e; p [e.class, e.message]; end`, "[RuntimeError, \"\"]\n"},
		{`f = Fiber.new { Fiber.yield }; f.resume; begin; f.raise "boom"; rescue => e; p [e.class, e.message]; end`, "[RuntimeError, \"boom\"]\n"},
		{`f = Fiber.new { Fiber.yield }; f.resume; begin; f.raise ArgumentError, "m", ["l1","l2"]; rescue => e; p [e.class, e.message, e.backtrace]; end`, "[ArgumentError, \"m\", [\"l1\", \"l2\"]]\n"},
		{`f = Fiber.new { Fiber.yield }; f.resume; c = StandardError.new("c"); begin; f.raise("x", cause: c); rescue => e; p [e.message, e.cause.message]; end`, "[\"x\", \"c\"]\n"},
		{`f = Fiber.new { Fiber.yield }; f.resume; begin; f.raise(cause: StandardError.new("c")); rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"only cause is given with no arguments\"]\n"},
		{`f = Fiber.new {}; p f.kill.class; p f.alive?`, "Fiber\nfalse\n"},
		{`f = Fiber.new {}; f.kill; p f.kill`, "false\n"},
		{`e = false; f = Fiber.new { begin; while true; Fiber.yield; end; ensure; e = true; end }; f.resume; f.kill; p [e, f.alive?]`, "[true, false]\n"},
		{`r = false; f = Fiber.new { begin; while true; Fiber.yield; end; rescue Exception; r = true; end }; f.resume; f.kill; p [r, f.alive?]`, "[false, false]\n"},
		{`f = Fiber.new { Fiber.current.kill; :no }; p f.resume; p f.alive?`, "nil\nfalse\n"},
		{`p Fiber.scheduler`, "nil\n"},
		{`s = Object.new; [:unblock,:kernel_sleep,:io_wait].each { |m| s.define_singleton_method(m){} }; begin; Fiber.set_scheduler(s); rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"Scheduler must implement #block\"]\n"},
		{`p Fiber.set_scheduler(nil)`, "nil\n"},
		{`t = Thread.new { sleep 0.01 }; t.join; p t.to_s.encoding`, "#<Encoding:BINARY (ASCII-8BIT)>\n"},
		{`t = Thread.new { }; t.join; t.name = "n"; p t.to_s.include?("@n")`, "true\n"},
		{`t = Thread.new { }; t.join; t.name = "平"; p t.to_s.encoding`, "#<Encoding:UTF-8>\n"},
		{"begin; Thread.current.name = \"a\\0b\"; rescue => e; p [e.class, e.message]; end", "[ArgumentError, \"string contains null byte\"]\n"},
		{`n = Object.new; def n.to_str; "viaстр"; end; Thread.current.name = n; p Thread.current.name`, "\"via\u0441\u0442\u0440\"\n"},
		{`begin; Thread.current.name = 12; rescue => e; p [e.class, e.message]; end`, "[TypeError, \"no implicit conversion of Integer into String\"]\n"},
		{`p Thread.current.priority`, "0\n"},
		{`t = Thread.new { sleep 0.01 }; t.priority = 9; p t.priority; t.priority = -9; p t.priority; t.join`, "3\n-3\n"},
		{`begin; Thread.current.priority = "x"; rescue => e; p [e.class, e.message]; end`, "[TypeError, \"no implicit conversion of String into Integer\"]\n"},
		{`Thread.current.priority = 2; t = Thread.new { Thread.current.priority }; p t.value; Thread.current.priority = 0`, "2\n"},
		{`begin; Thread.start; rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"tried to create Proc object without a block\"]\n"},
		{`p Thread.method(:fork) == Thread.method(:start)`, "true\n"},
		{`begin; Thread.allocate; rescue => e; p [e.class, e.message]; end`, "[TypeError, \"allocator undefined for Thread\"]\n"},
		{`p Thread.abort_on_exception; Thread.abort_on_exception = true; p Thread.abort_on_exception; Thread.abort_on_exception = false`, "false\ntrue\n"},
		{`p Thread.report_on_exception; Thread.report_on_exception = false; p Thread.report_on_exception; Thread.report_on_exception = true`, "true\nfalse\n"},
		{`p Thread.ignore_deadlock; Thread.ignore_deadlock = true; p Thread.ignore_deadlock; Thread.ignore_deadlock = false`, "false\ntrue\n"},
		{`t = Thread.new { }; t.join; p t.native_thread_id`, "nil\n"},
		{`p Thread.current.native_thread_id.is_a?(Integer)`, "true\n"},
		{`q = Queue.new; t = Thread.new { q.pop }; p t.join(0); q << 1; p t.join.equal?(t)`, "nil\ntrue\n"},
		{`t = Thread.new {}; t.join; p t.join(0.0).equal?(t); p t.join(nil).equal?(t)`, "true\ntrue\n"},
		{`t = Thread.new {}; t.join; begin; t.join(:foo); rescue => e; p [e.class, e.message]; end`, "[TypeError, \"can't convert Symbol into Float\"]\n"},
		{`t = Thread.new { Thread.current.report_on_exception = false; raise "inner" }; begin; t.join(5); rescue => e; p e.message; end`, "\"inner\"\n"},
		{`Thread.current.thread_variable_set(:tv, 1); Thread.current.thread_variable_set(:tv, nil); p Thread.current.thread_variable?(:tv)`, "false\n"},
		{`begin; Thread.current.thread_variable_get(12); rescue => e; p [e.class, e.message]; end`, "[TypeError, \"12 is not a symbol nor a string\"]\n"},
		{`p Thread.pending_interrupt?`, "false\n"},
		{`p Thread.current.pending_interrupt?`, "false\n"},
		{`begin; Thread.handle_interrupt(1) {}; rescue => e; p [e.class, e.message]; end`, "[TypeError, \"no implicit conversion of Integer into Hash\"]\n"},
		{`begin; Thread.handle_interrupt(RuntimeError => :never); rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"block is needed.\"]\n"},
		{`p Thread.handle_interrupt(RuntimeError => :never) { 7 }`, "7\n"},
		{`p Thread.handle_interrupt("x" => :never) { 7 }`, "7\n"},
		{`begin; Fiber.current.raise(RuntimeError, "x", ["a"], cause: RuntimeError.new("y")); rescue => e; p [e.message, e.cause.message, e.backtrace]; end`, "[\"x\", \"y\", [\"a\"]]\n"},
		{`e1 = RuntimeError.new("1"); e2 = RuntimeError.new("2"); f = Fiber.new { Fiber.yield }; f.resume; begin; f.raise(e1, cause: e1); rescue => e; p [e.message, e.cause]; end`, "[\"1\", nil]\n"},
		{`begin; Fiber[]; rescue ArgumentError => e; p e.class; end`, "ArgumentError\n"},
		{`begin; Fiber[:a, :b] = 1; rescue ArgumentError => e; p e.class; end`, "ArgumentError\n"},
		{`begin; Fiber.blocking; rescue => e; p e.class; end`, "LocalJumpError\n"},
		{`begin; Fiber.set_scheduler; rescue ArgumentError => e; p e.class; end`, "ArgumentError\n"},
		{`s = Object.new; [:block,:unblock,:kernel_sleep,:io_wait].each { |m| s.define_singleton_method(m){} }; p Fiber.set_scheduler(s).equal?(s); p Fiber.scheduler.equal?(s); Fiber.set_scheduler(nil)`, "true\ntrue\n"},
		{`root = Fiber.current; a = Fiber.new { root.transfer }; a.transfer; begin; a.raise "t"; rescue => e; p [e.class, e.message]; end`, "[RuntimeError, \"t\"]\n"},
		{`res = []; one = Fiber.new { Fiber.yield :y1; :never }; two = Fiber.new { res << one.resume; begin; one.raise; rescue; res << :rescued; end; res }; p two.resume`, "[:y1, :rescued]\n"},
		{`g = Fiber.new { Fiber.yield }; g.resume; Thread.new { begin; g.raise; rescue => e; p [e.class, e.message]; end }.join`, "[FiberError, \"fiber called across threads\"]\n"},
		{`p Thread.current.to_s.include?("run")`, "true\n"},
		{`t = Thread.new { }; t.join; p t.to_s.include?("dead")`, "true\n"},
		{`q = Queue.new; out = []; th = Thread.new { begin; Thread.handle_interrupt(RuntimeError => :never) { begin; q.pop; rescue RuntimeError; out << :inner; end }; rescue RuntimeError; out << :deferred; end }; q2 = 0; Thread.pass until th.stop?; th.raise "i"; q << 1; th.join; p out`, "[:deferred]\n"},
		{`q = Queue.new; out = []; th = Thread.new { begin; Thread.handle_interrupt(RuntimeError => :on_blocking) { begin; q.pop; rescue RuntimeError; out << :inner; end }; rescue RuntimeError; out << :deferred; end }; Thread.pass until th.stop?; th.raise "i"; q << 1; th.join; p out`, "[:inner]\n"},
		{`q = Queue.new; out = []; th = Thread.new { begin; Thread.handle_interrupt(RuntimeError => :immediate) { begin; q.pop; rescue RuntimeError; out << :inner; end }; rescue RuntimeError; out << :deferred; end }; Thread.pass until th.stop?; th.raise "i"; q << 1; th.join; p out`, "[:inner]\n"},
		{`done = false; begin; Thread.handle_interrupt(RuntimeError => :never) { cur = Thread.current; Thread.new { cur.raise "async" }.join; done = Thread.pending_interrupt?; raise "regular" }; rescue => e; p [e.message, done]; end`, "[\"async\", true]\n"},
		{`Thread.handle_interrupt(RuntimeError => :never) { cur = Thread.current; Thread.new { cur.raise "im" }.join; begin; Thread.handle_interrupt(RuntimeError => :immediate) { :no }; rescue => e; p [e.message, Thread.pending_interrupt?]; end }`, "[\"im\", false]\n"},
		{`begin; Thread.handle_interrupt(ArgumentError => :never) { cur = Thread.current; begin; Thread.new { cur.raise "unmatched" }.join; rescue; end; p :survived }; rescue => e; p [:outer, e.message]; end`, ":survived\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestWave24FiberKillFromDescendant covers fiber_check_killed: a fiber killed by
// a fiber it resumed does not carry on when control comes back — it unwinds at
// the switch-in, so the killer's own continuation never runs either. Asserted
// against MRI 4.0.5.
func TestWave24FiberKillFromDescendant(t *testing.T) {
	src := `
parent = nil
parent = Fiber.new do
  child = Fiber.new do
    parent.kill
    puts "child continued"
  end
  child.resume
  puts "parent continued"
end
parent.resume
p parent.alive?
`
	// rbgo does not yet bind a local assigned by an expression that closes over
	// it, so the fiber is reached through a constant instead.
	src = strings.Replace(src, "parent = nil\n", "", 1)
	src = strings.Replace(src, "parent = Fiber.new do", "PARENT = Fiber.new do", 1)
	src = strings.ReplaceAll(src, "parent.kill", "PARENT.kill")
	src = strings.ReplaceAll(src, "parent.resume", "PARENT.resume")
	src = strings.ReplaceAll(src, "p parent.alive?", "p PARENT.alive?")
	if got := eval(t, src); got != "false\n" {
		t.Errorf("kill from a descendant: got %q want %q", got, "false\n")
	}
}

// TestWave24ThreadPassStaysRunnable is the regression test for the hang that
// made six ruby/spec thread files time out: Thread.pass must leave the thread
// runnable, so a peer spinning on `t.status != "run"` — the shape every
// ruby/spec thread fixture uses — terminates.
func TestWave24ThreadPassStaysRunnable(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("no real threads on this platform")
	}
	src := `
STATE = []
t = Thread.new { Thread.pass until STATE[0] == :exit }
Thread.pass while t.status && t.status != "run"
p t.status
p t.stop?
STATE[0] = :exit
t.join
p t.status
`
	want := "\"run\"\nfalse\nfalse\n"
	if got := eval(t, src); got != want {
		t.Errorf("Thread.pass status: got %q want %q", got, want)
	}
}
