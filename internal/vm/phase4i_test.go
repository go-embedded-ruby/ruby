package vm_test

import "testing"

func TestSplatArguments(t *testing.T) {
	const f3 = "def f(a, b, c)\n  [a, b, c]\nend\n"
	const g = "def g(*xs)\n  xs\nend\n"
	tests := []struct{ name, src, want string }{
		{"splat_only", f3 + "a = [1, 2, 3]\np f(*a)", "[1, 2, 3]\n"},
		{"splat_after", f3 + `p f(1, *[2, 3])`, "[1, 2, 3]\n"},
		{"splat_before", f3 + `p f(*[1, 2], 3)`, "[1, 2, 3]\n"},
		{"multi_splat", g + `p g(*[1, 2], 3, *[4, 5])`, "[1, 2, 3, 4, 5]\n"},
		{"splat_var", g + "a = [1, 2, 3]\np g(*a)", "[1, 2, 3]\n"},
		{"splat_nonarray", g + `p g(*5)`, "[5]\n"},
		// array literals
		{"arr_splat_mid", "n = [10, 20]\np [0, *n, 30]", "[0, 10, 20, 30]\n"},
		{"arr_splat_only", "a = [1, 2, 3]\np [*a]", "[1, 2, 3]\n"},
		{"arr_two_splats", "a = [1, 2]\nb = [3, 4]\np [*a, *b]", "[1, 2, 3, 4]\n"},
		{"arr_empty_splat", "p [*[]]", "[]\n"},
		// splat with block
		{"splat_with_block", "def h(a, b)\n  yield(a + b)\nend\nh(*[3, 4]) { |s| p s }", "7\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
