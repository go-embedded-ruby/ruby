package vm_test

import (
	"strings"
	"testing"
)

func TestConversionMethods(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"int_str", `p Integer("42")`, "42\n"},
		{"int_ws", `p Integer("  42  ")`, "42\n"},
		{"int_float", `p Integer(3.9)`, "3\n"},
		{"int_neg", `p Integer("-17")`, "-17\n"},
		{"int_base", `p Integer("ff", 16)`, "255\n"},
		{"int_int", `p Integer(5)`, "5\n"},
		{"float_str", `p Float("3.14")`, "3.14\n"},
		{"float_int", `p Float(5)`, "5.0\n"},
		{"float_ws", `p Float("  2.5 ")`, "2.5\n"},
		{"float_float", `p Float(2.5)`, "2.5\n"},
		{"string_int", `p String(42)`, "\"42\"\n"},
		{"string_sym", `p String(:sym)`, "\"sym\"\n"},
		{"string_arr", `p String([1, 2])`, "\"[1, 2]\"\n"},
		{"array_nil", `p Array(nil)`, "[]\n"},
		{"array_arr", `p Array([1, 2, 3])`, "[1, 2, 3]\n"},
		{"array_scalar", `p Array(5)`, "[5]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestConversionErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"int_invalid", `Integer("abc")`, "ArgumentError"},
		{"int_partial", `Integer("12x")`, "ArgumentError"},
		{"int_type", `Integer(nil)`, "TypeError"},
		{"float_invalid", `Float("x")`, "ArgumentError"},
		{"float_type", `Float(nil)`, "TypeError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %s", tc.src, err, tc.want)
			}
		})
	}
}
