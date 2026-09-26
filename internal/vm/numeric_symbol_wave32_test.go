// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// numericSubclassPrelude defines the Numeric subclasses the generic Numeric
// methods are exercised through: a plain Numeric subclass answers none of the
// operators, so every method here has to reach for the RECEIVER's own #<, #>,
// #%, #/, #to_i, #to_r and #coerce — which is the whole point of
// numeric.c's definitions at tag v3_4_0.
const numericSubclassPrelude = `
class Bare < Numeric; end
class Sub < Numeric
  def <(o) = false
  def >(o) = true
  def to_i = 42
  def /(o) = 7.5
  def ==(o) = true
  def to_r = Rational(3, 4)
  def to_f = 1.5
end
class NegSub < Numeric
  def <(o) = true
  def >(o) = false
  def -@ = :negated
  def %(o) = -1.0
  def -(o) = :minus
end
class PosSub < Numeric
  def <(o) = false
  def >(o) = true
  def %(o) = 1.0
  def -(o) = :minus
end
`

// TestNumericGenericMethods pins the generic Numeric instance methods to
// numeric.c (and rational.c's rb_numeric_quo) at tag v3_4_0. Every `want` was
// taken from MRI 4.0.5.
func TestNumericGenericMethods(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// num_positive_p / num_negative_p / num_abs ask #> and #<, not #<=>.
		{"positive", `p Sub.new.positive?`, "true\n"},
		{"negative", `p Sub.new.negative?`, "false\n"},
		{"neg_subclass_negative", `p NegSub.new.negative?`, "true\n"},
		{"abs_non_negative", `p Sub.new.abs.class`, "Sub\n"},
		{"abs_negative_sends_uminus", `p NegSub.new.abs`, ":negated\n"},
		// num_div floors #/ after refusing a zero divisor.
		{"div", `p Sub.new.div(2)`, "7\n"},
		// num_eql: a different class is never eql?, a matching one asks #==.
		{"eql_same_class", `p Sub.new.eql?(Sub.new)`, "true\n"},
		{"eql_other_class", `p Sub.new.eql?(1)`, "false\n"},
		// rb_numeric_quo: a Float operand takes #fdiv, anything else goes
		// through #to_r and Rational#/.
		{"quo_rational", `p Sub.new.quo(2)`, "(3/8)\n"},
		{"quo_float", `p Sub.new.quo(2.0)`, "0.75\n"},
		// num_remainder, whose #< / #> order core/numeric/remainder_spec counts.
		{"remainder_zero_result", `p NegSub.new.remainder(0.0)`, "-1.0\n"},
		{"remainder_both_positive", `p PosSub.new.remainder(3)`, "1.0\n"},
		{"remainder_both_negative", `p NegSub.new.remainder(-3)`, "-1.0\n"},
		{"remainder_positive_negative", `p PosSub.new.remainder(-3)`, "4.0\n"}, // z - y, z being the Float 1.0
		{"remainder_negative_positive", `p NegSub.new.remainder(3)`, "-4.0\n"},
		// ... and its infinite-divisor escape hatch, which answers the receiver.
		{"remainder_infinite_divisor", `p NegSub.new.remainder(Float::INFINITY).class`, "NegSub\n"},
		// A non-Numeric operand goes through do_coerce first.
		{"remainder_coerces", `
class Coercer
  def coerce(o) = [PosSub.new, 3]
end
p PosSub.new.remainder(Coercer.new)`, "1.0\n"},
		// Integer and Float keep their own, nearer definitions.
		{"integer_keeps_own_quo", `p 5.quo(2)`, "(5/2)\n"},
		{"float_keeps_own_quo", `p 2.quo(2.5)`, "0.8\n"},
		{"integer_positive", `p 1.positive?, 0.positive?, (-1).positive?`, "true\nfalse\nfalse\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, numericSubclassPrelude+tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestNumericGenericErrors pins the raises, including the two error shapes a
// missing operator produces: rb_num_compare_with_zero's rb_cmperr when the
// receiver has no #< / #>, and rb_Float's / rb_convert_type's TypeError.
func TestNumericGenericErrors(t *testing.T) {
	cases := []struct{ name, src, class, msg string }{
		{"abs_without_less_than", `Bare.new.abs`, "ArgumentError", "comparison of Bare with 0 failed"},
		{"positive_without_greater_than", `Bare.new.positive?`, "ArgumentError", "comparison of Bare with 0 failed"},
		{"div_by_zero", `Sub.new.div(0)`, "ZeroDivisionError", "divided by 0"},
		{"div_by_float_zero", `Sub.new.div(0.0)`, "ZeroDivisionError", "divided by 0"},
		{"div_by_complex_zero", `Sub.new.div(Complex(0, 0))`, "ZeroDivisionError", "divided by 0"},
		{"quo_without_to_r", `Bare.new.quo(2)`, "TypeError", "can't convert Bare into Rational"},
		{"quo_to_r_not_rational", `
class BadToR < Numeric
  def to_r = 1
end
BadToR.new.quo(19)`, "TypeError", "can't convert BadToR to Rational (BadToR#to_r gives Integer)"},
		{"fdiv_without_to_f", `Bare.new.fdiv(2)`, "TypeError", "can't convert Bare into Float"},
		{"remainder_uncoercible", `Bare.new.remainder(Object.new)`, "TypeError", "Object can't be coerced into Bare"},
		{"remainder_bad_coerce", `
class BadCoerce
  def coerce(o) = 1
end
Bare.new.remainder(BadCoerce.new)`, "TypeError", "coerce must return [x, y]"},
		{"remainder_without_comparison", `
class NoLt < Numeric
  def %(o) = 5
end
NoLt.new.remainder(3)`, "ArgumentError", "comparison of NoLt with 0 failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			class, msg := evalErr(t, numericSubclassPrelude+tc.src)
			if class != tc.class || !strings.Contains(msg, tc.msg) {
				t.Fatalf("src=%q got %s: %q want %s: %q", tc.src, class, msg, tc.class, tc.msg)
			}
		})
	}
}

// TestSymbolNameAndDelegation pins the Symbol surface string.c v3_4_0 defines
// over rb_sym2str: #name hands back the ONE interned frozen String, #to_s a
// mutable copy of it, and #start_with? / #end_with? / #=~ are the String
// implementations over that name (so a Regexp operand works and $~ is set).
// The encodings are rb_str_intern's: an ASCII-only name is US-ASCII whatever
// it was interned from.
func TestSymbolNameAndDelegation(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"name", `p :ruby.name`, "\"ruby\"\n"},
		{"name_is_interned", `p :"ruby_3".name.equal?(:"ruby_#{1 + 2}".name)`, "true\n"},
		{"name_frozen", `p :symbol.name.frozen?`, "true\n"},
		{"name_ascii_encoding", `p :ruby.name.encoding`, "#<Encoding:US-ASCII>\n"},
		{"name_utf8_encoding", `p :ルビー.name.encoding`, "#<Encoding:UTF-8>\n"},
		{"name_binary_encoding", `p "\xff".b.to_sym.name.encoding`, "#<Encoding:BINARY (ASCII-8BIT)>\n"},
		{"to_s_encoding", `p :ruby.to_s.encoding`, "#<Encoding:US-ASCII>\n"},
		{"to_s_is_mutable", `p :ruby.to_s.frozen?`, "false\n"},
		{"inspect_encoding", `p :ruby.inspect.encoding`, "#<Encoding:US-ASCII>\n"},
		{"match_operator", `p(:abc =~ /b/)`, "1\n"},
		{"match_operator_no_match", `p(:a =~ /b/)`, "nil\n"},
		{"match_operator_sets_backref", `
:a =~ /(.)/
p $1`, "\"a\"\n"},
		{"start_with_regexp", `p :abc.start_with?(/a/)`, "true\n"},
		{"start_with_to_str", `
class S
  def to_str = "a"
end
p :abc.start_with?(S.new)`, "true\n"},
		{"end_with_to_str", `
class S
  def to_str = "c"
end
p :abc.end_with?(S.new)`, "true\n"},
		{"start_with_partial_character", `p :é.start_with?("\xC3")`, "false\n"},
		// The pairs string.c registers against one C function compare equal.
		{"id2name_is_to_s", `p Symbol.instance_method(:id2name) == Symbol.instance_method(:to_s)`, "true\n"},
		{"intern_is_to_sym", `p Symbol.instance_method(:intern) == Symbol.instance_method(:to_sym)`, "true\n"},
		{"next_is_succ", `p Symbol.instance_method(:next) == Symbol.instance_method(:succ)`, "true\n"},
		{"size_is_length", `p Symbol.instance_method(:size) == Symbol.instance_method(:length)`, "true\n"},
		{"slice_is_aref", `p Symbol.instance_method(:slice) == Symbol.instance_method(:[])`, "true\n"},
		{"case_equal_is_equal", `p Symbol.instance_method(:===) == Symbol.instance_method(:==)`, "true\n"},
		{"equality", `p(:abc == :abc, :abc == "abc", :abc === :abc)`, "true\nfalse\ntrue\n"},
		{"id2name_value", `p :abc.id2name`, "\"abc\"\n"},
		{"intern_value", `p :abc.intern`, ":abc\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestSymbolClassRefusals: string.c v3_4_0 Init_Symbol calls
// rb_undef_alloc_func(rb_cSymbol) and undefines Symbol.new.
func TestSymbolClassRefusals(t *testing.T) {
	cases := []struct{ name, src, class, msg string }{
		{"allocate", `Symbol.allocate`, "TypeError", "allocator undefined for Symbol"},
		{"new", `Symbol.new`, "NoMethodError", "undefined method 'new' for class Symbol"},
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

// TestDeadBlockWarnings pins the two rb_warn / rb_warning messages MRI emits
// for a block that cannot be used: array.c's rb_ary_index / rb_ary_rindex /
// rb_ary_fetch / rb_ary_initialize and hash.c's rb_hash_fetch_m. rb_warn
// prints unless $VERBOSE is nil; rb_warning only when it is true. Both carry
// the caller's "path:lineno: warning: " prefix, so the assertions match on the
// tail. $stderr is swapped for a StringIO to capture them, which is also how
// the specs' `complain` matcher works.
func TestDeadBlockWarnings(t *testing.T) {
	const capture = "require 'stringio'\n$stderr = StringIO.new\n"
	tests := []struct{ name, src, want string }{
		{"array_index", `p [1, 2, 3].index(2) { 9 }`, "1\nwarning: given block not used\n"},
		{"array_rindex", `p [1, 2, 3].rindex(2) { 9 }`, "1\nwarning: given block not used\n"},
		{"array_fetch", `p [1, 2, 3].fetch(9, :foo) { |i| i * i }`,
			"81\nwarning: block supersedes default value argument\n"},
		{"hash_fetch", `p({}.fetch(9, :foo) { |i| i * i })`,
			"81\nwarning: block supersedes default value argument\n"},
		{"array_new_with_default", "$VERBOSE = false\np Array.new(2, :x) { |i| i }",
			"[0, 1]\nwarning: block supersedes default value argument\n"},
		{"array_new_no_args", "$VERBOSE = true\np Array.new { raise }",
			"[]\nwarning: given block not used\n"},
		{"array_initialize_no_args", "$VERBOSE = true\np [1, 2, 3].send(:initialize) { raise }",
			"[]\nwarning: given block not used\n"},
		// rb_warning is silent below -w, and rb_warn is silent under -W0.
		{"warning_silent_by_default", "$VERBOSE = false\np Array.new { 1 }", "[]\n"},
		{"warn_silent_under_w0", "$VERBOSE = nil\np [1, 2, 3].index(2) { 9 }", "1\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The captured text keeps MRI's "path:lineno: " prefix; strip it so
			// the expectation does not depend on the temporary script's name.
			src := capture + tc.src + "\n$stdout.print $stderr.string.gsub(/^[^ ]*: /, \"\")\n"
			if got := eval(t, src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
