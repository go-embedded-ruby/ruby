// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestBignumFloatEqual covers exact Integer/Bignum-vs-Float equality: a Bignum
// equals a Float only when the float's exact value is that integer (never after
// merely rounding one to the other), in either operand order. Verified against
// ruby 4.0.6. Regression guard for comparing a Bignum against a double by first
// rounding the Bignum (which made 1e100 == 10**100 wrongly true and
// Integer(2e100) == 2e100 wrongly false).
func TestBignumFloatEqual(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Integer(2e100) == 2e100`, "true"},        // Bignum == exact-value Float
		{`p 2e100 == Integer(2e100)`, "true"},        // reversed
		{`p (10**100) == 1e100`, "false"},            // 1e100 rounds, != exact 10**100
		{`p 1e100 == (10**100)`, "false"},            // reversed
		{`p (10**100) == (10**100).to_f`, "false"},   // to_f rounds
		{`p (10**20) == 1e20`, "true"},               // 1e20 is exactly 10**20
		{`p (2**70) == 2.0**70`, "true"},             // power of two: exact
		{`p ((10**100) <=> 1e100)`, "-1"},            // spaceship already exact
		{`p 2 == 2.0`, "true"},                       // small Integer vs Float
		{`p 2.0 == 2`, "true"},                       // reversed
		{`p 2.eql?(2.0)`, "false"},                   // eql? does not cross types
		{`p (10**100).eql?((10**100).to_f)`, "false"}, // "
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
