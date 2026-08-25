// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringHex covers String#hex: the string is read as a base-16 integer with
// an optional sign and an optional 0x/0X prefix (b/d/o are hex digits, not radix
// prefixes), a single underscore is allowed between digits, leading whitespace is
// skipped, scanning stops at the first non-hex character, and the result is 0
// when no hex digit is present. A value beyond a machine word yields a Bignum.
// Verified against ruby 4.0.6.
func TestStringHex(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "0x0a".hex`, `10`},
		{`p "ff".hex`, `255`},
		{`p "0xff".hex`, `255`},
		{`p "0X1F".hex`, `31`},
		{`p "-1234".hex`, `-4660`},
		{`p "+0x10".hex`, `16`},
		{`p "  0xa".hex`, `10`},
		{`p "deadbeef".hex`, `3735928559`},
		{`p "1_0".hex`, `16`},
		// b/d/o are hex digits, so only 0x/0X is a prefix.
		{`p "0b11".hex`, `2833`},
		{`p "0d10".hex`, `3344`},
		{`p "0o17".hex`, `0`},
		// Stops at the first non-hex character; 0 when there is no digit.
		{`p "10 ".hex`, `16`},
		{`p "wombat".hex`, `0`},
		{`p "0xg".hex`, `0`},
		{`p "_10".hex`, `0`},
		{`p "0x".hex`, `0`},
		{`p "".hex`, `0`},
		// A value past 64 bits becomes a Bignum.
		{`p "0xFFFFFFFFFFFFFFFFFF".hex`, `4722366482869645213695`},
		// oct still resolves every radix prefix (shares accumInum with hex).
		{`p "0b1010".oct`, `10`},
		{`p "0x1f".oct`, `31`},
		{`p "777".oct`, `511`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
