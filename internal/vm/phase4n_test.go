package vm_test

import (
	"strings"
	"testing"
)

func TestDoubleSplatHashAndCall(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"hash_merge_then_pair", "h = {a: 1, b: 2}\np({**h, c: 3})", "{a: 1, b: 2, c: 3}\n"},
		{"hash_pair_then_merge", "h = {a: 1, b: 2}\np({c: 0, **h})", "{c: 0, a: 1, b: 2}\n"},
		{"hash_only_merge", "g = {x: 1}\np({**g})", "{x: 1}\n"},
		{"hash_merge_overrides", "h = {a: 9}\np({a: 1, **h})", "{a: 9}\n"},
		{"call_splat_kw", "def f(a:, b:)\n[a, b]\nend\nh = {a: 1, b: 2}\np f(**h)", "[1, 2]\n"},
		{"call_splat_plus_kw", "def f(a:, b:)\n[a, b]\nend\np f(**{a: 1}, b: 2)", "[1, 2]\n"},
		{"call_splat_into_rest", "def k(a:, **r)\n[a, r]\nend\np k(a: 1, **{b: 2, c: 3})", "[1, {b: 2, c: 3}]\n"},
		{"call_splat_into_kwrest", "def o(**opts)\nopts\nend\nh = {a: 1}\np o(**h, b: 9)", "{a: 1, b: 9}\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestDoubleSplatNonHash(t *testing.T) {
	err := runErr(t, `p({**5})`)
	if err == nil || !strings.Contains(err.Error(), "TypeError") ||
		!strings.Contains(err.Error(), "no implicit conversion of Integer into Hash") {
		t.Fatalf("got %v want TypeError no-implicit-conversion", err)
	}
}
