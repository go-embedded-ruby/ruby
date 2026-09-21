package vm

import "testing"

// Multiple assignment evaluates every left-hand RECEIVER before the right-hand
// side, the way MRI's pre/rhs/lhs anchor order does (compile.c
// compile_massign0, ruby/ruby v3_4_0:5848-5850). Every expectation here was
// taken from ruby 4.0.5.
func TestMasgnEvaluatesReceiversBeforeValues(t *testing.T) {
	checkSrc(t, "receivers first", `
$log = []
class T
  def initialize(n); @n = n; end
  def sub; $log << @n; self; end
  def x=(v); $log << [:set, @n, v]; end
end
a = T.new(:A)
b = T.new(:B)
def r(n); $log << n; n; end
a.sub.x, b.sub.x = r(:R1), r(:R2)
p $log`,
		`[:A, :B, :R1, :R2, [:set, :A, :R1], [:set, :B, :R2]]`)
}

// A splat inside an index target of a multiple assignment has a run-time
// argument count, so the assigned value is APPENDED to the argument array
// rather than sent with a fixed argc — MRI's `pushtoarray 1` rewrite in
// compile_massign_lhs (compile.c, ruby/ruby v3_4_0:5588-5619).
func TestMasgnSplatIndexTarget(t *testing.T) {
	checkSrc(t, "two splat index targets", "a = []\na[*[1]], a[*[2]] = 1, 2\np a", "[nil, 1, 2]")
	checkSrc(t, "splat plus a literal index", "x = [0]\na = []\na[*x, 1], b = 5, 6\np x, a, b", "[0]\n[5]\n6")
	// MRI appends to a COPY of the splatted array (its `dupsplat` branch,
	// v3_4_0:5595-5601), so the caller's array is not mutated.
	checkSrc(t, "splat source not mutated", "x = [1]\na = []\na[*x], b = 5, 6\np x, a, b", "[1]\n[nil, 5]\n6")
	// The assignment still evaluates to the right-hand side array.
	checkSrc(t, "value of the assignment", "a = []\nv = (a[*[1]], b = 1, 2)\np v", "[1, 2]")
}

// Every other multiple-assignment target kind, so the pre-evaluation walk and
// the store arms are exercised together.
func TestMasgnTargetKinds(t *testing.T) {
	checkSrc(t, "locals and a splat", "a, *b, c = 1, 2, 3, 4\np a, b, c", "1\n[2, 3]\n4")
	checkSrc(t, "nameless splat", "a, * = 1, 2, 3\np a", "1")
	checkSrc(t, "nested group", "(a, b), c = [1, 2], 3\np a, b, c", "1\n2\n3")
	checkSrc(t, "grouped sole target", "(a, b) = [1, 2]\np a, b", "1\n2")
	checkSrc(t, "nested group with a receiver", `
o = Struct.new(:z).new
(o.z, b), c = [1, 2], 3
p o.z, b, c`, "1\n2\n3")
	checkSrc(t, "ivar, cvar, gvar", `
class K
  @@cv = nil
  def go; @iv, @@cv, $gv = 1, 2, 3; [@iv, @@cv, $gv]; end
end
p K.new.go`, "[1, 2, 3]")
	checkSrc(t, "bare constant", "A, B = 1, 2\np A, B", "1\n2")
	checkSrc(t, "scoped constant", "class Q; end\nQ::A, Q::B = 1, 2\np Q::A, Q::B", "1\n2")
	checkSrc(t, "attribute setters", "o = Struct.new(:l, :r).new\no.l, o.r = 1, 2\np o.l, o.r", "1\n2")
	checkSrc(t, "fixed index targets", "a = []\nb = []\na[0], b[1] = 1, 2\np a, b", "[1]\n[nil, 2]")
	checkSrc(t, "splat right-hand side", "a, b = *[1, 2]\np a, b", "1\n2")
	checkSrc(t, "single right-hand value", "a, b = [1, 2]\np a, b", "1\n2")
}

