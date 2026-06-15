package vm_test

import (
	"strings"
	"testing"
)

func TestBlockPassAndToProc(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"forward_block", "def takes(&b)\ng(&b)\nend\ndef g(&b)\nb.call(7)\nend\np takes { |x| x * x }", "49\n"},
		{"forward_no_block", "def takes(&b)\ng(&b)\nend\ndef g(&b)\nb.nil?\nend\np takes", "true\n"},
		{"map_to_s", `p [1, 2, 3].map(&:to_s)`, "[\"1\", \"2\", \"3\"]\n"},
		{"map_upcase", `p ["a", "b"].map(&:upcase)`, "[\"A\", \"B\"]\n"},
		{"sym_to_proc_call", `p :upcase.to_proc.call("hi")`, "\"HI\"\n"},
		{"select_positive", `p [1, -2, 3].select(&:positive?)`, "[1, 3]\n"},
		{"reject_even", `p [1, 2, 3, 4].reject(&:even?)`, "[1, 3]\n"},
		{"splat_and_block_pass", "def fwd(*a, &b)\ncollect(*a, &b)\nend\ndef collect(x, y, &b)\n[b.call(x), b.call(y)]\nend\np fwd(2, 5) { |n| n + 1 }", "[3, 6]\n"},
		{"native_arity", `p :foo.to_proc.arity`, "-2\n"},
		{"proc_var_block_pass", "sq = nil\ndef cap(&b)\nb\nend\nsq = cap { |x| x * x }\np [1, 2, 3].map(&sq)", "[1, 4, 9]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestBlockPassToProcTypeError(t *testing.T) {
	src := "class Foo\ndef to_proc\n5\nend\nend\n[1].each(&Foo.new)"
	err := runErr(t, src)
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("got %v want TypeError", err)
	}
}
