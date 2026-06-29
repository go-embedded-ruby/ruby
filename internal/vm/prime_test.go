// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestPrime covers the Prime module and the Integer core extensions backed by
// github.com/go-ruby-prime/prime (the MRI-4.0.5 faithful port): primality,
// the take/first generators, Prime.each (bounded, unbounded-with-break, and the
// no-block Enumerator form), prime_division and its inverse, the Integer#prime?
// / Integer#prime_division extensions, and the Bignum path — every value
// asserted against MRI 4.0.5's stdlib Prime.
func TestPrime(t *testing.T) {
	cases := []struct{ src, want string }{
		// Prime.prime? — primality across the small and Bignum ranges.
		{`require "prime"; p Prime.prime?(2)`, "true\n"},
		{`require "prime"; p Prime.prime?(7)`, "true\n"},
		{`require "prime"; p Prime.prime?(1)`, "false\n"},
		{`require "prime"; p Prime.prime?(0)`, "false\n"},
		{`require "prime"; p Prime.prime?(-7)`, "false\n"},
		{`require "prime"; p Prime.prime?(8)`, "false\n"},
		{`require "prime"; p Prime.prime?(561)`, "false\n"}, // a Carmichael number
		{`require "prime"; p Prime.prime?(10 ** 20 + 39)`, "true\n"},
		// Integer#prime? — the core extension, Fixnum and Bignum.
		{`require "prime"; p 13.prime?`, "true\n"},
		{`require "prime"; p 14.prime?`, "false\n"},
		{`require "prime"; p (10 ** 20 + 39).prime?`, "true\n"},
		// Prime.take / Prime.first — the first n primes.
		{`require "prime"; p Prime.take(5)`, "[2, 3, 5, 7, 11]\n"},
		{`require "prime"; p Prime.first(5)`, "[2, 3, 5, 7, 11]\n"},
		{`require "prime"; p Prime.take(0)`, "[]\n"},
		{`require "prime"; p Prime.take(1)`, "[2]\n"},
		// Prime.each — bounded form yields primes <= ubound.
		{`require "prime"; r = []; Prime.each(11) { |x| r << x }; p r`, "[2, 3, 5, 7, 11]\n"},
		{`require "prime"; r = []; Prime.each(1) { |x| r << x }; p r`, "[]\n"},
		// Prime.each — unbounded (nil/absent bound) runs until break.
		{`require "prime"; r = []; Prime.each { |x| break if x > 10; r << x }; p r`, "[2, 3, 5, 7]\n"},
		{`require "prime"; r = []; Prime.each(nil) { |x| break if x > 7; r << x }; p r`, "[2, 3, 5, 7]\n"},
		// Prime.each with no block returns an Enumerator.
		{`require "prime"; p Prime.each.first(4)`, "[2, 3, 5, 7]\n"},
		{`require "prime"; p Prime.each.class`, "Enumerator\n"},
		// Prime.prime_division — [prime, exponent] pairs, including the sign pair.
		{`require "prime"; p Prime.prime_division(12)`, "[[2, 2], [3, 1]]\n"},
		{`require "prime"; p Prime.prime_division(360)`, "[[2, 3], [3, 2], [5, 1]]\n"},
		{`require "prime"; p Prime.prime_division(1)`, "[]\n"},
		{`require "prime"; p Prime.prime_division(-12)`, "[[-1, 1], [2, 2], [3, 1]]\n"},
		{`require "prime"; p Prime.prime_division(17)`, "[[17, 1]]\n"},
		// Integer#prime_division — the core extension.
		{`require "prime"; p 360.prime_division`, "[[2, 3], [3, 2], [5, 1]]\n"},
		// Prime.int_from_prime_division — the inverse, with and without a sign pair.
		{`require "prime"; p Prime.int_from_prime_division([[2, 2], [3, 2]])`, "36\n"},
		{`require "prime"; p Prime.int_from_prime_division([[-1, 1], [2, 2], [3, 1]])`, "-12\n"},
		{`require "prime"; p Prime.int_from_prime_division([])`, "1\n"},
		// Round-trip: int_from_prime_division(prime_division(n)) == n.
		{`require "prime"; p Prime.int_from_prime_division(Prime.prime_division(840))`, "840\n"},
		// defined? after require; require returns true once then false (preloaded twice).
		{`require "prime"; p defined?(Prime)`, "\"constant\"\n"},
		{`p require("prime"); p require("prime")`, "true\nfalse\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestPrimeLazyLoad asserts the prime surface is absent until `require "prime"`,
// matching MRI's lazy lib/prime.rb load.
func TestPrimeLazyLoad(t *testing.T) {
	if got := eval(t, `p defined?(Prime)`); got != "nil\n" {
		t.Errorf("Prime should be undefined before require, got %q", got)
	}
	if got := eval(t, `p 7.respond_to?(:prime?)`); got != "false\n" {
		t.Errorf("Integer#prime? should be absent before require, got %q", got)
	}
}

// TestPrimeErrors covers the error branches: a zero factorisation raises
// ZeroDivisionError (as MRI), a non-integer argument raises TypeError, and a
// malformed prime-division slice raises TypeError.
func TestPrimeErrors(t *testing.T) {
	cases := []struct {
		src, class, msg string
	}{
		{`require "prime"; Prime.prime_division(0)`, "ZeroDivisionError", "divided by 0"},
		{`require "prime"; 0.prime_division`, "ZeroDivisionError", "divided by 0"},
		{`require "prime"; Prime.prime?("x")`, "TypeError", "no implicit conversion of \"x\" into Integer"},
		{`require "prime"; Prime.prime_division(:sym)`, "TypeError", "no implicit conversion of :sym into Integer"},
		{`require "prime"; Prime.int_from_prime_division(5)`, "TypeError", "no implicit conversion of 5 into Array"},
		{`require "prime"; Prime.int_from_prime_division([[2]])`, "TypeError", "each prime-division entry must be a [prime, exponent] pair"},
		{`require "prime"; Prime.int_from_prime_division([5])`, "TypeError", "each prime-division entry must be a [prime, exponent] pair"},
		{`require "prime"; Prime.int_from_prime_division([["x", 1]])`, "TypeError", "no implicit conversion of \"x\" into Integer"},
	}
	for _, tc := range cases {
		class, msg := evalErr(t, tc.src)
		if class != tc.class || msg != tc.msg {
			t.Errorf("src=%q got=%s:%q want=%s:%q", tc.src, class, msg, tc.class, tc.msg)
		}
	}
}
