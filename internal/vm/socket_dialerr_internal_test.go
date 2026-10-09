// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"
)

// TestRaiseDialErrFallbacks covers the two arms of raiseDialErr that a real
// connect cannot reach from a test: an errno with no Errno::Exxx class
// registered for it, which MRI reports as a bare (still rescuable)
// SystemCallError, and an error carrying no errno and no resolution failure,
// which keeps SocketError.
//
// The coverage gate named raiseDialErr at 91.7% and the uncovered line was the
// SystemCallError arm. The question a named function asks is whether the branch
// is DEAD or merely unreached, and this one is unreached: which errnos have a
// class depends on what the platform's syscall package defines, so an errno
// outside that set is reachable in principle and must not silently become
// something else. Written the way raiseChdirErr's equivalent already was
// (dir_getwd_internal_test.go), rather than inventing a second idiom.
func TestRaiseDialErrFallbacks(t *testing.T) {
	// An errno number no platform table claims.
	unclaimed := syscall.Errno(0)
	for n := syscall.Errno(1); n < 4096; n++ {
		if _, ok := errnoClasses[int64(n)]; !ok {
			unclaimed = n
			break
		}
	}
	if unclaimed == 0 {
		t.Skip("every errno below 4096 has a class on this platform")
	}

	got := caughtRaise(t, func() { raiseDialErr(unclaimed, dialAddr("h", "1")) })
	if got.Class != "SystemCallError" {
		t.Errorf("unclaimed errno %d: class %q, want SystemCallError", unclaimed, got.Class)
	}
	// The address still reaches the message: an arm that drops it would leave a
	// caller unable to say WHICH peer failed, which is the whole point of the
	// suffix.
	if want := ` - connect(2) for "h" port 1`; !strings.HasSuffix(got.Message, want) {
		t.Errorf("unclaimed errno message = %q, want it to end with %q", got.Message, want)
	}

	// No errno and no DNS failure behind it: the class stays SocketError rather
	// than an invented errno. Inventing one would make the class a guess.
	got = caughtRaise(t, func() { raiseDialErr(errors.New("no errno here"), dialAddr("h", "1")) })
	if got.Class != "SocketError" || got.Message != "no errno here" {
		t.Errorf("errno-less error: got %q / %q, want SocketError / %q",
			got.Class, got.Message, "no errno here")
	}

	// And the Net::HTTP shape, which passes no address: no connect(2) suffix is
	// invented for a phase that may not have been a connect.
	got = caughtRaise(t, func() { raiseDialErr(unclaimed, "") })
	if got.Class != "SystemCallError" {
		t.Errorf("unclaimed errno, no address: class %q, want SystemCallError", got.Class)
	}
	if strings.Contains(got.Message, "connect(2)") {
		t.Errorf("message %q names connect(2) although no address was given", got.Message)
	}

	// A DNSError is a resolution failure whatever else it wraps, and it is
	// checked FIRST because the outer meaning is the one MRI reports.
	got = caughtRaise(t, func() {
		raiseDialErr(&net.DNSError{Err: "no such host", Name: "h"}, dialAddr("h", "1"))
	})
	if got.Class != "Socket::ResolutionError" {
		t.Errorf("DNSError: class %q, want Socket::ResolutionError", got.Class)
	}
}
