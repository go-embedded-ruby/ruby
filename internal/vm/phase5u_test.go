package vm_test

import "testing"

func TestBlockParamDestructuring(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"group", `p [[1, 2], [3, 4]].map { |(a, b)| a * b }`, "[2, 12]\n"},
		{"group_each", "r = []\n[[1, 2], [3, 4]].each { |(a, b)| r << a + b }\np r", "[3, 7]\n"},
		{"group_and_plain", "r = {a: 1, b: 2}.each_with_object([]) { |(k, v), m| m << \"#{k}=#{v}\" }\np r", "[\"a=1\", \"b=2\"]\n"},
		{"group_with_splat", `p [[1, 2, 3], [4, 5, 6]].map { |(x, *rest)| [x, rest] }`, "[[1, [2, 3]], [4, [5, 6]]]\n"},
		{"plain_auto_splat_unaffected", `p [[1, 2]].map { |a, b| a + b }`, "[3]\n"},
		{"single_param_unaffected", `p [1, 2, 3].map { |x| x * 2 }`, "[2, 4, 6]\n"},
		{"group_short", `p [[1]].map { |(a, b)| [a, b] }`, "[[1, nil]]\n"},
		{"two_groups", `p [[[1, 2], [3, 4]]].map { |(a, b), (c, d)| [a, b, c, d] }`, "[[1, 2, 3, 4]]\n"},
		{"empty_paren_lambda", "f = ->() { 42 }\np f.call", "42\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
