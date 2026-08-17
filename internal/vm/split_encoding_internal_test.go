// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringSplitEncoding covers String#split preserving the receiver's encoding
// on every result substring (and capture), across the String, Regexp, awk
// whitespace, empty-pattern and capture-group forms. The UTF-8 default is
// unchanged. Verified against ruby 4.0.6.
func TestStringSplitEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		// String, Regexp and whitespace forms inherit US-ASCII.
		{`p "a,b,c".force_encoding("US-ASCII").split(",").map(&:encoding).uniq`, `[#<Encoding:US-ASCII>]`},
		{`p "a1b1c".force_encoding("US-ASCII").split(/1/).map(&:encoding).uniq`, `[#<Encoding:US-ASCII>]`},
		{`p "a b c".force_encoding("US-ASCII").split.map(&:encoding).uniq`, `[#<Encoding:US-ASCII>]`},
		{`p "a b c".force_encoding("US-ASCII").split(" ").map(&:encoding).uniq`, `[#<Encoding:US-ASCII>]`},
		// Capture groups also inherit the receiver's encoding.
		{`p "h.i".force_encoding("US-ASCII").split(/(\.)/).map(&:encoding).uniq`, `[#<Encoding:US-ASCII>]`},
		// Empty-pattern (per-character) split inherits it too.
		{`p "abc".force_encoding("US-ASCII").split(//).map(&:encoding).uniq`, `[#<Encoding:US-ASCII>]`},
		// ASCII-8BIT (binary) is preserved.
		{`p "a-b".b.split("-").map(&:encoding).uniq`, `[#<Encoding:BINARY (ASCII-8BIT)>]`},
		// The UTF-8 default is unchanged.
		{`p "héllo".split("l").map(&:encoding).uniq`, `[#<Encoding:UTF-8>]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
