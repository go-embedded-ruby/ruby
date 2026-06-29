// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"testing"
)

// TestRaiseURIErrFallback covers raiseURIErr's defensive default branch: a
// library error that is none of the three concrete URI error types maps to
// URI::Error (MRI's common URI error class). The go-ruby-uri library only ever
// returns the typed errors, so this branch is unreachable through Ruby; it is
// exercised here directly with a plain error to prove the mapping and to keep the
// branch covered. raiseURIErr panics a RubyError, which this test recovers.
func TestRaiseURIErrFallback(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("raiseURIErr(plain error) did not raise")
		}
		re, ok := r.(RubyError)
		if !ok {
			t.Fatalf("recovered %T, want RubyError", r)
		}
		if re.Class != "URI::Error" {
			t.Fatalf("raised %q, want URI::Error", re.Class)
		}
		if re.Message != "some other failure" {
			t.Fatalf("message %q, want the underlying error text", re.Message)
		}
	}()
	raiseURIErr(errors.New("some other failure"))
}

// TestRaiseURIErrNil checks raiseURIErr is a no-op on a nil error (the success
// path every library call takes), so it returns rather than raising.
func TestRaiseURIErrNil(t *testing.T) {
	raiseURIErr(nil) // must not panic
}
