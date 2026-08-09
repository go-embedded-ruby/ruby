// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"path/filepath"
	"testing"
)

// TestConstGetBasic covers Module#const_get name forms, scoped paths, the
// toplevel qualifier and the inherit flag — verified against MRI (ruby 4.0.5).
func TestConstGetBasic(t *testing.T) {
	cases := []struct{ src, want string }{
		// String and Symbol names.
		{"module M; X = 1; end\np M.const_get(:X)", "1\n"},
		{"module M; X = 1; end\np M.const_get(\"X\")", "1\n"},
		// Scoped path from Object and from a nested module.
		{"module A; Y = 2; end\np Object.const_get(\"A::Y\")", "2\n"},
		{"module A; module B; Z = 3; end; end\np A.const_get(\"B::Z\")", "3\n"},
		// First segment resolves a toplevel constant even for a module receiver.
		{"CS = :cs\nmodule NS; end\np NS.const_get(:CS)", ":cs\n"},
		{"CS = :cs\nmodule NS; end\np NS.const_get(\"::CS\")", ":cs\n"},
		{"module NS2; INNER = :in; end\nmodule NS; end\np NS.const_get(\"NS2::INNER\")", ":in\n"},
		// A class receiver reaches a toplevel constant through Object (inherit).
		{"CS = 9\nclass K; end\np K.const_get(:CS)", "9\n"},
		// inherit=true searches the superclass chain; inherit=false does not.
		{"class A; Y = 2; end\nclass B < A; end\np B.const_get(:Y)", "2\n"},
		{"class A; Y = 2; end\nclass B < A; end\np B.const_get(:Y, true)", "2\n"},
		// A read-through of a constant defined directly on the receiver, inherit=false.
		{"class B; W = 5; end\np B.const_get(:W, false)", "5\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("const_get %q: got %q want %q", c.src, got, c.want)
		}
	}
}

// TestConstGetErrors covers const_get's error paths: malformed names, the
// inherit=false miss on an inherited/toplevel constant, a Symbol that carries a
// scope path, and scoping into a non-module value.
func TestConstGetErrors(t *testing.T) {
	cases := []struct{ src, class, msg string }{
		{"module M; end\nM.const_get(\"name\")", "NameError", "wrong constant name name"},
		{"module M; end\nM.const_get(\"__X__\")", "NameError", "wrong constant name __X__"},
		{"module M; end\nM.const_get(\"X=\")", "NameError", "wrong constant name X="},
		{"module M; end\nM.const_get(\"A::\")", "NameError", "wrong constant name A::"},
		{"module M; end\nM.const_get(\"::\")", "NameError", "wrong constant name ::"},
		// A Symbol may not be a scope path or carry a toplevel qualifier.
		{"module A; B = 1; end\nA.const_get(:'A::B')", "NameError", "wrong constant name A::B"},
		{"module A; end\nA.const_get(:'::A')", "NameError", "wrong constant name ::A"},
		// inherit=false: inherited / toplevel constants are not found.
		{"class A; Y = 2; end\nclass B < A; end\nB.const_get(:Y, false)", "NameError", "uninitialized constant B::Y"},
		{"CS = 1\nmodule M; end\nM.const_get(:CS, false)", "NameError", "uninitialized constant M::CS"},
		// A genuinely missing constant on the toplevel omits the "Object::" prefix.
		{"Object.const_get(:Nope)", "NameError", "uninitialized constant Nope"},
		{"module M; end\nM.const_get(:Nope)", "NameError", "uninitialized constant M::Nope"},
		// Scoping into a non-module value.
		{"NUM = 5\nObject.const_get(\"NUM::Foo\")", "TypeError", "NUM::Foo does not refer to class/module"},
	}
	for _, c := range cases {
		class, msg := evalErr(t, c.src)
		if class != c.class || msg != c.msg {
			t.Errorf("const_get %q: got %s/%q want %s/%q", c.src, class, msg, c.class, c.msg)
		}
	}
}

