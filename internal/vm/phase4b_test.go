package vm_test

import (
	"strings"
	"testing"
)

func TestExponentiation(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"int_pow", `p 2 ** 10`, "1024\n"},
		{"int_zero", `p 2 ** 0`, "1\n"},
		{"int_cube", `p 3 ** 3`, "27\n"},
		{"float_base", `p 2.0 ** 3`, "8.0\n"},
		{"int_float_exp", `p 2 ** 0.5`, "1.4142135623730951\n"},
		{"float_root", `p 4 ** 0.5`, "2.0\n"},
		{"right_assoc", `p 2 ** 3 ** 2`, "512\n"},
		{"neg_exp", `p 10 ** -1`, "0.1\n"},
		{"pow_method", `p 5.pow(2)`, "25\n"},
		{"float_pow_method", `p 2.0.pow(3)`, "8.0\n"},
		{"var", "x = 3\np x ** 2", "9\n"},
		{"precedence", `p 2 * 3 ** 2`, "18\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestExponentiationTypeError(t *testing.T) {
	if err := runErr(t, `2 ** "x"`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("got %v, want TypeError", err)
	}
}
