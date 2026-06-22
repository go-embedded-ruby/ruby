package vm_test

import (
	"strings"
	"testing"
)

// TestSymbolArrayCoerce covers Symbol string-ish methods, Array#<=>, nil.to_a and
// Integer/Float#coerce. Asserted against MRI Ruby 4.0.5.
func TestSymbolArrayCoerce(t *testing.T) {
	cases := []struct{ src, want string }{
		// Symbol: length/size, case methods (return Symbols), [], succ, empty?, with/without.
		{`p [:abc.length, :abc.size, :"".empty?, :abc.empty?]`, "[3, 3, true, false]\n"},
		{`p [:abc.upcase, :ABC.downcase, :hello.capitalize, :Hello.swapcase, :az.succ]`, "[:ABC, :abc, :Hello, :hELLO, :ba]\n"},
		{`p [:abc[1], :abc[1, 2], :abc[1..2]]`, "[\"b\", \"bc\", \"bc\"]\n"},
		{`p [:foo.start_with?("f"), :foo.start_with?("x", "fo"), :foo.start_with?("z"), :foo.end_with?("oo"), :foo.end_with?("x")]`, "[true, true, false, true, false]\n"},
		// Array#<=>: element-wise, then length; nil for an incomparable element or non-array.
		{`p [([1, 2, 3] <=> [1, 2, 4]), ([1, 2, 3] <=> [1, 2]), ([1, 2] <=> [1, 2, 3]), ([1, 2] <=> [1, 2])]`, "[-1, 1, -1, 0]\n"},
		{`p ([1, "a"] <=> [1, 2])`, "nil\n"}, // incomparable element
		{`p ([1, 2] <=> 5)`, "nil\n"},        // non-array operand
		// nil.to_a.
		{`p nil.to_a`, "[]\n"},
		// coerce: both-int stays Integer; any Float -> both Float.
		{`p [5.coerce(3), 5.coerce(2.0), 2.0.coerce(5)]`, "[[3, 5], [2.0, 5.0], [5.0, 2.0]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`5.coerce("x")`, `invalid value for Float(): "x"`},
		{`5.coerce(nil)`, "can't convert nil into Float"},
		{`5.coerce(true)`, "can't convert true into Float"},
		{`5.coerce([])`, "can't convert Array into Float"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
