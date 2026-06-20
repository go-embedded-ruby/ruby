package vm_test

import (
	"strings"
	"testing"
)

// TestScopeOperator covers the :: constant-scope operator: a native module
// constant (Math::PI), a user class constant (Foo::BAR, resolved via the global
// fall-back), and the :: method-call form (with and without arguments).
func TestScopeOperator(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Math::PI`, "3.141592653589793\n"},
		{`p Math::E`, "2.718281828459045\n"},
		{"class Foo\n  BAR = 42\nend\np Foo::BAR", "42\n"},
		{`p Math::sqrt(4)`, "2.0\n"}, // :: method call with arguments
		{"class Foo\n  def self.bar = 7\nend\np Foo::bar", "7\n"}, // :: method call, no args
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestScopeErrors covers an unset constant and a non-module receiver.
func TestScopeErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Math::NOPE`, "NameError"},
		{`5::Foo`, "TypeError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestMath covers the Math module: the constants, a unary function, a binary
// function, log with one and two arguments, and the non-numeric guard. Values
// computed from transcendentals are rounded to stay architecture-stable.
func TestMath(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Math.sin(0)`, "0.0\n"},
		{`p Math.cos(0)`, "1.0\n"},
		{`p Math.sqrt(2).round(9)`, "1.414213562\n"},
		{`p Math.hypot(3, 4)`, "5.0\n"},
		{`p Math.atan2(1, 1).round(9)`, "0.785398163\n"},
		{`p Math.log(Math::E).round(9)`, "1.0\n"},
		{`p Math.log(8, 2).round(9)`, "3.0\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if err := runErr(t, `Math.sqrt("x")`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("Math.sqrt(non-numeric) should raise TypeError, got %v", err)
	}
}
