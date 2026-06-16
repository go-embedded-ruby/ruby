package vm_test

import (
	"strings"
	"testing"
)

// Array#first(n)/#last(n) and Range#first(n)/#last(n) — the count argument
// returns a slice (previously it was ignored and the single element returned).
func TestFirstLastCount(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"arr_first_n", `p [1, 2, 3, 4, 5].first(3)`, "[1, 2, 3]\n"},
		{"arr_last_n", `p [1, 2, 3, 4, 5].last(3)`, "[3, 4, 5]\n"},
		{"arr_first", `p [1, 2, 3].first`, "1\n"},
		{"arr_last", `p [1, 2, 3].last`, "3\n"},
		{"arr_first_0", `p [1, 2, 3].first(0)`, "[]\n"},
		{"arr_first_over", `p [1, 2, 3].first(9)`, "[1, 2, 3]\n"},
		{"arr_last_over", `p [1, 2, 3].last(9)`, "[1, 2, 3]\n"},
		{"arr_empty_first", `p [].first`, "nil\n"},
		{"arr_empty_first_n", `p [].first(2)`, "[]\n"},
		{"arr_empty_last", `p [].last`, "nil\n"},
		{"arr_empty_last_n", `p [].last(2)`, "[]\n"},
		{"range_first_n", `p((1..5).first(3))`, "[1, 2, 3]\n"},
		{"range_last_n", `p((1..5).last(2))`, "[4, 5]\n"},
		{"range_excl_first_over", `p((1...5).first(9))`, "[1, 2, 3, 4]\n"},
		{"range_first", `p((1..5).first)`, "1\n"},
		{"range_last", `p((1..5).last)`, "5\n"},
		{"range_end_endless", `p((1..).end)`, "nil\n"},
		{"endless_first_n", `p((1..).first(4))`, "[1, 2, 3, 4]\n"},
		{"endless_first_0", `p((1..).first(0))`, "[]\n"},
		{"range_take", `p((1..5).take(2))`, "[1, 2]\n"},
		{"endless_take", `p((1..).take(3))`, "[1, 2, 3]\n"},
		{"range_drop", `p((1..5).drop(2))`, "[3, 4, 5]\n"},
		{"range_take_over", `p((1...5).take(9))`, "[1, 2, 3, 4]\n"},
		{"range_drop_over", `p((1..5).drop(9))`, "[]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestFirstLastErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"arr_first_neg", `[1].first(-1)`, "negative array size"},
		{"arr_last_neg", `[1].last(-1)`, "negative array size"},
		{"range_first_neg", `(1..5).first(-1)`, "negative array size"},
		{"range_last_neg", `(1..5).last(-1)`, "negative array size"},
		{"endless_last", `(1..).last`, "cannot get the last element of endless range"},
		{"endless_last_n", `(1..).last(2)`, "cannot get the last element of endless range"},
		{"endless_first_noninteger", `("a"..).first(2)`, "can't iterate from"},
		{"range_take_neg", `(1..5).take(-1)`, "attempt to take negative size"},
		{"range_drop_neg", `(1..5).drop(-1)`, "attempt to drop negative size"},
		{"endless_take_noninteger", `("a"..).take(2)`, "can't iterate from"},
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
