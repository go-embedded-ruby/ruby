package vm_test

import (
	"testing"
)

func TestKernelLoop(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"break_value", "n = 0\nr = loop { n += 1; break n * 10 if n >= 3 }\np r", "30\n"},
		{"break_bare", "i = 0\nloop { i += 1; break if i > 2 }\np i", "3\n"},
		{"break_returns_nil", `p(loop { break })`, "nil\n"},
		{"with_next", "sum = 0\nc = 0\nloop do\nc += 1\nnext if c.even?\nsum += c\nbreak if c >= 5\nend\np sum", "9\n"},
		{"counts_iterations", "calls = 0\nloop do\ncalls += 1\nbreak if calls == 10\nend\np calls", "10\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestLoopNoBlock(t *testing.T) {
	// Kernel#loop with no block returns an infinite Enumerator over itself (MRI
	// eval.c rb_f_loop / loop_size), not a LocalJumpError as it once did.
	if got := eval(t, `p loop.instance_of?(Enumerator)`); got != "true\n" {
		t.Fatalf("loop.instance_of?(Enumerator): got %q want \"true\\n\"", got)
	}
	if got := eval(t, `p loop.size`); got != "Infinity\n" {
		t.Fatalf("loop.size: got %q want \"Infinity\\n\"", got)
	}
}
