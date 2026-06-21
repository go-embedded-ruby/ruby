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
