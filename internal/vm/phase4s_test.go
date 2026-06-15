package vm_test

import "testing"

func TestStabbyLambda(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"paren_brace", "sq = ->(x) { x * x }\np sq.call(5)", "25\n"},
		{"no_params", "f = -> { 42 }\np f.call", "42\n"},
		{"two_params", "add = ->(a, b) { a + b }\np add.call(2, 3)", "5\n"},
		{"lambda_pred", `p ->(x){ x }.lambda?`, "true\n"},
		{"in_map", `p [1, 2].map(&->(n){ n + 1 })`, "[2, 3]\n"},
		{"do_end_body", "g = ->(x) do x + 1 end\np g.call(9)", "10\n"},
		{"index_call", `p ->(x) { x * 2 }[10]`, "20\n"},
		{"command_arg", `p ->(x){ x }.call(99)`, "99\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
