package vm

import (
	"math/big"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
)

// TestRatArithDefault covers ratArith's defensive default. Only the arithmetic
// opcodes (+, -, *, /, %) reach it through the interpreter, so a unit call with
// another opcode is the only way to exercise the fallthrough.
func TestRatArithDefault(t *testing.T) {
	wantRaise(t, "NoMethodError", func() {
		ratArith(bytecode.OpLt, big.NewRat(1, 2), big.NewRat(1, 3))
	})
}

// TestSimplestRatBetween covers the integer-return branches of the
// continued-fraction descent directly: an interval whose lower bound is itself
// an integer (floor returned) and one where only the ceiling lands in range.
func TestSimplestRatBetween(t *testing.T) {
	cases := []struct {
		lo, hi *big.Rat
		want   string
	}{
		// lo is an exact integer in range -> floorRat branch.
		{big.NewRat(2, 1), big.NewRat(5, 2), "2"},
		// floor (1) below range, ceil (2) in range -> ceilRat branch.
		{big.NewRat(3, 2), big.NewRat(5, 2), "2"},
		// no integer in range -> reciprocal recursion (simplest is 1/2).
		{big.NewRat(2, 5), big.NewRat(3, 5), "1/2"},
	}
	for _, c := range cases {
		if got := simplestRatBetween(c.lo, c.hi).RatString(); got != c.want {
			t.Errorf("simplestRatBetween(%v, %v) = %s, want %s", c.lo, c.hi, got, c.want)
		}
	}
}

// TestRationalizeFloatZero covers the zero short-circuit in rationalizeFloat.
func TestRationalizeFloatZero(t *testing.T) {
	if got := rationalizeFloat(0).RatString(); got != "0" {
		t.Errorf("rationalizeFloat(0) = %s, want 0", got)
	}
}
