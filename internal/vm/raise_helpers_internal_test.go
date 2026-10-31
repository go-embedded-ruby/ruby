// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// These two helpers are used by core tests and gem tests alike. They lived in
// the factory_bot and yaml tests until the rbgo_core build stopped compiling
// those files; they moved here unchanged.

// expectRaise runs fn and returns the class of the Ruby exception it raises (or
// "" if none). A non-Ruby panic is re-raised.
func expectRaise(t *testing.T, fn func()) (cls string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			re, ok := r.(RubyError)
			if !ok {
				panic(r)
			}
			cls = re.Class
		}
	}()
	fn()
	return ""
}

// rubyErr runs fn and returns the RubyError it panics with (failing the test if
// it does not panic with one). It is the white-box analogue of the black-box
// runErr helper, used to assert the binding's raise() paths directly.
func rubyErr(t *testing.T, fn func()) RubyError {
	t.Helper()
	var got RubyError
	func() {
		defer func() {
			re, ok := recover().(RubyError)
			if !ok {
				t.Fatalf("expected a RubyError panic, got %v", recover())
			}
			got = re
		}()
		fn()
	}()
	return got
}
