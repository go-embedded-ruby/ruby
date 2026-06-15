package vm_test

import "testing"

func TestBlockParamAndProcCall(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"capture_call", "def f(&b)\nb.call(10)\nend\np f { |x| x * 2 }", "20\n"},
		{"class", "def g(&b)\nb.class.to_s\nend\np g { }", "\"Proc\"\n"},
		{"nil_when_absent", "def h(&b)\nb.nil?\nend\np h", "true\n"},
		{"call_index_yield", "def m(&b)\n[b.call(3), b[4], b.yield(5)]\nend\np m { |x| x + 1 }", "[4, 5, 6]\n"},
		{"with_positional", "def n(a, &b)\nb.call(a)\nend\np n(3) { |x| x + 100 }", "103\n"},
		{"arity_two", "def ar(&b)\nb.arity\nend\np ar { |x, y| }", "2\n"},
		{"arity_one", "def ar(&b)\nb.arity\nend\np ar { |x| }", "1\n"},
		{"arity_zero", "def ar(&b)\nb.arity\nend\np ar { }", "0\n"},
		{"parenless", "def pf &b\nb.call(7)\nend\np pf { |x| x * x }", "49\n"},
		// Proc reification: MRI prints a pointer form; we use a stable "#<Proc>"
		// (an accepted representational divergence, asserted oracle-independently).
		{"inspect", "def f(&b)\np b\nend\nf { }", "#<Proc>\n"},
		{"to_s_interp", "def f(&b)\nputs \"got #{b}\"\nend\nf { }", "got #<Proc>\n"},
		{"truthy", "def f(&b)\nb ? \"yes\" : \"no\"\nend\np f { }", "\"yes\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
