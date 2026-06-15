package vm_test

import (
	"strings"
	"testing"
)

func TestArrayBatch(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"sum", `p [1, 2, 3, 4].sum`, "10\n"},
		{"sum_init", `p [1, 2, 3].sum(10)`, "16\n"},
		{"sum_empty", `p [].sum`, "0\n"},
		{"sum_strings", `p ["a", "b", "c"].sum("")`, "\"abc\"\n"},
		{"sum_floats", `p [1.5, 2.5].sum`, "4.0\n"},
		{"each_slice", "r = []\n[1, 2, 3, 4, 5].each_slice(2) { |s| r << s }\np r", "[[1, 2], [3, 4], [5]]\n"},
		{"rotate", `p [1, 2, 3, 4, 5].rotate`, "[2, 3, 4, 5, 1]\n"},
		{"rotate_n", `p [1, 2, 3, 4, 5].rotate(2)`, "[3, 4, 5, 1, 2]\n"},
		{"rotate_neg", `p [1, 2, 3, 4, 5].rotate(-1)`, "[5, 1, 2, 3, 4]\n"},
		{"rotate_empty", `p [].rotate`, "[]\n"},
		{"flatten_depth", `p [1, [2, [3, [4]]]].flatten(1)`, "[1, 2, [3, [4]]]\n"},
		{"flatten_full", `p [1, [2, [3]]].flatten`, "[1, 2, 3]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestEachSliceErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"no_block", `[1, 2].each_slice(1)`, "LocalJumpError"},
		{"bad_size", `[1, 2].each_slice(0) { |s| s }`, "ArgumentError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %s", tc.src, err, tc.want)
			}
		})
	}
}
