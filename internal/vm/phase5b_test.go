package vm_test

import (
	"strings"
	"testing"
)

func TestIntegerNumericBatch(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"gcd", `p 12.gcd(18)`, "6\n"},
		{"lcm", `p 4.lcm(6)`, "12\n"},
		{"lcm_self_zero", `p 0.lcm(5)`, "0\n"},
		{"lcm_arg_zero", `p 5.lcm(0)`, "0\n"},
		{"lcm_negative", `p((-4).lcm(6))`, "12\n"},
		{"bit_length", `p 255.bit_length`, "8\n"},
		{"bit_length_one", `p 1.bit_length`, "1\n"},
		{"bit_length_zero", `p 0.bit_length`, "0\n"},
		{"bit_length_neg_one", `p((-1).bit_length)`, "0\n"},
		{"bit_length_neg", `p((-256).bit_length)`, "8\n"},
		{"digits_default", `p 12345.digits`, "[5, 4, 3, 2, 1]\n"},
		{"digits_base", `p 255.digits(16)`, "[15, 15]\n"},
		{"digits_zero", `p 0.digits(16)`, "[0]\n"},
		{"digits_binary", `p 100.digits(2)`, "[0, 0, 1, 0, 0, 1, 1]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestIntegerNumericErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"digits_radix", `5.digits(1)`, "invalid radix 1"},
		{"digits_negative", `(-5).digits`, "out of domain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %q", tc.src, err, tc.want)
			}
		})
	}
}
