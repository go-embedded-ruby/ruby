package vm_test

import "testing"

func TestMultipleAssignment(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"basic", "a, b = 1, 2\np [a, b]", "[1, 2]\n"},
		{"from_array", "c, d = [10, 20]\np [c, d]", "[10, 20]\n"},
		{"more_targets", "e, f, g = 1, 2\np [e, f, g]", "[1, 2, nil]\n"},
		{"more_values", "h, i = 1, 2, 3\np [h, i]", "[1, 2]\n"},
		{"single_value", "x, y = 5\np [x, y]", "[5, nil]\n"},
		{"swap", "a = 1\nb = 2\na, b = b, a\np [a, b]", "[2, 1]\n"},
		{"trailing_splat", "j, k = 1, 2, 3, 4\nl, *m = 1, 2, 3, 4\np [l, m]", "[1, [2, 3, 4]]\n"},
		{"leading_splat", "n, o = 0, 0\np2, q = 0, 0\nr, s = 0, 0\nx2, *y2 = 0\n*aa, bb = 1, 2, 3\np [aa, bb]", "[[1, 2], 3]\n"},
		{"middle_splat", "g2, *h2, i2 = 1, 2, 3, 4, 5\np [g2, h2, i2]", "[1, [2, 3, 4], 5]\n"},
		{"splat_empty", "a2, *b2, c2 = 1, 2\np [a2, b2, c2]", "[1, [], 2]\n"},
		{"splat_too_short_post", "a3, b3, *c3 = 1\np [a3, b3, c3]", "[1, nil, []]\n"},
		{"leading_splat_short", "z3, w3 = 0, 0\nu3, v3 = 0, 0\nf3, g3 = 0, 0\nq3, r3 = 0, 0\n*d3, e3, f4 = 1\np [d3, e3, f4]", "[[], 1, nil]\n"},
		{"from_var", "arr = [100, 200]\nt, u = arr\np [t, u]", "[100, 200]\n"},
		{"expression_value", "result = (cc, dd = 7, 8)\np result", "[7, 8]\n"},
		{"reassign_existing", "aa4 = 9\nbb4 = 9\naa4, bb4 = 1, 2\np [aa4, bb4]", "[1, 2]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
