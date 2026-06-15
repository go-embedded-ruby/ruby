package vm_test

import (
	"strings"
	"testing"
)

func TestKeywordArgs(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"req_and_opt", "def f(a, b:, c: 10)\n[a, b, c]\nend\np f(1, b: 2)", "[1, 2, 10]\n"},
		{"opt_supplied", "def f(a, b:, c: 10)\n[a, b, c]\nend\np f(1, b: 2, c: 3)", "[1, 2, 3]\n"},
		{"order_independent", "def f(a, b:, c: 10)\n[a, b, c]\nend\np f(1, c: 9, b: 8)", "[1, 8, 9]\n"},
		{"only_kw", "def g(x:, y: 5)\nx + y\nend\np g(x: 10)", "15\n"},
		{"default_refs_param", "def d(base, step: base * 2)\n[base, step]\nend\np d(5)", "[5, 10]\n"},
		{"default_overridden", "def d(base, step: base * 2)\n[base, step]\nend\np d(5, step: 1)", "[5, 1]\n"},
		{"splat_and_kw", "def m(a, *rest, tag:)\n[a, rest, tag]\nend\np m(1, 2, 3, tag: :z)", "[1, [2, 3], :z]\n"},
		{"all_optional_no_args", "def h(a: 1, b: 2)\n[a, b]\nend\np h", "[1, 2]\n"},
		{"all_optional_partial", "def h(a: 1, b: 2)\n[a, b]\nend\np h(b: 9)", "[1, 9]\n"},
		{"parenless_kw", "def pk a:, b: 7\n[a, b]\nend\np pk(a: 3)", "[3, 7]\n"},
		{"hashrocket_callarg", "def h(x)\nx\nend\np h(\"k\" => 1, \"j\" => 2)", "{\"k\" => 1, \"j\" => 2}\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestKeywordArgErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"missing_one", "def f(a, b:)\n[a, b]\nend\nf(1)", "missing keyword: :b"},
		{"missing_many", "def f(a:, b:)\n[a, b]\nend\nf", "missing keywords: :a, :b"},
		{"unknown_one", "def f(b:)\nb\nend\nf(b: 1, d: 4)", "unknown keyword: :d"},
		{"unknown_many", "def f(b:)\nb\nend\nf(b: 1, d: 4, e: 5)", "unknown keywords: :d, :e"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %q", tc.src, err, tc.want)
			}
			if !strings.Contains(err.Error(), "ArgumentError") {
				t.Fatalf("src=%q got %v want ArgumentError", tc.src, err)
			}
		})
	}
}
