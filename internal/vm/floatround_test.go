package vm_test

import "testing"

// TestFloatRound covers Float#round's ndigits contract: no argument and ndigits
// <= 0 round to an Integer (to a power of ten for negatives); ndigits > 0 rounds
// to that many decimals and stays a Float. Asserted against MRI 4.0.5.
func TestFloatRound(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 2.5.round`, "3\n"},            // no arg → Integer, half away from zero
		{`p(-2.5.round)`, "-3\n"},         // negative, half away from zero
		{`p 1.57.round(0)`, "2\n"},        // ndigits 0 → Integer
		{`p 1.57.round(2)`, "1.57\n"},     // ndigits > 0 → Float
		{`p 3.14159.round(3)`, "3.142\n"}, // rounding to decimals
		{`p 1234.5.round(-2)`, "1200\n"},  // negative ndigits → power of ten
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
