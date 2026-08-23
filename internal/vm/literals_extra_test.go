package vm_test

import (
	"strings"
	"testing"
)

// TestSingleQuoteStrings covers single-quoted string literals: non-interpolating,
// with only \' and \\ as escapes (every other backslash is literal). Each value
// is asserted against MRI Ruby 4.0.5.
func TestSingleQuoteStrings(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 'hello'`, "\"hello\"\n"},
		{`p ''`, "\"\"\n"},
		{`puts 'no #{x} here'`, "no #{x} here\n"}, // no interpolation
		{`p 'a\'b'`, "\"a'b\"\n"},                 // \' -> literal quote
		{`p '\\'`, "\"\\\\\"\n"},                  // \\ -> one backslash
		{`puts 'c:\\dir'`, "c:\\dir\n"},
		{`p 'tab\tstays'`, "\"tab\\\\tstays\"\n"}, // \t is literal backslash-t
		{`p 'a' + 'b'`, "\"ab\"\n"},               // single-quote in an expression + concat
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// An unterminated single-quoted string is an ILLEGAL token -> parse error.
	if err := runErr(t, `x = 'oops`); err == nil || !strings.Contains(err.Error(), "unterminated string literal") {
		t.Errorf("unterminated single-quote: got %v", err)
	}
}

// TestFloatExponentLiterals covers scientific float notation (e/E with an
// optional sign), which always yields a Float — even with no fractional part.
// Asserted against MRI Ruby 4.0.5.
func TestFloatExponentLiterals(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 1.5e3`, "1500.0\n"},
		{`p 1e3`, "1000.0\n"},
		{`p 1.0e-3`, "0.001\n"},
		{`p 2E2`, "200.0\n"},
		{`p 1e+2`, "100.0\n"},
		{`p 1_000.5e1`, "10005.0\n"},
		{`p (1e3).class`, "Float\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFloatLiteralZeroSigns covers what the compiler's constant pool must not
// lose: a zero's sign, and a NaN's identity as a fresh constant. Both are
// literals whose printed form differs from a literal they compare equal (or
// unequal) to, so a pool keyed on the value gets them wrong — whichever of
// 0.0 / -0.0 the file mentioned first used to win both lines. Asserted
// against MRI Ruby 4.0.5.
func TestFloatLiteralZeroSigns(t *testing.T) {
	cases := []struct{ src, want string }{
		{"p 0.0\np(-0.0)", "0.0\n-0.0\n"},
		{"p(-0.0)\np 0.0", "-0.0\n0.0\n"},
		{`p [0.0, -0.0]`, "[0.0, -0.0]\n"},
		{`p 0.0 == -0.0`, "true\n"}, // equal, and still not interchangeable
		{`p (0.0).zero? && (-0.0).zero?`, "true\n"},
		{`p 1.0 / -0.0`, "-Infinity\n"}, // the sign is load-bearing, not cosmetic
		{`p 1.0 / 0.0`, "Infinity\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
