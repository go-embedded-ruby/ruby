package vm_test

import "testing"

func TestNegativeLiteralPrecedence(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"neg_method", `p(-2.abs)`, "2\n"},
		{"neg_pow_spaced", `p(-2 ** 2)`, "-4\n"},
		{"neg_pow", `p(-2**2)`, "-4\n"},
		{"neg_pow_chain", `p(-2 ** 2 ** 3)`, "-256\n"},
		{"neg_float_method", `p(-3.14.round)`, "-3\n"},
		{"neg_float_abs", `p(-2.0.abs)`, "2.0\n"},
		{"neg_int_method", `p(-10.gcd(4))`, "2\n"},
		{"neg_float_ceil", `p(-2.5.ceil)`, "-2\n"},
		{"plain_neg", `p(-5)`, "-5\n"},
		{"binary_minus", `p(2 - 1)`, "1\n"},
		{"binary_minus_method", `p(2 - 1.abs)`, "1\n"},
		{"minus_neg", `p(1 - -2)`, "3\n"},
		{"unary_var", "x = 5\np(-x)", "-5\n"},
		{"unary_var_method", "x = 5\np(-x.abs)", "-5\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
