package vm_test

import (
	"strings"
	"testing"
)

// The expected strings below were each verified byte-for-byte against MRI Ruby
// 4.0.x (`ruby -e ...`), the reference this VM targets for Numeric#step /
// Integer#step / Float#step and the Enumerator::ArithmeticSequence they return
// without a block. See core/numeric/step_spec.rb in the ruby/spec corpus.

// evalStep runs src and returns trimmed stdout, using the shared eval helper.
func evalStep(t *testing.T, src string) string {
	t.Helper()
	return strings.TrimRight(eval(t, src), "\n")
}

func TestNumericStepArithmeticSequence(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// The no-block form is an Enumerator::ArithmeticSequence (MRI 2.6+).
		{"class_int", `p 1.step(10, 2).class`, "Enumerator::ArithmeticSequence"},
		{"class_default_step", `p 1.step(10).class`, "Enumerator::ArithmeticSequence"},
		{"class_endless", `p 1.step.class`, "Enumerator::ArithmeticSequence"},
		{"class_keyword", `p 1.step(to: 10, by: 2).class`, "Enumerator::ArithmeticSequence"},
		{"class_float", `p 1.0.step(2.0, 0.5).class`, "Enumerator::ArithmeticSequence"},
		{"class_nil_limit", `p 1.step(nil, 2).class`, "Enumerator::ArithmeticSequence"},

		// The defining triple is exposed through the sequence accessors.
		{"begin", `p 1.step(10, 2).begin`, "1"},
		{"end", `p 1.step(10, 2).end`, "10"},
		{"end_endless", `p 1.step.end`, "nil"},
		{"step", `p 1.step(10, 2).step`, "2"},
		{"step_default", `p 1.step(10).step`, "1"},
		{"exclude_end", `p 1.step(10, 2).exclude_end?`, "false"},

		// #each / #to_a / #size / #first reuse the step machinery.
		{"to_a", `p 1.step(10, 2).to_a`, "[1, 3, 5, 7, 9]"},
		{"to_a_negative", `p 5.step(1, -2).to_a`, "[5, 3, 1]"},
		{"to_a_keyword", `p 1.step(to: 10, by: 2).to_a`, "[1, 3, 5, 7, 9]"},
		{"to_a_float", `p 1.0.step(3.0, 0.5).to_a`, "[1.0, 1.5, 2.0, 2.5, 3.0]"},
		{"size", `p 1.step(10, 2).size`, "5"},
		{"size_endless", `p 1.step.size`, "Infinity"},
		{"size_infinity_step", `p 1.step(10, Float::INFINITY).size`, "1"},
		{"first_no_arg", `p 1.step(10, 2).first`, "1"},
		{"first_n", `p 1.step(10, 2).first(3)`, "[1, 3, 5]"},
		{"endless_first", `p 1.step(nil, 2).first(3)`, "[1, 3, 5]"},

		// #last materialises the bounded sequence.
		{"last_no_arg", `p 1.step(10, 2).last`, "9"},
		{"last_n", `p 1.step(10, 2).last(2)`, "[7, 9]"},
		{"last_n_clamp", `p 1.step(10, 2).last(99)`, "[1, 3, 5, 7, 9]"},

		// An empty sequence (begin already past end) is still an ArithmeticSequence.
		{"empty_class", `p 1.step(0, 2).class`, "Enumerator::ArithmeticSequence"},
		{"empty_to_a", `p 1.step(0, 2).to_a`, "[]"},
		{"empty_first", `p 1.step(0, 2).first`, "nil"},
		{"empty_last", `p 1.step(0, 2).last`, "nil"},

		// A String step is NOT an ArithmeticSequence: MRI returns a plain Enumerator
		// whose #size raises only when asked.
		{"string_class", `p 1.step(5, "foo").class`, "Enumerator"},
		{"string_class_float", `p 1.1.step(5.1, "foo").class`, "Enumerator"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalStep(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestNumericStepArgErrors covers the argument-validation messages, each verified
// against MRI: a String step is an ArgumentError quoting the class, a nil step is
// a TypeError, and both the block form and the #size of the no-block form raise.
func TestNumericStepArgErrors(t *testing.T) {
	rescueClassMsg := func(expr string) string {
		return "begin; " + expr + "; puts 'NORAISE'; rescue Exception => e; puts e.class; puts e.message; end"
	}
	cases := []struct{ name, expr, want string }{
		{"int_string_numeric", `1.step(5, "1") {}`, "ArgumentError\ncomparison of String with 0 failed"},
		{"int_string_decimal", `1.step(5, "0.1") {}`, "ArgumentError\ncomparison of String with 0 failed"},
		{"int_string_ratio", `1.step(5, "1/3") {}`, "ArgumentError\ncomparison of String with 0 failed"},
		{"int_string_alpha", `1.step(5, "foo") {}`, "ArgumentError\ncomparison of String with 0 failed"},
		{"float_string", `1.1.step(5.1, "foo") {}`, "ArgumentError\ncomparison of String with 0 failed"},
		{"string_size", `1.step(5, "1").size`, "ArgumentError\ncomparison of String with 0 failed"},
		{"float_string_size", `1.1.step(5.1, "foo").size`, "ArgumentError\ncomparison of String with 0 failed"},
		{"nil_step_block", `1.step(5, nil) {}`, "TypeError\nstep must be numeric"},
		{"float_nil_step_block", `1.0.step(5.0, nil) {}`, "TypeError\nstep must be numeric"},
		{"zero_step", `1.step(5, 0) {}`, "ArgumentError\nstep can't be 0"},
		{"symbol_step_block", `1.step(5, :x) {}`, "ArgumentError\ncomparison of Symbol with 0 failed"},
		// The block-form last(n) rejects a negative count as MRI does.
		{"last_negative", `1.step(10, 2).last(-1)`, "ArgumentError\nnegative array size"},
		// #last on an endless sequence refuses rather than looping for ever.
		{"last_endless", `1.step.last`, "RangeError\ncannot get the last element of endless arithmetic sequence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalStep(t, rescueClassMsg(tc.expr))
			if got != tc.want {
				t.Errorf("expr=%q\n got=%q\nwant=%q", tc.expr, got, tc.want)
			}
		})
	}
}

// TestRangeStepStringUnchanged pins the shared walkers' behaviour for a String
// step on a numeric Range, which Numeric#step must not disturb: MRI's Range#step
// keeps its own (inherited) message, so this VM keeps its long-standing one.
func TestRangeStepStringUnchanged(t *testing.T) {
	src := "begin; (1..5).step(\"2\") {}; puts 'NORAISE'; rescue Exception => e; puts e.class; end"
	if got := evalStep(t, src); got != "ArgumentError" {
		t.Errorf("got=%q want ArgumentError", got)
	}
	// A numeric Range#step (no String) still drives rejectStringStep's pass branch.
	if got := evalStep(t, `p (1..6).step(2).to_a`); got != "[1, 3, 5]" {
		t.Errorf("got=%q want [1, 3, 5]", got)
	}
}

// TestNumericStepInfinity covers the infinite-step edge directly (no lambda
// forwarding): an infinite step yields the start value exactly once when it lies
// on the correct side of the limit, including the both-Infinity case.
func TestNumericStepInfinity(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"pos_both_inf", `r=[]; Float::INFINITY.step(Float::INFINITY, Float::INFINITY){|x| r<<x}; p r`, "[Infinity]"},
		{"neg_both_inf", `r=[]; Float::INFINITY.step(Float::INFINITY, -Float::INFINITY){|x| r<<x}; p r`, "[Infinity]"},
		{"pos_inf_step", `r=[]; 42.step(100, Float::INFINITY){|x| r<<x}; p r`, "[42.0]"},
		{"past_limit", `r=[]; 100.step(42, Float::INFINITY){|x| r<<x}; p r`, "[]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalStep(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}
