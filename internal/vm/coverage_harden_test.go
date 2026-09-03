package vm_test

import (
	"strings"
	"testing"
)

// TestCoverageHarden exercises numeric-equality, range, and singleton paths that
// the rest of the suite leaves partly uncovered, so the merged 100% coverage
// gate has margin against measurement jitter rather than sitting on the rounding
// boundary. Every result is checked against MRI Ruby 4.0.6.
func TestCoverageHarden(t *testing.T) {
	ok := []struct{ expr, want string }{
		// complexEqual: Complex==Complex, Complex==real (zero imaginary), and the
		// mismatching / non-numeric fall-throughs.
		{`Complex(1, 2) == Complex(1, 2)`, "true"},
		{`Complex(1, 2) == Complex(1, 3)`, "false"},
		{`Complex(3, 0) == 3`, "true"},
		{`Complex(1, 2) == 3`, "false"},
		{`Complex(1, 2) == "x"`, "false"},
		// rationalEqual: Rational==Rational, Rational==Float, and fall-throughs.
		{`Rational(1, 2) == Rational(1, 2)`, "true"},
		{`Rational(1, 2) == Rational(1, 3)`, "false"},
		{`Rational(1, 2) == 0.5`, "true"},
		{`Rational(1, 2) == 0.6`, "false"},
		{`Rational(1, 2) == "x"`, "false"},
		// Range#size across inclusive / exclusive / empty (rangeInts + rangeSize).
		{`(1..5).size`, "5"},
		{`(1...5).size`, "4"},
		{`(5..1).size`, "0"},
		// Range#to_a across inclusive / exclusive / empty / String (rangeElems).
		{`(1..5).to_a`, "[1, 2, 3, 4, 5]"},
		{`(1...5).to_a`, "[1, 2, 3, 4]"},
		{`(5..1).to_a`, "[]"},
		{`("a".."e").to_a`, `["a", "b", "c", "d", "e"]`},
		{`("a"..."e").to_a`, `["a", "b", "c", "d"]`},
		// objSingleton: an object with a singleton method vs one without.
		{`o = Object.new; def o.foo; 42; end; o.singleton_methods`, "[:foo]"},
		{`Object.new.singleton_methods`, "[]"},
	}
	for _, c := range ok {
		if got := eval(t, "p ("+c.expr+")"); got != c.want+"\n" {
			t.Errorf("p (%s) = %q, want %q", c.expr, got, c.want+"\n")
		}
	}

	errs := []struct{ src, want string }{
		// A non-integer, non-String range cannot iterate: rangeSize / rangeElems
		// raise the MRI "can't iterate from Float" TypeError.
		{`(1.5..2.5).to_a`, "can't iterate from"},
		{`(1.5..2.5).size`, "can't iterate from"},
	}
	for _, c := range errs {
		err := runErr(t, c.src)
		if err == nil {
			t.Errorf("runErr(%q) = nil, want error containing %q", c.src, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("runErr(%q) error = %q, want containing %q", c.src, err.Error(), c.want)
		}
	}
}
