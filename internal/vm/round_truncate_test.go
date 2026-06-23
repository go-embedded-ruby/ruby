package vm_test

import "testing"

// TestRoundTruncate covers Integer#round / Integer#truncate with negative ndigits
// (round is half away from zero, truncate toward zero; both leave an Integer
// unchanged with ndigits >= 0) and Float#truncate. Asserted against MRI Ruby 4.0.5.
func TestRoundTruncate(t *testing.T) {
	cases := []struct{ src, want string }{
		// Integer#round: half away from zero for negative ndigits; identity otherwise.
		{`p [1234.round(-2), 1234.round(-1), 1250.round(-2), 1251.round(-2), 1234.round(2), 1234.round]`, "[1200, 1230, 1300, 1300, 1234, 1234]\n"},
		{`p [(-1250).round(-2), (-1234).round(-1)]`, "[-1300, -1230]\n"},
		{`p [1234.round(-20), 5.round(-100)]`, "[0, 0]\n"}, // 10**n overflows int64 -> 0
		// Integer#truncate: toward zero for negative ndigits; identity otherwise.
		{`p [5.truncate, 1234.truncate(-2), 5.truncate(2), (-1234).truncate(-2), 9.truncate(-100)]`, "[5, 1200, 5, -1200, 0]\n"},
		// Float#truncate: ndigits > 0 keeps a Float, otherwise an Integer.
		{`p [3.14.truncate, 3.99.truncate(1), (-3.99).truncate, (-3.14).truncate(1), 3.14.truncate(0), 12.34.truncate(-1)]`, "[3, 3.9, -3, -3.1, 3, 10]\n"},
		// Result classes.
		{`p [1234.round(-2).class, 3.99.truncate(1).class, 3.99.truncate.class]`, "[Integer, Float, Integer]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
