package vm_test

import "testing"

// The `rescue` modifier: `risky rescue fallback` yields fallback when risky
// raises a StandardError, and binds tighter than `=` on an assignment RHS.
func TestRescueModifier(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"catches", `p(1 / 0 rescue "divzero")`, "\"divzero\"\n"},
		{"no_error", `p(42 rescue -1)`, "42\n"},
		{"assign_rhs_caught", "x = (1 / 0 rescue 7)\np x", "7\n"},
		{"assign_rhs_ok", "x = (10 rescue 7)\np x", "10\n"},
		{"assign_binds_tighter", "x = 1 / 0 rescue 99\np x", "99\n"},
		{"raise_caught", `p(raise("boom") rescue "caught")`, "\"caught\"\n"},
		{"endless_method", "def f(s) = Integer(s) rescue -1\np f(\"9\")\np f(\"bad\")", "9\n-1\n"},
		{"index_assign_rhs", "a = [1]\na[0] = (1 / 0 rescue 5)\np a", "[5]\n"},
		// Left-associative: the first rescue still raises, the second catches.
		{"chained", `p(raise("a") rescue raise("b") rescue 3)`, "3\n"},
		// A begin/rescue clause is NOT a modifier (it starts a new line).
		{"clause_not_modifier", "x = begin\n  1 / 0\nrescue\n  \"clause\"\nend\np x", "\"clause\"\n"},
		// Only StandardError-family errors are rescued by the bare modifier.
		{"only_standard_error", `p([1, 2].first rescue "no")`, "1\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
