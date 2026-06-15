package vm_test

import "testing"

func TestLogicalOperators(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// Boolean truth tables.
		{"and_tt", `puts(true && true)`, "true\n"},
		{"and_tf", `puts(true && false)`, "false\n"},
		{"and_ft", `puts(false && true)`, "false\n"},
		{"or_tf", `puts(true || false)`, "true\n"},
		{"or_ff", `puts(false || false)`, "false\n"},
		// Value semantics: && / || yield the deciding operand, not a coerced bool.
		{"and_value", `p(1 && 2)`, "2\n"},
		{"and_nil_short", `p(nil && 2)`, "nil\n"},
		{"or_nil", `p(nil || 3)`, "3\n"},
		{"or_truthy_short", `p(1 || 2)`, "1\n"},
		{"or_false_nil", `p(false || nil)`, "nil\n"},
		// Precedence: comparison binds tighter than &&, which binds tighter than ||.
		{"and_with_cmp", `p(2 > 1 && 3 > 2)`, "true\n"},
		{"or_with_cmp", `p(1 > 2 || 5)`, "5\n"},
		{"prec_or_below_and", `p(false || 2 > 1 && 4 > 3)`, "true\n"},
		// Short-circuit: the right side must not run when the left decides.
		{"and_short_circuit", "x = 0\nfalse && (x = 9)\np x", "0\n"},
		{"and_runs_right", "x = 0\ntrue && (x = 5)\np x", "5\n"},
		{"or_short_circuit", "y = 0\ntrue || (y = 9)\np y", "0\n"},
		{"or_runs_right", "y = 0\nfalse || (y = 7)\np y", "7\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
