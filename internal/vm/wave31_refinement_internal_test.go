// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestRefinementModuleHooksAreUndefined pins the three module hooks MRI
// UNDEFINES on Refinement (ruby/ruby v3_4_0 eval.c:2140-2142), which is what
// keeps Refinement.private_instance_methods(true) free of them while Module
// still carries its own.
func TestRefinementModuleHooksAreUndefined(t *testing.T) {
	got := runSrc(t, `
p Refinement.private_instance_methods(true).include?(:append_features)
p Refinement.private_instance_methods(true).include?(:prepend_features)
p Refinement.private_instance_methods(true).include?(:extend_object)
p Module.private_instance_methods(true).include?(:append_features)
`)
	want := "false\nfalse\nfalse\ntrue"
	if got != want {
		t.Errorf("Refinement hook visibility =\n%s\nwant\n%s", got, want)
	}
}

// TestRefinementIncludePrependRemoved covers the two TypeError branches added
// for a refinement RECEIVER: rb_mod_include / rb_mod_prepend raise before
// looking at their arguments (ruby/ruby v3_4_0 eval.c:1212 and eval.c:1266).
func TestRefinementIncludePrependRemoved(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"include",
			`Module.new { refine(String) { include Module.new } }`,
			"TypeError: Refinement#include has been removed",
		},
		{
			"prepend",
			`Module.new { refine(String) { prepend Module.new } }`,
			"TypeError: Refinement#prepend has been removed",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := refErr(t, c.src); got != c.want {
				t.Errorf("%s: got %q, want %q", c.src, got, c.want)
			}
		})
	}
}

// TestRefinementSuperIsTheRefinedClass covers the superclass link rb_mod_refine
// installs (ruby/ruby v3_4_0 eval.c:1497-1500): the refined class's methods are
// reachable from inside the refine block, so `alias`, #instance_methods and
// #method_defined? answer as MRI's do.
func TestRefinementSuperIsTheRefinedClass(t *testing.T) {
	got := runSrc(t, `
Module.new do
  refine Array do
    p method_defined?(:size)
    p instance_methods(false)
    p instance_method(:size).owner == Array
    alias :aliased_size :size
  end
end
`)
	want := "true\n[]\ntrue"
	if got != want {
		t.Errorf("refine block reflection =\n%s\nwant\n%s", got, want)
	}
}

// TestRefinementAncestorsStopsAtRefinedClass covers both arms of the
// Refinement#ancestors walk: the refinement itself is reported, and the walk
// BREAKS at the refined class instead of continuing into it — which is how
// MRI's rb_mod_ancestors hides the superclass link (v3_4_0 class.c:1584-1597).
func TestRefinementAncestorsStopsAtRefinedClass(t *testing.T) {
	got := runSrc(t, `
Module.new do
  refine Array do
    a = ancestors
    p a.size
    p a.first.equal?(self)
    p a.include?(Array)
  end
end
`)
	want := "1\ntrue\nfalse"
	if got != want {
		t.Errorf("Refinement#ancestors =\n%s\nwant\n%s", got, want)
	}
}

// TestRefinementSuperDoesNotWidenDispatch is the guard on the superclass link:
// a refinement that reaches the refined class for REFLECTION must not start
// answering sends for methods it does not define. refinedMethod resolves
// through lookupOwnOrIncluded, which never walks the superclass, so an
// unrefined name still reaches the receiver's own definition — and a class the
// refinement does not name is untouched.
func TestRefinementSuperDoesNotWidenDispatch(t *testing.T) {
	got := runSrc(t, `
k = Class.new do
  def foo; "plain foo"; end
  def bar; "plain bar"; end
end
r = Module.new do
  refine k do
    def foo; "refined foo"; end
  end
end
using r
o = k.new
p o.foo
p o.bar
p o.frozen?
p "str".size
`)
	want := `"refined foo"
"plain bar"
false
3`
	if got != want {
		t.Errorf("dispatch under a refinement =\n%s\nwant\n%s", got, want)
	}
}

// TestUsingAdoptsTheFrameCref covers both arms of adoptRefinementCref. Inside a
// Module.new body the frame's cref is the block's TEXTUAL scope while `using`
// records on the body's definee, so the entry is repointed and
// Module.used_refinements — a native that reads the caller's cref — sees the
// activation. At the top level the two already agree, so nothing is written and
// the same read answers from the unchanged entry.
func TestUsingAdoptsTheFrameCref(t *testing.T) {
	got := runSrc(t, `
r = Module.new do
  refine Integer do
    def foo; "foo"; end
  end
end

# Module.new body: scope and frame cref differ (the write arm).
Module.new do
  p Module.used_refinements.size
  using r
  p Module.used_refinements.size
  p Module.used_refinements.first.target
end

# Top level: the two already agree (the no-write arm).
p Module.used_refinements.size
using r
p Module.used_refinements.size
`)
	want := "0\n1\nInteger\n0\n1"
	if got != want {
		t.Errorf("used_refinements around using =\n%s\nwant\n%s", got, want)
	}
}

// TestUsingActivationIsPoppedWithTheFrame is the narrowness guard on the cref
// adoption: the entry adoptRefinementCref writes is popped when the body that
// called `using` returns, so a LATER Module.new body — one that never called
// `using` — reads no refinement at all.
func TestUsingActivationIsPoppedWithTheFrame(t *testing.T) {
	got := runSrc(t, `
r = Module.new do
  refine Integer do
    def foo; "foo"; end
  end
end
Module.new do
  using r
  p Module.used_refinements.size
end
Module.new do
  p Module.used_refinements.size
end
`)
	want := "1\n0"
	if got != want {
		t.Errorf("used_refinements after the using body returned =\n%s\nwant\n%s", got, want)
	}
}

// TestRefineAndUsingAreModulePrivate pins the declaration MRI gives them:
// rb_define_private_method for Module#refine and Module#using
// (ruby/ruby v3_4_0 eval.c:2130-2131), so both are callable only with an
// implicit receiver — and rb_undef_method(rb_cClass, "refine") (eval.c:2137),
// so a class cannot hold a refinement at all.
func TestRefineAndUsingAreModulePrivate(t *testing.T) {
	got := runSrc(t, `
p Module.private_instance_methods(true).include?(:refine)
p Module.private_instance_methods(true).include?(:using)
p Class.private_instance_methods(true).include?(:refine)
begin
  Module.new.public_send(:refine, String) { }
rescue NoMethodError => e
  p :refine_is_private
end
begin
  Class.new.send(:refine, String) { }
rescue NoMethodError => e
  p :class_refine_undefined
end
`)
	want := "true\ntrue\nfalse\n:refine_is_private\n:class_refine_undefined"
	if got != want {
		t.Errorf("refine/using declarations =\n%s\nwant\n%s", got, want)
	}
}
