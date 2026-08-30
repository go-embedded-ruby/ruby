package vm_test

import (
	"strings"
	"testing"
)

// TestFloatToIntegerDomain covers converting a Float to an Integer: a non-finite
// value (NaN / ±Infinity) raises FloatDomainError rather than returning 0 or
// crashing, and a finite value converts (as a Bignum when it exceeds a machine
// word). Verified against MRI Ruby 4.0.6.
func TestFloatToIntegerDomain(t *testing.T) {
	cases := []struct{ src, want string }{
		// Finite conversions are unchanged; a positive ndigits keeps a Float NaN.
		{`p 3.7.to_i`, "3\n"},
		{`p 3.7.round`, "4\n"},
		{`p 2.5.round`, "3\n"},
		{`p(-3.7.floor)`, "-4\n"},
		{`p 3.15.ceil`, "4\n"},
		{`p 3.99.truncate`, "3\n"},
		{`p 1234.5678.round(-2)`, "1200\n"},
		{`p (0.0/0.0).round(2)`, "NaN\n"},
		// A Float too large for a machine word becomes a Bignum.
		{`p 1e20.to_i`, "100000000000000000000\n"},
		{`p (-1e20).to_i`, "-100000000000000000000\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// A non-finite Float has no Integer value: FloatDomainError, whose message is
	// the value's name (MRI). Previously NaN#round crashed and NaN#to_i/#ceil/
	// #floor silently returned 0.
	errCases := []struct{ src, substr string }{
		{`Float::NAN.round`, "NaN"},
		{`Float::NAN.ceil`, "NaN"},
		{`Float::NAN.floor`, "NaN"},
		{`Float::NAN.to_i`, "NaN"},
		{`Float::NAN.to_int`, "NaN"},
		{`Float::NAN.truncate`, "NaN"},
		{`(1.0/0.0).round`, "Infinity"},
		{`(1.0/0.0).to_i`, "Infinity"},
		{`Float::INFINITY.floor`, "Infinity"},
		{`(-1.0/0.0).ceil`, "-Infinity"},
		{`(-1.0/0.0).to_i`, "-Infinity"},
	}
	for _, c := range errCases {
		err := runErr(t, c.src)
		if err == nil || !strings.Contains(err.Error(), "FloatDomainError") || !strings.Contains(err.Error(), c.substr) {
			t.Errorf("src=%q err=%v, want FloatDomainError containing %q", c.src, err, c.substr)
		}
	}
}
