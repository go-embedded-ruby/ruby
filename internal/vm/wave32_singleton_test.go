// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestSpecialSingletonClass pins MRI's special_singleton_class_of (class.c
// v3_4_0:2199): nil, true and false are immediates with no room for a per-object
// singleton class, so Ruby answers their CLASS instead. singleton_class_of
// returns it directly for T_NIL / T_TRUE / T_FALSE (class.c v3_4_0:2236-2242),
// one arm BELOW the "can't define singleton" raise that covers Integer, Bignum,
// Float and Symbol — which is the distinction rbgo did not draw: it raised for
// all seven.
//
// The consequence is not only that #singleton_class answers: a method defined
// "on" nil is an ordinary instance method of NilClass, reached by every nil in
// the program, and `nil.extend(M)` includes M into NilClass.
func TestSpecialSingletonClass(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// The class itself, not a fabricated singleton wrapping it.
		{`p nil.singleton_class`, "NilClass\n"},
		{`p true.singleton_class`, "TrueClass\n"},
		{`p false.singleton_class`, "FalseClass\n"},
		{`p nil.singleton_class.equal?(NilClass)`, "true\n"},
		{`p true.singleton_class.equal?(TrueClass)`, "true\n"},
		{`p false.singleton_class.equal?(FalseClass)`, "true\n"},

		// It is an ORDINARY class: #singleton_class? is false and #superclass is
		// Object, neither of which a fabricated singleton class would report.
		{`p nil.singleton_class.singleton_class?`, "false\n"},
		{`p nil.singleton_class.superclass`, "Object\n"},
		{`p true.singleton_class.superclass`, "Object\n"},

		// `class << nil` opens NilClass, so its defs and constants land there.
		{`class << nil; def z; 42; end; end; p nil.z`, "42\n"},
		{`p(class << true; self; end)`, "TrueClass\n"},
		{`class << false; K = 7; end; p FalseClass::K`, "7\n"},

		// define_singleton_method and extend route through the same class.
		{`nil.define_singleton_method(:dsm) { :ok }; p nil.dsm`, ":ok\n"},
		{`p nil.extend(Module.new { def em; :em; end }).em`, ":em\n"},

		// The four that DO raise stay raising (the arm above).
		{`begin; 1.singleton_class; rescue => e; p [e.class, e.message]; end`,
			"[TypeError, \"can't define singleton\"]\n"},
		{`begin; :a.singleton_class; rescue => e; p [e.class, e.message]; end`,
			"[TypeError, \"can't define singleton\"]\n"},
		{`begin; 1.5.singleton_class; rescue => e; p [e.class, e.message]; end`,
			"[TypeError, \"can't define singleton\"]\n"},
		{`begin; (10**40).singleton_class; rescue => e; p [e.class, e.message]; end`,
			"[TypeError, \"can't define singleton\"]\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestMetaclassChainEndsAtClass pins make_metaclass's last link: the metaclass
// of a class with no superclass takes Class as its superclass —
//
//	RCLASS_SET_SUPER(metaclass, super ? ENSURE_EIGENCLASS(super) : rb_cClass)
//	                                     (class.c v3_4_0:786)
//
// BasicObject is the only such class, so the one link closes the chain for every
// class in the program. Without it `#<Class:BasicObject>.superclass` was nil and
// a singleton class of a class could see nothing of Class: not its ancestors,
// not its instance methods, not its class methods.
func TestMetaclassChainEndsAtClass(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p BasicObject.singleton_class.superclass`, "Class\n"},
		{`p Object.singleton_class.superclass`, "#<Class:BasicObject>\n"},
		{`class A; end; p A.singleton_class.superclass`, "#<Class:Object>\n"},

		// The chain now reaches Class, so a class's singleton class inherits it.
		{`class A; end; p A.singleton_class.ancestors.include?(Class)`, "true\n"},
		{`class A; end; p A.singleton_class.ancestors`,
			"[#<Class:A>, #<Class:Object>, #<Class:BasicObject>, Class, Module, Object, Kernel, BasicObject]\n"},

		// Instance methods of Class are visible through it (the shape
		// language/singleton_class_spec.rb asserts).
		{`class A; end
class Class; def example_im; end; end
p A.singleton_class.method_defined?(:example_im, true)`, "true\n"},

		// And so are Class's own class methods.
		{`class A; end
class Class; def self.example_cm; end; end
p A.singleton_class.respond_to?(:example_cm)`, "true\n"},

		// Class.singleton_class is unaffected: Class HAS a superclass (Module), so
		// it follows the ordinary rule rather than the root fallback.
		{`p Class.singleton_class.superclass`, "#<Class:Module>\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