// A `rescue => target` clause reuses the same store arms WITHOUT the
// multiple-assignment pre-evaluation, so its receiver is compiled in place.
// These drive pushMasgnRecv's non-pre-evaluated path and storeMultiTarget's
// non-pre-evaluated setter paths.
func TestRescueIntoNonLocalTargets(t *testing.T) {
	checkSrc(t, "index target", `
a = []
begin; raise "e"; rescue => a[0]; end
p a[0].message`, `"e"`)
	checkSrc(t, "splat index target", `
a = []
x = [0]
begin; raise "e"; rescue => a[*x]; end
p a[0].message`, `"e"`)
	checkSrc(t, "attribute target", `
o = Struct.new(:z).new
begin; raise "e"; rescue => o.z; end
p o.z.message`, `"e"`)
	checkSrc(t, "scoped constant target", `
class K; end
begin; raise "e"; rescue => K::E; end
p K::E.message`, `"e"`)
	checkSrc(t, "ivar target", `
class K
  def go; begin; raise "e"; rescue => @e; end; @e.message; end
end
p K.new.go`, `"e"`)
}

// Module#private_constant is enforced on the qualified `Recv::NAME` path, on
// `defined?(A::B)` and on a COMPACT `class A::B` / `module A::B` reopen — and
// on nothing else. See variable.c set_const_visibility (ruby/ruby
// v3_4_0:3749-3786) and the lookup at :3114-3116.
func TestPrivateConstantEnforcement(t *testing.T) {
	checkSrc(t, "qualified read raises", `
module M
  X = 1
  private_constant :X
end
begin; M::X; rescue NameError => e; p e.message, e.name, e.receiver; end`,
		"\"private constant M::X referenced\"\n:X\nM")

	// The holder, not the class the lookup started from, is the reported receiver.
	checkSrc(t, "receiver is the defining module", `
class P
  X = 1
  private_constant :X
end
class C < P; end
begin; C::X; rescue NameError => e; p e.receiver, e.name; end`, "P\n:X")

	checkSrc(t, "unqualified reads still resolve", `
module M
  X = 1
  private_constant :X
  def self.from_self; X; end
  def self.defined_from_self; defined?(X); end
  module Nested
    def self.from_scope; X; end
  end
end
class Inc; include M; def from_include; X; end; end
p M.from_self, M.defined_from_self, M::Nested.from_scope, Inc.new.from_include`,
		"1\n\"constant\"\n1\n1")

	checkSrc(t, "const_get and const_defined? are not screened", `
module M
  X = 1
  private_constant :X
end
p M.const_get(:X), M.const_defined?(:X)`, "1\ntrue")

	checkSrc(t, "defined? is nil", `
module M
  X = 1
  private_constant :X
end
p defined?(M::X)`, "nil")

	checkSrc(t, "public_constant restores access", `
module M
  X = 1
  private_constant :X
  public_constant :X
end
p M::X, defined?(M::X)`, "1\n\"constant\"")

	checkSrc(t, "an undefined name is a NameError", `
module M; end
begin; M.send(:private_constant, :NOPE); rescue NameError => e; p e.message; end`,
		`"constant M::NOPE not defined"`)
}

// A private reference routes through #const_missing, so an override intercepts
// it and may return a value instead of raising — MRI raises only from the
// DEFAULT Module#const_missing (rb_mod_const_missing, ruby/ruby
// v3_4_0:2341-2351).
func TestPrivateConstantRoutesThroughConstMissing(t *testing.T) {
	checkSrc(t, "override intercepts", `
mod = Module.new
mod.const_set :Foo, true
mod.send :private_constant, :Foo
def mod.const_missing(name); name == :Foo ? name : super; end
p mod::Foo`, ":Foo")
}

// A COMPACT definition screens constant visibility (vm_insnhelper.c
// vm_const_get_under, ruby/ruby v3_4_0:5707-5717); a bare nested one does not.
func TestPrivateConstantBlocksScopedReopen(t *testing.T) {
	checkSrc(t, "compact module reopen raises", `
module C
  module PrivMod; end
  private_constant :PrivMod
end
begin; module C::PrivMod; end; rescue NameError => e; p e.message; end`,
		`"private constant C::PrivMod referenced"`)

	checkSrc(t, "compact class reopen raises", `
module C
  class PrivCls; end
  private_constant :PrivCls
end
begin; class C::PrivCls; end; rescue NameError => e; p e.message; end`,
		`"private constant C::PrivCls referenced"`)

	checkSrc(t, "a bare nested reopen is allowed", `
module C
  module Pub; end
end
module C
  module Pub; Y = 2; end
end
p C::Pub::Y`, "2")

	// The guard must not fire for a name that is not private, nor for a compact
	// definition that creates a brand-new constant.
	checkSrc(t, "compact reopen of a public constant", `
module C; module Pub; end; end
module C::Pub; Y = 3; end
p C::Pub::Y`, "3")
	checkSrc(t, "compact definition of a new constant", `
module C; end
module C::Fresh; Z = 4; end
p C::Fresh::Z`, "4")
}

