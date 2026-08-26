// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestKernelWarn covers Kernel#warn routing through Warning.warn: it builds one
// message (each argument newline-terminated, no doubled newline), passes a
// nil-or-converted category keyword, drops a message whose category is disabled
// and raises for an unknown one, validates the uplevel keyword, and does nothing
// with no message. Verified against ruby 4.0.6.
func TestKernelWarn(t *testing.T) {
	// Capture what Warning.warn receives by overriding it.
	const cap = `$c = nil; Warning.singleton_class.send(:define_method, :warn) { |m, category: nil| $c = [m, category] }; `
	cases := []struct{ src, want string }{
		// Each argument on its own line; an existing newline is not doubled.
		{cap + `warn("a", "b\n", "c"); p $c`, `["a\nb\nc\n", nil]`},
		{cap + `warn("hello"); p $c`, `["hello\n", nil]`},
		// A String category converts to a Symbol; nil stays nil.
		{cap + `Warning[:deprecated] = true; warn("x", category: "deprecated"); p $c`, `["x\n", :deprecated]`},
		// A disabled category drops the message (Warning.warn is not called).
		{cap + `Warning[:deprecated] = false; warn("x", category: :deprecated); p $c`, `nil`},
		// An enabled category passes through.
		{cap + `Warning[:experimental] = true; warn("x", category: :experimental); p $c`, `["x\n", :experimental]`},
		// No message does nothing.
		{cap + `warn; p $c`, `nil`},
		// An unknown category raises ArgumentError.
		{`begin; warn("x", category: :bogus); rescue ArgumentError => e; p e.message; end`, `"unknown category: bogus"`},
		// A non-Symbol/String category is a TypeError.
		{`begin; warn("x", category: 5); rescue TypeError => e; p e.message; end`, `"no implicit conversion of Integer into Symbol"`},
		// uplevel is validated: negative raises ArgumentError, non-Integer TypeError.
		{`begin; warn("x", uplevel: -1); rescue ArgumentError => e; p e.message; end`, `"negative level (-1)"`},
		{`begin; warn("x", uplevel: "a"); rescue TypeError => e; p e.message; end`, `"no implicit conversion of String into Integer"`},
		{cap + `warn("x", uplevel: 0); p $c`, `["x\n", nil]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
