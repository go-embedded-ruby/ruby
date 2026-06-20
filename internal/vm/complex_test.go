package vm_test

import (
	"strings"
	"testing"
)

// TestComplex covers Ruby Complex: construction, inspection/to_s, the readers and
// trig helpers, arithmetic (with coercion both ways), equality, and negation —
// every value asserted against MRI 4.0.5.
func TestComplex(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction + inspect (Integer parts kept; sign of the imaginary term).
		{`p Complex(1, 2)`, "(1+2i)\n"},
		{`p Complex(1, -2)`, "(1-2i)\n"},
		{`p Complex(3)`, "(3+0i)\n"}, // imaginary defaults to 0
		{`p Complex(1.5, 2)`, "(1.5+2i)\n"},
		// to_s drops the parentheses.
		{`puts Complex(1, 2)`, "1+2i\n"},
		{`puts Complex(1, -2)`, "1-2i\n"},
		// Readers.
		{`p Complex(1, 2).real`, "1\n"},
		{`p Complex(1, 2).imaginary`, "2\n"},
		{`p Complex(1, 2).imag`, "2\n"},
		{`p Complex(1, 2).abs2`, "5\n"},
		{`p Complex(3, 4).abs`, "5.0\n"},
		{`p Complex(3, 4).magnitude`, "5.0\n"},
		{`p Complex(0, 1).arg`, "1.5707963267948966\n"},
		{`p Complex(0, 1).angle`, "1.5707963267948966\n"},
		{`p Complex(0, 1).phase`, "1.5707963267948966\n"},
		{`p Complex(1, 2).conjugate`, "(1-2i)\n"},
		{`p Complex(1, 2).conj`, "(1-2i)\n"},
		{`p Complex(1, 2).rectangular`, "[1, 2]\n"},
		{`p Complex(1, 2).rect`, "[1, 2]\n"},
		{`p Complex(3, 4).polar`, "[5.0, 0.9272952180016122]\n"},
		{`p Complex(1, 2).to_s`, "\"1+2i\"\n"},
		{`p Complex(1, 2).inspect`, "\"(1+2i)\"\n"},
		// Arithmetic — exact on integer components for +/-/*, float for /.
		{`p Complex(1, 2) + Complex(3, 4)`, "(4+6i)\n"},
		{`p Complex(1, 2) - Complex(3, 4)`, "(-2-2i)\n"},
		{`p Complex(1, 2) * Complex(3, 4)`, "(-5+10i)\n"},
		{`p Complex(0, 1) * Complex(0, 1)`, "(-1+0i)\n"},
		{`p Complex(1.0, 2) / Complex(1, 1)`, "(1.5+0.5i)\n"},
		// Coercion of a real number, either operand order.
		{`p 2 + Complex(1, 1)`, "(3+1i)\n"},
		{`p Complex(1, 1) + 2`, "(3+1i)\n"},
		// Equality, including Complex(x, 0) == x in both orders.
		{`p Complex(2, 0) == 2`, "true\n"},
		{`p 2 == Complex(2, 0)`, "true\n"},
		{`p Complex(1, 2) == Complex(1, 2)`, "true\n"},
		{`p Complex(1, 2) == Complex(1, 3)`, "false\n"},
		{`p Complex(1, 0) == "x"`, "false\n"},
		// Negation, class, truthiness.
		{`p(-Complex(1, 2))`, "(-1-2i)\n"},
		{`p Complex(1, 2).class`, "Complex\n"},
		{`p(Complex(0, 0) ? "y" : "n")`, "\"y\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestComplexErrors covers the raising paths: non-numeric construction args, a
// non-coercible operand, and an operator Complex does not define.
func TestComplexErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Complex("a")`, "TypeError"},
		{`Complex(1, "b")`, "TypeError"},
		{`Complex(1, 2) + "x"`, "TypeError"},
		{`true + Complex(1, 1)`, "TypeError"},
		{`Complex(1, 2) % 1`, "NoMethodError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
