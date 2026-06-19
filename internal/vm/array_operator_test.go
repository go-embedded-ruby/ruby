package vm_test

import (
	"strings"
	"testing"
)

// Array operators: + (concat), - (difference), * (repeat or join), & (set
// intersection), | (set union).
func TestArrayOperators(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"concat", `p [1, 2] + [3, 4]`, "[1, 2, 3, 4]\n"},
		{"concat_empty", `p [] + [1]`, "[1]\n"},
		{"concat_strings", `p ["a", "b"] + ["c"]`, "[\"a\", \"b\", \"c\"]\n"},
		{"difference", `p [1, 1, 2, 3] - [1]`, "[2, 3]\n"},
		{"difference_multi", `p [1, 2, 3, 4] - [2, 4]`, "[1, 3]\n"},
		{"difference_empty", `p [1, 2, 3] - []`, "[1, 2, 3]\n"},
		{"repeat", `p [1, 2] * 3`, "[1, 2, 1, 2, 1, 2]\n"},
		{"repeat_zero", `p [1, 2] * 0`, "[]\n"},
		{"join", `p [1, 2, 3] * ","`, "\"1,2,3\"\n"},
		{"join_nested", `p [1, [2, 3]] * "-"`, "\"1-2-3\"\n"},
		{"intersection", `p [1, 1, 2, 3] & [1, 3, 4]`, "[1, 3]\n"},
		{"intersection_order", `p [3, 1, 2] & [2, 3]`, "[3, 2]\n"},
		{"intersection_empty", `p [1, 2] & [3, 4]`, "[]\n"},
		{"union", `p [1, 2, 2] | [2, 3]`, "[1, 2, 3]\n"},
		{"union_dedup_left", `p [1, 1, 2] | [2]`, "[1, 2]\n"},
		// send routes the fast-path operators through the same logic.
		{"send_plus", `p [1, 2].send(:+, [3])`, "[1, 2, 3]\n"},
		{"send_minus", `p [1, 2, 3, 1].send(:-, [1])`, "[2, 3]\n"},
		{"length", "x = [1, 2] + [3]\np x.length", "3\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestArrayOperatorErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"concat_non_array", `[1] + 2`, "into Array"},
		{"difference_non_array", `[1] - 2`, "into Array"},
		{"repeat_non_int", `[1] * nil`, "into Integer"},
		{"repeat_negative", `[1] * -2`, "negative argument"},
		{"intersection_non_array", `[1] & 2`, "into Array"},
		{"union_non_array", `[1] | 2`, "into Array"},
		// An operator with no Array meaning falls through to NoMethodError.
		{"divide", `[1] / 2`, "undefined method"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}
