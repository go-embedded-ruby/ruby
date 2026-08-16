// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringTimes covers String#* (repeat): MRI's NUM2LONG count coercion (Float
// truncates, a #to_int object is honoured, a Bignum that fits a long converts
// while a genuinely huge one raises RangeError), the negative-count ArgumentError,
// the too-big-result ArgumentError, and the empty-receiver short-circuit (which
// must not loop for a huge count). Verified against ruby 4.0.6.
func TestStringTimes(t *testing.T) {
	cases := []struct{ src, want string }{
		// Basic repeats.
		{`p("cool" * 0)`, `""`},
		{`p("cool" * 1)`, `"cool"`},
		{`p("cool" * 3)`, `"coolcoolcool"`},
		// Float count truncates toward zero.
		{`p("cool" * 3.1)`, `"coolcoolcool"`},
		{`p("a" * 3.999)`, `"aaa"`},
		// A non-Integer/#to_int object is coerced.
		{`o = Object.new; def o.to_int; 4; end; p("a" * o)`, `"aaaa"`},
		// Empty receiver short-circuits for any in-range count (no looping).
		{`p("" * (2**63 - 1) == "")`, "true"},
		{`p("" * 0)`, `""`},
		// Negative count raises ArgumentError (Integer, Float, and a boxed -2**63).
		{`p(("cool" * -3) rescue $!.class)`, "ArgumentError"},
		{`p(("cool" * -3.14) rescue $!.class)`, "ArgumentError"},
		{`p(("cool" * (-(2**63))) rescue $!.class)`, "ArgumentError"},
		// The minimum-long literal reaches the operator boxed as a Bignum that still
		// fits a machine long: it converts, then the negative check raises.
		{`p(("cool" * -0x8000_0000_0000_0000) rescue $!.class)`, "ArgumentError"},
		// A genuinely out-of-range Bignum raises RangeError.
		{`p(("cool" * 999999999999999999999) rescue $!.class)`, "RangeError"},
		{`p(("" * 999999999999999999999) rescue $!.class)`, "RangeError"},
		// A result too large for a machine long raises ArgumentError, not a panic.
		{`p(("abc" * (2**63 - 1)) rescue $!.class)`, "ArgumentError"},
		// A non-finite Float count is out of integer range (FloatDomainError, a
		// RangeError); assert the family so the exact class need not be pinned.
		{`p(("a" * (0.0 / 0.0))  rescue $!.is_a?(RangeError))`, "true"},
		{`p(("a" * (1.0 / 0.0))  rescue $!.is_a?(RangeError))`, "true"},
		{`p(("a" * (-1.0 / 0.0)) rescue $!.is_a?(RangeError))`, "true"},
		// An object with no integer coercion raises TypeError (the default path).
		{`p(("a" * :sym) rescue $!.class)`, "TypeError"},
		// The result keeps the receiver's encoding.
		{`p(("\xE3\x81\x82".dup.force_encoding(Encoding::UTF_8) * 2).encoding)`, "#<Encoding:UTF-8>"},
		// A String subclass repeat returns a base String instance.
		{`class MyS < String; end; p((MyS.new("cool") * 2).instance_of?(String))`, "true"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
