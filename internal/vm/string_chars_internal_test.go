// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringCharsBlockEncoding covers String#chars with a block (yields each
// character and returns the receiver) and String#chars / #each_char keeping the
// receiver's encoding on each character. Verified against ruby 4.0.6.
func TestStringCharsBlockEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		// #chars without a block returns the character array (unchanged).
		{`p "hello".chars`, `["h", "e", "l", "l", "o"]`},
		// #chars with a block yields each character and returns self.
		{`a = []; "hello".chars { |c| a << c }; p a`, `["h", "e", "l", "l", "o"]`},
		{`p "hello".chars { |c| }`, `"hello"`},
		// Characters keep the receiver's encoding.
		{`p "abc".force_encoding("US-ASCII").chars.map(&:encoding).uniq`, `[#<Encoding:US-ASCII>]`},
		{`p "ab".b.chars.map(&:encoding).uniq`, `[#<Encoding:BINARY (ASCII-8BIT)>]`},
		{`p "héllo".chars.map(&:encoding).uniq`, `[#<Encoding:UTF-8>]`},
		// #each_char yields characters in the receiver's encoding.
		{`a = []; "ab".force_encoding("US-ASCII").each_char { |c| a << c.encoding }; p a.uniq`, `[#<Encoding:US-ASCII>]`},
		{`p "abc".each_char.to_a`, `["a", "b", "c"]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
