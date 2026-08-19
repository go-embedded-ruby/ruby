// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringJustify covers String#ljust / #rjust / #center: default and custom
// padding, the width #to_int coercion, the pad-string #to_str coercion (and a
// String subclass pad), the no-padding-needed case, encoding preservation and the
// zero-width-padding ArgumentError. Verified against ruby 4.0.6.
func TestStringJustify(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "x".ljust(4)`, `"x   "`},
		{`p "x".rjust(4)`, `"   x"`},
		{`p "x".center(5)`, `"  x  "`},
		{`p "x".ljust(4, "-")`, `"x---"`},
		{`p "x".rjust(4, "ab")`, `"abax"`},
		{`p "x".center(5, "-")`, `"--x--"`},
		{`p "hello".ljust(3)`, `"hello"`}, // width <= length returns the receiver's content
		// Width converts via #to_int; a Float truncates.
		{`p "x".ljust(3.8)`, `"x  "`},
		{`o = Object.new; def o.to_int; 4; end; p "x".ljust(o)`, `"x   "`},
		// The pad string converts via #to_str (a String subclass too).
		{`o = Object.new; def o.to_str; "ab"; end; p "x".rjust(5, o)`, `"ababx"`},
		{`class MyS < String; end; p "x".ljust(4, MyS.new("."))`, `"x..."`},
		// The result keeps the receiver's encoding.
		{`p "x".b.ljust(3).encoding.to_s`, `"ASCII-8BIT"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// An empty pad string raises ArgumentError.
	if got := eval(t, `p (("x".ljust(3, ""); :no) rescue $!.class)`); got != "ArgumentError\n" {
		t.Errorf("zero-width pad: got=%q want ArgumentError", got)
	}
}

// TestStringInsert covers String#insert: positive and negative indices, the
// index #to_int and value #to_str coercion (with the value converted before the
// index is range-checked), the out-of-range IndexError and the frozen check.
// Verified against ruby 4.0.6.
func TestStringInsert(t *testing.T) {
	cases := []struct{ src, want string }{
		{`s = "abc"; s.insert(1, "X"); p s`, `"aXbc"`},
		{`s = "abc"; s.insert(-1, "X"); p s`, `"abcX"`},
		{`s = "abc"; s.insert(-2, "X"); p s`, `"abXc"`},
		{`s = "abc"; p s.insert(0, "Z")`, `"Zabc"`}, // returns self
		{`o = Object.new; def o.to_int; 1; end; s = "abc"; s.insert(o, "X"); p s`, `"aXbc"`},
		{`o = Object.new; def o.to_str; "X"; end; s = "abc"; s.insert(1, o); p s`, `"aXbc"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// An out-of-range index raises IndexError; a non-String value that cannot be
	// converted raises TypeError even when the index is also out of range.
	if got := eval(t, `p (("abc".insert(5, "x"); :no) rescue $!.class)`); got != "IndexError\n" {
		t.Errorf("insert out of range: got=%q want IndexError", got)
	}
	if got := eval(t, `p (("abc".insert(-6, "x"); :no) rescue $!.class)`); got != "IndexError\n" {
		t.Errorf("insert negative out of range: got=%q want IndexError", got)
	}
	if got := eval(t, `p (("abc".insert(1, 5); :no) rescue $!.class)`); got != "TypeError\n" {
		t.Errorf("insert bad value: got=%q want TypeError", got)
	}
	if got := eval(t, `p (("abc".freeze.insert(0, "x"); :no) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen insert: got=%q want FrozenError", got)
	}
}
