// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestMutexSleep covers Mutex#sleep, asserted against MRI 3.4: it releases the
// mutex, sleeps for the (validated) duration, then re-acquires it and returns the
// rounded seconds slept. A nil/absent duration parks the thread until woken.
func TestMutexSleep(t *testing.T) {
	cases := []struct{ src, want string }{
		// A bounded sleep returns the rounded seconds and re-locks the mutex.
		{`m = Mutex.new; m.lock; r = m.sleep(0.001); p [r, m.locked?]`, "[0, true]\n"},
		{`m = Mutex.new; m.lock; p m.sleep(0)`, "0\n"},
		// An unowned mutex is released by a thread that locks then sleeps forever:
		// the caller sees it parked (stop?) with the mutex unlocked.
		{`m = Mutex.new; t = Thread.new { m.lock; m.sleep }; p [t.stop?, m.locked?]`, "[true, false]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// Sleeping a mutex the current thread does not hold is a ThreadError.
		{`Mutex.new.sleep`, "unlock a mutex which is not locked"},
		// A negative duration is an ArgumentError — validated before the ownership
		// check, so it wins even on an unowned mutex.
		{`Mutex.new.sleep(-0.1)`, "must not be negative"},
		{`m = Mutex.new; m.lock; m.sleep(-1)`, "must not be negative"},
		// A non-numeric duration is a TypeError.
		{`m = Mutex.new; m.lock; m.sleep("x")`, "into time interval"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
