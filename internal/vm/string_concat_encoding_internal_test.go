// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringConcatEncoding covers String#<< / #concat: encoding negotiation
// (a compatible result, and Encoding::CompatibilityError when incompatible), an
// Integer codepoint argument (encoded per the receiver's encoding, with a
// US-ASCII receiver promoting to ASCII-8BIT for a 128..255 byte and a RangeError
// for an out-of-range codepoint), a #to_str argument, a String subclass, and
// #<<'s single-argument arity. Verified against ruby 4.0.6.
func TestStringConcatEncoding(t *testing.T) {
	trueCases := []string{
		// Two ASCII-only strings of different encodings combine (result UTF-8).
		`("abc".dup << "def".b) == "abcdef"`,
		`("a".dup << "b").encoding == Encoding::UTF_8`,
		// A US-ASCII receiver stays US-ASCII appending ASCII, becomes UTF-8 with é.
		`(("a".dup << 98) == "ab")`,
		// An Integer codepoint is encoded in the receiver's encoding.
		`("x".dup << 0x263a) == "x☺"`,
		// A US-ASCII receiver promotes to ASCII-8BIT for a high byte.
		`"a".dup.force_encoding("US-ASCII").tap { |s| s << 200 }.encoding == Encoding::BINARY`,
		// A #to_str argument is accepted.
		`(o = Object.new; def o.to_str; "Z"; end; "x".dup << o) == "xZ"`,
		// A String subclass instance argument is unwrapped to its value.
		`(k = Class.new(String); ("x".dup << k.new("y")) == "xy")`,
		// concat takes several arguments; << chains.
		`("a".dup.concat("b", "c")) == "abc"`,
		`("a".dup << "b" << "c") == "abc"`,
	}
	for _, src := range trueCases {
		if got := eval(t, "p ("+src+")"); got != "true\n" {
			t.Errorf("src=%q got=%q want true", src, got)
		}
	}
	// Error cases.
	errs := []struct{ src, cls string }{
		{`"é".dup << "\xff".b`, "Encoding::CompatibilityError"},             // incompatible non-ASCII
		{`"x".dup << 0x110000`, "RangeError"},                               // codepoint out of range
		{`"x".dup << -1`, "RangeError"},                                     // negative codepoint
		{`"x".dup << (2 ** 70)`, "RangeError"},                              // bignum codepoint
		{`"x".dup << :sym`, "TypeError"},                                    // no #to_str
		{`o = Object.new; def o.to_str; 5; end; "x".dup << o`, "TypeError"}, // #to_str returns non-String
		{`"x".dup.<<("a", "b")`, "ArgumentError"},                           // << arity
		{`"x".dup.prepend(5)`, "TypeError"},                                 // #prepend rejects a non-String
	}
	for _, c := range errs {
		if got := eval(t, `p ((`+c.src+`; :no) rescue $!.class)`); got != c.cls+"\n" {
			t.Errorf("%s: got=%q want %s", c.src, got, c.cls)
		}
	}
	// A NoMethodError raised inside #to_str propagates (it is not swallowed as a
	// TypeError).
	if got := eval(t, `o = Object.new; def o.to_str; nope; end
	                   p (("x".dup << o; :no) rescue $!.class)`); got != "NoMethodError\n" {
		t.Errorf("to_str NoMethodError: got=%q", got)
	}
	// A frozen receiver raises FrozenError.
	if got := eval(t, `p (("x".freeze << "y"; :no) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen <<: got=%q", got)
	}
}
