// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReopenLexParentAfterCompactDefine covers the lexParent upgrade: a module
// first created via a compact path (module A::B, whose nesting is only itself)
// and later reopened nested (module A; module B) must, in the nested body,
// resolve a bare constant defined in A. This is the constant-resolution bug that
// blocked Puppet's Pops (Types referencing Visitor). Verified against MRI 4.0.5.
func TestReopenLexParentAfterCompactDefine(t *testing.T) {
	src := "module A; end\n" +
		"module A::B; end\n" + // compact define: B.lexParent left nil
		"module A\n" +
		"  WANTED = 99\n" +
		"  module B\n" +
		"    GOT = WANTED\n" + // bare WANTED must resolve up to A
		"  end\n" +
		"end\n" +
		"p A::B::GOT\n"
	if got := eval(t, src); got != "99\n" {
		t.Errorf("got %q want \"99\\n\"", got)
	}

	// The same for a class created compactly then reopened nested.
	src2 := "class P; end\n" +
		"class P::Q; end\n" +
		"class P\n" +
		"  K = 7\n" +
		"  class Q\n" +
		"    V = K\n" +
		"  end\n" +
		"end\n" +
		"p P::Q::V\n"
	if got := eval(t, src2); got != "7\n" {
		t.Errorf("got %q want \"7\\n\"", got)
	}
}

// TestRescueRestoresRequireState covers the per-frame tracking-stack truncation:
// when a deep require raises a LoadError that is rescued in an outer file, a
// require_relative in the rescue body must resolve against the rescuing file, not
// the abandoned deep file. This backed Puppet's gettext fallback-to-stubs path.
func TestRescueRestoresRequireState(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "config.rb", "begin\n  require 'farlib'\nrescue LoadError\n  require_relative 'stubs'\nend\nputs 'STUBBED' if defined?(STUB_OK)\n")
	write(t, dir, "stubs.rb", "STUB_OK = true\n")
	sub := dir + "/gem"
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, sub, "farlib.rb", "require 'deeplib'\n")
	write(t, sub, "deeplib.rb", "require 'nonexistent_xyz_feature'\n")
	out, err := runInDir(t, dir,
		"$LOAD_PATH.unshift \""+sub+"\"\n"+
			"require_relative \"config\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "STUBBED\n" {
		t.Errorf("got %q want \"STUBBED\\n\"", out)
	}
}

// TestAutoloadBasic covers the headline path: a top-level autoload that fires on
// first reference, with autoload? reporting the path before and nil after, all
// verified against MRI 4.0.5 semantics.
func TestAutoloadBasic(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "foo.rb", "Foo = Object.new\ndef Foo.hi; 'hi from foo'; end\n")
	src := "autoload :Foo, \"" + dir + "/foo.rb\"\n" +
		"p autoload?(:Foo)\n" +
		"p Foo.hi\n" +
		"p autoload?(:Foo)\n"
	out, err := runInDir(t, dir, src)
	if err != nil {
		t.Fatal(err)
	}
	want := "\"" + dir + "/foo.rb\"\n\"hi from foo\"\nnil\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

