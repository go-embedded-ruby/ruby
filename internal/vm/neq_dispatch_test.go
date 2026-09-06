package vm_test

import "testing"

// TestNotEqualDispatch checks that `a != b` calls a user-defined #!= when the
// receiver overrides it (MRI dispatches the method), and otherwise keeps the
// default BasicObject#!= behaviour of !(a == b). Verified against MRI 4.0.6.
func TestNotEqualDispatch(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// A class that overrides #!=: the operator dispatches the override, so the
		// result is whatever #!= returns — not !(==).
		{`class C1; def ==(o); false; end; def !=(o); "custom"; end; end; p (C1.new != 1)`, "\"custom\"\n"},
		// #!= override returning nil (falsy) is returned verbatim, not coerced.
		{`class C2; def !=(o); nil; end; end; p (C2.new != 1)`, "nil\n"},
		// An override inherited from a superclass (below BasicObject) still dispatches.
		{`class B3; def !=(o); :sup; end; end; class C3 < B3; end; p (C3.new != 1)`, ":sup\n"},
		// #!= override reached through an included module.
		{`module M4; def !=(o); :mod; end; end; class C4; include M4; end; p (C4.new != 1)`, ":mod\n"},
		// No #!= override: the default BasicObject#!= gives !(self == other).
		{`class D1; def ==(o); true; end; end; p (D1.new != D1.new)`, "false\n"},
		{`class D2; def ==(o); false; end; end; p (D2.new != D2.new)`, "true\n"},
		// Built-in value types (no #!= override) keep structural inequality.
		{`p (1 != 2)`, "true\n"}, {`p (1 != 1)`, "false\n"},
		{`p ("a" != "b")`, "true\n"}, {`p (nil != nil)`, "false\n"},
		{`p ([1, 2] != [1, 3])`, "true\n"}, {`p ([1, 2] != [1, 2])`, "false\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("%s\n got=%q want=%q", c.src, got, c.want)
		}
	}
}
