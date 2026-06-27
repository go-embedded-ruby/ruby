package vm_test

import (
	"strings"
	"testing"
)

// TestMethodVisibilityEnforcement exercises private/protected/public enforcement
// on the send path. Every expected output is the MRI Ruby 4.0.5 result.
func TestMethodVisibilityEnforcement(t *testing.T) {
	checkCases(t, []runCase{
		// private: implicit-receiver call works, explicit-receiver call works via
		// self (MRI 2.7+ relaxation), bare def-default and `private def` both set it.
		{`class C; def pub; priv; end; private def priv; 42; end; end; p C.new.pub`, "42\n"},
		{`class C; def viaself; self.priv; end; private def priv; 7; end; end; p C.new.viaself`, "7\n"},
		{`class C; def pub; priv; end; private; def priv; 99; end; end; p C.new.pub`, "99\n"},

		// protected: callable when the caller's self is_a? the owner.
		{`class C; def initialize(v); @v=v; end; def >(o); val > o.val; end; protected def val; @v; end; end; p(C.new(2) > C.new(1))`, "true\n"},
		{`class C; def initialize(v); @v=v; end; def cmp(o); o.val; end; protected; def val; @v; end; end; p C.new(3).cmp(C.new(8))`, "8\n"},

		// private setter through self is allowed.
		{`class G; def initialize; self.x = 5; end; def get; @x; end; private; attr_writer :x; end; p G.new.get`, "5\n"},

		// public restores visibility (own + inherited).
		{`class C; def f; 1; end; private :f; public :f; end; p C.new.f`, "1\n"},
		{`class A; def f; 1; end; end; class B < A; private :f; end; class B; public :f; end; p B.new.f`, "1\n"},

		// redefining a previously-private method resets it to the body default.
		{`class C; def f; 1; end; private :f; end; class C; def f; 2; end; end; p C.new.f`, "2\n"},

		// private_class_method on inherited new; the factory uses implicit self.
		{`class P; def self.make; new; end; private_class_method :new; end; p P.make.class`, "P\n"},
		// class << self; private :new likewise.
		{`class Q; def self.make; new; end; class << self; private :new; end; end; p Q.make.class`, "Q\n"},
		// public_class_method restores it.
		{`class R; private_class_method :new; public_class_method :new; end; p R.new.class`, "R\n"},

		// respond_to? honours visibility and the include_private flag.
		{`class C; private def s; end; end; p C.new.respond_to?(:s)`, "false\n"},
		{`class C; private def s; end; end; p C.new.respond_to?(:s, true)`, "true\n"},
		{`class C; def pub; end; end; p C.new.respond_to?(:pub)`, "true\n"},
		{`class P; private_class_method :new; end; p P.respond_to?(:new)`, "false\n"},
		{`class P; private_class_method :new; end; p P.respond_to?(:new, true)`, "true\n"},

		// send bypasses visibility; public_send dispatches public ones fine.
		{`class C; private def s; 11; end; end; p C.new.send(:s)`, "11\n"},
		{`class C; def pub; 12; end; end; p C.new.public_send(:pub)`, "12\n"},

		// module_function: private as instance method, public as module method.
		{`module M; module_function; def mf; 7; end; end; p M.mf`, "7\n"},
		{`module M; module_function; def mf; 7; end; end; class I; include M; def go; mf; end; end; p I.new.go`, "7\n"},
		{`module M; module_function def mf; 7; end; end; p M.mf`, "7\n"},

		// private array-argument form.
		{`class C; def f; 1; end; def g; 2; end; private [:f, :g]; def call; f + g; end; end; p C.new.call`, "3\n"},

		// visibility resets to public at the top of a reopened body.
		{`class C; private; def a; 1; end; end; class C; def b; 2; end; end; p C.new.b`, "2\n"},

		// def returns the method name symbol.
		{`p(def foo; end)`, ":foo\n"},
		{`class C; p(def self.bar; end); end`, ":bar\n"},
		{`o = Object.new; p(def o.baz; end)`, ":baz\n"},
	})
}

// TestMethodVisibilityViolations asserts the NoMethodError raised (message and
// all) when a private/protected method is called through an explicit receiver.
func TestMethodVisibilityViolations(t *testing.T) {
	cases := []struct{ src, msg string }{
		{`class C; private def f; end; end; C.new.f`, "private method 'f' called for an instance of C"},
		{`class P; private_class_method :new; end; P.new`, "private method 'new' called for class P"},
		{`module M; private def f; end; end; class I; include M; end; I.new.f`, "private method 'f' called for an instance of I"},
		{`class C; def initialize(v); @v=v; end; protected def val; @v; end; end; C.new(1).val`, "protected method 'val' called for an instance of C"},
		{`class C; def s; end; private :s; end; C.new.public_send(:s)`, "private method 's' called for an instance of C"},
		// protected called from an unrelated object's self.
		{`class C; def initialize(v);@v=v;end; protected def val; @v; end; end; class Other; def grab(c); c.val; end; end; Other.new.grab(C.new(5))`, "protected method 'val' called for an instance of C"},
		// explicit send with a block argument is also gated.
		{`class C; private def f; yield; end; end; C.new.f { 1 }`, "private method 'f' called for an instance of C"},
		// explicit send with a splat argument list is gated.
		{`class C; private def f(*a); a; end; end; args=[1,2]; C.new.f(*args)`, "private method 'f' called for an instance of C"},
	}
	for _, c := range cases {
		err := runErr(t, c.src)
		if err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("src=%q\n got=%v\nwant contains %q", c.src, err, c.msg)
		}
	}
}

// TestVisibilityNameErrors covers marking a nonexistent method, on both the
// instance and class-method directives, and in a `class << self` body.
func TestVisibilityNameErrors(t *testing.T) {
	for _, c := range []struct{ src, msg string }{
		{`class C; private :nope; end`, "undefined method 'nope'"},
		{`class C; protected :nope; end`, "undefined method 'nope'"},
		{`class C; public :nope; end`, "undefined method 'nope'"},
		{`class C; private_class_method :nope; end`, "undefined method 'nope'"},
		{`class C; public_class_method :nope; end`, "undefined method 'nope'"},
		{`class C; class << self; private :nope; end; end`, "undefined method 'nope'"},
	} {
		err := runErr(t, c.src)
		if err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("src=%q\n got=%v\nwant contains %q", c.src, err, c.msg)
		}
	}
}

// TestVisibilityReceiverDescriptions covers the "class C" wording for a class
// receiver and that a class-method made private is rejected externally.
func TestVisibilityReceiverDescriptions(t *testing.T) {
	err := runErr(t, `class C; def self.x; end; private_class_method :x; end; C.x`)
	if err == nil || !strings.Contains(err.Error(), "private method 'x' called for class C") {
		t.Errorf("got %v", err)
	}
}
