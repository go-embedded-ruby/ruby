package vm_test

import "testing"

func TestBeginlessEndlessRanges(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// literals / inspect
		{"endless_inspect", `p (1..)`, "1..\n"},
		{"beginless_inspect", `p (..5)`, "..5\n"},
		{"normal_inspect", `p (1...5)`, "1...5\n"},
		{"endless_to_s", `puts(1..)`, "1..\n"},
		// === / cover with open bounds
		{"endless_eqq", `p((1..) === 100)`, "true\n"},
		{"endless_eqq_below", `p((1..) === 0)`, "false\n"},
		{"beginless_eqq", `p((..5) === 3)`, "true\n"},
		{"beginless_eqq_above", `p((..5) === 9)`, "false\n"},
		{"beginless_include_neg", `p((..5).include?(-100))`, "true\n"},
		{"beginless_cover_incomparable", `p((..5).cover?("x"))`, "false\n"},
		{"normal_eqq", `p((1..10) === 5)`, "true\n"},
		// array slicing
		{"arr_range", `p [1, 2, 3, 4, 5][1..3]`, "[2, 3, 4]\n"},
		{"arr_range_excl", `p [1, 2, 3, 4, 5][1...3]`, "[2, 3]\n"},
		{"arr_endless", `p [1, 2, 3, 4, 5][2..]`, "[3, 4, 5]\n"},
		{"arr_beginless", `p [1, 2, 3, 4, 5][..2]`, "[1, 2, 3]\n"},
		{"arr_neg", `p [1, 2, 3, 4, 5][..-2]`, "[1, 2, 3, 4]\n"},
		{"arr_oob", `p [1, 2, 3][9..]`, "nil\n"},
		{"arr_start_len", `p [1, 2, 3, 4, 5][1, 2]`, "[2, 3]\n"},
		{"arr_start_len_clamp", `p [1, 2, 3][1, 9]`, "[2, 3]\n"},
		{"arr_start_len_oob", `p [1, 2, 3][9, 1]`, "nil\n"},
		{"arr_start_len_neg", `p [1, 2, 3][1, -1]`, "nil\n"},
		{"arr_index", `p [1, 2, 3][1]`, "2\n"},
		// string slicing
		{"str_endless", `p "hello"[1..]`, "\"ello\"\n"},
		{"str_beginless", `p "hello"[..2]`, "\"hel\"\n"},
		// empty / reversed
		{"arr_empty_range", `p [1, 2, 3][2..1]`, "[]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
