// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringPlus covers String#+: concatenation, #to_str coercion of a
// non-String argument, encoding negotiation (a compatible result, and
// Encoding::CompatibilityError when incompatible), and the TypeError/NoMethodError
// argument cases. Verified against ruby 4.0.6.
func TestStringPlus(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "a" + "b"`, `"ab"`},
		{`p ("a" + "b").encoding == Encoding::UTF_8`, `true`},
		// A non-String argument converts via #to_str.
		{`o = Object.new; def o.to_str; "Z"; end; p "a" + o`, `"aZ"`},
		// An ASCII-only operand adopts the other's (ASCII-compatible) encoding.
		{`("x".b + "y").encoding.to_s == "ASCII-8BIT" ? (p true) : (p false)`, `true`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// Error cases.
	errs := []struct{ src, cls string }{
		{`"a" + 5`, "TypeError"}, // non-String, no #to_str
		{`o = Object.new; def o.to_str; 5; end; "a" + o`, "TypeError"}, // #to_str returns non-String
		{`"あ" + "れ".encode(Encoding::EUC_JP)`, "Encoding::CompatibilityError"},
	}
	for _, c := range errs {
		if got := eval(t, `p ((`+c.src+`; :no) rescue $!.class)`); got != c.cls+"\n" {
			t.Errorf("%s: got=%q want %s", c.src, got, c.cls)
		}
	}
	// A NoMethodError raised inside #to_str propagates.
	if got := eval(t, `o = Object.new; def o.to_str; nope; end
	                   p (("a" + o; :no) rescue $!.class)`); got != "NoMethodError\n" {
		t.Errorf("to_str NoMethodError: got=%q", got)
	}
}
