// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringElementSet covers String#[]= across every target form: index,
// index+length, Range, Regexp (with a capture group) and a substring, plus
// #to_int coercion of indices/bounds, #to_str coercion of the value, the
// append-at-length case, and the IndexError cases. Verified against ruby 4.0.6.
func TestStringElementSet(t *testing.T) {
	cases := []struct{ src, want string }{
		// Index and index+length.
		{`s = "hello"; s[1] = "E"; p s`, `"hEllo"`},
		{`s = "hello"; s[-1] = "O"; p s`, `"hellO"`},
		{`s = "hello"; s[1, 3] = "X"; p s`, `"hXo"`},
		{`s = "abc"; s[3] = "d"; p s`, `"abcd"`}, // index == length appends
		{`s = ""; s[0] = "bam"; p s`, `"bam"`},   // empty string, index 0
		{`s = ""; s[0, 0] = "ab"; p s`, `"ab"`},  // zero-length insert
		// Range (bounds convert via #to_int).
		{`s = "hello"; s[1..3] = "Y"; p s`, `"hYo"`},
		{`s = "hello"; o = Object.new; def o.to_int; 3; end; s[1..o] = "Y"; p s`, `"hYo"`},
		// #to_int index and #to_str value.
		{`s = "hello"; o = Object.new; def o.to_int; 1; end; s[o] = "E"; p s`, `"hEllo"`},
		{`s = "hello"; v = Object.new; def v.to_str; "Z"; end; s[0] = v; p s`, `"Zello"`},
		// Regexp: whole match, capture group; $~ is set.
		{`s = "hello"; s[/l+/] = "X"; p s`, `"heXo"`},
		{`s = "hello"; s[/(l)(l)/, 2] = "Y"; p s`, `"helYo"`},
		{`s = "hello"; s[/l+/] = "X"; p $~[0]`, `"ll"`},
		// Substring form replaces the first occurrence.
		{`s = "hello"; s["ll"] = "LL"; p s`, `"heLLo"`},
		// A String subclass selector is accepted.
		{`class MyS < String; end; s = "hello"; s[MyS.new("ll")] = "LL"; p s`, `"heLLo"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// IndexError / TypeError cases across every branch.
	errs := []struct{ src, cls string }{
		{`s = "abc"; s[4] = "x"`, "IndexError"},                       // index past the end
		{`s = "abc"; s[1, -1] = "x"`, "IndexError"},                   // negative length
		{`s = "abc"; s["z"] = "x"`, "IndexError"},                     // substring not found
		{`s = "abc"; s[/z/] = "x"`, "IndexError"},                     // regexp not matched
		{`s = "aaa bbb ccc"; s[/a (bbb) c/, 2] = "d"`, "IndexError"},  // capture out of range
		{`s = "aaa bbb ccc"; s[/a (bbb) c/, -2] = "d"`, "IndexError"}, // negative capture past 0
		{`s = "xb"; s[/(a)|b/, 1] = "x"`, "IndexError"},               // in-range but non-participating group
		{`s = "abc"; s[0] = 5`, "TypeError"},                          // value not String-coercible
		{`s = "abc".freeze; s[0] = "x"`, "FrozenError"},               // frozen receiver
	}
	for _, c := range errs {
		if got := eval(t, `p ((`+c.src+`; :no) rescue $!.class)`); got != c.cls+"\n" {
			t.Errorf("%s: got=%q want %s", c.src, got, c.cls)
		}
	}
}
