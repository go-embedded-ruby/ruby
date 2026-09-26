// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestNegationOperatorsDispatch covers issue #663 and the two neighbours it
// asked about.
//
// In MRI none of `!`, `!=` and `!~` is a compiler-synthesised negation. All
// three are real methods on BasicObject / Kernel — rb_obj_not, rb_obj_not_equal
// and rb_obj_not_match (object.c v3_4_0:264, :280, :1678; installed at :4265,
// :4266, :4427). `!` and `!=` get SPECIALISED instructions, opt_not and
// opt_neq, which compute the answer in place only while the receiver's method
// is still the built-in cfunc and otherwise fall back to the send
// (vm_insnhelper.c v3_4_0:6665, :7019). `!~` gets no specialisation at all:
// idNMatch is absent from compile.c's opt_* table (v3_4_0:4260-4297).
//
// Measured against MRI 4.0.5, rbgo diverged on all three, each for a different
// reason:
//
//   - `!~` was compiled as `!(a =~ b)`, so a user-defined #!~ was DEAD CODE and
//     `Object.new.send(:!~, 1)` reported the wrong missing method;
//   - `!` was inlined unconditionally, so a redefined #! never ran anywhere;
//   - `!=` dispatched only for a method found on the class chain below
//     BasicObject, so a singleton `def o.!=` and a module-supplied one
//     (o.extend M) were both invisible and the opcode inverted #== instead.
func TestNegationOperatorsDispatch(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// A class definition reaches all three.
		{`class C
  def !~(o); "cls-nmatch"; end
  def !=(o); "cls-neq"; end
  def !; "cls-not"; end
end
c = C.new
p [c !~ 1, c != 1, !c]`, `["cls-nmatch", "cls-neq", "cls-not"]` + "\n"},

		// A SINGLETON definition reaches all three. This is the half the old `!=`
		// check could not see: it walked classOf(a).super, which skips the
		// singleton class entirely.
		{`o = Object.new
o.define_singleton_method(:!~) { |x| "sing-nmatch" }
o.define_singleton_method(:!=) { |x| "sing-neq" }
o.define_singleton_method(:!) { "sing-not" }
p [o !~ 1, o != 1, !o]`, `["sing-nmatch", "sing-neq", "sing-not"]` + "\n"},

		// A module mixed in with #extend reaches all three, for the same reason.
		{`m = Module.new do
  def !~(x); "mod-nmatch"; end
  def !=(x); "mod-neq"; end
  def !; "mod-not"; end
end
q = Object.new.extend(m)
p [q !~ 1, q != 1, !q]`, `["mod-nmatch", "mod-neq", "mod-not"]` + "\n"},

		// A BasicObject subclass: below BasicObject, so the class chain reaches it.
		{`class D < BasicObject
  def !=(o); "bo-neq"; end
  def !; "bo-not"; end
end
d = D.new
::Kernel.p [d != 1, !d]`, `["bo-neq", "bo-not"]` + "\n"},

		// The DEFAULTS, unchanged. Each still answers what the built-in does, so
		// the specialisation is a specialisation and not a behaviour change.
		{`p [!true, !false, !nil, !0, !"", ![]]`,
			"[false, true, true, false, false, false]\n"},
		{`p [1 != 2, 1 != 1, "a" != "a", nil != nil, Object.new != Object.new]`,
			"[true, false, false, false, true]\n"},
		{`p ["abc" !~ /b/, "abc" !~ /z/, nil !~ /z/]`, "[false, true, true]\n"},

		// Kernel#!~ is a REAL method now, not a compiler shape, so the whole
		// reflective surface answers as MRI's does.
		{`p Object.new.respond_to?(:!~)`, "true\n"},
		{`p Kernel.instance_method(:!~).owner`, "Kernel\n"},
		{`p Object.new.method(:!~).arity`, "1\n"},

		// …and #send reaches it, which is the difference that shows the default is
		// really `!(self =~ other)`: the missing method reported is #=~, not #!~.
		{`begin; Object.new.send(:!~, 1); rescue NoMethodError => e; p e.message; end`,
			"\"undefined method '=~' for an instance of Object\"\n"},

		// The default sends #=~, so a redefined #=~ is what runs under it.
		{`class E; def =~(o); o == 7; end; end
p [E.new !~ 7, E.new !~ 8]`, "[false, true]\n"},

		// `!=` still dispatches #== when nobody redefined #!= — not an identity
		// compare (rb_obj_not_equal calls id_eq, object.c v3_4_0:282).
		{`class F; def ==(o); true; end; end
p [F.new != F.new, F.new != 1]`, "[false, false]\n"},

		// And a REDEFINED #! is dispatched from inside a lowered/branching
		// position too, not only from a `p` argument.
		{`class G; def !; :truthy_never; end; end
g = G.new
r = []
3.times { r << !g }
p r`, "[:truthy_never, :truthy_never, :truthy_never]\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestNegationCacheInvalidates is the CONTROL for the inline cache
// notValueCached keeps: a cached "nobody redefined #!" verdict must not survive
// the redefinition that makes it false. MRI invalidates the same verdict
// through its method-state serial; rbgo's cache re-validates against
// globalMethodSerial, which every definition path bumps.
//
// Each case negates the SAME receiver class at the SAME instruction before and
// after the change, so a stale cache shows up as the old answer coming back.
func TestNegationCacheInvalidates(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// Fill the cache with the default, then redefine #! on the class.
		{`class H; end
h = H.new
r = []
2.times do |i|
  class H; def !; :custom; end; end if i == 1
  r << !h
end
p r`, "[false, :custom]\n"},

		// Fill the cache with the default, then give the OBJECT a singleton #!.
		{`i = Object.new
r = []
2.times do |n|
  i.define_singleton_method(:!) { :sing } if n == 1
  r << !i
end
p r`, "[false, :sing]\n"},

		// Same for #!=, whose verdict goes through the same resolver.
		{`class J; end
j = J.new
r = []
2.times do |n|
  class J; def !=(o); :custom_neq; end; end if n == 1
  r << (j != 1)
end
p r`, "[true, :custom_neq]\n"},

		// And the other direction: remove the override and the default returns.
		{`class K; def !; :custom; end; end
k = K.new
r = []
2.times do |n|
  K.send(:remove_method, :!) if n == 1
  r << !k
end
p r`, "[:custom, false]\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