// TestConstGetToStrCoercion covers the #to_str conversion of a non-String,
// non-Symbol name and its TypeError paths.
func TestConstGetToStrCoercion(t *testing.T) {
	if got := eval(t, "module M; Bar = 7; end\nclass NL; def to_str; \"Bar\"; end; end\np M.const_get(NL.new)"); got != "7\n" {
		t.Errorf("to_str coercion: got %q", got)
	}
	// #to_str returning a non-String is a TypeError.
	if class, _ := evalErr(t, "class NL; def to_str; 123; end; end\nObject.const_get(NL.new)"); class != "TypeError" {
		t.Errorf("non-String to_str: got %s want TypeError", class)
	}
	// An object without #to_str is a TypeError.
	if class, msg := evalErr(t, "Object.const_get(Object.new)"); class != "TypeError" || msg == "" {
		t.Errorf("no to_str: got %s/%q want TypeError", class, msg)
	}
}

// TestConstGetAutoload covers const_get triggering a pending autoload while
// resolving the first segment (the inherit autoload branch of constSegGet).
func TestConstGetAutoload(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "zzz.rb", "Zzz = 42\n")
	src := "autoload :Zzz, \"" + dir + "/zzz.rb\"\n" +
		"p Object.const_get(:Zzz)\n"
	out, err := runInDir(t, dir, src)
	if err != nil {
		t.Fatal(err)
	}
	if out != "42\n" {
		t.Errorf("const_get autoload: got %q want %q", out, "42\n")
	}
}

// TestConstMissingHook covers the #const_missing hook firing for both the
// scope-resolution operator (Recv::Name) and const_get, including a private
// override, and the default hook's NameError (which carries #name).
func TestConstMissingHook(t *testing.T) {
	// Override fires for the literal `::` form.
	if got := eval(t, "class D; def self.const_missing(n); \"missing #{n}\"; end; end\np D::Nope"); got != "\"missing Nope\"\n" {
		t.Errorf("const_missing literal: got %q", got)
	}
	// Override fires for const_get, receiving a Symbol.
	if got := eval(t, "class D; def self.const_missing(n); n; end; end\np D.const_get(:Zap)"); got != ":Zap\n" {
		t.Errorf("const_missing via const_get: got %q", got)
	}
	// A private override is still called.
	if got := eval(t, "k = Class.new do\n  def self.const_missing(n); \"Found:#{n}\"; end\n  private_class_method :const_missing\nend\np k::Hello"); got != "\"Found:Hello\"\n" {
		t.Errorf("private const_missing: got %q", got)
	}
	// The default hook on an anonymous module still raises NameError.
	if class, _ := evalErr(t, "Module.new.const_get(:Nope)"); class != "NameError" {
		t.Errorf("anonymous const_missing: got %s want NameError", class)
	}
	// The raised NameError carries the offending name.
	if got := eval(t, "begin; Object.const_get(:Nope); rescue NameError => e; p e.name; end"); got != ":Nope\n" {
		t.Errorf("NameError#name: got %q want :Nope", got)
	}
	// Calling the default const_missing directly raises, Object omitting its prefix.
	if _, msg := evalErr(t, "Object.const_missing(:Zap)"); msg != "uninitialized constant Zap" {
		t.Errorf("Object.const_missing message: got %q", msg)
	}
	if _, msg := evalErr(t, "module M; end\nM.const_missing(:Zap)"); msg != "uninitialized constant M::Zap" {
		t.Errorf("Module.const_missing message: got %q", msg)
	}
}

