package vm_test

import (
	"strings"
	"testing"
)

// TestAncestorsAndSuper covers the ancestor-chain method resolution: super
// walking included and prepended modules and the singleton chain, Module#prepend
// / #ancestors / #include?, and the Kernel module — all vs MRI Ruby 4.0.5.
func TestAncestorsAndSuper(t *testing.T) {
	cases := []struct{ src, want string }{
		// super walks an included module (previously skipped straight to the
		// superclass).
		{`module M; def g; "M+" + super; end; end
		  class A; def g; "A"; end; end
		  class B < A; include M; def g; super; end; end
		  p B.new.g`, "\"M+A\"\n"},
		// prepend: the module runs before the class's own method and super reaches it.
		{`module Log; def run; "[" + super + "]"; end; end
		  class Job; prepend Log; def run; "work"; end; end
		  p Job.new.run`, "\"[work]\"\n"},
		{`module M; def x; 1; end; end
		  class C; prepend M; def x; 2; end; end
		  p C.new.x`, "1\n"},
		// Two prepends stack most-recent-first, each calling super.
		{`module A; def f; "A" + super; end; end
		  module B; def f; "B" + super; end; end
		  class C; prepend A; prepend B; def f; "C"; end; end
		  p C.new.f`, "\"BAC\"\n"},
		// Plain inheritance super still works (regression).
		{`class A; def f; "a"; end; end
		  class B < A; def f; super + "b"; end; end
		  p B.new.f`, "\"ab\"\n"},
		// Class-method (singleton) super, previously broken.
		{`class A; def self.g; "A"; end; end
		  class B < A; def self.g; "B+" + super; end; end
		  p B.g`, "\"B+A\"\n"},
		// Module#ancestors (Kernel now modelled).
		{`module M; end; class A; end; class B < A; include M; end; p B.ancestors`,
			"[B, M, A, Object, Kernel, BasicObject]\n"},
		{`module P; end; class C; prepend P; end; p C.ancestors`,
			"[P, C, Object, Kernel, BasicObject]\n"},
		{`p Object.ancestors`, "[Object, Kernel, BasicObject]\n"},
		// A module that itself includes / prepends another module is expanded.
		{`module Inner; end; module Outer; include Inner; end; class C; include Outer; end; p C.ancestors`,
			"[C, Outer, Inner, Object, Kernel, BasicObject]\n"},
		{`module Pre; end; module Outer; prepend Pre; end; class C; include Outer; end; p C.ancestors`,
			"[C, Pre, Outer, Object, Kernel, BasicObject]\n"},
		// is_a? / include? see modules and Kernel.
		{`p 1.is_a?(Kernel)`, "true\n"},
		{`module P; end; class C; prepend P; end; p C.new.is_a?(P)`, "true\n"},
		{`p Object.include?(Kernel)`, "true\n"},
		{`module M; end; class C; include M; end; p C.include?(M)`, "true\n"},
		{`module M; end; class C; end; p C.include?(M)`, "false\n"},
		{`module M; end; p M.include?(M)`, "false\n"}, // a module never includes itself
		{`p String.include?(Comparable)`, "true\n"},
		// The prepended hook fires, mirroring included.
		{`module P; def self.prepended(b); puts "prepended #{b}"; end; end
		  class C; prepend P; end`, "prepended C\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// super with no method above it raises NoMethodError — for both an instance
	// method and a class method.
	for _, src := range []string{
		`class A; def f; super; end; end; A.new.f`,
		`class A; def self.g; super; end; end; A.g`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "super: no superclass method") {
			t.Errorf("src=%q got=%v want NoMethodError", src, err)
		}
	}
	// include? rejects a non-module argument.
	if err := runErr(t, `Object.include?(1)`); err == nil || !strings.Contains(err.Error(), "expected Module") {
		t.Errorf("include?(1): got %v want TypeError", err)
	}
}
