package vm_test

import (
	"strings"
	"testing"
)

func TestOptionalParams(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"default_used", "def g(n, gr = \"Hi\")\n  gr + n\nend\np g(\"x\")", "\"Hix\"\n"},
		{"default_overridden", "def g(n, gr = \"Hi\")\n  gr + n\nend\np g(\"x\", \"Yo\")", "\"Yox\"\n"},
		{"two_defaults_none", "def a(x, y = 10, z = 20)\n  x + y + z\nend\np a(1)", "31\n"},
		{"two_defaults_one", "def a(x, y = 10, z = 20)\n  x + y + z\nend\np a(1, 2)", "23\n"},
		{"two_defaults_all", "def a(x, y = 10, z = 20)\n  x + y + z\nend\np a(1, 2, 3)", "6\n"},
		{"default_uses_prev", "def f(a, b = a * 2)\n  [a, b]\nend\np f(5)", "[5, 10]\n"},
		{"default_uses_prev_override", "def f(a, b = a * 2)\n  [a, b]\nend\np f(5, 99)", "[5, 99]\n"},
		{"no_params", "def f\n  42\nend\np f", "42\n"},
		{"all_required", "def f(a, b)\n  a + b\nend\np f(3, 4)", "7\n"},
		{"empty_block_params", "r = 0\n[1, 2, 3].each { | | r = r + 1 }\np r", "3\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestArityErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"too_few_optional", "def f(a, b = 1)\nend\nf", "given 0, expected 1..2"},
		{"too_many_optional", "def f(a, b = 1)\nend\nf(1, 2, 3)", "given 3, expected 1..2"},
		{"too_few_required", "def f(a, b)\nend\nf(1)", "given 1, expected 2"},
		{"too_many_required", "def f(a)\nend\nf(1, 2)", "given 2, expected 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %q", tc.src, err, tc.want)
			}
		})
	}
}
