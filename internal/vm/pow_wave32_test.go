// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestPowExponentiationSemantics pins Integer#** / Integer#pow / Float#** to
// numeric.c's fix_pow and rb_float_pow and bignum.c's rb_big_pow at tag
// v3_4_0. Every `want` below was taken from `ruby -e` on MRI 4.0.5.
func TestPowExponentiationSemantics(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// fix_pow settles a ±1 base BEFORE the exponent's sign, so these stay
		// Integers where a general negative exponent yields a Rational.
		{"one_to_negative", `p 1 ** -5`, "1\n"},
		{"minus_one_even", `p((-1) ** -4)`, "1\n"},
		{"minus_one_odd", `p((-1) ** -5)`, "-1\n"},
		{"minus_one_big_even", `p((-1) ** 4611686018427387904)`, "1\n"},
		{"minus_one_big_odd", `p((-1) ** 4611686018427387905)`, "-1\n"},
		// fix_pow_inverted: rb_rational_raw(1, base ** -e).
		{"negative_exponent", `p 5 ** -1`, "(1/5)\n"},
		{"negative_exponent_class", `p (5 ** -1).class`, "Rational\n"},
		{"negative_exponent_neg_base", `p((-2) ** -1)`, "(-1/2)\n"},
		{"negative_exponent_even", `p((-2) ** -2)`, "(1/4)\n"},
		// The remaining integral arms.
		{"zero_exponent", `p 7 ** 0`, "1\n"},
		{"zero_base", `p 0 ** 5`, "0\n"},
		{"fixnum_power", `p 2 ** 40`, "1099511627776\n"},
		{"bignum_exponent_on_one", `p 1 ** 4611686018427387904`, "1\n"},
		// The 16 GB BIGLEN_LIMIT of rb_big_pow: the result may be enormous but
		// must be an Integer, and past the limit it is an ArgumentError.
		{"within_limit_bit_length", `p (2 ** 40000000).bit_length`, "40000001\n"},
		{"bignum_base_within_limit", `p ((2 ** 70) ** 500000).bit_length`, "35000001\n"},
		// Float exponent (fix_pow's RB_FLOAT_TYPE_P arm).
		{"float_exponent_zero", `p 7 ** 0.0`, "1.0\n"},
		{"zero_base_negative_float", `p 0 ** -1.0`, "Infinity\n"},
		{"zero_base_positive_float", `p 0 ** 2.0`, "0.0\n"},
		{"one_base_float", `p 1 ** 2.5`, "1.0\n"},
		{"float_exponent", `p 2 ** 2.0`, "4.0\n"},
		{"negative_base_half_power", `p((-2) ** 0.5)`, "(0.0+1.4142135623730951i)\n"},
		{"negative_base_neg_half", `p((-2) ** -0.5)`, "(0.0-0.7071067811865475i)\n"},
		{"negative_base_three_halves", `p((-2) ** 1.5)`, "(0.0-2.82842712474619i)\n"},
		{"negative_base_integral_float", `p((-2) ** 2.0)`, "4.0\n"},
		// Rational and other operands take rb_num_coerce_bin.
		{"rational_exponent", `p 2 ** Rational(2, 1)`, "(4/1)\n"},
		{"rational_half_exponent", `p 9 ** Rational(1, 2)`, "3.0\n"},
		{"coerce_protocol", `
class C
  def coerce(o) = [o, 3]
end
p 2 ** C.new`, "8\n"},
		// Float#** / rb_float_pow.
		{"float_squared", `p 3.0 ** 2`, "9.0\n"},
		{"float_int_exponent", `p 2.0 ** 3`, "8.0\n"},
		{"float_bignum_exponent", `p 2.0 ** (2 ** 70)`, "Infinity\n"},
		{"float_float_exponent", `p 2.0 ** 0.5`, "1.4142135623730951\n"},
		{"float_negative_base", `p((-2.0) ** 0.5)`, "(0.0+1.4142135623730951i)\n"},
		{"float_coerce", `
class D
  def coerce(o) = [o, 2.0]
end
p 4.0 ** D.new`, "16.0\n"},
		// Each quadrant of the half-turn reduction inside sincospi.
		{"polar_quadrant0", `p((-2.0) ** 0.1)`, "(1.0193171355373614+0.3311962140437956i)\n"},
		{"polar_quadrant1", `p((-2.0) ** (1.0 / 3))`, "(0.6299605249474366+1.0911236359717214i)\n"},
		{"polar_quadrant2_half", `p((-2.0) ** 0.75)`, "(-1.1892071150027212+1.189207115002721i)\n"},
		{"polar_quadrant2", `p((-2.0) ** 1.1)`, "(-2.0386342710747223-0.6623924280875918i)\n"},
		{"polar_quadrant3", `p((-2.0) ** 1.6)`, "(0.9367643554147175-2.8830642348724616i)\n"},
		// rb_dbl_complex_new_polar_pi's sign flip on an odd half turn.
		{"polar_half_turn_flip", `p((-2.0) ** 2.5)`, "(0.0+5.65685424949238i)\n"},
		{"polar_neg_half_turn", `p((-2.0) ** -1.5)`, "(0.0+0.3535533905932738i)\n"},
		// Integer#pow(exp, mod) — rb_int_powm, whose result carries the sign of
		// the modulus ("handles sign like #divmod does").
		{"powm_positive", `p 2.pow(5, 12)`, "8\n"},
		{"powm_negative_modulus", `p 2.pow(5, -12)`, "-4\n"},
		{"powm_negative_base", `p((-2).pow(5, 12))`, "4\n"},
		{"powm_negative_both", `p((-2).pow(5, -12))`, "-8\n"},
		{"powm_bignum", `p 2.pow(61, 5843009213693951)`, "3697379018277258\n"},
		{"powm_exact_multiple", `p 4.pow(3, 8)`, "0\n"},
		// eql? spans the Integer/Bignum representation split, and Rational and
		// Complex compare by value (numeric.c's num_eql).
		{"eql_across_representations", `p(((-2) ** 63).eql?(-9223372036854775808))`, "true\n"},
		{"eql_rational", `p Rational(1, 1).eql?(Rational(1, 1))`, "true\n"},
		{"eql_rational_vs_integer", `p Rational(1, 1).eql?(1)`, "false\n"},
		{"eql_integer_vs_rational", `p 1.eql?(Rational(1, 1))`, "false\n"},
		{"eql_rational_in_array", `p [Rational(1, 2)].eql?([Rational(1, 2)])`, "true\n"},
		{"eql_complex_in_array", `p [Complex(1, 2)].eql?([Complex(1, 2)])`, "true\n"},
		{"eql_complex_component_type", `p [Complex(1, 2)].eql?([Complex(1.0, 2)])`, "false\n"},
		{"eql_complex_vs_integer", `p [Complex(1, 2)].eql?([1])`, "false\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestPowExponentiationErrors pins the raises: the BIGLEN_LIMIT refusal (which
// used to be an unbounded computation that hung core/integer/pow_spec.rb), the
// 0 ** -1 ZeroDivisionError, coerce_failed's naming of the offending operand
// and rb_int_powm's two distinct messages.
func TestPowExponentiationErrors(t *testing.T) {
	cases := []struct{ name, src, class, msg string }{
		{"exponent_too_large", `100000000 ** 1000000000`, "ArgumentError", "exponent is too large"},
		{"bignum_exponent_too_large", `(2 ** 70) ** (2 ** 70)`, "ArgumentError", "exponent is too large"},
		{"zero_to_minus_one", `0 ** -1`, "ZeroDivisionError", "divided by 0"},
		{"zero_to_negative_rational", `0 ** Rational(-1, 1)`, "ZeroDivisionError", "divided by 0"},
		// coerce_failed shows the immediates by value and everything else by class.
		{"string_exponent", `13 ** "10"`, "TypeError", "String can't be coerced into Integer"},
		{"symbol_exponent", `13 ** :symbol`, "TypeError", ":symbol can't be coerced into Integer"},
		{"nil_exponent", `13 ** nil`, "TypeError", "nil can't be coerced into Integer"},
		{"true_exponent", `13 ** true`, "TypeError", "true can't be coerced into Integer"},
		{"bignum_string_exponent", `(2 ** 70) ** "10"`, "TypeError", "String can't be coerced into Integer"},
		{"float_string_exponent", `2.0 ** "10"`, "TypeError", "String can't be coerced into Float"},
		{"float_nil_exponent", `2.0 ** nil`, "TypeError", "nil can't be coerced into Float"},
		// rb_int_powm checks the exponent first, then its sign, then the modulus.
		{"powm_non_integer_exponent", `2.pow("x", 5)`, "TypeError",
			"Integer#pow() 2nd argument not allowed unless a 1st argument is integer"},
		{"powm_negative_exponent", `2.pow(-1, 5)`, "RangeError",
			"Integer#pow() 1st argument cannot be negative when 2nd argument specified"},
		{"powm_non_integer_modulus", `2.pow(5, 2.0)`, "TypeError",
			"Integer#pow() 2nd argument not allowed unless all arguments are integers"},
		{"powm_zero_modulus", `2.pow(5, 0)`, "ZeroDivisionError", "divided by 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			class, msg := evalErr(t, tc.src)
			if class != tc.class || !strings.Contains(msg, tc.msg) {
				t.Fatalf("src=%q got %s: %q want %s: %q", tc.src, class, msg, tc.class, tc.msg)
			}
		})
	}
}
