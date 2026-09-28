// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"testing"
)

// TestCrefChainIsPerSiteNotPerClass is the witness that made the chain
// necessary. A::B is CREATED nested in A, so its lexParent slot says A; it is
// then REOPENED with a compact path inside Outer. MRI's nesting there is
// [A::B, Outer] — the compact path's parent is not pushed, and the enclosing
// module is — so A::X is invisible and Outer::Y is visible. Reading the
// nesting off the class's one lexParent slot got BOTH ends wrong: it showed A
// and hid Outer.
//
// Verified against ruby 4.0.5 (arm64-darwin25), which prints exactly the want
// below for this source.
func TestCrefChainIsPerSiteNotPerClass(t *testing.T) {
	src := `module A; X = "A::X"; class B; end; module MB; end; end
module Outer
  Y = "Outer::Y"
  class A::B
    p Module.nesting
    p defined?(X), defined?(Y)
  end
  module A::MB
    p Module.nesting
  end
end
class A::B
  p Module.nesting
end
`
	want := "[A::B, Outer]\nnil\n\"constant\"\n[A::MB, Outer]\n[A::B]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestCompactDefinitionBodyCannotSeeTheParentNamespace is the same divergence
// read through a method rather than Module.nesting: `class A::B` is not
// `module A; class B`, and a bare X in the compact body must not reach A::X.
// The NameError names the cref base, A::B, as MRI's uninitialized_constant
// does.
func TestCompactDefinitionBodyCannotSeeTheParentNamespace(t *testing.T) {
	src := `module A2; X = "A2::X"; class B; end; end
class A2::B
  def m; X; end
end
begin; A2::B.new.m; rescue NameError => e; puts "#{e.class}: #{e.message}"; end
module A2
  class B
    def n; X; end
  end
end
puts A2::B.new.n
`
	want := "NameError: uninitialized constant A2::B::X\nA2::X\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestNestingInsideObjectBodyIsObject covers the one place the old walk
// STOPPED too early: it terminated the chain at Object, so a `class Object`
// body reported an empty nesting and a class opened inside it lost the Object
// link. MRI's rb_mod_nesting stops before the chain's BOTTOM link, not before
// Object, so both bodies name Object.
func TestNestingInsideObjectBodyIsObject(t *testing.T) {
	src := `class Object
  p Module.nesting
  class InObj; p Module.nesting; end
end
p Module.nesting
`
	want := "[Object]\n[InObj, Object]\n[]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestSingletonClassBodyChainsToItsEnclosingScope: `class << obj` pushes the
// singleton class onto the enclosing chain like any other body.
func TestSingletonClassBodyChainsToItsEnclosingScope(t *testing.T) {
	src := `module Wrap
  P = Object.new
  class << P
    p Module.nesting.length
    p Module.nesting[1]
  end
end
`
	want := "2\nWrap\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestBareConstantMissRoutesThroughConstMissing: MRI's rb_const_get_0 hands an
// unresolved constant to #const_missing on the cref base rather than raising,
// so an override answers for a BARE constant, not only for Recv::NAME. rbgo
// raised straight from the bytecode and never consulted the hook.
func TestBareConstantMissRoutesThroughConstMissing(t *testing.T) {
	src := `class Z
  def self.const_missing(n); "cm:#{n}"; end
  def m; Nope; end
end
p Z.new.m
module MZ
  def self.const_missing(n); "mcm:#{n}"; end
  def self.g; Nope2; end
end
p MZ.g
`
	want := "\"cm:Nope\"\n\"mcm:Nope2\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestUninitializedConstantNameErrorIsPopulated: the default hook's NameError
// names the scope it searched and carries it as #receiver, with the bare
// constant as #name. rbgo reported a bare message and a nil receiver.
// The top level is Object and is NOT spelled out in the message, but IS the
// receiver — the asymmetry is MRI's (uninitialized_constant, variable.c).
func TestUninitializedConstantNameErrorIsPopulated(t *testing.T) {
	src := `class Q
  def m; Missing1; end
end
begin; Q.new.m; rescue NameError => e; p e.message, e.name, e.receiver; end
module MM
  module NN
    def self.go; Missing2; end
  end
end
begin; MM::NN.go; rescue NameError => e; p e.message, e.name, e.receiver; end
begin; Missing3; rescue NameError => e; p e.message, e.name, e.receiver; end
begin; ::Missing4; rescue NameError => e; p e.message, e.name, e.receiver; end
module Sc; end
begin; Sc::Missing5; rescue NameError => e; p e.message, e.name, e.receiver; end
`
	want := "\"uninitialized constant Q::Missing1\"\n:Missing1\nQ\n" +
		"\"uninitialized constant MM::NN::Missing2\"\n:Missing2\nMM::NN\n" +
		"\"uninitialized constant Missing3\"\n:Missing3\nObject\n" +
		"\"uninitialized constant Missing4\"\n:Missing4\nObject\n" +
		"\"uninitialized constant Sc::Missing5\"\n:Missing5\nSc\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestUninitializedConstantInASingletonScopeIsQualified covers constErrPath's
// anonymous arm: a singleton class has no .name, and MRI still qualifies the
// message with rb_class_path, which renders `#<Class:Q2>`.
func TestUninitializedConstantInASingletonScopeIsQualified(t *testing.T) {
	src := `class Q2
  class << self
    def s; Missing6; end
  end
end
begin; Q2.s; rescue NameError => e; p e.message; end
`
	want := "\"uninitialized constant #<Class:Q2>::Missing6\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestConstErrPathLeavesANilScopeUnqualified covers the arm no Ruby source can
// reach: every call site passes a real module or Object, so the nil guard —
// which is there because scopedNameFor's has always been — needs a direct
// call to be exercised.
func TestConstErrPathLeavesANilScopeUnqualified(t *testing.T) {
	vm := New(os.Stderr)
	if got := vm.constErrPath(nil, "K"); got != "K" {
		t.Errorf("nil scope => %q, want %q", got, "K")
	}
	if got := vm.constErrPath(vm.cObject, "K"); got != "K" {
		t.Errorf("Object scope => %q, want %q", got, "K")
	}
}

// TestCrefNestingSkipsANilLinkAndStopsOnACycle covers crefLink's two defensive
// arms directly. A chain is built by the running program, and the lexParent
// walk this replaced had to grow the very same cycle guard after a
// `class << o; CONST = self; end` made a class its own lexical parent and a
// walk with no end allocated 17 GB. Neither arm is reachable from Ruby source
// today, which is exactly why it is asserted here rather than assumed.
func TestCrefNestingSkipsANilLinkAndStopsOnACycle(t *testing.T) {
	a, b := newClass("CN_A", nil), newClass("CN_B", nil)
	chain := crefPush(a, crefPush(nil, crefPush(b, nil)))
	got := crefNesting(chain)
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("nil link not skipped: %v", got)
	}

	loop := &crefLink{klass: a}
	loop.next = loop
	if got := crefNesting(loop); len(got) != 1 || got[0] != a {
		t.Errorf("cycle not stopped: %v", got)
	}
}

// TestNestingFallsBackToLexParentForAnUnrelatedScope: the chain answers only
// for the frame it belongs to. Asked about some other module — which is what
// Module#const_get and an autoload retry do — nesting must fall back to the
// lexParent walk rather than hand back the running frame's nesting.
func TestNestingFallsBackToLexParentForAnUnrelatedScope(t *testing.T) {
	vm := New(os.Stderr)
	outer := newClass("NF_Outer", nil)
	outer.isModule = true
	inner := newClass("NF_Outer::NF_Inner", nil)
	inner.isModule = true
	inner.lexParent = outer

	// No frame is running, so there is no chain: the walk is all there is.
	if got := vm.nesting(inner); len(got) != 2 || got[0] != inner || got[1] != outer {
		t.Fatalf("lexParent walk => %v, want [inner outer]", got)
	}

	// With a chain in place for a DIFFERENT scope, the answer must not change.
	vm.curCref = crefPush(outer, nil)
	if got := vm.nesting(inner); len(got) != 2 || got[0] != inner || got[1] != outer {
		t.Errorf("unrelated chain leaked into the answer: %v", got)
	}
	// And when it IS this scope's chain, the chain wins over the walk.
	vm.curCref = crefPush(inner, nil)
	if got := vm.nesting(inner); len(got) != 1 || got[0] != inner {
		t.Errorf("chain ignored: %v", got)
	}
}

// TestConstBaseOfTheTopLevelIsObject covers constBase's empty-nesting arm:
// there is no innermost scope at the top level, and MRI's vm_get_const_base
// answers Object.
func TestConstBaseOfTheTopLevelIsObject(t *testing.T) {
	vm := New(os.Stderr)
	if got := vm.constBase(vm.cObject); got != vm.cObject {
		t.Errorf("constBase(Object) => %v, want Object", got)
	}
	m := newClass("CB_M", nil)
	m.isModule = true
	if got := vm.constBase(m); got != m {
		t.Errorf("constBase(CB_M) => %v, want CB_M", got)
	}
}