// A bare top-level `include M` mixes M into Object — MRI defines it as a
// private singleton method on main forwarding to Module#include on Object
// (eval.c top_include, ruby/ruby v3_4_0:1862-1866). The RSpec include MATCHER
// this VM publishes on Object stays reachable, told apart by its arguments.
func TestTopLevelInclude(t *testing.T) {
	checkSrc(t, "mixes into Object", `
module M; CONST_IN_M = 42; def hello; :hi; end; end
include M
p Object.ancestors.include?(M), CONST_IN_M, 1.respond_to?(:hello)`,
		"true\n42\ntrue")

	checkSrc(t, "returns Object", `
module M; end
p(include M)`, "Object")

	checkSrc(t, "several modules at once", `
module A; end
module B; end
include A, B
p Object.ancestors.include?(A), Object.ancestors.include?(B)`, "true\ntrue")

	// A non-module argument is the matcher, not the language construct.
	checkSrc(t, "non-module argument is the matcher", `
require "rspec"
expect([1, 2, 3]).to include(2)
puts "ok"`, "ok")
	checkSrc(t, "no argument is the matcher", `
require "rspec"
p include().class`, "RSpec::Matchers::BuiltIn::BaseMatcher")
}

// The value of a multiple assignment is the right-hand side AS THE EXPRESSION
// PRODUCED IT: MRI dups it before expandarray (compile.c compile_massign0,
// ruby/ruby v3_4_0:5804-5810), so a single right-hand value is returned
// unwrapped.
func TestMasgnValueIsTheRightHandSide(t *testing.T) {
	checkSrc(t, "single value", "p((a, b, c = 1))", "1")
	checkSrc(t, "single value with a splat target", "p((*a = 1))", "1")
	checkSrc(t, "leading splat target", "p((a, *b = 1))", "1")
	checkSrc(t, "several values", "p((a, b = 1, 2))", "[1, 2]")
	checkSrc(t, "array value", "p((a, b = [1, 2]))", "[1, 2]")
	checkSrc(t, "splatted value", "p((a, b = *[1, 2]))", "[1, 2]")
}

// Destructuring converts with #to_ary and never #to_a, an object that declines
// is destructured as a one-element list, and a #to_ary returning a non-Array is
// a TypeError — MRI's expandarray via rb_check_array_type (vm_insnhelper.c
// vm_expandarray, ruby/ruby v3_4_0:1933-1942).
func TestMasgnDestructuringUsesToAry(t *testing.T) {
	checkSrc(t, "to_ary converts", "class A; def to_ary; [1, 2]; end; end\na, b = A.new\np [a, b]", "[1, 2]")
	checkSrc(t, "to_a is not consulted",
		"class O; def to_a; [1, 2]; end; end\na, b = O.new\np [a.class, b]", "[O, nil]")
	checkSrc(t, "to_ary returning nil declines",
		"class N; def to_ary; nil; end; end\na, b = N.new\np [a.class, b]", "[N, nil]")
	checkSrc(t, "a plain object is a one-element list", "a, b = 1\np [a, b]", "[1, nil]")
	checkSrc(t, "to_ary returning a non-Array is a TypeError", `
class Bad; def to_ary; 5; end; end
begin; a, b = Bad.new; rescue TypeError => e; p e.message; end`,
		`"can't convert Bad to Array (Bad#to_ary gives Integer)"`)
	// An Array SUBCLASS instance is already T_ARRAY, so no conversion is tried.
	checkSrc(t, "an Array subclass is not converted", `
class MyAry < Array; def to_ary; raise "forbidden"; end; end
a, b = MyAry.new([1, 2])
p [a, b]`, "[1, 2]")
	// An overridden #respond_to? is honoured, as MRI's rb_check_funcall does.
	checkSrc(t, "an overridden respond_to? is honoured", `
class NoRt
  def to_ary; [1, 2]; end
  def respond_to?(n, p = false); n == :to_ary ? false : super; end
end
a, b = NoRt.new
p [a.class, b]`, "[NoRt, nil]")
	// A nested group destructures its incoming value the same way.
	checkSrc(t, "a nested group converts too",
		"class M; def to_ary; [1, 2]; end; end\na, (b, c), d = 1, M.new, 4\np [a, b, c, d]", "[1, 1, 2, 4]")
}
