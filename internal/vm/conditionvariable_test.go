// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestConditionVariable covers Thread::ConditionVariable against MRI 3.4:
// #wait releases the mutex and sleeps (delegating to mutex.sleep), #signal wakes
// the longest-waiting thread (FIFO), #broadcast wakes all, and #marshal_dump
// raises TypeError.
func TestConditionVariable(t *testing.T) {
	cases := []struct{ src, want string }{
		// #wait delegates to #sleep on the given object with the timeout.
		{`o = Object.new; def o.sleep(x); $stdout.puts "slept #{x}"; end; ConditionVariable.new.wait(o, 1234); ""`, "slept 1234\n"},
		// #signal wakes exactly the first waiter, releasing it in FIFO order.
		{`m = Mutex.new; cv = ConditionVariable.new; r = []
ts = 3.times.map { |i| Thread.new { m.synchronize { r << "w#{i}"; cv.wait(m); r << "d#{i}" } } }
Thread.pass until ts.all?(&:stop?)
3.times { |i| m.synchronize { cv.signal }; Thread.pass until r.size == 3 + i + 1 }
ts.each(&:join); p r`,
			`["w0", "w1", "w2", "d0", "d1", "d2"]` + "\n"},
		// #broadcast releases every waiter.
		{`m = Mutex.new; cv = ConditionVariable.new
ts = 4.times.map { Thread.new { m.synchronize { cv.wait(m) }; :ok } }
Thread.pass until ts.all?(&:stop?)
m.synchronize { cv.broadcast }
p ts.map(&:value)`, "[:ok, :ok, :ok, :ok]\n"},
		// #signal/#broadcast return self; #signal with no waiters is a no-op.
		{`cv = ConditionVariable.new; p [cv.signal.equal?(cv), cv.broadcast.equal?(cv)]`, "[true, true]\n"},
		// A waiter can also be released by a direct Thread#wakeup (via mutex.sleep's park).
		{`m = Mutex.new; cv = ConditionVariable.new; t = Thread.new { m.synchronize { cv.wait(m) }; :woke }
Thread.pass until t.stop?; t.wakeup; p t.value`, ":woke\n"},
		// to_s / inspect / truthiness.
		{`cv = ConditionVariable.new; p [cv.to_s, cv.inspect, (cv ? true : false)]`, `["#<Thread::ConditionVariable>", "#<Thread::ConditionVariable>", true]` + "\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// #marshal_dump raises TypeError (can't dump).
	if err := runErr(t, `ConditionVariable.new.marshal_dump`); err == nil || !strings.Contains(err.Error(), "can't dump") {
		t.Errorf("marshal_dump got %v want TypeError can't dump", err)
	}
	// #wait requires a mutex argument.
	if err := runErr(t, `ConditionVariable.new.wait`); err == nil || !strings.Contains(err.Error(), "given 0") {
		t.Errorf("wait() got %v want ArgumentError", err)
	}
}
