package vm_test

import (
	"strings"
	"testing"
)

func TestProcAndLambda(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"lambda_call", "sq = lambda { |x| x * x }\np sq.call(5)", "25\n"},
		{"proc_call", "add = proc { |a, b| a + b }\np add.call(2, 3)", "5\n"},
		{"lambda_pred_true", `p lambda { |x| x }.lambda?`, "true\n"},
		{"lambda_pred_false", `p proc { }.lambda?`, "false\n"},
		{"lambda_class", `p lambda { }.class.to_s`, "\"Proc\"\n"},
		{"lambda_arity", `p lambda { |x, y| x }.arity`, "2\n"},
		{"proc_arity", `p proc { |x| x }.arity`, "1\n"},
		{"map_with_proc", `p [1, 2, 3].map(&proc { |x| x + 10 })`, "[11, 12, 13]\n"},
		{"map_with_lambda", "l = lambda { |n| n * 3 }\np [1, 2].map(&l)", "[3, 6]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestProcLambdaNoBlock(t *testing.T) {
	for _, src := range []string{`lambda`, `proc`} {
		err := runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "ArgumentError") {
			t.Fatalf("src=%q got %v want ArgumentError", src, err)
		}
	}
}
