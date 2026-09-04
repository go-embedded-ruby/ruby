// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestModuleMutatorFrozen covers the FrozenError raised by the structural
// mutators alias_method / undef_method / remove_method on a frozen receiver,
// and the MRI ordering rules around it: a call with NO arguments raises nothing
// (the frozen state is only consulted per name argument), and a name that
// cannot be coerced raises TypeError BEFORE the frozen check fires. Verified
// against MRI (ruby 4.0.x).
func TestModuleMutatorFrozen(t *testing.T) {
	cases := []struct{ src, class, msg string }{
		// alias_method on a frozen class/module.
		{"class Bar; def a; end; end\nBar.freeze\nBar.alias_method(:b, :a)",
			"FrozenError", "can't modify frozen Class: Bar"},
		{"module Foo; def a; end; end\nFoo.freeze\nFoo.alias_method(:b, :a)",
			"FrozenError", "can't modify frozen Module: Foo"},
		// undef_method: a present name on a frozen class raises FrozenError, and a
		// MISSING name still raises FrozenError (frozen beats the NameError).
		{"class Bar; def a; end; end\nBar.freeze\nBar.undef_method(:a)",
			"FrozenError", "can't modify frozen Class: Bar"},
		{"class Bar; end\nBar.freeze\nBar.undef_method(:nope)",
			"FrozenError", "can't modify frozen Class: Bar"},
		// undef_method: a non-name argument is a TypeError even on a frozen class,
		// because coercion precedes the frozen check.
		{"class Bar; end\nBar.freeze\nBar.undef_method(Object.new)",
			"TypeError", ""},
		// remove_method: FrozenError, and coercion-before-frozen as above.
		{"class Bar; def a; end; end\nBar.freeze\nBar.remove_method(:a)",
			"FrozenError", "can't modify frozen Class: Bar"},
		{"class Bar; end\nBar.freeze\nBar.remove_method(Object.new)",
			"TypeError", ""},
	}
	for _, c := range cases {
		class, msg := evalErr(t, c.src)
		if class != c.class {
			t.Errorf("%q: got class %s want %s (msg %q)", c.src, class, c.class, msg)
			continue
		}
		if c.msg != "" && msg != c.msg {
			t.Errorf("%q: got msg %q want %q", c.src, msg, c.msg)
		}
	}
}

// TestModuleMutatorNoArgsFrozen checks that alias/undef/remove with no arguments
// on a frozen receiver raise nothing (MRI) — the loop never reaches the frozen
// check — returning the receiver.
func TestModuleMutatorNoArgsFrozen(t *testing.T) {
	cases := []struct{ src, want string }{
		{"class Bar; end\nBar.freeze\np Bar.undef_method", "Bar\n"},
		{"class Bar; end\nBar.freeze\np Bar.remove_method", "Bar\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("%q: got %q want %q", c.src, got, c.want)
		}
	}
}

// TestUndefMethodMissingName covers the NameError wording undef_method raises for
// a name defined nowhere: it names a module as "module" and a class as "class",
// and reports the receiver by its #to_s. Verified against MRI.
func TestUndefMethodMissingName(t *testing.T) {
	cases := []struct{ src, class, msg string }{
		{"module Foo; end\nFoo.undef_method(:nope)",
			"NameError", "undefined method 'nope' for module 'Foo'"},
		{"class Bar; end\nBar.undef_method(:nope)",
			"NameError", "undefined method 'nope' for class 'Bar'"},
	}
	for _, c := range cases {
		class, msg := evalErr(t, c.src)
		if class != c.class || msg != c.msg {
			t.Errorf("%q: got %s/%q want %s/%q", c.src, class, msg, c.class, c.msg)
		}
	}
}

// TestRemoveMethodMissingName covers remove_method's NameError for a name not
// defined directly on the receiver ("not defined in <to_s>").
func TestRemoveMethodMissingName(t *testing.T) {
	class, msg := evalErr(t, "class Bar; end\nBar.remove_method(:nope)")
	if class != "NameError" || msg != "method 'nope' not defined in Bar" {
		t.Errorf("got %s/%q", class, msg)
	}
}

// TestModuleDirectivesArePrivate checks that module_function and the bare
// visibility directives are PRIVATE instance methods of Module, matching MRI.
func TestModuleDirectivesArePrivate(t *testing.T) {
	for _, name := range []string{"module_function", "private", "public", "protected"} {
		src := "p Module.private_instance_methods.include?(:" + name + ")"
		if got := eval(t, src); got != "true\n" {
			t.Errorf("%s private?: got %q want \"true\\n\"", name, got)
		}
	}
}

// TestConstantVisibilityValidation covers private_constant / public_constant:
// each named constant must be defined directly on the receiver (an inherited or
// missing name is a NameError), and a valid call returns the receiver. The
// access control itself is not enforced. Verified against MRI.
func TestConstantVisibilityValidation(t *testing.T) {
	errCases := []string{
		// A constant inherited from a superclass is not "defined in" the subclass.
		"c1 = Class.new\nc1.const_set(:Foo, true)\nc2 = Class.new(c1)\nc2.send(:private_constant, :Foo)",
		// A name defined nowhere.
		"Module.new.send(:private_constant, :Nope)",
		"Module.new.send(:public_constant, :Nope)",
	}
	for _, src := range errCases {
		if class, _ := evalErr(t, src); class != "NameError" {
			t.Errorf("%q: got class %s want NameError", src, class)
		}
	}
	// A valid directive (own constant, String or Symbol name) returns the receiver.
	okCases := []struct{ src, want string }{
		{"m = Module.new\nm.const_set(:X, 1)\np m.send(:private_constant, :X).equal?(m)", "true\n"},
		{"m = Module.new\nm.const_set(:X, 1)\np m.send(:public_constant, \"X\").equal?(m)", "true\n"},
	}
	for _, c := range okCases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("%q: got %q want %q", c.src, got, c.want)
		}
	}
}

// TestNameArgToStrCoercion checks that module_function and alias_method coerce a
// non-Symbol/String name argument through #to_str (MRI), rather than rejecting
// it outright.
func TestNameArgToStrCoercion(t *testing.T) {
	cases := []struct{ src, want string }{
		// module_function(obj_with_to_str) converts the given name and exposes it as
		// a module method.
		{"module M\n  def foo; 7; end\n  o = Object.new\n  def o.to_str; \"foo\"; end\n  module_function(o)\nend\np M.foo", "7\n"},
		// alias_method(new, old) with a #to_str-bearing new name.
		{"class C\n  def orig; 42; end\n  o = Object.new\n  def o.to_str; \"al\"; end\n  alias_method(o, :orig)\nend\np C.new.al", "42\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("%q: got %q want %q", c.src, got, c.want)
		}
	}
}
