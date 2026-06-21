package vm_test

import (
	"strings"
	"testing"
)

// TestMethodObjects covers Object#method and the Method class — call/[]/===,
// name, arity (required / optional+splat / native), owner, receiver and to_proc
// (as a block) — asserted against MRI Ruby 4.0.5.
func TestMethodObjects(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [1, 2, 3].method(:sum).call`, "6\n"},
		{`p "hello".method(:upcase).call`, "\"HELLO\"\n"},
		{`p "hi".method(:upcase).name`, ":upcase\n"},
		{`m = [10, 20, 30].method(:[]); p m[1]`, "20\n"}, // [] alias
		{"class C; def add(a, b); a + b; end; end\np C.new.method(:add).arity", "2\n"},
		{"class C; def f(a, b = 2, *c); end; end\np C.new.method(:f).arity", "-2\n"},
		{`p "x".method(:upcase).arity`, "-1\n"}, // native -> variadic
		{`p [1, 2, 3].method(:sum).receiver`, "[1, 2, 3]\n"},
		{`p [1, 2].map(&[10, 20, 30].method(:[]))`, "[20, 30]\n"}, // to_proc as a block
		// a singleton method resolves through method().
		{"o = Object.new\no.define_singleton_method(:s) { 5 }\np o.method(:s).call", "5\n"},
		// owner of a class method.
		{"class C; def self.cm; end; end\np C.method(:cm).owner == C", "true\n"},
		// arity of a block-defined (proc-backed) singleton method.
		{"o = Object.new\no.define_singleton_method(:s) { 5 }\np o.method(:s).arity", "0\n"},
		// a Method object is truthy.
		{`p ("x".method(:upcase) ? 1 : 2)`, "1\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMethodErrors covers method() of a missing name -> NameError, and the
// Method's string form.
func TestMethodErrors(t *testing.T) {
	if err := runErr(t, `Object.new.method(:nope)`); err == nil || !strings.Contains(err.Error(), "NameError") {
		t.Errorf("missing method: got %v", err)
	}
	if got := eval(t, "class C; def f; end; end\np C.new.method(:f).inspect"); got != "\"#<Method: C#f>\"\n" {
		t.Errorf("inspect got %q", got)
	}
}
