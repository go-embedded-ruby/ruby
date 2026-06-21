package vm_test

import (
	"strings"
	"testing"
)

func TestNumericMethods(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// Integer predicates / sign
		{"abs", `p((-5).abs)`, "5\n"},
		{"abs_pos", `p 5.abs`, "5\n"},
		{"even", `p 6.even?`, "true\n"},
		{"odd", `p 7.odd?`, "true\n"},
		{"zero", `p 0.zero?`, "true\n"},
		{"positive", `p 5.positive?`, "true\n"},
		{"negative", `p((-3).negative?)`, "true\n"},
		{"succ", `p 5.succ`, "6\n"},
		{"next", `p 5.next`, "6\n"},
		{"pred", `p 5.pred`, "4\n"},
		// Integer conversions
		{"to_i", `p 5.to_i`, "5\n"},
		{"to_int", `p 5.to_int`, "5\n"},
		{"to_f", `p 5.to_f`, "5.0\n"},
		{"to_s", `p 255.to_s`, "\"255\"\n"},
		{"to_s_hex", `p 255.to_s(16)`, "\"ff\"\n"},
		{"to_s_bin", `p 10.to_s(2)`, "\"1010\"\n"},
		// Integer arithmetic helpers
		{"gcd", `p 12.gcd(18)`, "6\n"},
		{"divmod", `p 17.divmod(5)`, "[3, 2]\n"},
		{"divmod_neg", `p((-17).divmod(5))`, "[-4, 3]\n"},
		{"digits", `p 123.digits`, "[3, 2, 1]\n"},
		{"digits_zero", `p 0.digits`, "[0]\n"},
		{"chr", `p 65.chr`, "\"A\"\n"},
		// Integer iterators
		{"upto", "r = []\n1.upto(3) { |i| r << i }\np r", "[1, 2, 3]\n"},
		{"downto", "r = []\n3.downto(1) { |i| r << i }\np r", "[3, 2, 1]\n"},
		// Float
		{"f_abs", `p((-3.14).abs)`, "3.14\n"},
		{"f_zero", `p 0.0.zero?`, "true\n"},
		{"f_positive", `p 1.5.positive?`, "true\n"},
		{"f_negative", `p((-1.5).negative?)`, "true\n"},
		{"f_to_f", `p 1.5.to_f`, "1.5\n"},
		{"f_to_i", `p 1.9.to_i`, "1\n"},
		{"f_to_int", `p((-1.9).to_int)`, "-1\n"},
		{"f_ceil", `p 1.1.ceil`, "2\n"},
		{"f_floor", `p 1.9.floor`, "1\n"},
		{"f_round", `p 2.5.round`, "3\n"},
		{"f_round_neg", `p((-2.5).round)`, "-3\n"},
		{"f_nan", `p (0.0 / 0).nan?`, "true\n"},
		{"f_nan_false", `p 1.0.nan?`, "false\n"},
		{"f_finite", `p 1.0.finite?`, "true\n"},
		{"f_finite_false", `p (1.0 / 0).finite?`, "false\n"},
		{"f_inf_pos", `p (1.0 / 0).infinite?`, "1\n"},
		{"f_inf_neg", `p((-1.0 / 0).infinite?)`, "-1\n"},
		{"f_inf_nil", `p 2.0.infinite?`, "nil\n"},
		// Comparable mixed into Integer / Float / String
		{"int_clamp", `p 5.clamp(1, 3)`, "3\n"},
		{"int_between", `p 7.between?(1, 10)`, "true\n"},
		{"float_clamp", `p 9.9.clamp(1.0, 3.0)`, "3.0\n"},
		{"string_between", `p "b".between?("a", "c")`, "true\n"},
		{"string_clamp", `p "z".clamp("a", "m")`, "\"m\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestNumericErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"to_s_bad_radix", `1.to_s(99)`, "ArgumentError"},
		{"divmod_zero", `1.divmod(0)`, "ZeroDivisionError"},
		{"digits_negative", `(-5).digits`, "Math::DomainError"},
		{"chr_out_of_range", `9999.chr`, "RangeError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v want %s", err, tc.want)
			}
		})
	}
	// upto/downto with no block return an Enumerator (MRI semantics).
	if got := eval(t, `p 1.upto(3).to_a`); got != "[1, 2, 3]\n" {
		t.Errorf("upto.to_a got %q", got)
	}
	if got := eval(t, `p 3.downto(1).to_a`); got != "[3, 2, 1]\n" {
		t.Errorf("downto.to_a got %q", got)
	}
}
