package vm_test

import (
	"strings"
	"testing"
)

// TestStringToIBase covers String#to_i(base): the base argument, radix prefixes,
// uppercase/lowercase/decimal digits, signs, underscores, bignum promotion and the
// invalid-radix error. Asserted against MRI Ruby 4.0.5.
func TestStringToIBase(t *testing.T) {
	cases := []struct{ src, want string }{
		// Explicit base, prefixes, digit ranges.
		{`p ["ff".to_i(16), "0xff".to_i(16), "1F".to_i(16), "0b101".to_i(2), "777".to_i(8), "z".to_i(36)]`, "[255, 255, 31, 5, 511, 35]\n"},
		// Signs, junk-terminated, whitespace, no valid digits.
		{`p ["+5".to_i, "-42xyz".to_i, "  7  ".to_i, "abc".to_i, "".to_i]`, "[5, -42, 7, 0, 0]\n"},
		// Underscores: between digits ok; leading or doubled stops.
		{`p ["1_000".to_i, "1__0".to_i, "_5".to_i]`, "[1000, 1, 0]\n"},
		// base 0 auto-detects a prefix (else decimal); a prefix not matching an
		// explicit base is not consumed.
		{`p ["101".to_i(0), "0x1f".to_i(0), "0b11".to_i(0), "0xff".to_i(10)]`, "[101, 31, 3, 0]\n"},
		// Octal and decimal prefixes (0o / 0d).
		{`p ["0o17".to_i(0), "0o20".to_i(8), "0d42".to_i(0)]`, "[15, 16, 42]\n"},
		// Bignum promotion.
		{`p "ffffffffffffffffff".to_i(16)`, "4722366482869645213695\n"},
		{`p "-0xff".to_i(16)`, "-255\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	for _, src := range []string{`"x".to_i(1)`, `"x".to_i(37)`} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "invalid radix") {
			t.Errorf("src=%q err=%v, want an invalid radix error", src, err)
		}
	}
}
