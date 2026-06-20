package vm_test

import (
	"strings"
	"testing"
)

// TestIntegerPowModulo covers Integer#pow(exp, mod) — modular exponentiation —
// and Comparable#clamp with a Range or nil bounds, against MRI 4.0.5.
func TestNumericExtras(t *testing.T) {
	cases := []struct{ src, want string }{
		// Modular exponentiation: base**exp mod m.
		{`p 3.pow(4, 5)`, "1\n"},
		{`p 2.pow(10, 1000)`, "24\n"},
		{`p 5.pow(117, 19)`, "1\n"},
		{`p 2.pow(3)`, "8\n"}, // one-arg ** still works
		// clamp with a Range.
		{`p 15.clamp(1..10)`, "10\n"},
		{`p 0.clamp(1..10)`, "1\n"},
		{`p 5.clamp(1..10)`, "5\n"},
		{`p 2.5.clamp(0.0..1.0)`, "1.0\n"},
		// clamp with a beginless / endless range (one bound only).
		{`p 15.clamp(..10)`, "10\n"},
		{`p 0.clamp(1..)`, "1\n"},
		{`p 5.clamp(1..)`, "5\n"},
		// clamp with explicit bounds, including nil.
		{`p 5.clamp(1, 10)`, "5\n"},
		{`p 5.clamp(1, nil)`, "5\n"},
		{`p 5.clamp(nil, 3)`, "3\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNumericExtraErrors covers the raising paths: a negative exponent or zero
// modulus or non-integer modulus for pow, and an exclusive clamp range.
func TestNumericExtraErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`2.pow(-1, 5)`, "RangeError"},      // no modular inverse, like MRI
		{`3.pow(4, 0)`, "ZeroDivisionError"}, // zero modulus
		{`3.pow(4, 1.5)`, "TypeError"},       // non-integer modulus
		{`5.clamp(1...10)`, "ArgumentError"}, // exclusive range
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
