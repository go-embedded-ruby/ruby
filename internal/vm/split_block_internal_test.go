// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringSplitBlock covers String#split with a block: it yields each split
// substring (honouring the pattern and limit) and returns the receiver itself,
// rather than the array. An empty receiver yields nothing and still returns
// self. Without a block the array is returned unchanged. Verified against
// ruby 4.0.6.
func TestStringSplitBlock(t *testing.T) {
	cases := []struct{ src, want string }{
		// Yields each substring; whitespace default.
		{`a = []; "chunky bacon".split { |s| a << s }; p a`, `["chunky", "bacon"]`},
		// Returns the receiver (self), not the array.
		{`p "chunky bacon".split { |s| }`, `"chunky bacon"`},
		// Empty pattern splits into characters.
		{`a = []; "chunky".split("") { |s| a << s }; p a`, `["c", "h", "u", "n", "k", "y"]`},
		// String and Regexp patterns.
		{`a = []; "a:b:c".split(":") { |s| a << s }; p a`, `["a", "b", "c"]`},
		{`a = []; "a1b2c".split(/\d/) { |s| a << s }; p a`, `["a", "b", "c"]`},
		// Limit is honoured under the block form.
		{`a = []; "a b c".split(" ", 2) { |s| a << s }; p a`, `["a", "b c"]`},
		// An empty receiver yields nothing and returns self.
		{`a = []; r = "".split(/x/) { |s| a << s }; p a; p r`, "[]\n\"\""},
		// Without a block the array is returned unchanged.
		{`p "a b c".split`, `["a", "b", "c"]`},
		{`p "a:b".split(":")`, `["a", "b"]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
