// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// refErr runs src and returns the raised error's "Class: message" string, or ""
// when the program completes without raising.
func refErr(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, rerr := New(io.Discard).Run(iseq); rerr != nil {
		return rerr.Error()
	}
	return ""
}

// TestRefineArgumentValidation covers refine's ArgumentError (no argument / no
// block) and TypeError (non class/module argument) branches.
func TestRefineArgumentValidation(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"no arg", `Module.new { refine {} }`, "ArgumentError"},
		{"non class", `Module.new { refine("foo") {} }`, "TypeError: wrong argument type String (expected Class or Module)"},
		{"no block", `Module.new { refine String }`, "ArgumentError"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := refErr(t, c.src); !strings.HasPrefix(got, c.want) {
				t.Errorf("got %q, want prefix %q", got, c.want)
			}
		})
	}
}

// TestUsingArgumentValidation covers using's ArgumentError (no argument),
// TypeError (String / Class rather than Module) and the method-scope RuntimeError.
func TestUsingArgumentValidation(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"no arg toplevel", `using`, "ArgumentError"},
		{"string", `Module.new { using "foo" }`, "TypeError: wrong argument type String (expected Module)"},
		{"class", `Module.new { using Class.new }`, "TypeError: wrong argument type Class (expected Module)"},
		{"in method", `class C; def m; using Module.new {}; end; end; C.new.m`, "RuntimeError"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := refErr(t, c.src); !strings.HasPrefix(got, c.want) {
				t.Errorf("got %q, want prefix %q", got, c.want)
			}
		})
	}
	if got := refErr(t, `class C; def m; using Module.new {}; end; end; C.new.m`); !strings.Contains(got, "not permitted in methods") {
		t.Errorf("method-scope message = %q", got)
	}
}

// TestRefineCanonicalActivation is the canonical example: a top-level `using`
// activates a String refinement for the remainder of the top-level scope. This
// also covers refinementScope's non-RClass (main object) branch.
func TestRefineCanonicalActivation(t *testing.T) {
	src := `
module M
  refine String do
    def shout; upcase + "!"; end
  end
end
using M
puts "hi".shout`
	if got := runSrc(t, src); got != "HI!" {
		t.Errorf("got %q, want HI!", got)
	}
}

