// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestFormatArgumentCoercion covers the VM-aware argument coercion of
// Kernel#sprintf / Kernel#format / String#% / IO#printf: %s#to_s, %p#inspect,
// the integer verbs' #to_int/#to_i, the float verbs' #to_f, and %{name}#to_s —
// all asserted against MRI Ruby 3.4.
func TestFormatArgumentCoercion(t *testing.T) {
	cases := []struct{ src, want string }{
		// %s calls #to_s (not #to_str).
		{`o = Object.new; def o.to_s; "abc"; end; p("%s" % o)`, "\"abc\"\n"},
		{`o = Object.new; def o.to_s; "abc"; end; p(sprintf("%s", o))`, "\"abc\"\n"},
		{`o = Object.new; def o.to_s; "abc"; end; p(format("[%5s]", o))`, "\"[  abc]\"\n"},
		// %p calls #inspect.
		{`o = Object.new; def o.inspect; "<I>"; end; p("%p" % o)`, "\"<I>\"\n"},
		// Integer verbs coerce via #to_int first.
		{`o = Object.new; def o.to_int; 10; end; def o.to_i; 99; end; p("%d" % o)`, "\"10\"\n"},
		{`o = Object.new; def o.to_int; 10; end; p("%b" % o)`, "\"1010\"\n"},
		{`o = Object.new; def o.to_int; 255; end; p("%x" % o)`, "\"ff\"\n"},
		{`o = Object.new; def o.to_int; 8; end; p("%o" % o)`, "\"10\"\n"},
		// #to_i is used when #to_int is unavailable.
		{`o = Object.new; def o.to_i; 10; end; p("%b" % o)`, "\"1010\"\n"},
		// Float verbs coerce via #to_f.
		{`o = Object.new; def o.to_f; 9.6; end; p("%f" % o)`, "\"9.600000\"\n"},
		{`o = Object.new; def o.to_f; 1.5; end; p("%.2e" % o)`, "\"1.50e+00\"\n"},
		// %{name} converts the hash value with #to_s.
		{`o = Object.new; def o.to_s; "42"; end; p("%{k}" % {k: o})`, "\"42\"\n"},
		{`p("%{k}" % {k: 42})`, "\"42\"\n"},
		// Native operands keep exact rendering (fast path, no dispatch).
		{`p("%d %s %.1f" % [7, "hi", 3.25])`, "\"7 hi 3.2\"\n"},
		// A coercion method returning a non-String renders that value (fallback path).
		{`o = Object.new; def o.inspect; 42; end; p("%p" % o)`, "\"42\"\n"},
		// #to_f returning an Integer is accepted for float verbs.
		{`o = Object.new; def o.to_f; 3; end; p("%.1f" % o)`, "\"3.0\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFormatCoercionErrors covers the TypeError paths of format argument
// coercion: an integer verb whose #to_int/#to_i returns a non-Integer, and a
// float verb whose #to_f returns a non-numeric.
func TestFormatCoercionErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`o = Object.new; def o.to_int; :nope; end
begin; "%d" % o; rescue TypeError; puts "TE"; end`, "TE\n"},
		{`o = Object.new; def o.to_i; :nope; end
begin; "%d" % o; rescue TypeError; puts "TE"; end`, "TE\n"},
		{`o = Object.new; def o.to_f; "nope"; end
begin; "%f" % o; rescue TypeError; puts "TE"; end`, "TE\n"},
		// An object answering none of the coercions is a TypeError (no implicit conversion).
		{`o = Object.new
begin; "%d" % o; rescue TypeError; puts "TE"; end`, "TE\n"},
		{`o = Object.new
begin; "%f" % o; rescue TypeError; puts "TE"; end`, "TE\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
