package vm_test

import (
	"strings"
	"testing"
)

// TestDefWithReceiver covers `def recv.name` singleton-method definitions: on a
// plain object (its singleton class) and on a class/constant (a class method),
// including params, setters, and endless form. MRI Ruby 4.0.5.
func TestDefWithReceiver(t *testing.T) {
	cases := []struct{ src, want string }{
		{`o = Object.new; def o.greet; "hi"; end; p o.greet`, "\"hi\"\n"},
		{`o = Object.new; def o.add(a, b); a + b; end; p o.add(2, 3)`, "5\n"},
		{`o = Object.new; def o.val=(v); @v = v; end; def o.val; @v; end; o.val = 7; p o.val`, "7\n"},
		{`class C; end; def C.make; "made"; end; p C.make`, "\"made\"\n"},
		{`class Config; end; def Config.setup = "ok"; p Config.setup`, "\"ok\"\n"},
		// The singleton method is on that object only, not its class.
		{`o = Object.new; def o.f; 1; end; p Object.new.respond_to?(:f)`, "false\n"},
		// A method body still sees self.
		{`o = Object.new; def o.who; self; end; p o.who.equal?(o)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// A singleton method can't be defined on an immediate value (Integer): the
	// same TypeError define_singleton_method raises.
	if err := runErr(t, `n = 5; def n.double; self * 2; end`); err == nil ||
		!strings.Contains(err.Error(), "can't define singleton method") {
		t.Errorf("integer singleton: got %v want TypeError", err)
	}
}

// TestDefWithParenReceiver covers the parenthesized singleton-receiver form
// `def (expr).name`, added to the parser in go-ruby-parser v0.1.1. The parser
// emits the SAME MethodDef/Recv shape as the bare `def expr.name` form, so
// compileSingletonReceiver lowers it identically — this asserts the whole
// parse+compile+run pipeline end-to-end. Oracle: MRI Ruby 4.0.5.
func TestDefWithParenReceiver(t *testing.T) {
	cases := []struct{ src, want string }{
		// Local-variable receiver, with a keyword parameter.
		{`obj = Object.new; def (obj).kw(a:); a; end; p obj.kw(a: 5)`, "5\n"},
		// Constant (class) receiver → a class method.
		{`def (String).greet; "hi"; end; p String.greet`, "\"hi\"\n"},
		// Method-call receiver: the receiver object is the call's result.
		{`x = Object.new; def (x.itself).m; 1; end; p x.m`, "1\n"},
		// Endless form with a parenthesized receiver.
		{`o = Object.new; def (o).f = 9; p o.f`, "9\n"},
		// Parenthesized `self` receiver at the top level (main's singleton).
		{`def (self).top; 42; end; p top`, "42\n"},
		// Still a singleton method: defined on that object only, not its class.
		{`o = Object.new; def (o).g; 1; end; p Object.new.respond_to?(:g)`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
