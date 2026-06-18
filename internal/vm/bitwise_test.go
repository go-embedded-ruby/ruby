package vm_test

import (
	"strings"
	"testing"
)

// Integer bitwise and shift operators: << >> & | ^ ~ (arbitrary precision).
func TestBitwise(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"shl", `p 5 << 2`, "20\n"},
		{"shr", `p 20 >> 2`, "5\n"},
		{"shl_promotes", `p 1 << 70`, "1180591620717411303424\n"},
		{"shr_demotes", `p((1 << 70) >> 68)`, "4\n"},
		{"shl_negative_amount", `p 20 << -2`, "5\n"},
		{"shr_negative_amount", `p 5 >> -2`, "20\n"},
		{"and", `p 12 & 10`, "8\n"},
		{"or", `p 12 | 10`, "14\n"},
		{"xor", `p 12 ^ 10`, "6\n"},
		{"not", `p ~5`, "-6\n"},
		{"double_not", `p ~~5`, "5\n"},
		{"and_bignum", `p((2 ** 100 + 5) & 7)`, "5\n"},
		{"or_bignum", `p((2 ** 100) | 1)`, "1267650600228229401496703205377\n"},
		{"xor_bignum", `p((2 ** 100) ^ (2 ** 100))`, "0\n"},
		{"not_bignum", `p(~(2 ** 100))`, "-1267650600228229401496703205377\n"},
		{"shr_negative_value", `p(-8 >> 1)`, "-4\n"},
		// Precedence: shift > & > | / ^ > comparison > +.
		{"prec_plus_shift", `p 1 + 2 << 3`, "24\n"},
		{"prec_and_or", `p 12 & 10 | 1`, "9\n"},
		{"prec_and_cmp", `p 1 & 2 < 3`, "true\n"},
		{"prec_and_xor", `p 5 & 3 ^ 1`, "0\n"},
		{"chained_shl", "a = []\na << 1 << 2\np a", "[1, 2]\n"},
		{"command_arg_not", `p ~5`, "-6\n"},
		{"in_block", "f = ->(x){ x & 1 }\np f.call(7)", "1\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestBitwiseErrors(t *testing.T) {
	for _, src := range []string{`12 & 2.0`, `12 | "x"`, `5 ^ nil`} {
		err := runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "TypeError") {
			t.Errorf("src=%q: got %v, want TypeError", src, err)
		}
	}
}
