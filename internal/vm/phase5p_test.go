package vm_test

import (
	"strings"
	"testing"
)

func TestArrayWindowBatch(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"take_while", `p [1, 2, 3, 4, 1].take_while { |x| x < 3 }`, "[1, 2]\n"},
		{"take_while_all", `p [1, 2, 3].take_while { |x| x < 10 }`, "[1, 2, 3]\n"},
		{"take_while_none", `p [5, 1].take_while { |x| x < 3 }`, "[]\n"},
		{"drop_while", `p [1, 2, 3, 4, 1].drop_while { |x| x < 3 }`, "[3, 4, 1]\n"},
		{"drop_while_all", `p [1, 2, 3].drop_while { |x| x < 10 }`, "[]\n"},
		{"drop_while_none", `p [5, 1].drop_while { |x| x < 3 }`, "[5, 1]\n"},
		{"each_cons", "r = []\n[1, 2, 3, 4].each_cons(2) { |a| r << a }\np r", "[[1, 2], [2, 3], [3, 4]]\n"},
		{"each_cons_returns_self", `p [1, 2, 3, 4].each_cons(2) { |a| a }`, "[1, 2, 3, 4]\n"},
		{"each_cons_too_big", `p [1, 2].each_cons(3) { |a| a }`, "[1, 2]\n"},
		{"each_slice_returns_self", `p [1, 2, 3, 4].each_slice(2) { |a| a }`, "[1, 2, 3, 4]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestArrayWindowErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"cons_no_block", `[1, 2].each_cons(2)`, "LocalJumpError"},
		{"cons_bad_size", `[1, 2].each_cons(0) { |a| a }`, "invalid size"},
		{"take_while_no_block", `[1, 2].take_while`, "LocalJumpError"},
		{"drop_while_no_block", `[1, 2].drop_while`, "LocalJumpError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %q", tc.src, err, tc.want)
			}
		})
	}
}
