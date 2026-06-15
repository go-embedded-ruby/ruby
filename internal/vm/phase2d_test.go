package vm_test

import (
	"strings"
	"testing"
)

func TestRanges(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"literal_inspect", `p(1..5)`, "1..5\n"},
		{"literal_inspect_excl", `p(1...5)`, "1...5\n"},
		{"to_s", `puts(1..5)`, "1..5\n"},
		{"class", `puts((1..5).class)`, "Range\n"},
		{"to_a", `p((1..5).to_a)`, "[1, 2, 3, 4, 5]\n"},
		{"to_a_excl", `p((1...5).to_a)`, "[1, 2, 3, 4]\n"},
		{"to_a_empty", `p((5..1).to_a)`, "[]\n"},
		{"size", `puts((1..5).size)`, "5\n"},
		{"size_excl", `puts((1...5).size)`, "4\n"},
		{"size_empty", `puts((5..1).size)`, "0\n"},
		{"count", `puts((1..5).count)`, "5\n"},
		{"begin", `puts((1..5).begin)`, "1\n"},
		{"end", `puts((1..5).end)`, "5\n"},
		{"first", `puts((1..5).first)`, "1\n"},
		{"last", `puts((1...5).last)`, "5\n"},
		{"min", `puts((1..5).min)`, "1\n"},
		{"max", `puts((1..5).max)`, "5\n"},
		{"max_excl", `puts((1...5).max)`, "4\n"},
		{"min_empty", `p((5..1).min)`, "nil\n"},
		{"max_empty", `p((5..1).max)`, "nil\n"},
		{"include_true", `puts((1..5).include?(3))`, "true\n"},
		{"include_excl_end", `puts((1...5).include?(5))`, "false\n"},
		{"cover_incl_end", `puts((1..5).cover?(5))`, "true\n"},
		{"member", `puts((1..5).member?(0))`, "false\n"},
		{"cover_float_member", `puts((1..10).cover?(2.5))`, "true\n"},
		{"cover_incomparable", `puts((1..5).cover?("x"))`, "false\n"},
		{"string_range_cover", `puts(("a".."c").cover?("b"))`, "true\n"},
		{"string_range_cover_below", `puts(("b".."d").cover?("a"))`, "false\n"},
		{"string_range_cover_int", `puts(("a".."c").cover?(1))`, "false\n"},
		{"exclude_end_false", `puts((1..5).exclude_end?)`, "false\n"},
		{"exclude_end_true", `puts((1...5).exclude_end?)`, "true\n"},
		{"each", "s = 0\n(1..3).each { |x| s = s + x }\nputs s", "6\n"},
		{"map", `p((1..3).map { |x| x * x })`, "[1, 4, 9]\n"},
		{"eq_true", `puts((1..5) == (1..5))`, "true\n"},
		{"eq_diff_excl", `puts((1..5) == (1...5))`, "false\n"},
		{"eq_diff_bound", `puts((1..5) == (1..6))`, "false\n"},
		{"eq_non_range", `puts((1..5) == "x")`, "false\n"},
		{"var_range", "r = 0..3\np r.to_a", "[0, 1, 2, 3]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestRangeEachNoBlock(t *testing.T) {
	if err := runErr(t, `(1..3).each`); err == nil || !strings.Contains(err.Error(), "LocalJumpError") {
		t.Fatalf("got %v want LocalJumpError", err)
	}
}

func TestRangeMapNoBlock(t *testing.T) {
	if err := runErr(t, `(1..3).map`); err == nil || !strings.Contains(err.Error(), "LocalJumpError") {
		t.Fatalf("got %v want LocalJumpError", err)
	}
}

// Float ranges raise on iteration in Ruby ("can't iterate from Float"); integer
// ranges only is the current phase's iterable subset.
func TestRangeFloatIterate(t *testing.T) {
	if err := runErr(t, `(1.0..5.0).to_a`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("got %v want TypeError", err)
	}
}

func TestRangeFloatSize(t *testing.T) {
	if err := runErr(t, `(1.0..5.0).size`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("got %v want TypeError", err)
	}
}

func TestRangeFloatEach(t *testing.T) {
	if err := runErr(t, `(1.0..3.0).each { |x| x }`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("got %v want TypeError", err)
	}
}