// TestRefineLexicalNonLeak proves a refinement activated in one scope is not
// active in a sibling scope nor outside it.
func TestRefineLexicalNonLeak(t *testing.T) {
	src := `
R = Module.new { refine(String) { def foo; "foo"; end } }
Module.new do
  using R
  puts "in: " + "x".foo
end
Module.new do
  begin; "x".foo; rescue NoMethodError; puts "sibling: nme"; end
end
begin; "x".foo; rescue NoMethodError; puts "outer: nme"; end`
	want := "in: foo\nsibling: nme\nouter: nme"
	if got := runSrc(t, src); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRefineSuper covers super inside a refinement resolving in the refined class
// only: it reaches the class method, skips a sibling refinement, and raises
// NoMethodError when the refined class has no such method.
func TestRefineSuper(t *testing.T) {
	src := `
class C; def foo; [:C]; end; end
R1 = Module.new { refine(C) { def foo; [:R1]; end } }
R2 = Module.new { refine(C) { def foo; [:R2] + super; end } }
Module.new do
  using R1
  using R2
  p C.new.foo
end
Rb1 = Module.new { refine(C) { def bar; "hidden"; end } }
Rb2 = Module.new { refine(C) { def bar; super; end } }
Module.new do
  using Rb1
  using Rb2
  begin; C.new.bar; rescue NoMethodError; puts "bar: nme"; end
end`
	want := "[:R2, :C]\nbar: nme"
	if got := runSrc(t, src); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRefineSingletonPriority proves a per-object singleton method wins over an
// active refinement (refinedMethod's objSingleton branch).
func TestRefineSingletonPriority(t *testing.T) {
	src := `
class C2; def foo; "class"; end; end
R = Module.new { refine(C2) { def foo; "refine"; end } }
Module.new do
  using R
  obj = C2.new
  def obj.foo; "singleton"; end
  puts obj.foo
end`
	if got := runSrc(t, src); got != "singleton" {
		t.Errorf("got %q, want singleton", got)
	}
}

// TestRefineSubclassPriority proves a method defined in a subclass wins over a
// refinement of a superclass (refinedMethod's own-method branch).
func TestRefineSubclassPriority(t *testing.T) {
	src := `
class Base; def foo; "base"; end; end
class Sub < Base; def foo; "sub"; end; end
R = Module.new { refine(Base) { def foo; "refine"; end } }
Module.new do
  using R
  puts Sub.new.foo
end`
	if got := runSrc(t, src); got != "sub" {
		t.Errorf("got %q, want sub", got)
	}
}

// TestRefineFallThroughToClass covers a refinement that targets the receiver's
// class but does not define the called method: dispatch falls through to the
// class's own method (the continue + own-method branches of refinedMethod).
func TestRefineFallThroughToClass(t *testing.T) {
	src := `
class C3; def foo; "cfoo"; end; end
R = Module.new { refine(C3) {} }
Module.new do
  using R
  puts C3.new.foo
end`
	if got := runSrc(t, src); got != "cfoo" {
		t.Errorf("got %q, want cfoo", got)
	}
}

// TestRefineSiblingActive proves that inside a refinement method every sibling
// refinement of the same holder module is active for a direct send to another
// refined class (activeRefinements' isRefinement branch + collectRefinements).
func TestRefineSiblingActive(t *testing.T) {
	src := `
R = Module.new do
  refine Integer do
    def label; "n" + to_s; end
  end
  refine Array do
    def describe; first.label; end
  end
end
Module.new do
  using R
  puts [7].describe
end`
	if got := runSrc(t, src); got != "n7" {
		t.Errorf("got %q, want n7", got)
	}
}

// TestRefineModuleInclusion proves `using` activates refinements from an included
// module's ancestors, with descendant refinements overriding ancestor ones.
func TestRefineModuleInclusion(t *testing.T) {
	src := `
Inc = Module.new { refine(Integer) { def j; "inc"; end } }
Over = Module.new do
  include Inc
  refine Integer do
    def j; "over"; end
  end
end
Module.new do
  using Over
  puts 5.j
end`
	if got := runSrc(t, src); got != "over" {
		t.Errorf("got %q, want over", got)
	}
}

// TestRefinementReflection covers Refinement#target, Module#refinements (with and
// without entries), Module#used_modules, and the class of a refinement module.
func TestRefinementReflection(t *testing.T) {
	src := `
R = Module.new { refine(String) { def x; end } }
ref = R.refinements.first
puts ref.class
puts ref.target
puts R.refinements.length
puts Module.new.refinements.length
Used = Module.new { using R; using R }
puts Used.used_modules == [R]
puts Module.new.used_modules.length`
	want := "Refinement\nString\n1\n0\ntrue\n0"
	if got := runSrc(t, src); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRefineSingletonMethodScope proves a refinement activated in a module body
// is active in a `def self.m` defined in that body (refinementParent's metaclass
// delegation).
func TestRefineSingletonMethodScope(t *testing.T) {
	src := `
class K; def foo; "orig"; end; end
R = Module.new { refine(K) { def foo; "refined"; end } }
M = Module.new do
  using R
  def self.call(k); k.foo; end
end
puts M.call(K.new)`
	if got := runSrc(t, src); got != "refined" {
		t.Errorf("got %q, want refined", got)
	}
}

// TestRefineWithLiteralBlock covers dispatching a refined method that is called
// with a literal block (the block-carrying OpSend refinement branch).
func TestRefineWithLiteralBlock(t *testing.T) {
	src := `
R = Module.new { refine(Array) { def each_twice; each { |x| yield x; yield x }; end } }
Module.new do
  using R
  out = []
  [1, 2].each_twice { |x| out << x }
  p out
end`
	if got := runSrc(t, src); got != "[1, 1, 2, 2]" {
		t.Errorf("got %q, want [1, 1, 2, 2]", got)
	}
}

// TestRefineWithBlockArg covers dispatching a refined method invoked with an
// explicit &block argument (the OpSendBlockArg refinement branch).
func TestRefineWithBlockArg(t *testing.T) {
	src := `
R = Module.new { refine(Integer) { def apply; yield self; end } }
Module.new do
  using R
  blk = ->(x) { x * 10 }
  p 5.apply(&blk)
end`
	if got := runSrc(t, src); got != "50" {
		t.Errorf("got %q, want 50", got)
	}
}

// TestRefineAccumulationReuse proves repeated refine of the same class reuse one
// Refinement module (refinementFor's existing-entry branch).
func TestRefineAccumulationReuse(t *testing.T) {
	src := `
selves = []
M = Module.new { refine(String) { selves << self } }
M.module_eval { refine(String) { selves << self } }
puts selves[0].equal?(selves[1])
puts M.refinements.length`
	want := "true\n1"
	if got := runSrc(t, src); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRefineUndefinedStillRaises proves that with a refinement active, a call to
// a wholly undefined method still routes to method_missing (refinedMethod walks
// the whole ancestor chain and returns nil at the end).
func TestRefineUndefinedStillRaises(t *testing.T) {
	src := `
R = Module.new { refine(Integer) { def foo; end } }
Module.new do
  using R
  begin; 5.no_such_method; rescue NoMethodError; puts "nme"; end
end`
	if got := runSrc(t, src); got != "nme" {
		t.Errorf("got %q, want nme", got)
	}
}

// TestRefineClassMethodScope proves a refinement activated in a class body is
// active in a method defined under `class << self` (refinementParent's metaclass
// delegation).
func TestRefineClassMethodScope(t *testing.T) {
	src := `
class K2; def foo; "orig"; end; end
R = Module.new { refine(K2) { def foo; "refined"; end } }
class Host
  using R
  class << self
    def call(k); k.foo; end
  end
end
puts Host.call(K2.new)`
	if got := runSrc(t, src); got != "refined" {
		t.Errorf("got %q, want refined", got)
	}
}

// TestRefinedMethodNilScope directly exercises refinedMethod's nil-scope guard,
// which the interpreter's frame-carried definee never triggers on its own.
func TestRefinedMethodNilScope(t *testing.T) {
	vm := New(io.Discard)
	if m := vm.refinedMethod(nil, object.IntValue(1), "foo"); m != nil {
		t.Errorf("nil scope should yield no refinement, got %v", m)
	}
}
