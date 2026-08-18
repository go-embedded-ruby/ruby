// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringSliceBang covers String#slice! across every argument form: index,
// index+length, Range, Regexp (with a capture group), and a substring, plus the
// #to_int coercion of indices/bounds, encoding preservation, and $~. It also
// checks String#slice is a true alias of #[]. Verified against ruby 4.0.6.
func TestStringSliceBang(t *testing.T) {
	cases := []struct{ src, want string }{
		// #slice is the same method record as #[].
		{`p String.instance_method(:slice) == String.instance_method(:[])`, "true"},
		// Index and index+length.
		{`s = "hello"; r = s.slice!(1); p r; p s`, `"e"` + "\n" + `"hllo"`},
		{`s = "hello"; r = s.slice!(-1); p r; p s`, `"o"` + "\n" + `"hell"`},
		{`s = "hello"; r = s.slice!(1, 2); p r; p s`, `"el"` + "\n" + `"hlo"`},
		{`s = "hello"; p s.slice!(9); p s`, "nil\n" + `"hello"`},
		// Range (bounds convert via #to_int through coerceRangeBounds).
		{`s = "hello"; r = s.slice!(1..3); p r; p s`, `"ell"` + "\n" + `"ho"`},
		{`s = "hello"; o = Object.new; def o.to_int; 3; end; p s.slice!(1..o); p s`, `"ell"` + "\n" + `"ho"`},
		// #to_int index.
		{`s = "hello"; o = Object.new; def o.to_int; 1; end; p s.slice!(o); p s`, `"e"` + "\n" + `"hllo"`},
		// Regexp: whole match and a capture group; $~ is set.
		{`s = "hello world"; r = s.slice!(/o.*o/); p r; p s`, `"o wo"` + "\n" + `"hellrld"`},
		{`s = "hello"; r = s.slice!(/(l)(l)/, 2); p r; p s`, `"l"` + "\n" + `"helo"`},
		{`s = "hello"; s.slice!(/l+/); p $~[0]`, `"ll"`},
		// Regexp with no match returns nil and clears $~.
		{`s = "abc"; p s.slice!(/z/); p $~`, "nil\nnil"},
		// A capture index out of range, or a negative index reaching group 0, is nil.
		{`s = "hello there"; p s.slice!(/[aeiou](.)\1/, 2); p s`, "nil\n" + `"hello there"`},
		{`s = "hello there"; p s.slice!(/[aeiou](.)\1/, -2); p s`, "nil\n" + `"hello there"`},
		{`s = "hello"; o = Object.new; def o.to_int; 1; end; p s.slice!(/(l)(l)/, o); p s`, `"l"` + "\n" + `"helo"`},
		// An in-range but non-participating capture group returns "" (deletes nothing).
		{`s = "xb"; r = s.slice!(/(a)|b/, 1); p r; p s`, `""` + "\n" + `"xb"`},
		// Substring form deletes the first occurrence, or nil when absent.
		{`s = "hello world"; r = s.slice!("o w"); p r; p s`, `"o w"` + "\n" + `"hellorld"`},
		{`s = "hello"; p s.slice!("z"); p s`, "nil\n" + `"hello"`},
		// A String subclass argument is accepted and returns a plain String.
		{`class MyS < String; end; s = "hello"; r = s.slice!(MyS.new("el")); p r; p r.class; p s`, `"el"` + "\nString\n" + `"hlo"`},
		// A binary string slices by bytes and keeps ASCII-8BIT.
		{`s = "abc".b; r = s.slice!(1); p r.encoding.to_s; p s`, `"ASCII-8BIT"` + "\n" + `"ac"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// slice! on a frozen string raises FrozenError.
	if got := eval(t, `p (("x".freeze.slice!(0); :no) rescue $!.class)`); got != "FrozenError\n" {
		t.Errorf("frozen slice!: got=%q want FrozenError", got)
	}
	// A capture index with no #to_int raises TypeError.
	if got := eval(t, `p (("hello".slice!(/l/, :x); :no) rescue $!.class)`); got != "TypeError\n" {
		t.Errorf("bad capture index: got=%q want TypeError", got)
	}
}
