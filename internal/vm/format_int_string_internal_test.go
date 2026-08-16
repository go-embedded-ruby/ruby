// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestFormatIntegerStringArg covers a String operand to the integer format
// directives (%d/%i/%u/%o/%b/%x/%X): MRI parses it exactly as Kernel#Integer
// (base 0, prefix-detected radix, digit-separator underscores legal only between
// two digits), so a valid literal converts and a malformed one raises
// ArgumentError — rather than the former blind underscore strip, which accepted
// "123__456". Verified against ruby 4.0.6.
func TestFormatIntegerStringArg(t *testing.T) {
	cases := []struct{ src, want string }{
		// Valid literals, radix detected from the prefix (not the directive).
		{`p "%d" % "10"`, `"10"`},
		{`p "%d" % "0777"`, `"511"`},        // leading 0 -> octal
		{`p "%d" % "0x42"`, `"66"`},         // hex
		{`p "%d" % "0b1101"`, `"13"`},       // binary
		{`p "%d" % "0b1101_0000"`, `"208"`}, // underscore between digits
		{`p "%d" % "1_000"`, `"1000"`},
		{`p "%x" % "10"`, `"a"`}, // string parsed as Integer(10), then hex-formatted
		{`p "%o" % "8"`, `"10"`},
		// A Float truncates toward zero (not Integer() parsing).
		{`p "%d" % 3.9`, `"3"`},
		// An object uses #to_int then #to_i (not Integer() parsing).
		{`o = Object.new; def o.to_int; 6; end; p "%d" % o`, `"6"`},
		// Malformed literals raise ArgumentError like Kernel#Integer.
		{`p ("%d" % "123__456") rescue p $!.class`, "ArgumentError"},
		{`p ("%d" % "0__7_7_7") rescue p $!.class`, "ArgumentError"},
		{`p ("%d" % "_1") rescue p $!.class`, "ArgumentError"},
		{`p ("%d" % "1_") rescue p $!.class`, "ArgumentError"},
		{`p ("%d" % "") rescue p $!.class`, "ArgumentError"},
		{`p ("%d" % "x") rescue p $!.class`, "ArgumentError"},
		{`p ("%d" % "5x") rescue p $!.class`, "ArgumentError"},
		{`p ("%d" % "08") rescue p $!.class`, "ArgumentError"},
		{`p ("%b" % "0b2") rescue p $!.class`, "ArgumentError"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