// TestAutoloadQueryUndefined: autoload? for a constant with no registration is
// nil, and a String constant name is accepted.
func TestAutoloadQueryUndefined(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "foo.rb", "Foo = 1\n")
	out, err := runInDir(t, dir,
		"p autoload?(:Nope)\n"+
			"autoload :Foo, \""+dir+"/foo.rb\"\n"+
			"p autoload?(\"Foo\")\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "nil\n\"" + dir + "/foo.rb\"\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

// TestAutoloadAlreadyDefined: registering an autoload for an already-defined
// constant is inert — autoload? reports nil and the constant keeps its value.
func TestAutoloadAlreadyDefined(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "foo.rb", "X = 99\n")
	out, err := runInDir(t, dir,
		"X = 1\n"+
			"autoload :X, \""+dir+"/foo.rb\"\n"+
			"p autoload?(:X)\n"+
			"p X\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "nil\n1\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadNestedModule: autoload inside a module registers on that module;
// the bare reference inside resolves it, M::Bar resolves it from outside, and
// autoload? reports nil once defined.
func TestAutoloadNestedModule(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "bar.rb", "module M\n  Bar = 42\nend\n")
	out, err := runInDir(t, dir,
		"module M\n"+
			"  autoload :Bar, \""+dir+"/bar.rb\"\n"+
			"  p autoload?(:Bar)\n"+
			"end\n"+
			"p M::Bar\n"+
			"p M.autoload?(:Bar)\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "\"" + dir + "/bar.rb\"\n42\nnil\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

// TestAutoloadModuleForm: Module#autoload / #autoload? on an explicit receiver,
// and the return value (nil) of both autoload forms.
func TestAutoloadModuleForm(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "bar.rb", "module M\n  Bar = 7\nend\n")
	out, err := runInDir(t, dir,
		"module M; end\n"+
			"p(autoload(:Top, \"x.rb\"))\n"+
			"p(M.autoload(:Bar, \""+dir+"/bar.rb\"))\n"+
			"p M.autoload?(:Bar)\n"+
			"p M::Bar\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "nil\nnil\n\"" + dir + "/bar.rb\"\n7\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

// TestAutoloadDuplicateReplaces: a second autoload of the same constant replaces
// the first path.
func TestAutoloadDuplicateReplaces(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "empty.rb", "z = 1\n")
	write(t, dir, "foo.rb", "Foo = Object.new\ndef Foo.hi; 'real'; end\n")
	out, err := runInDir(t, dir,
		"autoload :Foo, \""+dir+"/empty.rb\"\n"+
			"autoload :Foo, \""+dir+"/foo.rb\"\n"+
			"p autoload?(:Foo)\n"+
			"p Foo.hi\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "\"" + dir + "/foo.rb\"\n\"real\"\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

// TestAutoloadConstGetTriggers / const_defined? does NOT: const_get fires the
// autoload, const_defined? reports true without loading (autoload? still set).
func TestAutoloadConstGetVsDefined(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "foo.rb", "Foo = Object.new\ndef Foo.hi; 'g'; end\n")
	out, err := runInDir(t, dir,
		"autoload :Foo, \""+dir+"/foo.rb\"\n"+
			"p Object.const_defined?(:Foo)\n"+
			"p autoload?(:Foo)\n"+
			"p Object.const_get(:Foo).hi\n"+
			"p autoload?(:Foo)\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "true\n\"" + dir + "/foo.rb\"\n\"g\"\nnil\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

// TestAutoloadDefined: defined?(Const) and defined?(M::Bar) report "constant"
// for a pending autoload WITHOUT triggering the require.
func TestAutoloadDefined(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "foo.rb", "Foo = 1\n")
	write(t, dir, "bar.rb", "module M\n  Bar = 2\nend\n")
	out, err := runInDir(t, dir,
		"autoload :Foo, \""+dir+"/foo.rb\"\n"+
			"p defined?(Foo)\n"+
			"p autoload?(:Foo)\n"+
			"module M; autoload :Bar, \""+dir+"/bar.rb\"; end\n"+
			"p defined?(M::Bar)\n"+
			"p M.autoload?(:Bar)\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "\"constant\"\n\"" + dir + "/foo.rb\"\n\"constant\"\n\"" + dir + "/bar.rb\"\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

// TestAutoloadDefinedFalse: defined?(Const) is nil when neither a constant nor a
// pending autoload exists.
func TestAutoloadDefinedFalse(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	out, err := runInDir(t, dir, "p defined?(Nope)\np defined?(Object::Nope)\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "nil\nnil\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadLexical: an autoload registered at the top level fires when the
// constant is first referenced bare inside a nested method.
func TestAutoloadLexical(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "foo.rb", "Foo = Object.new\ndef Foo.hi; 'lx'; end\n")
	out, err := runInDir(t, dir,
		"autoload :Foo, \""+dir+"/foo.rb\"\n"+
			"module N; def self.go; Foo.hi; end; end\n"+
			"p N.go\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "\"lx\"\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadRequireNoConst: the require runs but does not define the constant —
// MRI raises NameError on the bare reference.
func TestAutoloadRequireNoConst(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "empty.rb", "z = 1\n")
	out, err := runInDir(t, dir,
		"autoload :Zed, \""+dir+"/empty.rb\"\n"+
			"begin\n  Zed\nrescue Exception => e\n  p e.class\n  p e.message\nend\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "NameError\n\"uninitialized constant Zed\"\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadLoadError: a missing autoload file propagates LoadError when the
// constant is referenced.
func TestAutoloadLoadError(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	out, err := runInDir(t, dir,
		"autoload :Q, \""+dir+"/does_not_exist.rb\"\n"+
			"begin; Q; rescue Exception => e; p e.class; end\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "LoadError\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadScopedMissing: M::Bar with no constant and no autoload raises
// NameError (the scopedConst miss after the autoload check).
func TestAutoloadScopedMissing(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	out, err := runInDir(t, dir,
		"module M; end\n"+
			"begin; M::Bar; rescue Exception => e; puts e.class; puts e.message; end\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "NameError") || !strings.Contains(out, "uninitialized constant M::Bar") {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadScopedNoConstAfterRequire: M::Bar where the autoload file runs but
// does not define Bar still raises NameError (the post-autoload re-resolve miss).
func TestAutoloadScopedNoConstAfterRequire(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "empty.rb", "z = 1\n")
	out, err := runInDir(t, dir,
		"module M; autoload :Bar, \""+dir+"/empty.rb\"; end\n"+
			"begin; M::Bar; rescue Exception => e; puts e.class; end\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "NameError") {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadConstNameError: a non-symbol/string name argument raises
// TypeError, and a lowercase name raises NameError (wrong constant name).
func TestAutoloadConstNameError(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	out, err := runInDir(t, dir,
		"begin; autoload(1, \"x\"); rescue Exception => e; puts e.class; end\n"+
			"begin; autoload(:foo, \"x\"); rescue Exception => e; puts e.class; end\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "TypeError\nNameError\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadPathTypeError: a non-String path argument raises TypeError, for
// both the Kernel and Module forms.
func TestAutoloadPathTypeError(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	out, err := runInDir(t, dir,
		"begin; autoload(:Foo, 1); rescue Exception => e; puts e.class; end\n"+
			"module M; end\n"+
			"begin; M.autoload(:Foo, 1); rescue Exception => e; puts e.class; end\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "TypeError\nTypeError\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadModuleQueryAlreadyDefined: Module#autoload? returns nil when the
// constant is already defined directly in the receiver's table.
func TestAutoloadModuleQueryAlreadyDefined(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	out, err := runInDir(t, dir,
		"module M; Bar = 5; end\n"+
			"p M.autoload?(:Bar)\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "nil\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadModuleQueryUndefined: Module#autoload? returns nil for a constant
// that is neither defined nor registered on the receiver.
func TestAutoloadModuleQueryUndefined(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	out, err := runInDir(t, dir,
		"module M; end\n"+
			"p M.autoload?(:Nope)\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "nil\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadBareInModuleMethod: a bare constant reference inside a module's own
// method resolves a pending autoload registered on that module via the lexical
// nesting (not the ancestor chain).
func TestAutoloadBareInModuleMethod(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "barm.rb", "module M\n  Bar = 7\nend\n")
	out, err := runInDir(t, dir,
		"module M\n"+
			"  autoload :Bar, \""+dir+"/barm.rb\"\n"+
			"  def self.go; Bar; end\n"+
			"end\n"+
			"p M.go\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "7\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadScopedUnrelated: M::Other where M holds an autoload for a DIFFERENT
// constant exercises the no-match path (autoloads map present, name absent) and
// the Object/BasicObject skip in the ancestor walk, ending in NameError. defined?
// of the same reference is nil.
func TestAutoloadScopedUnrelated(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	out, err := runInDir(t, dir,
		"module M; autoload :Bar, \""+dir+"/x.rb\"; end\n"+
			"p defined?(M::Other)\n"+
			"begin; M::Other; rescue Exception => e; puts e.class; end\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "nil\nNameError\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadDefinedScopedAncestorSkip: defined?(M::Bar) where M only registers
// the autoload for Bar (not in its own const table) exercises hasScopedConst's
// autoload-in-ancestor scan including the Object/BasicObject skip for a non-Object
// receiver.
func TestAutoloadDefinedScopedAncestorSkip(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	out, err := runInDir(t, dir,
		"class Base; autoload :Bar, \""+dir+"/x.rb\"; end\n"+
			"class Sub < Base; end\n"+
			"p defined?(Sub::Bar)\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "\"constant\"\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadClassScopedUnrelated: C::Other where C is a class holding an
// autoload for a different constant walks C's full ancestor chain (reaching and
// skipping Object/BasicObject) before raising NameError.
func TestAutoloadClassScopedUnrelated(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	out, err := runInDir(t, dir,
		"class C; autoload :Bar, \""+dir+"/x.rb\"; end\n"+
			"begin; C::Other; rescue Exception => e; puts e.class; end\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "NameError\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadDefinedInNesting: defined?(Bar) inside a module method reports
// "constant" via the pending autoload found in the lexical nesting.
func TestAutoloadDefinedInNesting(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	out, err := runInDir(t, dir,
		"module M\n"+
			"  autoload :Bar, \""+dir+"/x.rb\"\n"+
			"  def self.chk; defined?(Bar); end\n"+
			"end\n"+
			"p M.chk\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "\"constant\"\n" {
		t.Errorf("got %q", out)
	}
}

// TestAutoloadConstDefinedSuperchain: const_defined? walks the superclass chain
// for a pending autoload registered on an ancestor.
func TestAutoloadConstDefinedSuperchain(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	write(t, dir, "bar.rb", "class Base\n  Bar = 1\nend\n")
	out, err := runInDir(t, dir,
		"class Base; autoload :Bar, \""+dir+"/bar.rb\"; end\n"+
			"class Sub < Base; end\n"+
			"p Sub.const_defined?(:Bar)\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "true\n" {
		t.Errorf("got %q", out)
	}
}
