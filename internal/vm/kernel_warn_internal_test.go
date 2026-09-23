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
		// With a level, error.c v3_4_0 rb_warn_m prefixes the message —
		// "path:lineno: warning: " from rb_ec_backtrace_location_ary, or the bare
		// "warning: " when that yields no location. rbgo records no line numbers,
		// so no frame can supply a path and the no-location form is what every
		// level produces; MRI would say "<file>:<line>: warning: x\n" here. Closing
		// that gap needs line tracking in internal/compiler.
		{cap + `warn("x", uplevel: 0); p $c`, `["warning: x\n", nil]`},
		// A level too large for the stack is exactly the no-location case, and
		// there rbgo and MRI agree byte for byte.
		{cap + `warn("x", uplevel: 100); p $c`, `["warning: x\n", nil]`},
		// $VERBOSE nil makes Kernel#warn a no-op: rb_warn_m wraps its whole body
		// in `if (!NIL_P(ruby_verbose) && argc > 0)`, so nothing is written and
		// Warning.warn is never called.
		{cap + `$VERBOSE = nil; warn("x"); p $c`, `nil`},
		// rb_io_puts assembles the message, so an Array argument writes one line
		// per element and an empty Array writes nothing.
		{cap + `warn(["line 1", "line 2"]); p $c`, `["line 1\nline 2\n", nil]`},
		// A lone String that already ends in a newline is kept VERBATIM — rb_warn_m
		// skips rb_io_puts entirely for it (end_with_asciichar(str, '\n')).
		{cap + `warn("x\n"); p $c`, `["x\n", nil]`},
		{cap + `warn("a\n", "b"); p $c`, `["a\nb\n", nil]`},
		{cap + `warn([]); p $c`, `nil`},
		// NUM2LONG takes the level, so a Float or Rational truncates.
		{cap + `warn("x", uplevel: 0.9); p $c`, `["warning: x\n", nil]`},
		// A Warning.warn that takes exactly one argument is called without the
		// category keyword (rb_warn_category's rb_warning_warn_arity() == 1).
		{`$c = nil; Warning.singleton_class.send(:define_method, :warn) { |m| $c = m }; warn("x"); p $c`, `"x\n"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
