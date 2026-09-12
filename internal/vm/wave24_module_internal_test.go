// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// writeRB writes src to a fresh file under t.TempDir() and returns its path,
// with every backslash-free path usable directly inside a Ruby double-quoted
// literal. Used by the autoload tests, which need real files to require.
func writeRB(t *testing.T, name, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return filepath.ToSlash(p)
}

// TestAutoloadArgumentContract covers Module#autoload's argument order: the name
// is coerced first (TypeError), then the path (#to_path), then the constant-name
// check, then the empty-feature check, and only then the frozen check. Each
// answer was compared against ruby 4.0.5.
func TestAutoloadArgumentContract(t *testing.T) {
	for _, tc := range []struct {
		name, src, class, msg string
	}{
		{"name not a symbol or string", `module M; end; M.autoload(5, "x.rb")`,
			"TypeError", "5 is not a symbol nor a string"},
		{"path not a string", `module M; end; M.autoload(:A, 5)`,
			"TypeError", "no implicit conversion of Integer into String"},
		{"name not a constant", `module M; end; M.autoload(:a, "x.rb")`,
			"NameError", "autoload must be constant name: a"},
		{"empty feature", `module M; end; M.autoload(:A, "")`,
			"ArgumentError", "empty feature name"},
		{"frozen module", `module M; end; M.freeze; M.autoload(:A, "x.rb")`,
			"FrozenError", "can't modify frozen Module: M"},
		{"autoload? name not a symbol or string", `module M; end; M.autoload?(5)`,
			"TypeError", "5 is not a symbol nor a string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			class, msg := evalErr(t, tc.src)
			if class != tc.class || msg != tc.msg {
				t.Errorf("got %s: %q, want %s: %q", class, msg, tc.class, tc.msg)
			}
		})
	}

	// #to_path converts a non-String path, and a lowercase name reported by
	// autoload? is nil rather than an error (MRI's rb_check_id yields no id).
	src := `module M; end
o = Object.new
def o.to_path; "conv.rb"; end
M.autoload(:A, o)
p M.autoload?(:A)
p M.autoload?(:a)
p M.autoload?(:Missing)
`
	if got, want := eval(t, src), "\"conv.rb\"\nnil\nnil\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestAutoloadConstantIsReserved covers the constant being visible before the
