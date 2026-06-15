package vm_test

import "testing"

// A method body may carry rescue/ensure clauses without an explicit begin.
func TestMethodLevelRescue(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{
			"rescue_specific",
			"def safe(a, b)\n  a / b\nrescue ZeroDivisionError\n  0\nend\np safe(10, 2)\np safe(10, 0)",
			"5\n0\n",
		},
		{
			"rescue_bind_and_ensure",
			"def f\n  raise \"boom\"\nrescue => e\n  \"caught: #{e.message}\"\nensure\n  puts \"cleanup\"\nend\np f",
			"cleanup\n\"caught: boom\"\n",
		},
		{
			"ensure_only",
			"def f\n  42\nensure\n  puts \"done\"\nend\np f",
			"done\n42\n",
		},
		{
			"multiple_clauses",
			"def f\n  raise TypeError, \"t\"\nrescue ArgumentError\n  \"arg\"\nrescue TypeError => e\n  \"type: #{e.message}\"\nend\np f",
			"\"type: t\"\n",
		},
		// a plain method body (no rescue) still works
		{"plain", "def f\n  1 + 1\nend\np f", "2\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
