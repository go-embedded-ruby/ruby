package aot

import (
	"strings"
	"testing"
)

// l3 compiles src's first method as a level-3 kernel.
func l3(t *testing.T, src string) (string, bool) {
	t.Helper()
	iseq := methodISeq(t, src)
	return CompileSpecialized(iseq, "f", iseq.Name)
}

// TestL3Supported exercises every integer-kernel form: each comparison operator
// (in a branch), the four checked arithmetic ops, unary negation, self-
// recursion, a parameterless kernel, and an extra (non-parameter) local.
func TestL3Supported(t *testing.T) {
	cases := []struct{ name, src, contains string }{
		{"fib_lt_recursion", "def fib(n) = n < 2 ? n : fib(n - 1) + fib(n - 2)\nfib(5)", "vm.f_k("},
		{"mul", "def m(a, b) = a * b\nm(1, 2)", "aotMul("},
		{"div", "def m(a, b) = a / b\nm(1, 2)", "aotDiv("},
		{"mod", "def m(a, b) = a % b\nm(1, 2)", "aotMod("},
		{"neg", "def m(a) = -a\nm(1)", "aotNeg("},
		{"cmp_gt", "def m(a) = a > 0 ? 1 : 2\nm(1)", "k0 > k1"},
		{"cmp_le", "def m(a) = a <= 0 ? 1 : 2\nm(1)", "k0 <= k1"},
		{"cmp_ge", "def m(a) = a >= 0 ? 1 : 2\nm(1)", "k0 >= k1"},
		{"cmp_eq", "def m(a) = a == 0 ? 1 : 2\nm(1)", "k0 == k1"},
		{"cmp_ne", "def m(a) = a != 0 ? 1 : 2\nm(1)", "k0 != k1"},
		{"no_param", "def m = 5\nm", "f_k()"},
		{"extra_local", "def m(n)\n  x = n + 1\n  x + x\nend\nm(1)", "l1 = k"},
		{"while_loop", "def m(n)\n  r = 1\n  while n > 1\n    r = r * n\n    n = n - 1\n  end\n  r\nend\nm(3)", "goto L"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, ok := l3(t, c.src)
			if !ok {
				t.Fatalf("%s should be an integer kernel", c.name)
			}
			if !strings.Contains(src, c.contains) {
				t.Errorf("missing %q:\n%s", c.contains, src)
			}
			assertGoParses(t, src)
		})
	}
}

// TestL3Bails covers every reason CompileSpecialized declines the kernel: the
// level-1 floor rejecting the method, and each integer-kernel-specific veto.
func TestL3Bails(t *testing.T) {
	cases := []struct{ name, src string }{
		{"l1_rejects", "def m(*a) = a\nm"},                                          // splat: Compile itself bails
		{"float_const", "def m = 1.5\nm"},                                           // non-Integer constant
		{"array", "def m = [1, 2]\nm"},                                              // unsupported kernel opcode
		{"compare_not_branched", "def m(a, b) = a < b\nm(1, 2)"},                    // comparison result escapes
		{"branch_without_compare", "def m(a) = a ? 1 : 2\nm(1)"},                    // truthiness branch, no compare
		{"non_self_send", "def m(a) = a.abs\nm(-1)"},                                // a send that is not self-recursion
		{"return_self", "def m = self\nm"},                                          // returns self, not an int64
		{"nil_escapes", "def m(n)\n  while n > 0\n    n = n - 1\n  end\nend\nm(1)"}, // while value (nil) is returned
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := l3(t, c.src); ok {
				t.Errorf("%s should not be a kernel", c.name)
			}
		})
	}
}
