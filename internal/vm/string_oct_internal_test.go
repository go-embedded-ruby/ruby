// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringOct covers String#oct: default octal, the 0x/0b/0o/0d base prefixes,
// a leading sign and whitespace, the underscore separator, lenient parsing that
// stops at the first invalid digit (0 when none), and a Bignum result. Verified
// against ruby 4.0.6.
func TestStringOct(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "777".oct`, `511`},
		{`p "0x1a".oct`, `26`},
		{`p "0b101".oct`, `5`},
		{`p "0o17".oct`, `15`},
		{`p "0d99".oct`, `99`},
		{`p "-777".oct`, `-511`},
		{`p "+0xa".oct`, `10`},
		{`p "  0xff".oct`, `255`}, // leading whitespace
		{`p "1_0".oct`, `8`},      // underscore separator (octal 10)
		{`p "1__0".oct`, `1`},     // a doubled underscore stops parsing
		{`p "_7".oct`, `0`},       // a leading underscore is invalid
		{`p "1z9".oct`, `1`},      // stops at the first invalid digit
		{`p "08".oct`, `0`},       // 8 is not an octal digit
		{`p "abc".oct`, `0`},      // no digits
		{`p "0x".oct`, `0`},       // a prefix with no digits
		{`p "".oct`, `0`},
		{`p "0XFF".oct`, `255`},                             // uppercase prefix
		{`p ("1" * 30).oct`, `176848577040768610699874889`}, // Bignum result
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
