// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringTransformEncoding covers the case/order/whitespace transforms
// (upcase, downcase, capitalize, swapcase, reverse, strip, lstrip, rstrip)
// keeping the receiver's encoding on their result, rather than defaulting to
// UTF-8. The UTF-8 default and ASCII-8BIT are preserved, and the transforms'
// values are unchanged. Verified against ruby 4.0.6.
func TestStringTransformEncoding(t *testing.T) {
	// Each transform of a US-ASCII receiver stays US-ASCII.
	usascii := []string{"upcase", "downcase", "capitalize", "swapcase", "reverse", "strip", "lstrip", "rstrip"}
	for _, m := range usascii {
		src := `p "Hello  ".force_encoding("US-ASCII").` + m + `.encoding`
		if got := eval(t, src); got != "#<Encoding:US-ASCII>\n" {
			t.Errorf("%s: got=%q want US-ASCII", m, got)
		}
	}
	cases := []struct{ src, want string }{
		// ASCII-8BIT (binary) is preserved.
		{`p "AB".b.downcase.encoding`, `#<Encoding:BINARY (ASCII-8BIT)>`},
		{`p "  x  ".b.strip.encoding`, `#<Encoding:BINARY (ASCII-8BIT)>`},
		// The UTF-8 default is unchanged.
		{`p "héllo".upcase.encoding`, `#<Encoding:UTF-8>`},
		{`p "abc".reverse.encoding`, `#<Encoding:UTF-8>`},
		// Values are unchanged by the encoding threading.
		{`p "Hello".upcase`, `"HELLO"`},
		{`p "Hello".downcase`, `"hello"`},
		{`p "hello world".capitalize`, `"Hello world"`},
		{`p "Hello".swapcase`, `"hELLO"`},
		{`p "abc".reverse`, `"cba"`},
		{`p "  hi  ".strip`, `"hi"`},
		{`p "  hi  ".lstrip`, `"hi  "`},
		{`p "  hi  ".rstrip`, `"  hi"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
