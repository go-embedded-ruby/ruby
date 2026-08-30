package vm_test

import (
	"strings"
	"testing"
)

// TestInstanceMethodsAndSymbolCompare covers Module#instance_methods,
// Object#methods, and Symbol#<=> / Symbol being Comparable. Asserted against MRI
// Ruby 4.0.5 (method lists are sorted, since their order is implementation-defined).
func TestInstanceMethodsAndSymbolCompare(t *testing.T) {
	cases := []struct{ src, want string }{
		// instance_methods(false): own methods only; default: includes inherited.
		{`class A; def foo; end; def bar; end; end; p A.instance_methods(false).sort`, "[:bar, :foo]\n"},
		{`class A; def foo; end; end
class B < A; def baz; end; end
p [B.instance_methods(false).sort, B.instance_methods.include?(:foo)]`, "[[:baz], true]\n"},
		// Included module methods show up in the full list, not in (false).
		{`module M; def mm; end; end
class A; include M; def foo; end; end
p [A.instance_methods(false).sort, A.instance_methods.include?(:mm)]`, "[[:foo], true]\n"},
		// Object#methods includes inherited and singleton methods.
		{`class A; def foo; end; end; p [A.new.methods.include?(:foo), A.new.methods.include?(:to_s)]`, "[true, true]\n"},
		{`o = Object.new; def o.special; end; p o.methods.include?(:special)`, "true\n"},
		// Symbol#<=> and Comparable.
		{`p [(:a <=> :b), (:b <=> :a), (:a <=> :a), (:a <=> "x")]`, "[-1, 1, 0, nil]\n"},
		{`p [:a < :b, :c > :a, :a.between?(:a, :z)]`, "[true, true, true]\n"},
		{`p [:c, :a, :b].sort`, "[:a, :b, :c]\n"},
		{`p [:banana, :apple, :cherry].min`, ":apple\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestModuleReflectionC37 covers Module#module_exec, #singleton_class?,
// #public_instance_method and #set_temporary_name, asserted against MRI 4.0.6.
func TestModuleReflectionC37(t *testing.T) {
	cases := []struct{ src, want string }{
		// module_exec runs the block with the module as self, passing arguments.
		{`class C1; end; C1.module_exec(7) { |x| define_method(:v) { x } }; p C1.new.v`, "7\n"},
		// singleton_class?: a per-object singleton and a class metaclass are
		// singletons; an ordinary class or module is not.
		{`p [Integer.singleton_class?, Integer.singleton_class.singleton_class?, Object.new.singleton_class.singleton_class?, Module.new.singleton_class?]`, "[false, true, true, false]\n"},
		// public_instance_method returns an UnboundMethod for a public method.
		{`class C2; def foo; 1; end; end; m = C2.public_instance_method(:foo); p [m.class, m.bind(C2.new).call]`, "[UnboundMethod, 1]\n"},
		// set_temporary_name assigns and clears a non-permanent name.
		{`m = Module.new; m.set_temporary_name("handy"); n1 = m.name; m.set_temporary_name(nil); p [n1, m.name]`, "[\"handy\", nil]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`class C3; private def foo; end; end; C3.public_instance_method(:foo)`, "is private"},
		{`class C3b; protected def bar; end; end; C3b.public_instance_method(:bar)`, "is protected"},
		{`class C3c; end; C3c.public_instance_method(:nope)`, "undefined method 'nope'"},
		{`Object.set_temporary_name("x")`, "can't change permanent name"},
		{`Module.new.set_temporary_name("Foo::Bar")`, "must not be a constant path"},
		{`Module.new.set_temporary_name("Const")`, "must not be a constant path"},
		{`Module.new.set_temporary_name("")`, "empty class/module name"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

// TestClassSubclasses covers Class#subclasses: the classes directly inheriting
// from self, excluding included/prepended modules and deeper descendants.
// Asserted against MRI Ruby 4.0.6.
func TestClassSubclasses(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class SubA; end; class SubB < SubA; end; class SubC < SubA; end; p SubA.subclasses.map(&:name).sort`, "[\"SubB\", \"SubC\"]\n"},
		{`p Class.new.subclasses`, "[]\n"},
		{`class SubD; end; class SubE < SubD; end; class SubF < SubE; end; p SubD.subclasses.map(&:name)`, "[\"SubE\"]\n"},
		// A module mixed into the parent is not a subclass.
		{`parent = Class.new; child = Class.new(parent); parent.include(Module.new); p parent.subclasses == [child]`, "true\n"},
		{`parent = Class.new; child = Class.new(parent); parent.prepend(Module.new); p parent.subclasses == [child]`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestModuleDeprecateConstant covers Module#deprecate_constant: marking existing
// constants (returning self), the NameError for an undefined name, and the
// deprecation warning on read via constant scope and const_get — emitted only
// while Warning[:deprecated] is enabled. Asserted against MRI Ruby 4.0.6.
func TestModuleDeprecateConstant(t *testing.T) {
	cases := []struct{ src, want string }{
		{`module DcA; X = 1; deprecate_constant :X; end; p DcA::X`, "1\n"},
		{`module DcB; Y = 2; end; p DcB.deprecate_constant(:Y).equal?(DcB)`, "true\n"},
		// The warning fires on scoped access and const_get when enabled...
		{`require "stringio"
module DcC; Z = 3; deprecate_constant :Z; end
Warning[:deprecated] = true; $stderr = StringIO.new
DcC::Z; DcC.const_get(:Z); DcC.const_get("Z")
p $stderr.string.scan("is deprecated").length`, "3\n"},
		// ...and stays silent when disabled.
		{`require "stringio"
module DcD; W = 4; deprecate_constant :W; end
Warning[:deprecated] = false; $stderr = StringIO.new
DcD::W
p $stderr.string`, "\"\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if err := runErr(t, `module DcE; deprecate_constant :Nope; end`); err == nil ||
		!strings.Contains(err.Error(), "constant DcE::Nope not defined") {
		t.Errorf("deprecate_constant(undefined): err=%v", err)
	}
}
