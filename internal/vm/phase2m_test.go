package vm_test

import (
	"strings"
	"testing"
)

func TestBreakNext(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// break in a block terminates the iterator
		{"break_each", "r = []\n[1, 2, 3, 4].each { |x| break if x == 3; r << x }\np r", "[1, 2]\n"},
		{"break_value", `p([1, 2, 3].each { |x| break x * 10 if x == 2 })`, "20\n"},
		{"break_bare_nil", `p([1, 2, 3].each { |x| break })`, "nil\n"},
		// next in a block returns from that iteration
		{"next_skip", "r = []\n[1, 2, 3, 4].each { |x| next if x.even?; r << x }\np r", "[1, 3]\n"},
		{"next_value_map", `p([1, 2, 3].map { |x| next 0 if x == 2; x })`, "[1, 0, 3]\n"},
		// break unwinds to the outer call even through Ruby-level Enumerable#map
		{"break_through_map", `p([1, 2, 3, 4].map { |x| break 99 if x == 3; x })`, "99\n"},
		// break/next in while loops
		{"while_break", "i = 0\nwhile i < 10\n  i = i + 1\n  break if i == 5\nend\np i", "5\n"},
		{"while_next", "i = 0\nn = 0\nwhile i < 6\n  i = i + 1\n  next if i.even?\n  n = n + 1\nend\np n", "3\n"},
		// nested blocks: break exits the innermost iterator
		{"nested_break", "r = []\n[1, 2].each { |a| [10, 20].each { |b| break if b == 20; r << b } }\np r", "[10, 10]\n"},
		// break inside a block nested in a loop still targets the block
		{"break_block_in_loop", "r = []\ni = 0\nwhile i < 2\n  [7, 8].each { |x| break if x == 8; r << x }\n  i = i + 1\nend\np r", "[7, 7]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestBreakNextErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"break_top_level", `break`, "Invalid break"},
		{"next_top_level", `next`, "Invalid next"},
		{"break_in_method", "def f\n  break\nend\nf", "Invalid break"},
		// a non-break panic (runtime error) inside a block must propagate through
		// the break-catching send, not be swallowed.
		{"runtime_error_in_block", `[1, 2].each { |x| nil.no_such_method }`, "NoMethodError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %s", tc.src, err, tc.want)
			}
		})
	}
}
