package vm_test

import (
	"strings"
	"testing"
)

func TestBlocksAndYield(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"yield_no_args",
			"def twice\n  yield\n  yield\nend\ntwice { puts \"hi\" }", "hi\nhi\n"},
		{"yield_with_arg_block_param",
			"def g(n)\n  yield n\nend\ng(\"Bob\") { |x| puts \"hi \" + x }", "hi Bob\n"},
		{"yield_paren_args",
			"def g\n  yield(1, 2)\nend\ng { |a, b| puts a + b }", "3\n"},
		{"yield_command_args",
			"def g\n  yield 7\nend\ng { |a| puts a }", "7\n"},
		{"integer_times",
			"3.times { |i| puts i }", "0\n1\n2\n"},
		{"block_given_true_false",
			"def m\n  if block_given?\n    yield\n  else\n    \"none\"\n  end\nend\nputs m { \"some\" }\nputs m", "some\nnone\n"},
		{"closure_mutates_enclosing_local",
			"total = 0\n3.times { |i| total = total + i }\nputs total", "3\n"},
		{"closure_reads_enclosing_local",
			"base = 10\nadder = 0\n2.times { |i| adder = base + i }\nputs adder", "11\n"},
		{"block_arity_pads_nil",
			"def g\n  yield 1\nend\ng { |a, b| puts a\n  p b }", "1\nnil\n"},
		{"block_arity_truncates",
			"def g\n  yield 1, 2, 3\nend\ng { |a| puts a }", "1\n"},
		{"nested_blocks_depth2",
			"total = 0\n2.times { |i| 3.times { |j| total = total + 1 } }\nputs total", "6\n"},
		{"block_with_no_params",
			"def g\n  yield\nend\nx = 5\ng { x = x + 1 }\nputs x", "6\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestBlockErrors(t *testing.T) {
	tests := []struct{ src, want string }{
		{"def f\n  yield\nend\nf", "LocalJumpError"},   // yield without a block
		{`5.times`, "LocalJumpError"},                   // times without a block
	}
	for _, tc := range tests {
		if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("src=%q got %v want %q", tc.src, err, tc.want)
		}
	}
}
