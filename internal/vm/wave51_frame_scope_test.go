package vm_test

import (
	"strings"
	"testing"
)

// TestNativeReachesCallerScope covers what a frame carrying its (env, self,
// definee) triple makes possible: a NATIVE method reaching the calling scope.
// Every `want` below is the measured answer of ruby 4.0.5 (/opt/homebrew/bin/ruby,
// "ruby 4.0.5 (2026-05-20 revision 64336ffd0e) +PRISM [arm64-darwin25]"), not
// rbgo's — the shapes marked "was NoMethodError" all used to raise, because the
// only route to the caller's locals was a compiler rewrite of the bareword
// `eval(str)` into `eval(str, binding)`.
func TestNativeReachesCallerScope(t *testing.T) {
	cases := []struct{ src, want string }{
		// The shape that already worked, kept as the control.
		{`def m; x = 41; eval("x + 1"); end; p m`, "42\n"},
		{`def m; x = 41; eval("x + 1", binding); end; p m`, "42\n"},
		// #send: no compiler rewrite can see this one (was NoMethodError).
		{`def m; x = 41; send(:eval, "x + 1"); end; p m`, "42\n"},
		// An alias and a Method object reach the same body (were NoMethodError).
		{`alias ev eval; def m; x = 41; ev("x + 1"); end; p m`, "42\n"},
		{`def m; x = 41; method(:eval).call("x + 1"); end; p m`, "42\n"},
		// Arities other than 1: an explicit nil scope, and the 4-argument form
		// (were NoMethodError, the rewrite only ever fired for exactly one argument).
		{`def m; x = 7; eval("x", nil); end; p m`, "7\n"},
		{`def m; x = 7; eval("x", nil, "f.rb", 10); end; p m`, "7\n"},
		// eval takes the LOCALS from the caller and `self` from the RECEIVER: the two
		// halves come from different places, which ruby 4.0.5 measures as [5, 1].
		{`x = 1; p 5.send(:eval, "[self, x]")`, "[5, 1]\n"},
		// Kernel#binding is a METHOD, so #send reaches it (was NoMethodError).
		{`p send(:binding).class`, "Binding\n"},
		{`def r; zz = 1; send(:binding); end; p r.local_variables`, "[:zz]\n"},
		// ... and it is PRIVATE, as rb_define_global_function makes it.
		{`p Object.new.respond_to?(:binding)`, "false\n"},
		{`p Object.private_method_defined?(:binding)`, "true\n"},
		// A Binding carries the method context of the frame it was captured in, so
		// eval'd code reports that method however far away the eval happens.
		{`def q; binding; end; b = q; p b.eval("__method__")`, ":q\n"},
		// The DEFINEE comes from the calling frame as well, so a `def` inside
		// eval(str) lands where MRI's caller-cref puts it. Derived from self instead
		// (K is a Class) it landed as an instance method and K.m raised NoMethodError;
		// ruby 4.0.5 answers K for both lines below.
		{`class K; class << self; def mk; eval "def m; self; end"; m; end; end; end; p K.mk`, "K\n"},
		{`class K2; class << self; def mk; eval("def m; self; end", binding); m; end; end; end; p K2.mk`, "K2\n"},
		{`p Class.new { eval("def m; 7; end") }.new.m`, "7\n"},
		// TOPLEVEL_BINDING reports MRI's synthetic location rather than nothing.
		{`p TOPLEVEL_BINDING.source_location`, "[\"<main>\", 0]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// Arity 0, as rb_f_binding is registered with.
		{`send(:binding, 1)`, "wrong number of arguments (given 1, expected 0)"},
		// Private, so an explicit receiver is refused (ruby 4.0.5: "private method
		// 'binding' called for an instance of Object").
		{`Object.new.binding`, "private method 'binding' called"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

// TestErrinfoIsPerRescueClause pins the $! lifecycle. MRI keeps errinfo in the
// innermost rescue FRAME's last local (errinfo_place, eval.c ruby_4_0:2009-2029),
// so leaving a clause reverts $! to the enclosing view for free. Every `want` is
// the measured answer of ruby 4.0.5; the four marked "(was the inner one)" are
// cases where rbgo used to report a DIFFERENT exception, not merely a stale one.
func TestErrinfoIsPerRescueClause(t *testing.T) {
	cases := []struct{ src, want string }{
		// Inside the clause: the exception. The control for everything below.
		{`begin; raise "a"; rescue; p $!.message; end`, "\"a\"\n"},
		// After the clause completes: back to the enclosing view, which is nil here.
		{`begin; raise "a"; rescue; end; p $!`, "nil\n"},
		// A second begin in the same frame no longer starts with the first's exception.
		{`begin; raise "a"; rescue; end; begin; p $!; rescue; end`, "nil\n"},
		// Nested: the OUTER exception comes back when the inner clause ends
		// (was the inner one).
		{`begin; raise "outer"; rescue; begin; raise "inner"; rescue; end; p $!.message; end`, "\"outer\"\n"},
		// An `else` clause and an enclosing `ensure` are both outside the clause
		// (was the inner one).
		{`begin; raise "a"; rescue; else; end; p $!`, "nil\n"},
		{`begin; begin; raise "a"; rescue; end; ensure; p $!; end`, "nil\n"},
		// `retry` leaves the clause too: after one that succeeds, $! is the outer
		// value (was the inner one).
		{`n = 0; begin; n += 1; raise "r" if n < 2; rescue; retry; end; p [n, $!]`, "[2, nil]\n"},
		// A bare `raise` after a successful rescue raises a FRESH RuntimeError
		// rather than re-raising the handled exception (was the inner one).
		{`begin; raise "a"; rescue; end; begin; raise; rescue => e; p [e.class, e.message]; end`,
			"[RuntimeError, \"\"]\n"},
		// A method called FROM a rescue clause still sees the clause's exception:
		// its own frame does not close the caller's scope.
		{`def peek; $!.message; end; begin; raise "a"; rescue; p peek; end`, "\"a\"\n"},
		// A method that rescued internally leaves nothing behind for its caller.
		{`def h; begin; raise "h"; rescue; end; end; h; p $!`, "nil\n"},
		// An ensure nested INSIDE a rescue clause is still inside it.
		{`begin; raise "a"; rescue; begin; 1; ensure; p $!.message; end; end`, "\"a\"\n"},
		// An ensure reached by an in-flight exception sees that exception.
		{`begin; begin; raise "x"; ensure; p $!.message; end; rescue; end`, "\"x\"\n"},
		// A `return` out of a rescue clause closes its scope: the enclosing clause's
		// exception comes back, and an intervening ensure still inside the frame
		// sees it (three ruby/spec examples measure exactly this shape).
		{`def f; begin; raise "outer"; rescue; begin; raise "inner"; rescue; return; ensure; p $!.message; end; end; end; f; p $!`,
			"\"outer\"\nnil\n"},
		// The return the compiler cannot see: it lives in ANOTHER ISeq, so the
		// interpreter closes the scope as the unwind leaves the frame.
		{`def g; [1].each { begin; raise "e"; rescue; return; end }; end; g; p $!`, "nil\n"},
		{`def k; 1.times { return }; end; def bar; begin; raise "b"; rescue; k; end; end; bar; p $!`, "nil\n"},
		// $! is read-only to Ruby, and the reserved cell the lowering uses does not
		// change that.
		{`begin; eval("$! = RuntimeError.new"); rescue NameError => e; p e.message; end`,
			"\"$! is a read-only variable\"\n"},
		// $@ follows $!.
		{`begin; raise "a"; rescue; end; p $@`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestPrivateConstantUnderLeadingColon2 covers `::Name`, the one qualified
// constant form that could still read a constant Module#private_constant had
// marked. Measured on ruby 4.0.5: the reference raises NameError while the
// BAREWORD still resolves, and defined?(::Name) is nil while defined?(Name) is
// "constant".
func TestPrivateConstantUnderLeadingColon2(t *testing.T) {
	const setup = "PRIV_C = 1; Object.send :private_constant, :PRIV_C; "
	cases := []struct{ src, want string }{
		{setup + `p defined?(::PRIV_C)`, "nil\n"},
		{setup + `p defined?(PRIV_C)`, "\"constant\"\n"},
		{setup + `p PRIV_C`, "1\n"},
		{setup + `p defined?(::NOT_THERE_AT_ALL)`, "nil\n"},
		// A public constant is unaffected by either form.
		{`PUB_C = 2; p [::PUB_C, defined?(::PUB_C)]`, "[2, \"constant\"]\n"},
		// An overriding const_missing intercepts the private reference, exactly as
		// it does for the Recv::NAME form.
		{setup + `def Object.const_missing(n); [:cm, n]; end; p ::PRIV_C`, "[:cm, :PRIV_C]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	if err := runErr(t, setup+`::PRIV_C`); err == nil || !strings.Contains(err.Error(), "private constant") {
		t.Errorf("::PRIV_C err=%v, want a private-constant NameError", err)
	}
}
