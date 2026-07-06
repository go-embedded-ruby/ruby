// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	zeitwerk "github.com/go-ruby-zeitwerk/zeitwerk"
)

// zwTree writes files (relative path -> contents) under a fresh temp directory
// and returns the directory. Intermediate directories are created as needed, so a
// key like "admin/user.rb" produces the admin/ subtree. The tree is removed when
// the test ends (t.TempDir).
func zwTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	return root
}

// zwCatch runs fn and returns the RubyError it panics with, failing if it does not
// panic (used to assert a raise from a native called directly, off the eval path).
func zwCatch(t *testing.T, fn func()) RubyError {
	t.Helper()
	var re RubyError
	caught := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				var ok bool
				re, ok = r.(RubyError)
				if !ok {
					t.Fatalf("panic was not a RubyError: %#v", r)
				}
				caught = true
			}
		}()
		fn()
	}()
	if !caught {
		t.Fatal("expected a RubyError panic, got none")
	}
	return re
}

// TestZeitwerkErrorTree covers the require "zeitwerk" feature probe and the error
// hierarchy mirroring the gem.
func TestZeitwerkErrorTree(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "zeitwerk"`, "true\n"},
		{`require "zeitwerk"; p require "zeitwerk"`, "false\n"},
		{`require "zeitwerk"; p Zeitwerk::Error < StandardError`, "true\n"},
		{`require "zeitwerk"; p Zeitwerk::SetupRequired < Zeitwerk::Error`, "true\n"},
		{`require "zeitwerk"; p Zeitwerk::NameError < NameError`, "true\n"},
		{`require "zeitwerk"; p Zeitwerk::Loader.new.is_a?(Zeitwerk::Loader)`, "true\n"},
		{`require "zeitwerk"; p Zeitwerk::Loader.new.class`, "Zeitwerk::Loader\n"},
		{`require "zeitwerk"; p Zeitwerk::Inflector.new.class`, "Zeitwerk::Inflector\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestZeitwerkAutoloadAndEager drives the core flow: setup wires an autoload for a
// top-level file (referencing the constant requires it lazily), and eager_load
// requires every managed file up front.
func TestZeitwerkAutoloadAndEager(t *testing.T) {
	dir := zwTree(t, map[string]string{
		"user.rb": "class User; def self.hi; 'hi'; end; end\n",
	})
	src := `require "zeitwerk"
loader = Zeitwerk::Loader.new
loader.push_dir(` + q(dir) + `)
loader.setup
p User.hi
loader.eager_load
p defined?(User)`
	if got := eval(t, src); got != "\"hi\"\n\"constant\"\n" {
		t.Errorf("got %q", got)
	}
}

// TestZeitwerkNamespaceDir covers a directory that maps to an implicit namespace
// module (autovivified at setup) plus a nested file autoloaded under it.
func TestZeitwerkNamespaceDir(t *testing.T) {
	dir := zwTree(t, map[string]string{
		"admin/dashboard.rb": "module Admin; class Dashboard; def self.ok; 'ok'; end; end; end\n",
	})
	src := `require "zeitwerk"
loader = Zeitwerk::Loader.new
loader.push_dir(` + q(dir) + `)
loader.setup
p Admin.class
p Admin::Dashboard.ok`
	if got := eval(t, src); got != "Module\n\"ok\"\n" {
		t.Errorf("got %q", got)
	}
}

// TestZeitwerkNamespaceKwargs covers push_dir's namespace: keyword in each accepted
// form (Symbol, multi-segment String, a real Module, and nil for the top level),
// exercising zeitwerkNamespace and — for the String form — the resolveParent /
// autovivify path that creates intermediate namespaces that have no managed dir.
func TestZeitwerkNamespaceKwargs(t *testing.T) {
	// Symbol namespace.
	sym := zwTree(t, map[string]string{"user.rb": "module Api; class User; def self.k; 1; end; end; end\n"})
	if got := eval(t, `require "zeitwerk"
l = Zeitwerk::Loader.new
l.push_dir(`+q(sym)+`, namespace: :Api)
l.setup
p Api::User.k`); got != "1\n" {
		t.Errorf("symbol namespace: %q", got)
	}

	// Multi-segment String namespace: Api and Api::V1 have no managed dir, so
	// resolveParent autovivifies both before attaching Api::V1::User.
	str := zwTree(t, map[string]string{"user.rb": "module Api; module V1; class User; def self.k; 2; end; end; end; end\n"})
	if got := eval(t, `require "zeitwerk"
l = Zeitwerk::Loader.new
l.push_dir(`+q(str)+`, namespace: "Api::V1")
l.setup
p Api::V1::User.k
p Api::V1.class`); got != "2\nModule\n" {
		t.Errorf("string namespace: %q", got)
	}

	// A real Module as namespace.
	mod := zwTree(t, map[string]string{"user.rb": "module Store; class User; def self.k; 3; end; end; end\n"})
	if got := eval(t, `require "zeitwerk"
module Store; end
l = Zeitwerk::Loader.new
l.push_dir(`+q(mod)+`, namespace: Store)
l.setup
p Store::User.k`); got != "3\n" {
		t.Errorf("module namespace: %q", got)
	}

	// namespace: nil is the top level, and a bare push_dir (no kwargs) too.
	top := zwTree(t, map[string]string{"thing.rb": "class Thing; def self.k; 4; end; end\n"})
	if got := eval(t, `require "zeitwerk"
l = Zeitwerk::Loader.new
l.push_dir(`+q(top)+`, namespace: nil)
l.push_dir(`+q(top)+`, ignored_kw: true)
l.setup
p Thing.k`); got != "4\n" {
		t.Errorf("nil namespace: %q", got)
	}
}

// TestZeitwerkExplicitNamespaceFile covers the resolveParent tryAutoload branch:
// a directory admin/ whose namespace is defined by an admin.rb file, so setting up
// the child forces the namespace file to load first.
func TestZeitwerkExplicitNamespaceFile(t *testing.T) {
	dir := zwTree(t, map[string]string{
		"admin.rb":      "module Admin; def self.here; 'ns'; end; end\n",
		"admin/user.rb": "module Admin; class User; def self.k; 'u'; end; end; end\n",
	})
	src := `require "zeitwerk"
l = Zeitwerk::Loader.new
l.push_dir(` + q(dir) + `)
l.setup
p Admin::User.k
p Admin.here`
	if got := eval(t, src); got != "\"u\"\n\"ns\"\n" {
		t.Errorf("got %q", got)
	}
}

// TestZeitwerkNamespaceFileMissingConst covers the resolveParent branch where the
// pending namespace autoload runs but does NOT define the namespace, so it is
// autovivified instead: admin.rb is empty yet admin/user.rb still resolves.
func TestZeitwerkNamespaceFileMissingConst(t *testing.T) {
	dir := zwTree(t, map[string]string{
		"admin.rb":      "# empty: does not define Admin\n",
		"admin/user.rb": "module Admin; class User; def self.k; 'ok'; end; end; end\n",
	})
	src := `require "zeitwerk"
l = Zeitwerk::Loader.new
l.push_dir(` + q(dir) + `)
l.setup
p Admin.class
p Admin::User.k`
	if got := eval(t, src); got != "Module\n\"ok\"\n" {
		t.Errorf("got %q", got)
	}
}

// TestZeitwerkTwoLoadersSharedNamespace covers the zeitwerkDefineAutoload branch
// where a managed directory maps to a namespace already defined as a Module: two
// loaders each own a common/ directory, so the second loader's setup finds Common
// already autovivified by the first.
func TestZeitwerkTwoLoadersSharedNamespace(t *testing.T) {
	root1 := zwTree(t, map[string]string{"common/a.rb": "module Common; class A; def self.k; 'a'; end; end; end\n"})
	root2 := zwTree(t, map[string]string{"common/b.rb": "module Common; class B; def self.k; 'b'; end; end; end\n"})
	src := `require "zeitwerk"
l1 = Zeitwerk::Loader.new
l1.push_dir(` + q(root1) + `)
l1.setup
l2 = Zeitwerk::Loader.new
l2.push_dir(` + q(root2) + `)
l2.setup
p Common::A.k
p Common::B.k`
	if got := eval(t, src); got != "\"a\"\n\"b\"\n" {
		t.Errorf("got %q", got)
	}
}

// TestZeitwerkCallbacks covers on_setup / on_load / on_unload, including the block
// firing, the :ANY default vs a specific cpath filter, and the block-less no-ops.
func TestZeitwerkCallbacks(t *testing.T) {
	dir := zwTree(t, map[string]string{
		"user.rb": "class User; end\n",
		"post.rb": "class Post; end\n",
	})

	// on_setup fires after setup; on_load(:ANY) fires for each constant at eager_load.
	src := `require "zeitwerk"
$setup = 0
$loaded = []
l = Zeitwerk::Loader.new
l.push_dir(` + q(dir) + `)
l.on_setup { $setup += 1 }
l.on_load { |cpath, abspath| $loaded << cpath }
l.on_load                      # block-less: no-op
l.on_setup                     # block-less: no-op
l.setup
l.eager_load
p $setup
p $loaded.sort`
	if got := eval(t, src); got != "1\n[\"Post\", \"User\"]\n" {
		t.Errorf("on_setup/on_load: %q", got)
	}

	// on_load(:User) (a Symbol target) fires only for User.
	src = `require "zeitwerk"
$hit = []
l = Zeitwerk::Loader.new
l.push_dir(` + q(dir) + `)
l.on_load(:User) { |cpath, abspath| $hit << cpath }
l.setup
l.eager_load
p $hit`
	if got := eval(t, src); got != "[\"User\"]\n" {
		t.Errorf("on_load(cpath): %q", got)
	}

	// on_unload(:ANY) sees every constant; a specific target filters (match/no-match).
	src = `require "zeitwerk"
$un = []
$only = []
l = Zeitwerk::Loader.new
l.push_dir(` + q(dir) + `)
l.on_unload { |cpath, abspath| $un << cpath }
l.on_unload("User") { |cpath, abspath| $only << cpath }
l.on_unload                    # block-less: no-op
l.setup
l.unload
p $un.sort
p $only`
	if got := eval(t, src); got != "[\"Post\", \"User\"]\n[\"User\"]\n" {
		t.Errorf("on_unload: %q", got)
	}
}

// TestZeitwerkUnloadRemovesConsts covers unload clearing managed constants of a
// nested tree — exercising zeitwerkRemoveConst's normal-delete, autoload-delete,
// and already-removed-parent arms — after which the constants no longer resolve.
func TestZeitwerkUnloadRemovesConsts(t *testing.T) {
	dir := zwTree(t, map[string]string{
		"user.rb":       "class User; end\n",
		"admin/role.rb": "module Admin; class Role; end; end\n",
	})
	src := `require "zeitwerk"
l = Zeitwerk::Loader.new
l.push_dir(` + q(dir) + `)
l.setup
p User
p Admin::Role
l.unload
r = []
begin; User; rescue NameError; r << "user_gone"; end
begin; Admin::Role; rescue NameError; r << "role_gone"; end
p r`
	if got := eval(t, src); got != "User\nAdmin::Role\n[\"user_gone\", \"role_gone\"]\n" {
		t.Errorf("got %q", got)
	}
}

// TestZeitwerkReload covers reload (enable_reloading + setup, then reload re-runs
// unload+setup) so a constant still resolves afterwards.
func TestZeitwerkReload(t *testing.T) {
	dir := zwTree(t, map[string]string{
		"user.rb":       "class User; def self.k; 'u'; end; end\n",
		"admin/role.rb": "module Admin; class Role; def self.k; 'r'; end; end; end\n",
	})
	src := `require "zeitwerk"
l = Zeitwerk::Loader.new
l.enable_reloading
l.push_dir(` + q(dir) + `)
l.setup
l.reload
p User.k
p Admin::Role.k`
	if got := eval(t, src); got != "\"u\"\n\"r\"\n" {
		t.Errorf("got %q", got)
	}
}

// TestZeitwerkIgnoreCollapse covers ignore (excluded file) and collapse (a
// directory promoted into its parent namespace).
func TestZeitwerkIgnoreCollapse(t *testing.T) {
	dir := zwTree(t, map[string]string{
		"user.rb":        "class User; def self.k; 'u'; end; end\n",
		"skip.rb":        "class Skip; end\n",
		"concerns/hi.rb": "class Hi; def self.k; 'h'; end; end\n",
	})
	src := `require "zeitwerk"
l = Zeitwerk::Loader.new
l.push_dir(` + q(dir) + `)
l.ignore(` + q(filepath.Join(dir, "skip.rb")) + `)
l.collapse(` + q(filepath.Join(dir, "concerns")) + `)
l.setup
p User.k
p Hi.k
p defined?(Skip) ? true : false`
	if got := eval(t, src); got != "\"u\"\n\"h\"\nfalse\n" {
		t.Errorf("got %q", got)
	}
}

// TestZeitwerkInflector covers Zeitwerk::Inflector#camelize (default rule + acronym
// override, and the 2-arg form) and the loader's inflector affecting the scan.
func TestZeitwerkInflector(t *testing.T) {
	if got := eval(t, `require "zeitwerk"
inf = Zeitwerk::Inflector.new
p inf.camelize("html_parser")
inf.inflect("html_parser" => "HTMLParser")
p inf.camelize("html_parser")
p inf.camelize("user", "/abs/user.rb")`); got != "\"HtmlParser\"\n\"HTMLParser\"\n\"User\"\n" {
		t.Errorf("inflector: %q", got)
	}

	dir := zwTree(t, map[string]string{"html_parser.rb": "class HTMLParser; def self.k; 'p'; end; end\n"})
	if got := eval(t, `require "zeitwerk"
l = Zeitwerk::Loader.new
l.inflector.inflect("html_parser" => "HTMLParser")
l.push_dir(`+q(dir)+`)
l.setup
p HTMLParser.k`); got != "\"p\"\n" {
		t.Errorf("loader inflector: %q", got)
	}
}

// TestZeitwerkForGem covers Loader.for_gem: from the eval path the caller file is
// unknown, so it roots at the current directory and returns a usable Loader.
func TestZeitwerkForGem(t *testing.T) {
	if got := eval(t, `require "zeitwerk"; p Zeitwerk::Loader.for_gem.is_a?(Zeitwerk::Loader)`); got != "true\n" {
		t.Errorf("for_gem: %q", got)
	}
}

// TestZeitwerkForGemWithCaller covers for_gem's caller-file branch (it ignores the
// entry file and roots at its directory) by running it with a file on the stack.
func TestZeitwerkForGemWithCaller(t *testing.T) {
	vm := New(io.Discard)
	dir := t.TempDir()
	entry := filepath.Join(dir, "mygem.rb")
	if err := os.WriteFile(entry, []byte("# gem entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vm.fileStack = append(vm.fileStack, entry)
	c := vm.consts["Zeitwerk::Loader"].(*RClass)
	got := c.smethods["for_gem"].native(vm, c, nil, nil)
	z, ok := got.(*ZeitwerkLoader)
	if !ok {
		t.Fatalf("for_gem returned %T", got)
	}
	if err := z.l.Setup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

// TestZeitwerkForGemBadDir covers for_gem's PushDir-error arm: a caller file whose
// directory does not exist raises Zeitwerk::Error.
func TestZeitwerkForGemBadDir(t *testing.T) {
	vm := New(io.Discard)
	vm.fileStack = append(vm.fileStack, filepath.Join(t.TempDir(), "gone", "entry.rb"))
	c := vm.consts["Zeitwerk::Loader"].(*RClass)
	re := zwCatch(t, func() { c.smethods["for_gem"].native(vm, c, nil, nil) })
	if re.Class != "Zeitwerk::Error" {
		t.Errorf("class = %q, want Zeitwerk::Error", re.Class)
	}
}

// TestZeitwerkErrors covers the raising arms: lifecycle-order errors, a missing
// root, and the argument/type checks on push_dir / camelize / inflect / on_load.
func TestZeitwerkErrors(t *testing.T) {
	dir := zwTree(t, map[string]string{"user.rb": "class User; end\n"})
	cases := []struct{ src, class string }{
		// eager_load / reload before setup.
		{`require "zeitwerk"; Zeitwerk::Loader.new.eager_load`, "Zeitwerk::SetupRequired"},
		{`require "zeitwerk"
l = Zeitwerk::Loader.new
l.enable_reloading
l.reload`, "Zeitwerk::SetupRequired"},
		// reload without enable_reloading, after setup.
		{`require "zeitwerk"
l = Zeitwerk::Loader.new
l.push_dir(` + q(dir) + `)
l.setup
l.reload`, "Zeitwerk::Error"},
		// push_dir on a missing root.
		{`require "zeitwerk"; Zeitwerk::Loader.new.push_dir("/no/such/dir/here")`, "Zeitwerk::Error"},
		// push_dir with no argument.
		{`require "zeitwerk"; Zeitwerk::Loader.new.push_dir`, "ArgumentError"},
		// bad namespace type.
		{`require "zeitwerk"; Zeitwerk::Loader.new.push_dir(` + q(dir) + `, namespace: 123)`, "TypeError"},
		// camelize / inflect argument and type checks.
		{`require "zeitwerk"; Zeitwerk::Inflector.new.camelize`, "ArgumentError"},
		{`require "zeitwerk"; Zeitwerk::Inflector.new.inflect`, "ArgumentError"},
		{`require "zeitwerk"; Zeitwerk::Inflector.new.inflect(42)`, "TypeError"},
		// on_load target must be a String/Symbol.
		{`require "zeitwerk"
l = Zeitwerk::Loader.new
l.on_load(123) { }`, "TypeError"},
	}
	for _, c := range cases {
		if class, _ := evalErr(t, c.src); class != c.class {
			t.Errorf("src=%q class=%q want=%q", c.src, class, c.class)
		}
	}
}

// TestZeitwerkSetupError covers the #setup raising arm: the engine's Setup returns
// an error when a pushed root disappears before setup runs, surfaced as
// Zeitwerk::Error.
func TestZeitwerkSetupError(t *testing.T) {
	vm := New(io.Discard)
	z := vm.newZeitwerkLoader()
	dir := t.TempDir()
	if err := z.l.PushDir(dir, ""); err != nil {
		t.Fatalf("push_dir: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	c := vm.consts["Zeitwerk::Loader"].(*RClass)
	re := zwCatch(t, func() { c.methods["setup"].native(vm, z, nil, nil) })
	if re.Class != "Zeitwerk::Error" {
		t.Errorf("class = %q, want Zeitwerk::Error", re.Class)
	}
}

// TestZeitwerkRemoveConstNested covers zeitwerkRemoveConst descending into a
// still-present parent to remove a nested constant, and its no-op arm when the
// namespace is already gone. (During a full unload the parent is always removed
// first, so the descend is reached only from this direct call.)
func TestZeitwerkRemoveConstNested(t *testing.T) {
	vm := New(io.Discard)
	admin := vm.zeitwerkAutovivify(vm.cObject, "Admin")
	vm.zeitwerkAutovivify(admin, "Role")

	vm.zeitwerkRemoveConst("Admin::Role")
	if _, ok := admin.consts["Role"]; ok {
		t.Error("Admin::Role should have been removed")
	}
	if _, ok := vm.cObject.consts["Admin"]; !ok {
		t.Error("Admin should remain")
	}
	if _, ok := vm.consts["Admin::Role"]; ok {
		t.Error("flat Admin::Role should have been removed")
	}
	// A cpath whose namespace is absent is a no-op (must not panic).
	vm.zeitwerkRemoveConst("Nope::Missing")
}

// TestZeitwerkWrapperProtocol covers the value-protocol methods (ToS / Inspect /
// Truthy) on both wrappers, whose Ruby surface routes through explicit methods.
func TestZeitwerkWrapperProtocol(t *testing.T) {
	l := &ZeitwerkLoader{l: zeitwerk.NewLoader()}
	if l.ToS() != "#<Zeitwerk::Loader>" || l.Inspect() != "#<Zeitwerk::Loader>" || !l.Truthy() {
		t.Errorf("loader protocol: %q %q %v", l.ToS(), l.Inspect(), l.Truthy())
	}
	in := &ZeitwerkInflector{in: zeitwerk.NewInflector()}
	if in.ToS() != "#<Zeitwerk::Inflector>" || in.Inspect() != "#<Zeitwerk::Inflector>" || !in.Truthy() {
		t.Errorf("inflector protocol: %q %q %v", in.ToS(), in.Inspect(), in.Truthy())
	}
}

// q renders s as a double-quoted Ruby string literal for embedding a filesystem
// path into a test script (temp paths have no quotes/backslashes to escape).
func q(s string) string { return "\"" + s + "\"" }
