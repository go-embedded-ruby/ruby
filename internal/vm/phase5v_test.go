package vm_test

import (
	"strings"
	"testing"
)

func TestArrayBangMethods(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"sort_bang", "a = [3, 1, 2]\na.sort!\np a", "[1, 2, 3]\n"},
		{"sort_bang_inplace", "a = [3, 1, 2]\np a.sort!.equal?(a)", "true\n"},
		{"map_bang", "b = [1, 2, 3]\nb.map! { |x| x * 2 }\np b", "[2, 4, 6]\n"},
		{"reverse_bang", "c = [1, 2, 3]\nc.reverse!\np c", "[3, 2, 1]\n"},
		{"select_bang_changed", "d = [1, 2, 3, 4]\np d.select! { |x| x.even? }\np d", "[2, 4]\n[2, 4]\n"},
		{"select_bang_unchanged", "e = [2, 4]\np e.select! { |x| x.even? }\np e", "nil\n[2, 4]\n"},
		{"filter_bang", "f = [1, 2, 3]\np f.filter! { |x| x > 1 }\np f", "[2, 3]\n[2, 3]\n"},
		{"reject_bang_changed", "g = [1, 2, 3, 4]\np g.reject! { |x| x.even? }\np g", "[1, 3]\n[1, 3]\n"},
		{"reject_bang_unchanged", "h = [1, 3]\np h.reject! { |x| x.even? }\np h", "nil\n[1, 3]\n"},
		{"compact_bang_changed", "i = [1, nil, 2]\np i.compact!\np i", "[1, 2]\n[1, 2]\n"},
		{"compact_bang_unchanged", "j = [1, 2]\np j.compact!", "nil\n"},
		{"uniq_bang_changed", "k = [3, 1, 2, 1]\np k.uniq!\np k", "[3, 1, 2]\n[3, 1, 2]\n"},
		{"uniq_bang_unchanged", "l = [1, 2, 3]\np l.uniq!", "nil\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestArrayBangNoBlock(t *testing.T) {
	for _, src := range []string{`[1].map!`, `[1].select!`, `[1].reject!`} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "LocalJumpError") {
			t.Fatalf("src=%q got %v want LocalJumpError", src, err)
		}
	}
}
