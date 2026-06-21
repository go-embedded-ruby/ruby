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
