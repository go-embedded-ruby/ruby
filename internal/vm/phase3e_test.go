package vm_test

import "testing"

func TestTernary(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"true_branch", `p(5 > 3 ? "big" : "small")`, "\"big\"\n"},
		{"false_branch", `p(1 == 2 ? "a" : "b")`, "\"b\"\n"},
		{"predicate_cond", "x = 0\np(x.zero? ? \"zero\" : \"nonzero\")", "\"zero\"\n"},
		{"literal_true", `p(true ? 1 : 2)`, "1\n"},
		{"nested_else", `p(false ? 1 : (2 > 1 ? 3 : 4))`, "3\n"},
		{"nested_right_assoc", `p(false ? 1 : true ? 3 : 4)`, "3\n"},
		{"assigned", "y = 5 > 3 ? 10 : 20\np y", "10\n"},
		{"in_interp", "n = 7\nputs \"#{n.even? ? \"e\" : \"o\"}\"", "o\n"},
		{"in_block", `p([1, 2, 3].map { |i| i.even? ? "e" : "o" })`, "[\"o\", \"e\", \"o\"]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
