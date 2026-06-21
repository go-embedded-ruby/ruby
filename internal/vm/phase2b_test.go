package vm_test

import (
	"strings"
	"testing"
)

func TestArrays(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"literal_inspect", `p [1, :a, "x"]`, "[1, :a, \"x\"]\n"},
		{"empty_literal", `p []`, "[]\n"},
		{"trailing_comma", `p [1, 2,]`, "[1, 2]\n"},
		{"length", `puts [1, 2, 3].length`, "3\n"},
		{"size", `puts [1, 2, 3].size`, "3\n"},
		{"index", `puts [10, 20, 30][1]`, "20\n"},
		{"index_negative", `puts [10, 20, 30][-1]`, "30\n"},
		{"index_out_of_range", `p [1, 2][5]`, "nil\n"},
		{"index_negative_out", `p [1, 2][-5]`, "nil\n"},
		{"index_set", "a = [1, 2, 3]\na[0] = 9\np a", "[9, 2, 3]\n"},
		{"index_append", "a = [1, 2]\na[2] = 3\np a", "[1, 2, 3]\n"},
		{"index_set_negative", "a = [1, 2, 3]\na[-1] = 9\np a", "[1, 2, 9]\n"},
		{"index_assign_value", "a = [1]\nputs(a[0] = 7)", "7\n"},
		{"push_returns_self", "a = [1]\np a.push(2, 3)", "[1, 2, 3]\n"},
		{"first_last", `puts [5, 6, 7].first`, "5\n"},
		{"last", `puts [5, 6, 7].last`, "7\n"},
		{"first_empty", `p [].first`, "nil\n"},
		{"last_empty", `p [].last`, "nil\n"},
		{"empty_true", `puts [].empty?`, "true\n"},
		{"empty_false", `puts [1].empty?`, "false\n"},
		{"include_true", `puts [1, 2, 3].include?(2)`, "true\n"},
		{"include_false", `puts [1, 2, 3].include?(9)`, "false\n"},
		{"each", "s = 0\n[1, 2, 3].each { |x| s = s + x }\nputs s", "6\n"},
		{"map", `p [1, 2, 3].map { |x| x * x }`, "[1, 4, 9]\n"},
		{"eq_true", `puts([1, 2] == [1, 2])`, "true\n"},
		{"eq_diff_elem", `puts([1, 2] == [1, 3])`, "false\n"},
		{"eq_diff_len", `puts([1] == [1, 2])`, "false\n"},
		{"eq_non_array", `puts([1] == "x")`, "false\n"},
		{"class", `puts [].class`, "Array\n"},
		{"puts_flattens", `puts [1, 2, 3]`, "1\n2\n3\n"},
		{"puts_empty_array", `puts []`, ""},
		{"puts_nested", `puts [[1], [2, 3]]`, "1\n2\n3\n"},
		{"command_form", `p [1, 2]`, "[1, 2]\n"},
		{"nested_index", `puts [[1, 2], [3, 4]][1][0]`, "3\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestArrayErrors(t *testing.T) {
	tests := []struct{ src, want string }{
		{"a = [1]\na[5] = 9", "IndexError"},
		{"a = [1]\na[-5] = 9", "IndexError"},
		{`p [1]["x"]`, "TypeError"},
	}
	for _, tc := range tests {
		if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("src=%q got %v want %q", tc.src, err, tc.want)
		}
	}
}
