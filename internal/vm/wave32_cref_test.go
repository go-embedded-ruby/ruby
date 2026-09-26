// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestRefinementReachesBlocksOfItsScope pins the reach of a `using`.
//
// MRI keeps a cref CHAIN, and `using` does not merely record a module: it
// REPLACES the calling frame's cref with a duplicate carrying the refinements
// — rb_vm_cref_replace_with_duplicated_cref (vm.c v3_4_0:1931) →
// vm_cref_replace_with_duplicated_cref (vm_insnhelper.c v3_4_0:912), from
// mod_using (eval.c:1547) and top_using (eval.c:1890). Every block created
// after it in that frame captures the replacement, every scope pushed inside it
// chains to it, and rb_method_entry_with_refinements walks the whole chain.
//
// rbgo records one *RClass per frame, and the block-literal path read the
// lexCref LOCAL, settled before `using` ran. A refinement activated in a
// Module.new / Class.new body therefore reached NOTHING written inside it — not
// a nested Module.new, not Class.new, not #class_eval, not #instance_eval or
// #instance_exec, not even a plain `each`.
//
// Every expectation here was taken from MRI 4.0.5.
func TestRefinementReachesBlocksOfItsScope(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// Every block shape written inside the body that called `using`.
		{`R = Module.new { refine(Integer) { def foo; "R"; end } }
r = []
Module.new do
  using R
  r << (1.foo rescue "ERR")
  Module.new { r << (1.foo rescue "ERR") }
  Class.new  { r << (1.foo rescue "ERR") }
  [0].each   { r << (1.foo rescue "ERR") }
  Object.new.instance_exec { r << (1.foo rescue "ERR") }
  Object.new.instance_eval { r << (1.foo rescue "ERR") }
end
p r`, `["R", "R", "R", "R", "R", "R"]` + "\n"},

		// …and a block whose DEFINEE is switched to an unrelated class.
		{`R = Module.new { refine(Integer) { def foo; "R"; end } }
class Zz; end
r = []
Module.new do
  using R
  Zz.class_eval  { r << (1.foo rescue "ERR") }
  Zz.module_eval { r << (1.foo rescue "ERR") }
end
p r`, `["R", "R"]` + "\n"},

		// A method of the class whose body called `using`, and the blocks and
		// lambdas written inside THAT method — the third link, which a Proc has to
		// carry itself because the method frame's cref is the textual one.
		{`R = Module.new { refine(Integer) { def foo; "R"; end } }
c = Class.new do
  using R
  def direct;  1.foo rescue "ERR"; end
  def in_block; [0].map { 1.foo rescue "ERR" }.first; end
  def via_call; b = -> { 1.foo rescue "ERR" }; b.call; end
  def via_iexec; b = -> { 1.foo rescue "ERR" }; self.instance_exec(&b); end
end
o = c.new
p [o.direct, o.in_block, o.via_call, o.via_iexec]`,
			`["R", "R", "R", "R"]` + "\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestCrefChainKeepsItsOtherEnd is the CONTROL: widening refinement lookup to
// the second end of the chain must not disturb what the FIRST end answers, nor
// what a cref is otherwise for.
//
// Two things could have broken and one of them briefly did. Resolving
// refinements from the frame's cref ALONE cost three examples in
// core/module/refine_spec.rb, because a `refine` block's own refinements hang
// off the pushed link (its klass is the refinement module, rb_yield_refine_block)
// and not off the textual one. And a cref is chiefly for CONSTANTS: MRI skips a
// pushed_by_eval cref when resolving one, so a constant inside a class_eval
// block must still resolve textually rather than against the eval target.
func TestCrefChainKeepsItsOtherEnd(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// A refine block sees its holder's sibling refinements (the pushed link).
		{`M = Module.new do
  refine(String) { def sa; "sa"; end }
  refine(Integer) { def sb; "x".sa + "sb"; end }
end
using M
p 1.sb`, "\"sasb\"\n"},

		// A constant inside a class_eval block resolves TEXTUALLY: :top, not the
		// :inq defined on the eval target.
		{`KTOP = :top
class Q; KTOP = :inq; end
Q.class_eval { p KTOP }`, ":top\n"},

		// …and Module.nesting inside it is still the textual nesting.
		{`class Q; end
Q.class_eval { p Module.nesting }`, "[]\n"},
		{`module P
  class Q; end
  Q.class_eval { p Module.nesting }
end`, "[P]\n"},

		// A `def` inside a class_eval lands on the eval target while its body's
		// constants keep the textual scope.
		{`CT = :outer
class Q2; CT = :inner; end
Q2.class_eval { def ct; CT; end }
p [Q2.new.ct, Q2.instance_method(:ct).owner]`, "[:outer, Q2]\n"},

		// With no refinement anywhere, nothing about block cref capture moved.
		{`class A3; end
A3.class_eval do
  def foo; :foo; end
  XC = 1
end
p [A3.new.foo, A3.const_defined?(:XC, false), Object.const_defined?(:XC, false)]`,
			"[:foo, false, true]\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
