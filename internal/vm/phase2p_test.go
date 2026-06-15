package vm_test

import (
	"strings"
	"testing"
)

func TestArrayMethods(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"sort_int", `p [3, 1, 2].sort`, "[1, 2, 3]\n"},
		{"sort_str", `p ["b", "a", "c"].sort`, "[\"a\", \"b\", \"c\"]\n"},
		{"sort_single", `p [5].sort`, "[5]\n"},
		{"sort_by", `p [3, 1, 2].sort_by { |x| -x }`, "[3, 2, 1]\n"},
		{"reverse", `p [1, 2, 3].reverse`, "[3, 2, 1]\n"},
		{"reverse_empty", `p [].reverse`, "[]\n"},
		{"uniq", `p [1, 1, 2, 3, 3].uniq`, "[1, 2, 3]\n"},
		{"flatten", `p [1, [2, [3, 4]], 5].flatten`, "[1, 2, 3, 4, 5]\n"},
		{"flatten_flat", `p [1, 2].flatten`, "[1, 2]\n"},
		{"compact", `p [1, nil, 2, nil].compact`, "[1, 2]\n"},
		{"join_sep", `p [1, 2, 3].join("-")`, "\"1-2-3\"\n"},
		{"join_nested", `p [1, [2, 3]].join("-")`, "\"1-2-3\"\n"},
		{"join_default", `p [1, 2, 3].join`, "\"123\"\n"},
		{"index_found", `p [1, 2, 3].index(2)`, "1\n"},
		{"index_missing", `p [1, 2, 3].index(9)`, "nil\n"},
		{"min_by", `p ["aa", "b", "ccc"].min_by { |s| s.length }`, "\"b\"\n"},
		{"max_by", `p ["aa", "b", "ccc"].max_by { |s| s.length }`, "\"ccc\"\n"},
		{"min_by_empty", `p [].min_by { |x| x }`, "nil\n"},
		{"max_by_tie", `p ["aa", "bb"].max_by { |s| s.length }`, "\"aa\"\n"},
		{"take", `p [1, 2, 3].take(2)`, "[1, 2]\n"},
		{"take_over", `p [1, 2].take(9)`, "[1, 2]\n"},
		{"drop", `p [1, 2, 3].drop(2)`, "[3]\n"},
		{"drop_over", `p [1, 2].drop(9)`, "[]\n"},
		{"each_with_object", `p([1, 2, 3].each_with_object([]) { |x, acc| acc << x * 2 })`, "[2, 4, 6]\n"},
		// sort does not mutate the receiver
		{"sort_nonmutating", "a = [3, 1, 2]\na.sort\np a", "[3, 1, 2]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestArrayMethodErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"sort_incomparable", `[1, "x"].sort`, "ArgumentError"},
		{"sort_by_no_block", `[1].sort_by`, "LocalJumpError"},
		{"min_by_no_block", `[1].min_by`, "LocalJumpError"},
		{"each_with_object_no_block", `[1].each_with_object([])`, "LocalJumpError"},
		{"take_negative", `[1].take(-1)`, "ArgumentError"},
		{"drop_negative", `[1].drop(-1)`, "ArgumentError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %s", tc.src, err, tc.want)
			}
		})
	}
}
