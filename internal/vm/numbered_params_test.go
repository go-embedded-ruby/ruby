package vm_test

import (
	"strings"
	"testing"
)

// Ruby 3.4 implicit block parameters: numbered (_1.._9) and `it`.
func TestNumberedParams(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"single", `p [1, 2, 3].map { _1 * 2 }`, "[2, 4, 6]\n"},
		{"two", `p [[1, 2], [3, 4]].map { _1 + _2 }`, "[3, 7]\n"},
		{"reduce", `p [1, 2, 3].reduce(0) { _1 + _2 }`, "6\n"},
		{"sort", `p [3, 1, 2].sort { _1 <=> _2 }`, "[1, 2, 3]\n"},
		{"hash", `p({a: 1, b: 2}.map { "#{_1}=#{_2}" })`, "[\"a=1\", \"b=2\"]\n"},
		{"do_end", "p [1, 2].map do\n  _1 + 100\nend", "[101, 102]\n"},
		{"lambda", "f = ->{ _1 * _1 }\np f.call(5)", "25\n"},
		{"it", `p [1, 2, 3].map { it * 10 }`, "[10, 20, 30]\n"},
		{"it_method", `p [1, 2, 3].select { it.odd? }`, "[1, 3]\n"},
		{"it_lambda", "f = ->{ it + 1 }\np f.call(9)", "10\n"},
		{"it_nested_ok", `p [[1]].map { it.map { it } }`, "[[1]]\n"},
		{"it_then_numbered", `p [[1]].map { it.map { _1 } }`, "[[1]]\n"},
		{"numbered_then_it", `p [[1]].map { _1.map { it } }`, "[[1]]\n"},
		{"numbered_inner_only", `p [[1]].map { _1.map { 9 } }`, "[[9]]\n"},
		{"explicit_wins", `p [[1]].map { |x| x.map { _1 } }`, "[[1]]\n"},
		// An explicit `it` parameter and an `it` method shadow the implicit form.
		{"explicit_it_param", `p [1, 2].map { |it| it + 1 }`, "[2, 3]\n"},
		{"it_method_call", "def it\n  99\nend\np it", "99\n"},
		// Auto-splat: a 2-numbered-param block over single array elements.
		{"autosplat", `p [[1, 2], [3, 4]].map { _2 }`, "[2, 4]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestNumberedParamErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"nested_numbered", `p [[1]].map { _1.map { _2 } }`, "numbered parameter is already used in outer block"},
		{"mix_it_numbered", `p [1].map { _1 + it }`, "`it` is not allowed together with numbered parameters"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}
