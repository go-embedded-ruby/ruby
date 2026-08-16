// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringSplitArgCoercion covers String#split argument coercion: a pattern
// that is neither a String, a Regexp nor nil is converted with #to_str, and the
// limit with #to_int. A nil limit is not a default — MRI raises TypeError — and
// a limit with no integer conversion raises TypeError too. Verified against
// ruby 4.0.6.
func TestStringSplitArgCoercion(t *testing.T) {
	cases := []struct{ src, want string }{
		// Pattern via #to_str.
		{`o = Object.new; def o.to_str; "::"; end; p "hello::world".split(o)`, `["hello", "world"]`},
		// Limit via #to_int (String and Regexp pattern forms).
		{`o = Object.new; def o.to_int; 2; end; p "1.2.3.4".split(".", o)`, `["1", "2.3.4"]`},
		{`o = Object.new; def o.to_int; 2; end; p "1a2a3".split(/a/, o)`, `["1", "2a3"]`},
		// A String/Regexp/nil pattern is left as-is (no coercion attempt).
		{`p "a:b".split(":")`, `["a", "b"]`},
		{`p "a1b".split(/\d/)`, `["a", "b"]`},
		{`p "a b".split(nil)`, `["a", "b"]`},
		// An Integer limit passes straight through.
		{`p "a.b.c".split(".", 2)`, `["a", "b.c"]`},
		// A nil limit raises TypeError (it is not a default).
		{`p ("a.b".split(".", nil)) rescue p $!.class`, "TypeError"},
		// A limit with no integer conversion raises TypeError.
		{`p ("a.b".split(".", "three")) rescue p $!.class`, "TypeError"},
		// A pattern that is neither String/Regexp/nil nor #to_str-able raises TypeError.
		{`p ("a.b".split(Object.new)) rescue p $!.class`, "TypeError"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
