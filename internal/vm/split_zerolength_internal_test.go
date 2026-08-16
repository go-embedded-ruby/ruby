// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringSplitZeroLength covers String#split where the pattern matches a
// zero-length string (an empty Regexp, an empty-string argument, or an empty
// capture group): a zero-width match at end-of-string closes the final character
// field and leaves a trailing empty field, which is kept for a negative or
// large-enough positive limit and stripped by the default limit 0. Verified
// against ruby 4.0.6.
func TestStringSplitZeroLength(t *testing.T) {
	cases := []struct{ src, want string }{
		// Empty Regexp: default strips the trailing empty; -1 keeps it.
		{`p "hello".split(//)`, `["h", "e", "l", "l", "o"]`},
		{`p "hello".split(//, -1)`, `["h", "e", "l", "l", "o", ""]`},
		{`p "hello".split(//, 0)`, `["h", "e", "l", "l", "o"]`},
		{`p "hello".split(//, 5)`, `["h", "e", "l", "l", "o"]`},
		{`p "hello".split(//, 6)`, `["h", "e", "l", "l", "o", ""]`},
		{`p "hello".split(//, 2)`, `["h", "ello"]`},
		{`p "hello".split(//, 1)`, `["hello"]`},
		// Empty-string argument behaves the same.
		{`p "hi!".split("")`, `["h", "i", "!"]`},
		{`p "hi!".split("", -1)`, `["h", "i", "!", ""]`},
		{`p "hi!".split("", 4)`, `["h", "i", "!", ""]`},
		// Empty capture group: captures interleave, trailing empties follow the limit.
		{`p "hi!".split(/()/)`, `["h", "", "i", "", "!"]`},
		{`p "hi!".split(/()/, -1)`, `["h", "", "i", "", "!", "", ""]`},
		// A non-empty capture case is unchanged.
		{`p "hello".split(/(el)/)`, `["h", "el", "lo"]`},
		{`p "AabB".split(/([a-z])+/)`, `["A", "b", "B"]`},
		// A non-empty delimiter ending at end-of-string still trails per limit.
		{`p "a,b,".split(",")`, `["a", "b"]`},
		{`p "a,b,".split(",", -1)`, `["a", "b", ""]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
