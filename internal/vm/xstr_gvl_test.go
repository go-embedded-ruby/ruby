// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !wasm

package vm_test

import "testing"

// TestABacktickDoesNotStopTheOtherThreads (#802).
//
// A backtick command used to hold the interpreter lock: while `cmd` ran, every
// other Ruby Thread stopped. That is the one thing a program has to be able to
// rely on NOT happening -- the usual way to bound a command that might wedge is
// to watch it from another thread, and that thread was asleep for exactly as
// long as the command it was supposed to be watching.
//
// The assertion samples DURING the command rather than after it, which is the
// only way to see the difference: once the backtick returns, a frozen thread
// runs and the end state looks identical either way. So the other thread
// records WHEN it woke, and a thread that was frozen cannot have woken before
// the command finished.
//
// Margin is 12x, not a tuned threshold -- measured on this host, the waking
// thread reports 0.05s when it runs concurrently and 0.61s when it does not,
// and the test asks whether it is under 0.4s. The 0.05 is its own sleep.
func TestABacktickDoesNotStopTheOtherThreads(t *testing.T) {
	checkCases(t, []runCase{
		{`t0 = Time.now
woke = nil
th = Thread.new { sleep 0.05; woke = Time.now - t0 }
` + "`sleep 0.6`" + `
th.join
p(woke < 0.4)`, "true\n"},
	})
}

// TestKernelSleepStillYields is the control that made the finding above
// specific rather than a general statement about the scheduler: hand-off works,
// and the subprocess path was what did not use it. Keeping the control next to
// the case means a future regression in either one is attributable.
func TestKernelSleepStillYields(t *testing.T) {
	checkCases(t, []runCase{
		{`t0 = Time.now
woke = nil
th = Thread.new { sleep 0.05; woke = Time.now - t0 }
sleep 0.6
th.join
p(woke < 0.4)`, "true\n"},
	})
}