// TestConstDefined covers Module#const_defined? name forms, scoped paths, the
// toplevel qualifier, the inherit flag and its error paths — against MRI.
func TestConstDefined(t *testing.T) {
	truthy := []string{
		"module A; Y = 2; end\np Object.const_defined?(\"A::Y\")",
		"module A; Y = 2; end\np A.const_defined?(:Y)",
		"module A; Y = 2; end\np A.const_defined?(\"Y\")",
		"CS = 1\nmodule M; end\np M.const_defined?(:CS)",                // toplevel fallback for a module
		"class A; Y = 2; end\nclass B < A; end\np B.const_defined?(:Y)", // inherited via superclass
		"p Object.const_defined?(\"::Object\")",                         // toplevel qualifier
		"module A; B = 1; end\np A.const_defined?(\"B\")",
	}
	for _, s := range truthy {
		if got := eval(t, s); got != "true\n" {
			t.Errorf("const_defined? %q: got %q want true", s, got)
		}
	}
	falsy := []string{
		"module M; end\np M.const_defined?(:Nope)",
		"module M; end\np M.const_defined?(\"NotExist::Name\")",
		"p Object.const_defined?(\"::NoSuchTop\")",
		"class A; Y = 2; end\nclass B < A; end\np B.const_defined?(:Y, false)", // inherit=false
		"CS = 1\nmodule M; end\np M.const_defined?(:CS, false)",
		"class K; end\np K.const_defined?(:NoSuchName)", // exercises the Object-skip scan
	}
	for _, s := range falsy {
		if got := eval(t, s); got != "false\n" {
			t.Errorf("const_defined? %q: got %q want false", s, got)
		}
	}
	// Malformed names raise NameError; a non-module in a path raises TypeError.
	if class, msg := evalErr(t, "module M; end\nM.const_defined?(\"name\")"); class != "NameError" || msg != "wrong constant name name" {
		t.Errorf("const_defined? bad name: got %s/%q", class, msg)
	}
	if class, _ := evalErr(t, "NUM = 5\nObject.const_defined?(\"NUM::Foo\")"); class != "TypeError" {
		t.Errorf("const_defined? non-module path: got %s want TypeError", class)
	}
}

// TestConstDefinedAutoloadPending covers const_defined? reporting a pending
// autoload as defined WITHOUT triggering the require, across the inherit,
// inherit=false and toplevel-fallback paths.
func TestConstDefinedAutoloadPending(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	// A non-existent path: if const_defined? triggered it, a LoadError would raise.
	miss := "\"" + dir + "/none.rb\""
	cases := []string{
		"module M; autoload :Bar, " + miss + "; end\np M.const_defined?(:Bar)",
		"module M; autoload :Bar, " + miss + "; end\np M.const_defined?(:Bar, false)",
		"autoload :Zt, " + miss + "\nmodule NS; end\np NS.const_defined?(:Zt)",
	}
	for _, s := range cases {
		out, err := runInDir(t, dir, s)
		if err != nil {
			t.Fatalf("const_defined? pending %q: %v", s, err)
		}
		if out != "true\n" {
			t.Errorf("const_defined? pending %q: got %q want true", s, out)
		}
	}
}

// TestIncludedModules covers Module#included_modules: the modules (includes and
// prepends, transitively) in the ancestor chain, excluding the receiver.
func TestIncludedModules(t *testing.T) {
	cases := []struct{ src, want string }{
		{"p String.included_modules", "[Comparable, Kernel]\n"},
		{"module Mb; end\nmodule Ma; include Mb; end\np Ma.included_modules", "[Mb]\n"},
		{"module Ma; end\np Ma.included_modules", "[]\n"},
		{"module Mp; end\nmodule Mi; end\nclass WP; prepend Mp; include Mi; end\np WP.included_modules", "[Mp, Mi, Kernel]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("included_modules %q: got %q want %q", c.src, got, c.want)
		}
	}
}

// TestRemoveClassVariable covers Module#remove_class_variable: the value return
// and removal, the NameError for an absent variable, and the malformed-name
// NameError branches (no @@, single @, no @).
func TestRemoveClassVariable(t *testing.T) {
	if got := eval(t, "class C; @@v = :mvar; end\np C.remove_class_variable(:@@v)"); got != ":mvar\n" {
		t.Errorf("remove_class_variable return: got %q", got)
	}
	if got := eval(t, "class C; @@v = 1; end\nC.remove_class_variable(:@@v)\np C.class_variable_defined?(:@@v)"); got != "false\n" {
		t.Errorf("remove_class_variable removal: got %q", got)
	}
	errs := []struct{ src, msg string }{
		{"class C; end\nC.remove_class_variable(:@@nope)", "class variable @@nope not defined for C"},
	}
	for _, c := range errs {
		if class, msg := evalErr(t, c.src); class != "NameError" || msg != c.msg {
			t.Errorf("remove_class_variable %q: got %s/%q want NameError/%q", c.src, class, msg, c.msg)
		}
	}
	// Malformed class-variable names raise NameError.
	for _, bad := range []string{":@mvar", ":mvar"} {
		if class, _ := evalErr(t, "class C; @@v=1; end\nC.remove_class_variable("+bad+")"); class != "NameError" {
			t.Errorf("remove_class_variable %s: got %s want NameError", bad, class)
		}
	}
}
