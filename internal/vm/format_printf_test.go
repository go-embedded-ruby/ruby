// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestKernelPrintf covers Kernel#printf: writing to the current $stdout when the
// first argument is a String, writing to an explicit IO/StringIO target
// otherwise, and coercing the format argument with #to_str — all asserted
// against MRI Ruby 3.4.
func TestKernelPrintf(t *testing.T) {
	cases := []struct{ src, want string }{
		// String first argument: format the rest and write to $stdout.
		{`printf("%d\n", 42)`, "42\n"},
		{`printf("%05.2f|%s", 3.1, "x")`, "03.10|x"},
		// A reassigned $stdout (StringIO) captures the output.
		{`require "stringio"
old = $stdout; $stdout = StringIO.new(+"")
printf("%x", 255)
s = $stdout.string; $stdout = old
p s`, "\"ff\"\n"},
		// Explicit IO target: the first non-String argument receives #write.
		{`require "stringio"
io = StringIO.new(+"")
printf(io, "%b", 10)
p io.string`, "\"1010\"\n"},
		// The format argument is coerced with #to_str.
		{`require "stringio"
obj = Object.new; def obj.to_str; "to_str: %i"; end
io = StringIO.new(+"")
printf(io, obj, 42)
p io.string`, "\"to_str: 42\"\n"},
		// No arguments returns nil and writes nothing.
		{`p printf`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKernelPrintfErrors covers printf's error branches: an IO target with no
// format string (ArgumentError).
func TestKernelPrintfErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "stringio"
io = StringIO.new(+"")
begin; printf(io); rescue ArgumentError; puts "AE"; end`, "AE\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFormatStringCoercion covers coerceFormatString for Kernel#sprintf/#format:
// a #to_str format object is accepted, a non-String #to_str result and an object
// answering neither raise TypeError, and a zero-argument call is an
// ArgumentError.
func TestFormatStringCoercion(t *testing.T) {
	cases := []struct{ src, want string }{
		// #to_str format object is honoured.
		{`obj = Object.new; def obj.to_str; "n=%d"; end; p(sprintf(obj, 7))`, "\"n=7\"\n"},
		{`obj = Object.new; def obj.to_str; "n=%d"; end; p(format(obj, 7))`, "\"n=7\"\n"},
		// #to_str returning a non-String raises TypeError.
		{`obj = Object.new; def obj.to_str; 42; end
begin; sprintf(obj); rescue TypeError; puts "TE"; end`, "TE\n"},
		// An object answering neither String nor #to_str raises TypeError.
		{`obj = Object.new
begin; sprintf(obj); rescue TypeError; puts "TE"; end`, "TE\n"},
		// A non-String, non-object format (Integer) raises TypeError.
		{`begin; sprintf(42); rescue TypeError; puts "TE"; end`, "TE\n"},
		// Zero arguments is an ArgumentError.
		{`begin; sprintf; rescue ArgumentError; puts "AE"; end`, "AE\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
