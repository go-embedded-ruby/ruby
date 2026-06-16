package vm_test

import "testing"

func TestEnumerableCompleteness(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// Hash gains min_by/max_by/sort_by/sum(block)/count(block) via Enumerable.
		{"hash_min_by", "p({a: 1, b: 3, c: 2}.min_by { |k, v| v })", "[:a, 1]\n"},
		{"hash_max_by", "p({a: 1, b: 3, c: 2}.max_by { |k, v| v })", "[:b, 3]\n"},
		{"hash_sort_by", "p({a: 1, b: 3, c: 2}.sort_by { |k, v| v })", "[[:a, 1], [:c, 2], [:b, 3]]\n"},
		{"hash_sum_block", "p({a: 1, b: 3, c: 2}.sum { |k, v| v })", "6\n"},
		{"hash_count_block", "p({a: 1, b: 3, c: 2}.count { |k, v| v > 1 })", "2\n"},
		{"hash_count_bare", "p({a: 1, b: 2}.count)", "2\n"},
		// Range Enumerable forms.
		{"range_sum_block", "p (1..5).sum { |x| x * 2 }", "30\n"},
		{"range_count_block", "p (1..10).count { |x| x % 3 == 0 }", "3\n"},
		{"range_count_bare", "p (1..5).count", "5\n"},
		{"range_count_arg", "p (1..5).count(3)", "1\n"},
		{"range_min_by", "p (1..5).min_by { |x| -x }", "5\n"},
		{"range_sort_by", "p (1..3).sort_by { |x| -x }", "[3, 2, 1]\n"},
		// Array#sort_by correctness (was broken: keys not permuted with elements).
		{"array_sort_by", `p [3, 1, 4, 1, 5, 9, 2, 6].sort_by { |x| x }`, "[1, 1, 2, 3, 4, 5, 6, 9]\n"},
		{"array_sort_by_len", `p ["bb", "a", "ccc"].sort_by { |s| s.length }`, "[\"a\", \"bb\", \"ccc\"]\n"},
		// Array sum still native + works with init.
		{"array_sum_init", `p [1, 2, 3].sum(10)`, "16\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
