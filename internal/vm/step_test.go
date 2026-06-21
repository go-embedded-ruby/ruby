package vm_test

import (
	"strings"
	"testing"
)

// TestStep covers Range#step and Integer#step (integer and float walks, both
// directions, blockless Enumerator, and the error cases) against MRI Ruby 4.0.5.
func TestStep(t *testing.T) {
	cases := []struct{ src, want string }{
		// Range#step — integer.
		{`p (1..10).step(2).to_a`, "[1, 3, 5, 7, 9]\n"},
		{`p (1...10).step(2).to_a`, "[1, 3, 5, 7, 9]\n"},
		{`p (1..3).step.to_a`, "[1, 2, 3]\n"}, // default step 1
		{`r = []; (1..10).step(3) { |i| r << i }; p r`, "[1, 4, 7, 10]\n"},
		{`p (1..10).step(3) { |i| }`, "1..10\n"}, // block form returns the range
		{`p (5..1).step(1).to_a`, "[]\n"},        // empty
		// Range#step — float.
		{`p (1.0..2.0).step(0.5).to_a`, "[1.0, 1.5, 2.0]\n"},
		{`p (3.0..1.0).step(-0.5).to_a`, "[3.0, 2.5, 2.0, 1.5, 1.0]\n"},
		{`p (10...2).step(-2).to_a`, "[10, 8, 6, 4]\n"},   // exclusive, negative step
		{`p (10..2).step(-2).to_a`, "[10, 8, 6, 4, 2]\n"}, // inclusive, negative step
		{`p (1..10).step(2.5).to_a`, "[1.0, 3.5, 6.0, 8.5]\n"},
		// Integer#step.
		{`p 1.step(10, 2).to_a`, "[1, 3, 5, 7, 9]\n"},
		{`p 10.step(1, -2).to_a`, "[10, 8, 6, 4, 2]\n"},
		{`p 1.step(10).to_a`, "[1, 2, 3, 4, 5, 6, 7, 8, 9, 10]\n"}, // default step 1
		{`p 1.step(5) { |i| }`, "1\n"},                            // block form returns self
		{`p (1..5).step(2).class`, "Enumerator\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// step can't be 0 (integer and float walks) raises ArgumentError; missing the
	// Integer#step limit raises too; a non-numeric range can't be stepped (the
	// same TypeError Range#each already raises for non-integer endpoints).
	for _, src := range []string{
		`(1..3).step(0) { |x| }`,
		`(1.0..3.0).step(0.0) { |x| }`,
		`1.step(3, 0) { |x| }`,
		`1.step`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
			t.Errorf("src=%q got=%v want ArgumentError", src, err)
		}
	}
	if err := runErr(t, `("a".."c").step(1) { |x| }`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("string-range step: got=%v want TypeError", err)
	}
}
