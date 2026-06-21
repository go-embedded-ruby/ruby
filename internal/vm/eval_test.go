package vm_test

import (
	"strings"
	"testing"
)

// TestEval covers Kernel#eval — the embedded front-end compiling and running Ruby
// at runtime. Values are asserted against MRI Ruby 4.0.5.
func TestEval(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p eval("1 + 2")`, "3\n"},
		{`p eval("[1, 2, 3].reduce(:+)")`, "6\n"},
		{`x = eval("10"); p x + 5`, "15\n"}, // eval returns the last value
		// def lands in the caller's context (top level → callable afterwards).
		{`eval("def gg; 42; end"); p gg`, "42\n"},
		// class definition through eval.
		{"eval(\"class Ev1; def v; 7; end; end\"); p Ev1.new.v", "7\n"},
		// eval runs against the caller's self: instance variables are visible.
		{"class Ev2\n  def initialize; @n = 9; end\n  def r; eval(\"@n * 2\"); end\nend\np Ev2.new.r", "18\n"},
		// constants resolve in eval.
		{`K = 5; p eval("K + 1")`, "6\n"},
		// a runtime error inside eval is an ordinary, rescuable Ruby error.
		{`p (eval("1 / 0") rescue "rescued")`, "\"rescued\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestEvalErrors covers eval's raising paths: a non-String argument (TypeError),
// a parse error and a compile error (both SyntaxError).
func TestEvalErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`eval(123)`, "TypeError"},       // non-String argument
		{`eval("1 +")`, "SyntaxError"},   // parse error
		{`eval("break")`, "SyntaxError"}, // compile error ("Invalid break")
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestSyntaxErrorHierarchy checks SyntaxError sits under ScriptError/Exception —
// NOT under StandardError — so a bare rescue does not catch it (matches MRI).
func TestSyntaxErrorHierarchy(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p SyntaxError.new("x").is_a?(Exception)`, "true\n"},
		{`p SyntaxError.new("x").is_a?(StandardError)`, "false\n"},
		{`p ScriptError.new("x").is_a?(Exception)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
