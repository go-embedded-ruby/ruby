package vm_test

import (
	"strings"
	"testing"
)

func TestSplatParams(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"one_extra", "def f(a, *rest)\n  [a, rest]\nend\np f(1)", "[1, []]\n"},
		{"many_extra", "def f(a, *rest)\n  [a, rest]\nend\np f(1, 2, 3)", "[1, [2, 3]]\n"},
		{"all_splat_empty", "def g(*all)\n  all\nend\np g", "[]\n"},
		{"all_splat", "def g(*all)\n  all\nend\np g(1, 2, 3)", "[1, 2, 3]\n"},
		{"optional_and_splat_min", "def h(a, b = 10, *rest)\n  [a, b, rest]\nend\np h(1)", "[1, 10, []]\n"},
		{"optional_and_splat_mid", "def h(a, b = 10, *rest)\n  [a, b, rest]\nend\np h(1, 2)", "[1, 2, []]\n"},
		{"optional_and_splat_full", "def h(a, b = 10, *rest)\n  [a, b, rest]\nend\np h(1, 2, 3, 4)", "[1, 2, [3, 4]]\n"},
		{"sum", "def sum(*ns)\n  t = 0\n  ns.each { |n| t += n }\n  t\nend\np sum(1, 2, 3, 4)", "10\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestSplatArityError(t *testing.T) {
	if err := runErr(t, "def f(a, *r)\nend\nf"); err == nil || !strings.Contains(err.Error(), "given 0, expected 1+") {
		t.Fatalf("got %v, want 'expected 1+'", err)
	}
}
