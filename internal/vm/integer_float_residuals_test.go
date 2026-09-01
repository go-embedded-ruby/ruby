package vm_test

import (
	"strings"
	"testing"
)

// TestIntegerFloatResiduals covers the MRI 4.0.6 conformance work on
// core/integer, core/float and core/numeric: floored Bignum division/modulo,
// Float#% Inf/NaN and signed-zero rules, exact Integer#fdiv, Integer#div and
// Float#divmod operand rules, round precision coercion, Bignum#bit_length,
// Float#<=> edge cases, coerce via Kernel#Float, and the residual aliases.
func TestIntegerFloatResiduals(t *testing.T) {
	const coercePrelude = `
class FdivCoerce; def coerce(o); [1, 10]; end; end
class DivCoerce;  def coerce(o); [10, 2]; end; end
class ModCoerce;  def coerce(o); [o, 2.0]; end; end
class ToIntArg;   def to_int; -2; end; end
class InfPos;     def infinite?; 1;   end; end
class InfNeg;     def infinite?; -1;  end; end
class InfNil;     def infinite?; nil; end; end
`
	cases := []struct{ name, src, want string }{
		// --- floored Bignum / and % (divisor's sign) ---
		{"big_div_neg_divisor", `puts(4 / -(2**64))`, "-1\n"},
		{"big_div_pos", `puts((10**50) / (10**40 + 1))`, "9999999999\n"},
		{"big_div_both_neg", `puts(-(10**50) / -(10**40 + 1))`, "9999999999\n"},
		{"big_mod_neg_divisor", `puts(13 % -(2**64))`, "-18446744073709551603\n"},
		{"big_mod_pos", `puts((10**50) % (10**40 + 1))`, "9999999999999999999999999999990000000001\n"},

		// --- Float#% operator: Inf / NaN / signed zero ---
		{"fmod_pos_inf", `puts(4.2 % Float::INFINITY)`, "4.2\n"},
		{"fmod_neg_inf", `puts(4.2 % -Float::INFINITY)`, "-Infinity\n"},
		{"fmod_inf_self", `puts((Float::INFINITY % 42).nan?)`, "true\n"},
		{"fmod_neg_zero", `r = -0.0 % 42; puts(r); puts(1/r < 0)`, "-0.0\ntrue\n"},
		{"fmod_plain", `puts(6543.21 % 137 < 105)`, "true\n"},

		// --- Float#% / #modulo methods (dispatch path) ---
		{"fmod_method_inf", `puts(4.2.modulo(Float::INFINITY))`, "4.2\n"},
		{"fmod_method_coerce", coercePrelude + `puts(4.2.send(:%, ModCoerce.new))`, "0.20000000000000018\n"},
		{"fmod_modulo_alias", `puts(Float.instance_method(:modulo) == Float.instance_method(:%))`, "true\n"},

		// --- Integer#coerce / Float#coerce via Kernel#Float ---
		{"int_coerce_int", `p 1.coerce(2)`, "[2, 1]\n"},
		{"int_coerce_string", `p 1.coerce("2")`, "[2.0, 1.0]\n"},
		{"float_coerce_string", `p 2.0.coerce("2.5")`, "[2.5, 2.0]\n"},

		// --- Integer#fdiv (exact, huge Bignums) ---
		{"fdiv_bignum_tiny", `puts(1.fdiv(10**323) == 1.0e-323)`, "true\n"},
		{"fdiv_bignum_ratio", `puts((10**344).fdiv(9 * 10**342))`, "11.11111111111111\n"},
		{"fdiv_zero_inf", `puts(1.fdiv(0).infinite?)`, "1\n"},
		{"fdiv_neg_zero_inf", `puts(-1.fdiv(0).infinite?)`, "-1\n"},
		{"fdiv_float", `puts(8.fdiv(9.0) > 0.88)`, "true\n"},
		{"fdiv_rational", `puts(1.fdiv(Rational(1, 5)))`, "5.0\n"},
		{"fdiv_rational_zero", `puts(1.fdiv(Rational(0, 1)).infinite?)`, "1\n"},
		{"fdiv_coerce", coercePrelude + `puts(1.fdiv(FdivCoerce.new))`, "0.1\n"},

		// --- Float#fdiv ---
		{"float_fdiv", `puts(8.0.fdiv(2))`, "4.0\n"},

		// --- Integer#div operand rules ---
		{"div_bignum", `puts((2**70).div(2**60))`, "1024\n"},
		{"div_float", `puts(1.div(0.2))`, "5\n"},
		{"div_rational", `puts(5.div(Rational(2, 1)))`, "2\n"},
		{"div_coerce", coercePrelude + `puts(5.div(DivCoerce.new))`, "5\n"},

		// --- Float#divmod ---
		{"float_divmod", `p 13.0.divmod(4)`, "[3, 1.0]\n"},

		// --- round precision: #to_int coercion, finite Float truncation ---
		{"round_to_int_mock", coercePrelude + `puts(12345.round(ToIntArg.new))`, "12300\n"},
		{"round_float_ndigits", `puts(12.345678.round(3.999))`, "12.346\n"},
		{"round_big_float_ndigits", `puts(0.42.round(2.0**30))`, "0.42\n"},
		{"int_round_pos_ndigits", `puts(42.round(2))`, "42\n"},

		// --- Bignum#bit_length ---
		{"bit_length_pos", `puts((2**70).bit_length)`, "71\n"},
		{"bit_length_neg", `puts((-2**70).bit_length)`, "70\n"},

		// --- Float#<=> ---
		{"cmp_nan_right", `p 1.0 <=> (0.0/0.0)`, "nil\n"},
		{"cmp_nan_left", `p (0.0/0.0) <=> 1.0`, "nil\n"},
		{"cmp_int", `puts(1.5 <=> 5)`, "-1\n"},
		{"cmp_rational", `puts(1.5 <=> Rational(3, 2))`, "0\n"},
		{"cmp_inf_infpos", coercePrelude + `puts(Float::INFINITY <=> InfPos.new)`, "0\n"},
		{"cmp_neg_inf_infpos", coercePrelude + `puts(-Float::INFINITY <=> InfPos.new)`, "-1\n"},
		{"cmp_inf_infneg", coercePrelude + `puts(Float::INFINITY <=> InfNeg.new)`, "1\n"},
		{"cmp_inf_infnil", coercePrelude + `puts(Float::INFINITY <=> InfNil.new)`, "1\n"},
		{"cmp_coerce", coercePrelude + `puts(2.33 <=> ModCoerce.new)`, "1\n"},
		{"cmp_non_numeric", `p 1.0 <=> "1"`, "nil\n"},

		// --- residual aliases (identity of the two UnboundMethods) ---
		{"alias_int_inspect", `puts(Integer.instance_method(:inspect) == Integer.instance_method(:to_s))`, "true\n"},
		{"alias_int_magnitude", `puts(Integer.instance_method(:magnitude) == Integer.instance_method(:abs))`, "true\n"},
		{"alias_int_next", `puts(Integer.instance_method(:next) == Integer.instance_method(:succ))`, "true\n"},
		{"alias_float_to_int", `puts(Float.instance_method(:to_int) == Float.instance_method(:to_i))`, "true\n"},
		{"alias_float_magnitude", `puts(Float.instance_method(:magnitude) == Float.instance_method(:abs))`, "true\n"},
		{"alias_num_modulo", `puts(Numeric.instance_method(:modulo) == Numeric.instance_method(:%))`, "true\n"},
		{"alias_num_magnitude", `puts(Numeric.instance_method(:magnitude) == Numeric.instance_method(:abs))`, "true\n"},
		{"alias_num_conj", `puts(Numeric.instance_method(:conj) == Numeric.instance_method(:conjugate))`, "true\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestIntegerFloatResidualErrors covers the raising branches: floored Bignum
// division by zero, Float#% zero and Float#divmod NaN/Infinity/zero, argument
// coercion failures, round-precision RangeError/TypeError, and the arity guards.
func TestIntegerFloatResidualErrors(t *testing.T) {
	const prelude = `
class BadToInt; def to_int; "x"; end; end
`
	cases := []struct{ name, src, want string }{
		// --- division by zero ---
		{"big_div_zero", `(2**70) / 0`, "ZeroDivisionError"},
		{"big_mod_zero", `(2**70) % 0`, "ZeroDivisionError"},
		{"float_mod_zero_int", `1.0 % 0`, "ZeroDivisionError"},
		{"float_mod_zero_float", `1.0 % 0.0`, "ZeroDivisionError"},
		{"float_modulo_zero", `1.0.modulo(0)`, "ZeroDivisionError"},
		{"int_div_float_zero", `10.div(0.0)`, "ZeroDivisionError"},
		{"int_div_int_zero", `10.div(0)`, "ZeroDivisionError"},

		// --- Float#divmod domain / zero ---
		{"divmod_nan_self", `Float::NAN.divmod(1)`, "FloatDomainError"},
		{"divmod_nan_other", `1.0.divmod(Float::NAN)`, "FloatDomainError"},
		{"divmod_inf_self", `Float::INFINITY.divmod(1)`, "FloatDomainError"},
		{"divmod_zero", `1.0.divmod(0)`, "ZeroDivisionError"},
		{"divmod_non_numeric", `1.0.divmod("x")`, "TypeError"},

		// --- coerce / fdiv operand errors ---
		{"int_coerce_nil", `1.coerce(nil)`, "TypeError"},
		{"int_coerce_bad_string", `1.coerce(":)")`, "ArgumentError"},
		{"int_fdiv_non_numeric", `1.fdiv("x")`, "TypeError"},
		{"float_fdiv_non_numeric", `1.0.fdiv("x")`, "TypeError"},

		// --- round precision RangeError / TypeError ---
		{"round_bignum_ndigits", `42.round(10**40)`, "RangeError"},
		{"round_beyond_int", `42.round(1 << 31)`, "RangeError"},
		{"round_infinity", `42.round(Float::INFINITY)`, "RangeError"},
		{"round_nan", `42.round(Float::NAN)`, "RangeError"},
		{"round_no_to_int", `5.round("x")`, "TypeError"},
		{"round_to_int_bad", prelude + `5.round(BadToInt.new)`, "TypeError"},

		// --- Float#<=> misbehaving coerce ---
		{"cmp_bad_coerce", `
class Bad; def coerce(o); :incorrect; end; end
4.2 <=> Bad.new`, "coerce must return"},

		// --- arity guards ---
		{"gcd_arity", `12.gcd(30, 20)`, "given 2, expected 1"},
		{"lcm_arity", `12.lcm(30, 20)`, "ArgumentError"},
		{"gcdlcm_arity", `12.gcdlcm(30, 20)`, "ArgumentError"},
		{"to_r_arity", `287.to_r(2)`, "given 1, expected 0"},
		{"rationalize_arity", `1.rationalize(1, 2)`, "expected 0..1"},
		{"float_rationalize_arity", `1.0.rationalize(1, 2)`, "ArgumentError"},
		{"imaginary_arity", `1.imaginary(1)`, "ArgumentError"},
		{"fdiv_arity", `1.fdiv(6, 0.2)`, "ArgumentError"},
		{"float_fdiv_arity", `1.0.fdiv(6, 0.2)`, "ArgumentError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}
