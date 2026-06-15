package vm_test

import "testing"

func TestDoubleSplatKwrest(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"only_kwrest", "def f(**o)\no\nend\np f(a: 1, b: 2)", "{a: 1, b: 2}\n"},
		{"kwrest_empty", "def f(**o)\no\nend\np f", "{}\n"},
		{"pos_and_kwrest", "def g(a, **o)\n[a, o]\nend\np g(1, x: 2, y: 3)", "[1, {x: 2, y: 3}]\n"},
		{"pos_no_kw", "def g(a, **o)\n[a, o]\nend\np g(1)", "[1, {}]\n"},
		{"named_and_rest", "def h(a:, **rest)\n[a, rest]\nend\np h(a: 1, b: 2, c: 3)", "[1, {b: 2, c: 3}]\n"},
		{"named_no_extra", "def h(a:, **rest)\n[a, rest]\nend\np h(a: 1)", "[1, {}]\n"},
		{"all_param_kinds", "def m(x, *pos, k:, **kw)\n[x, pos, k, kw]\nend\np m(1, 2, 3, k: 9, extra: 0)", "[1, [2, 3], 9, {extra: 0}]\n"},
		{"nonsymbol_key", "def f(**o)\no\nend\np f(\"x\" => 1)", "{\"x\" => 1}\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
