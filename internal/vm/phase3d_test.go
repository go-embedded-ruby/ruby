package vm_test

import (
	"strings"
	"testing"
)

// Method chaining continues after a block: recv.map { … }.join, etc.
func TestChainAfterBlock(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"map_join", `p [1, 2, 3].map { |n| n * 2 }.join(",")`, "\"2,4,6\"\n"},
		{"select_map", `p [1, 2, 3].select { |n| n.odd? }.map { |n| n * 10 }`, "[10, 30]\n"},
		{"map_sum", `p [1, 2, 3].map { |x| x + 1 }.sum`, "9\n"},
		{"chain_three", `p [1, 2, 3].map { |n| n * n }.select { |n| n > 3 }.reverse`, "[9, 4]\n"},
		{"index_after_block", `p [1, 2, 3, 4].select { |n| n.even? }.first`, "2\n"},
		{"in_interp", `puts "r #{[1, 2, 3].map { |n| n * 2 }.join("-")}"`, "r 2-4-6\n"},
		{"hash_map_chain", `p({a: 1, b: 2}.map { |k, v| v }.sum)`, "3\n"},
		// a block still does NOT attach to a non-call (local var), so `{` there is
		// left for the surrounding context; chaining still works on plain calls
		{"plain_chain", `p [3, 1, 2].sort.reverse`, "[3, 2, 1]\n"},
		// do...end chains too
		{"do_end_chain", "r = [1, 2, 3].map do |n|\n  n * 2\nend.join(\",\")\np r", "\"2,4,6\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// A block can only attach to a method call; following a non-call it is left for
// the surrounding parse (here a syntax error).
func TestBlockOnNonCall(t *testing.T) {
	if err := runErr(t, `nil { 1 }`); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("got %v, want a parse error", err)
	}
}
