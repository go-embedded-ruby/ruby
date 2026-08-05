// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestThreadWakeup covers the cooperative wakeup machinery (Thread.stop,
// Thread#wakeup, Thread#run, and no-argument Kernel#sleep) against MRI 3.4: a
// thread parks until another thread wakes it, and waking a dead thread raises.
func TestThreadWakeup(t *testing.T) {
	cases := []struct{ src, want string }{
		// Thread.stop parks the current thread; wakeup resumes it.
		{`t = Thread.new { Thread.stop; puts "woke" }; Thread.pass until t.stop?; t.wakeup; t.join; puts "done"`, "woke\ndone\n"},
		// Thread#run also resumes a stopped thread.
		{`t = Thread.new { Thread.stop; puts "ran" }; Thread.pass until t.stop?; t.run; t.join`, "ran\n"},
		// No-argument sleep parks until woken.
		{`t = Thread.new { sleep; puts "slept" }; Thread.pass until t.status == "sleep"; t.wakeup; t.join`, "slept\n"},
		// A parked thread reports status "sleep" and stop? true.
		{`t = Thread.new { Thread.stop }; Thread.pass until t.stop?; p [t.status, t.stop?]; t.wakeup; t.join`, `["sleep", true]` + "\n"},
		// wakeup returns self.
		{`t = Thread.new { Thread.stop }; Thread.pass until t.stop?; p t.wakeup.equal?(t); t.join`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// Waking a finished thread is a ThreadError ("killed thread").
		{`t = Thread.new {}; t.join; t.wakeup`, "killed thread"},
		{`t = Thread.new {}; t.join; t.run`, "killed thread"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