// file is loaded: const_defined? (with and without inherit), Module#constants
// and autoload? all report it, and Module#const_added fires on registration.
func TestAutoloadConstantIsReserved(t *testing.T) {
	src := `module M
  def self.const_added(n); (@seen ||= []) << n; end
  autoload :A, "a.rb"
  autoload :A, "b.rb"
end
p M.const_defined?(:A), M.const_defined?(:A, false), M.constants, M.autoload?(:A)
p M.instance_variable_get(:@seen)
`
	want := "true\ntrue\n[:A]\n\"b.rb\"\n[:A]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}

	// An autoload registered for an ALREADY defined constant is inert, and the
	// ancestor search is what autoload?'s inherit flag switches off.
	src2 := `module M; X = 1; autoload :X, "x.rb"; end
p M.autoload?(:X)
class P; end
class C < P; end
P.autoload :Z, "z.rb"
p C.autoload?(:Z), C.autoload?(:Z, false)
`
	if got, want := eval(t, src2), "nil\n\"z.rb\"\nnil\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestAutoloadSurvivesAFailedRequire covers tryAutoload's restore: a require
// that raises leaves the autoload registered, so the constant stays in
// Module#constants and autoload? still reports the path, while a require that
// completes without defining the constant retires it.
func TestAutoloadSurvivesAFailedRequire(t *testing.T) {
	boom := writeRB(t, "boom.rb", "raise 'boom'\n")
	src := `module M; autoload :A, "` + boom + `"; end
begin
  M::A
rescue RuntimeError => e
  p e.message
end
p M.autoload?(:A), M.const_defined?(:A, false)
`
	if got, want := eval(t, src), "\"boom\"\ntrue\n"; !strings.HasPrefix(got, "\"boom\"\n") {
		t.Errorf("got %q want prefix %q", got, want)
	}
	if got := eval(t, src); !strings.Contains(got, `"`+boom+`"`) {
		t.Errorf("autoload? should still report the path, got %q", got)
	}

	// A file that loads but does not define the constant retires the autoload.
	quiet := writeRB(t, "quiet.rb", "$loaded = ($loaded || 0) + 1\n")
	src2 := `module M; autoload :B, "` + quiet + `"; end
begin; M::B; rescue NameError; end
p M.autoload?(:B), $loaded
begin; M::B; rescue NameError; end
p $loaded
`
	if got, want := eval(t, src2), "nil\n1\n1\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestLoadedFeaturesDrivesRequire covers $LOADED_FEATURES as the authority on
// what has been required: doRequire records each file there, a file removed from
// the list is loaded again, and a require whose file raises is not recorded at
// all.
func TestLoadedFeaturesDrivesRequire(t *testing.T) {
	once := writeRB(t, "once.rb", "$n = ($n || 0) + 1\n")
	src := `require "` + once + `"
p $n, $".include?("` + once + `")
p require("` + once + `")
p $n
$".delete("` + once + `")
p require("` + once + `")
p $n
`
	if got, want := eval(t, src), "1\ntrue\nfalse\n1\ntrue\n2\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}

	raiser := writeRB(t, "raiser.rb", "$r = ($r || 0) + 1\nraise 'nope'\n")
	src2 := `begin; require "` + raiser + `"; rescue RuntimeError; end
p $".include?("` + raiser + `"), $r
begin; require "` + raiser + `"; rescue RuntimeError; end
p $r
`
	if got, want := eval(t, src2), "false\n1\n2\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestFeatureBookkeepingEdges covers the defensive paths of the $LOADED_FEATURES
// helpers: an unresolvable feature name, and a $LOADED_FEATURES that is not an
// Array (a program may assign anything to a global).
func TestFeatureBookkeepingEdges(t *testing.T) {
	vm := New(os.Stderr)
	if got := vm.featureAbsPath("no_such_feature_zz"); got != "" {
		t.Errorf("featureAbsPath of a missing file = %q, want \"\"", got)
	}
	dir := t.TempDir()
	if got := vm.featureAbsPath(dir); got != "" {
		t.Errorf("featureAbsPath of a directory = %q, want \"\"", got)
	}
	if vm.featureLoaded("no_such_feature_zz") {
		t.Error("featureLoaded of a missing file should be false")
	}

	// A resolvable but unloaded file is not "loaded"; once recorded it is, and
	// recording it twice leaves one entry.
	p := writeRB(t, "feat.rb", "")
	if vm.featureLoaded(p) {
		t.Error("an unrequired file should not be loaded")
	}
	vm.loaded[p] = true
	vm.noteLoadedFeature(p)
	vm.noteLoadedFeature(p)
	arr := vm.globals["$LOADED_FEATURES"].(*object.Array)
	n := 0
	for _, v := range arr.Elems {
		if s, ok := v.(*object.String); ok && s.Str() == p {
			n++
		}
	}
	if n != 1 {
		t.Errorf("$LOADED_FEATURES holds %d copies of the feature, want 1", n)
	}
	if !vm.featureLoaded(p) {
		t.Error("a recorded file should be loaded")
	}
	vm.forgetLoadedFeature(p)
	if vm.loaded[p] || vm.featureDropped(p) {
		t.Error("forgetLoadedFeature should clear both the cache and the list")
	}
	vm.forgetLoadedFeature(p) // already gone: the list walk finds nothing

	// With a non-Array $LOADED_FEATURES nothing is recorded and nothing is
	// considered dropped.
	vm.loaded[p] = true
	vm.globals["$LOADED_FEATURES"] = object.NilV
	vm.noteLoadedFeature(p)
	if vm.featureDropped(p) {
		t.Error("featureDropped needs an Array to decide; want false")
	}
	vm.forgetLoadedFeature(p)
}

// TestScopeVisibilityIsPerEvalBody covers module_eval / class_eval / module_exec
// giving their body a fresh scope visibility, and a bare public/private/protected
// cancelling an earlier module_function toggle.
func TestScopeVisibilityIsPerEvalBody(t *testing.T) {
	src := `m1 = Module.new { private; module_eval { def t1; end }; module_eval " def t2; end " }
p m1.public_instance_methods(false).sort
m2 = Module.new { module_eval { private }; def t1; end }
p m2.public_instance_methods(false)
m3 = Module.new { module_eval { private; def t1; end } }
p m3.private_instance_methods(false)
m4 = Module.new { module_function; module_exec { def t1; end } }
p m4.public_instance_methods(false), m4.singleton_methods
m5 = Module.new { module_function; public; def t1; end }
p m5.public_instance_methods(false), m5.singleton_methods
`
	want := "[:t1, :t2]\n[:t1]\n[:t1]\n[:t1]\n[]\n[:t1]\n[]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestModuleClassPath covers the lazily computed class path: a module bound to a
// constant of an anonymous module reports "#<Module:0x…>::N" and picks up the
// real path once the outer module is itself named.
func TestModuleClassPath(t *testing.T) {
	src := `m = Module.new
m::K = Module.new
p m::K.name =~ /\A#<Module:0x\h+>::K\z/ ? true : m::K.name
Outer = m
p m::K.name
p Module.new.name
`
	if got, want := eval(t, src), "true\n\"Outer::K\"\nnil\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got := moduleBaseName("A::B::C"); got != "C" {
		t.Errorf("moduleBaseName(A::B::C) = %q want \"C\"", got)
	}
	if got := moduleBaseName("C"); got != "C" {
		t.Errorf("moduleBaseName(C) = %q want \"C\"", got)
	}

	vm := New(os.Stderr)
	if got := vm.qualifiedConstName(nil, "X"); got != "X" {
		t.Errorf("qualifiedConstName(nil) = %q want \"X\"", got)
	}
	if got := vm.qualifiedConstName(vm.cObject, "X"); got != "X" {
		t.Errorf("qualifiedConstName(Object) = %q want \"X\"", got)
	}
	if got := vm.qualifiedConstName(vm.cModule, "X"); got != "Module::X" {
		t.Errorf("qualifiedConstName(Module) = %q want \"Module::X\"", got)
	}
	if got := vm.moduleDescription(vm.cKernel); got != "module 'Kernel'" {
		t.Errorf("moduleDescription(Kernel) = %q", got)
	}
	if got := vm.moduleDescription(vm.cObject); got != "class 'Object'" {
		t.Errorf("moduleDescription(Object) = %q", got)
	}
	// A module whose lexical chain closes into a ring is still answered.
	ring := newClass("R", nil)
	ring.isModule, ring.named = true, true
	ring.lexParent = ring
	if !modulePermanentlyNamed(ring) {
		t.Error("a named module that is its own lexical parent is permanently named")
	}
	// The path walk stops on the same ring rather than qualifying R with itself
	// forever.
	if got := vm.moduleClassPath(ring); got != "R" {
		t.Errorf("moduleClassPath of a self-nested module = %q want \"R\"", got)
	}
}

// TestSetTemporaryName covers the permanence rule, the constant-path refusal and
// the nil form, which makes the module anonymous again and clears the paths of
// the modules nested in it.
func TestSetTemporaryName(t *testing.T) {
	src := `m = Module.new
m::N = Module.new
m.set_temporary_name "m"
p m::N.name
m.set_temporary_name nil
p m::N.name, m.name
m2 = Module.new
m2.set_temporary_name "a::B"
p m2.name
m2.set_temporary_name "A::B::"
p m2.name
m2.set_temporary_name "A::::B"
p m2.name
m2.set_temporary_name "A="
p m2.name
`
	want := "\"m::N\"\nnil\nnil\n\"a::B\"\n\"A::B::\"\n\"A::::B\"\n\"A=\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}

	for _, tc := range []struct{ src, class, msg string }{
		{`Object.set_temporary_name "x"`, "RuntimeError", "can't change permanent name"},
		{`Module.new.set_temporary_name ""`, "ArgumentError", "empty class/module name"},
		{`Module.new.set_temporary_name "A::B"`, "ArgumentError",
			"the temporary name must not be a constant path to avoid confusion"},
		{`Module.new.set_temporary_name "::A"`, "ArgumentError",
			"the temporary name must not be a constant path to avoid confusion"},
	} {
		class, msg := evalErr(t, tc.src)
		if class != tc.class || msg != tc.msg {
			t.Errorf("%s: got %s: %q, want %s: %q", tc.src, class, msg, tc.class, tc.msg)
		}
	}

	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"", false}, {"A", true}, {"A::B", true}, {"::A", true}, {"::A::B", true},
		{"a::B", false}, {"A::b", false}, {"A::B::", false}, {"A::::B", false},
		{"A=", false}, {"name", false}, {"Template['foo.rb']", false},
	} {
		if got := isConstantPath(tc.in); got != tc.want {
			t.Errorf("isConstantPath(%q) = %v want %v", tc.in, got, tc.want)
		}
	}
}

// TestClearNestedClassPathsStopsAtARing covers the recursion guard: a module
// nested in itself must not send clearNestedClassPaths into a loop.
func TestClearNestedClassPathsStopsAtARing(t *testing.T) {
	outer := newClass("outer", nil)
	outer.isModule, outer.named = true, false
	inner := newClass("Inner", nil)
	inner.isModule, inner.named, inner.lexParent = true, true, outer
	// Ring: outer holds inner, and inner holds outer back as a constant whose
	// lexical home is inner — the walk must stop rather than recurse forever.
	outer.lexParent = inner
	outer.consts = map[string]object.Value{"Inner": inner, "Num": object.IntValue(1)}
	inner.consts = map[string]object.Value{"Outer": outer}
	clearNestedClassPaths(outer, map[*RClass]bool{})
	if inner.named || inner.name != "" {
		t.Errorf("nested module not cleared: name=%q named=%v", inner.name, inner.named)
	}
}

// TestVisibilityOfAnInheritedMethodAddsAnEntry covers rb_export_method's two
// rules: a module falls back to Object when its own ancestors do not carry the
// method, and re-declaring an inherited method's visibility adds an entry to the
// receiver — which the own-only listings report and Module#method_added sees.
func TestVisibilityOfAnInheritedMethodAddsAnEntry(t *testing.T) {
	src := `class Object; def w24_pub; end; end
m = Module.new { private :w24_pub }
p m.private_instance_methods(false)
p Object.private_instance_methods(true).include?(:w24_pub)
class A; def foo; end; end
class B < A
  def self.method_added(n); (@added ||= []) << n; end
  private :foo
  private :foo
  def self.added; @added; end
end
p B.private_instance_methods(false), B.added, B.instance_methods(false)
class C < A; protected :foo; end
p C.protected_instance_methods(false), C.public_instance_methods(false)
`
	want := "[:w24_pub]\nfalse\n[:foo]\n[:foo]\n[]\n[:foo]\n[]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}

	for _, tc := range []struct{ src, msg string }{
		{`Module.new { private :no_such_w24 }`, "undefined method 'no_such_w24' for module '#<Module:"},
		{`Class.new { private :no_such_w24 }`, "undefined method 'no_such_w24' for class '#<Class:"},
		{`class W24; class << self; private :no_such_w24; end; end`,
			"undefined method 'no_such_w24' for class 'W24'"},
	} {
		class, msg := evalErr(t, tc.src)
		if class != "NameError" || !strings.HasPrefix(msg, tc.msg) {
			t.Errorf("%s: got %s: %q, want NameError starting %q", tc.src, class, msg, tc.msg)
		}
	}
}

// TestAutoloadPathOfALoadedFeature covers autoload? reporting nil once the file
// it names has been loaded: MRI's check_autoload_required consults
// rb_feature_provided, so an autoload whose feature is already provided is
// settled even though the constant it named was never defined.
func TestAutoloadPathOfALoadedFeature(t *testing.T) {
	f := writeRB(t, "silent.rb", "$w24 = 1\n")
	src := `module M; autoload :A, "` + f + `"; end
p M.autoload?(:A).nil?
require "` + f + `"
p M.autoload?(:A), $w24
`
	if got, want := eval(t, src), "false\nnil\n1\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestClassPathOfADeeplyNestedModule covers the middle of the lexical walk: a
// module three scopes deep takes its qualification from each enclosing scope in
// turn, and only the outermost contributes its whole name.
func TestClassPathOfADeeplyNestedModule(t *testing.T) {
	src := `module A; module B; module C; end; end; end
p A::B::C.name, A::B::C.to_s
m = Module.new
module m::Q; end
p m::Q.name
`
	if got, want := eval(t, src), "\"A::B::C\"\n\"A::B::C\"\n\"Q\"\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestUndefinedInstanceMethods covers Module#undefined_instance_methods: the
// receiver's own undef entries only, never an ancestor's.
func TestUndefinedInstanceMethods(t *testing.T) {
	src := `class P; def a; end; def b; end; undef_method :a; end
class C < P; undef_method :b; end
module M; def m1; end; end
class D < C; include M; undef_method :m1; end
p P.undefined_instance_methods, C.undefined_instance_methods
p D.undefined_instance_methods, M.undefined_instance_methods
`
	if got, want := eval(t, src), "[:a]\n[:b]\n[:m1]\n[]\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestUsedRefinements covers Module.used_refinements reading the CALLER's scope:
// the refinements imported there, and an empty list where none are.
func TestUsedRefinements(t *testing.T) {
	src := `module R; refine(String) { def w24; end }; end
p Module.used_refinements
module Uses
  using R
  p Module.used_refinements.size
  p Module.used_refinements.first.class
end
`
	if got, want := eval(t, src), "[]\n1\nRefinement\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestDeprecatedAndPrivateConstantAcceptAPendingAutoload covers the constant
// directives treating a reserved autoload entry as a defined constant.
func TestDeprecatedAndPrivateConstantAcceptAPendingAutoload(t *testing.T) {
	src := `module M
  autoload :A, "a.rb"
  private_constant :A
  public_constant :A
  deprecate_constant :A
end
p M.autoload?(:A)
`
	if got, want := eval(t, src), "\"a.rb\"\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	class, msg := evalErr(t, `module M; private_constant :Nope; end`)
	if class != "NameError" || msg != "constant M::Nope not defined" {
		t.Errorf("got %s: %q", class, msg)
	}
	class, msg = evalErr(t, `module M; deprecate_constant :Nope; end`)
	if class != "NameError" || msg != "constant M::Nope not defined" {
		t.Errorf("got %s: %q", class, msg)
	}
}

// TestThreadBacktrace covers Thread#backtrace and #backtrace_locations: the
// synthetic first frame, the start/length/Range argument forms with their
// ArgumentErrors, the nil-for-overshoot answer, and the nil a dead thread gives.
func TestThreadBacktrace(t *testing.T) {
	src := `def w24
  a = Thread.current.backtrace
  p a.class, a.length >= 2
  p a[1..-1] == Thread.current.backtrace(1)
  p Thread.current.backtrace(100), Thread.current.backtrace(a.length)
  p Thread.current.backtrace(0, nil) == Thread.current.backtrace(0)
  p Thread.current.backtrace(1.1, 1.1) == Thread.current.backtrace(1, 1)
  p Thread.current.backtrace(0..-1).length == a.length
  p Thread.current.backtrace(100..-1)
  l = Thread.current.backtrace_locations
  p l.first.class
  p l[1..-1].map(&:to_s) == caller_locations(0..-1).map(&:to_s)
end
w24
t = Thread.new {}
t.join
p t.backtrace, t.backtrace_locations
live = Thread.new { sleep }
p live.backtrace.class
live.kill
`
	want := "Array\ntrue\ntrue\nnil\n[]\ntrue\ntrue\ntrue\nnil\n" +
		"Thread::Backtrace::Location\ntrue\nnil\nnil\nArray\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}

	for _, tc := range []struct{ src, msg string }{
		{`Thread.current.backtrace(-1)`, "negative level (-1)"},
		{`Thread.current.backtrace(0, -1)`, "negative size (-1)"},
		{`Thread.current.backtrace_locations(-1)`, "negative level (-1)"},
	} {
		class, msg := evalErr(t, tc.src)
		if class != "ArgumentError" || msg != tc.msg {
			t.Errorf("%s: got %s: %q, want ArgumentError: %q", tc.src, class, msg, tc.msg)
		}
	}
}
