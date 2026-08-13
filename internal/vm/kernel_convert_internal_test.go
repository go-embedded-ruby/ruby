// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestKernelIntegerConvert covers Kernel#Integer: the direct numeric/String
// cases, the String radix and underscore rules (including an explicit base,
// which Go's ParseInt cannot handle alone), out-of-int64 values promoting to a
// Bignum, and MRI's #to_int → #to_str → #to_i coercion protocol for any other
// object. Every expected value was verified against ruby 4.0.6 (aarch64-mingw).
func TestKernelIntegerConvert(t *testing.T) {
	values := []struct{ src, want string }{
		{`p Integer(42)`, "42"},
		{`p Integer(3.9)`, "3"},   // truncates toward zero
		{`p Integer(-2.5)`, "-2"}, // "
		{`p Integer(2e100)`, "20000000000000000318057822195198360936721617127890562779562655115495677544340762121626939971713630208"}, // huge Float → Bignum
		{`p Integer(10**40)`, "10000000000000000000000000000000000000000"},                                                       // Bignum passes through
		{`p Integer("11")`, "11"},
		{`p Integer("0xff")`, "255"},   // 0x auto-detected at base 0
		{`p Integer("0b101")`, "5"},    // 0b
		{`p Integer("0o17")`, "15"},    // 0o
		{`p Integer("1_1")`, "11"},     // underscore, base 0
		{`p Integer("1_1", 10)`, "11"}, // underscore, explicit base
		{`p Integer("777", 8)`, "511"},
		{`p Integer("z", 36)`, "35"},
		{`p Integer("9" * 30)`, "999999999999999999999999999999"},                                     // out of int64 → Bignum
		{`o = Object.new; def o.to_int; 42; end; p Integer(o)`, "42"},                                 // #to_int
		{`o = Object.new; def o.to_i; 7; end; p Integer(o)`, "7"},                                     // #to_i
		{`o = Object.new; def o.to_int; nil; end; def o.to_i; 5; end; p Integer(o)`, "5"},             // to_int nil → to_i
		{`o = Object.new; def o.to_str; "55"; end; p Integer(o)`, "55"},                               // #to_str parsed
		{`o = Object.new; def o.to_int; "x"; end; def o.to_i; 9; end; p Integer(o)`, "9"},             // to_int non-Integer → to_i
		{`p Integer("x", exception: false)`, "nil"},                                                   // exception: false swallows
		{`p Integer(nil, exception: false)`, "nil"},                                                   // "
	}
	for _, c := range values {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}

	errs := []struct{ src, class, msg string }{
		{`Integer(nil)`, "TypeError", "can't convert nil into Integer"},
		{`Integer(true)`, "TypeError", "can't convert true into Integer"},
		{`Integer(:foo)`, "TypeError", "can't convert Symbol into Integer"},
		{`Integer(Object.new)`, "TypeError", "can't convert Object into Integer"},
		{`Integer("bad")`, "ArgumentError", `invalid value for Integer(): "bad"`},
		{`Integer("_1")`, "ArgumentError", `invalid value for Integer(): "_1"`},
		{`Integer("1__1", 10)`, "ArgumentError", `invalid value for Integer(): "1__1"`},
		{`Integer(5, 10)`, "ArgumentError", "base specified for non string value"},
		{`Integer(nil, 10)`, "TypeError", "can't convert nil into Integer"}, // nil beats the base check
		{`Integer("11", 99)`, "ArgumentError", "invalid radix 99"},
		{`o = Object.new; def o.to_str; "bad"; end; Integer(o)`, "ArgumentError", `invalid value for Integer(): "bad"`}, // #to_str result parsed and rejected
		{`o = Object.new; def o.to_i; "x"; end; Integer(o)`, "TypeError", "can't convert Object to Integer (Object#to_i gives String)"},
		{`o = Object.new; def o.to_int; "x"; end; def o.to_i; nil; end; Integer(o)`, "TypeError", "can't convert Object to Integer (Object#to_i gives NilClass)"},
		{`o = Object.new; def o.to_int; nil; end; Integer(o)`, "TypeError", "can't convert Object into Integer"},
	}
	for _, c := range errs {
		class, msg := evalErr(t, c.src)
		if class != c.class || msg != c.msg {
			t.Errorf("src=%q got %s:%q want %s:%q", c.src, class, msg, c.class, c.msg)
		}
	}
}

// TestKernelFloatConvert covers Kernel#Float: the numeric/String cases, an
// out-of-range literal yielding ±Infinity rather than an error, a Bignum
// argument, and the #to_f coercion protocol. Verified against ruby 4.0.6.
func TestKernelFloatConvert(t *testing.T) {
	values := []struct{ src, want string }{
		{`p Float(3.14)`, "3.14"},
		{`p Float(5)`, "5.0"},
		{`p Float(10**40).infinite?`, "nil"}, // finite Bignum → finite Float
		{`p Float("1.5")`, "1.5"},
		{`p Float("1_000.5")`, "1000.5"},       // underscores
		{`p Float("1e400")`, "Infinity"},       // overflow → Infinity, not an error
		{`o = Object.new; def o.to_f; 2.5; end; p Float(o)`, "2.5"}, // #to_f
		{`p Float("x", exception: false)`, "nil"},
	}
	for _, c := range values {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}

	errs := []struct{ src, class, msg string }{
		{`Float(nil)`, "TypeError", "can't convert nil into Float"},
		{`Float(false)`, "TypeError", "can't convert false into Float"},
		{`Float(Object.new)`, "TypeError", "can't convert Object into Float"},
		{`Float("bad")`, "ArgumentError", `invalid value for Float(): "bad"`},
	}
	for _, c := range errs {
		class, msg := evalErr(t, c.src)
		if class != c.class || msg != c.msg {
			t.Errorf("src=%q got %s:%q want %s:%q", c.src, class, msg, c.class, c.msg)
		}
	}
}

// TestKernelFloatHexString covers Kernel#Float's hexadecimal string form: a bare
// hex float (no binary exponent), a fractional part, a sign, a p-exponent, and
// the digit-separator underscore — legal strictly between two hex digits and
// rejected next to the 0x prefix or the p exponent (even though Go's ParseFloat
// would accept a prefix-adjacent one). Verified against ruby 4.0.6.
func TestKernelFloatHexString(t *testing.T) {
	values := []struct{ src, want string }{
		{`p Float("0x10")`, "16.0"},   // bare hex float (== 0x10p0)
		{`p Float("0x0.8")`, "0.5"},   // fractional part
		{`p Float("-0x10")`, "-16.0"}, // sign
		{`p Float("0x1p4")`, "16.0"},  // explicit binary exponent
		{`p Float("0x1.8p1")`, "3.0"},
		{`p Float("0x1_0")`, "16.0"},   // underscore between hex digits
		{`p Float("0xa_b")`, "171.0"},  // underscore between a-f digits
		{`p Float("0x1_0a")`, "266.0"}, // ruby 3.4.3+: underscore allowed with a-f
	}
	for _, c := range values {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}

	// Misplaced underscores raise, even the 0x-prefix-adjacent one ParseFloat alone
	// would accept.
	for _, src := range []string{
		`Float("0x_1")`, `Float("0x_10p10")`, `Float("0x10p_10")`, `Float("0x1_p0")`,
	} {
		if class, _ := evalErr(t, src); class != "ArgumentError" {
			t.Errorf("src=%q got %s want ArgumentError", src, class)
		}
	}
}
