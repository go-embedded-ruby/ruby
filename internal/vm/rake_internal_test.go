// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rake "github.com/go-ruby-rake/rake"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// req is the require prelude prepended to every Rake DSL snippet.
const req = "require \"rake\"\n"

// TestRakeFeature covers the require probe and the module/class shape.
func TestRakeFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "rake"`, "true\n"},
		{`require "rake"; p require "rake"`, "false\n"},
		{`p require "rake/dsl_definition"`, "true\n"},
		{`p require "rake/file_list"`, "true\n"},
		{`require "rake"; p Rake.is_a?(Module)`, "true\n"},
		{`require "rake"; p Rake::Task.is_a?(Class)`, "true\n"},
		{`require "rake"; p Rake::FileTask < Rake::Task`, "true\n"},
		{`require "rake"; p Rake::Application.is_a?(Class)`, "true\n"},
		{`require "rake"; p Rake::FileList.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRakeInvokeOrder covers the action-block seam and the depth-first,
// prerequisite-first, invoke-once ordering.
func TestRakeInvokeOrder(t *testing.T) {
	src := req + `
o = []
task :a do o << "a" end
task :b => :a do o << "b" end
task :top => [:b, :a] do o << "top" end
Rake::Task[:top].invoke
Rake::Task[:top].invoke   # once-guard: no re-run
p o`
	if got, want := eval(t, src), "[\"a\", \"b\", \"top\"]\n"; got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestRakeArgs covers task arguments flowing through to the action block as a Hash
// and the arg_names reader (the no-Hash form with a flattened arg-name array).
func TestRakeArgs(t *testing.T) {
	cases := []struct{ src, want string }{
		{req + `o=nil; task :greet, [:name] do |t, args| o = args[:name] end; Rake::Task[:greet].invoke("bob"); p o`, "\"bob\"\n"},
		{req + `task :greet, [:name]; p Rake::Task[:greet].arg_names`, "[:name]\n"},
		{req + `task :m, :a, :b; p Rake::Task[:m].arg_names`, "[:a, :b]\n"},
		{req + `task :t, [:a] => :d; p [Rake::Task[:t].arg_names, Rake::Task[:t].prerequisites]`, "[[:a], [\"d\"]]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRakeDepForms covers the dependency-argument decoding: scalar, array, a
// non-string coerced via #to_s, and an explicit nil dependency.
func TestRakeDepForms(t *testing.T) {
	cases := []struct{ src, want string }{
		{req + `task :a; task :b => :a; p Rake::Task[:b].prerequisites`, "[\"a\"]\n"},
		{req + `task :a => [:x, :y]; p Rake::Task[:a].prerequisites`, "[\"x\", \"y\"]\n"},
		{req + `task :a => 5; p Rake::Task[:a].prerequisites`, "[\"5\"]\n"},
		{req + `task :a => nil; p Rake::Task[:a].prerequisites`, "[]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRakeNamespace covers namespace scope resolution, an anonymous namespace, a
// block-less namespace, and the scope reader.
func TestRakeNamespace(t *testing.T) {
	cases := []struct{ src, want string }{
		{req + `o=[]; namespace :outer do task :inner do o << "inner" end end; Rake::Task["outer:inner"].invoke; p o`, "[\"inner\"]\n"},
		{req + `task :inner; namespace :outer do task :inner end; p Rake::Task["outer:inner"].scope`, "\"outer\"\n"},
		{req + `o=[]; namespace do task :x do o << "x" end end; Rake::Task["_anon_1:x"].invoke; p o`, "[\"x\"]\n"},
		{req + `namespace nil do task :y end; p Rake::Task.task_defined?("_anon_1:y")`, "true\n"},
		{req + `namespace :n; p Rake::Task.task_defined?(:whatever)`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRakeTaskSurface covers the remaining Rake::Task instance methods.
func TestRakeTaskSurface(t *testing.T) {
	cases := []struct{ src, want string }{
		{req + `task :a; t=Rake::Task[:a]; p [t.name, t.to_s]`, "[\"a\", \"a\"]\n"},
		{req + `desc "Build it"; task :a; p [Rake::Task[:a].comment, Rake::Task[:a].full_comment]`, "[\"Build it\", \"Build it\"]\n"},
		{req + `task :a; p [Rake::Task[:a].comment, Rake::Task[:a].full_comment]`, "[nil, nil]\n"},
		{req + `task :a; p Rake::Task[:a].needed?`, "true\n"},
		{req + `p desc("hi")`, "\"hi\"\n"},
		{req + `o=[]; task :a do o<<1 end; t=Rake::Task[:a]; b=t.already_invoked?; t.invoke; p [b, t.already_invoked?]`, "[false, true]\n"},
		{req + `o=[]; task :a do o<<"x" end; Rake::Task[:a].execute; Rake::Task[:a].execute; p o`, "[\"x\", \"x\"]\n"},
		{req + `o=[]; task :a do o<<1 end; Rake::Task[:a].invoke; Rake::Task[:a].invoke; Rake::Task[:a].reenable; Rake::Task[:a].invoke; p o`, "[1, 1]\n"},
		{req + `task :a=>:b; Rake::Task[:a].clear; p Rake::Task[:a].prerequisites`, "[]\n"},
		{req + `task :a; task :b=>:a; p Rake::Task[:b].prerequisite_tasks.map(&:name)`, "[\"a\"]\n"},
		{req + `file "tgt" do end; p Rake::Task["tgt"].class`, "Rake::FileTask\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRakeEnhance covers Task#enhance with dependencies + a block, and the
// block-only (no-dependency) form.
func TestRakeEnhance(t *testing.T) {
	cases := []struct{ src, want string }{
		{req + `o=[]; task :a do o<<"a" end; Rake::Task[:a].enhance([:b]) do o<<"enh" end; task :b do o<<"b" end; Rake::Task[:a].invoke; p o`, "[\"b\", \"a\", \"enh\"]\n"},
		{req + `o=[]; task :a do o<<"a" end; Rake::Task[:a].enhance { o<<"e" }; Rake::Task[:a].invoke; p o`, "[\"a\", \"e\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRakeApplication covers the Rake::Application registry surface (the per-VM
// registry via Rake.application, and an independent .new registry).
func TestRakeApplication(t *testing.T) {
	cases := []struct{ src, want string }{
		{req + `task :a; app=Rake.application; p [app.class, app.task_defined?(:a), app.task_defined?(:z)]`, "[Rake::Application, true, false]\n"},
		{req + `task :a; app=Rake.application; p [app[:a].name, app.lookup("a").name, app.lookup("z")]`, "[\"a\", \"a\", nil]\n"},
		{req + `task :a; app=Rake.application; p app.tasks.map(&:name)`, "[\"a\"]\n"},
		{req + `task :a; app=Rake.application; app.clear; p app.tasks`, "[]\n"},
		{req + `na=Rake::Application.new; p [na.class, na.tasks]`, "[Rake::Application, []]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRakeFileList covers the FileList include/exclude/ext/resolve/clear_exclude
// surface over literal entries (no filesystem needed).
func TestRakeFileList(t *testing.T) {
	cases := []struct{ src, want string }{
		{req + `p Rake::FileList.new("a", "b").to_a`, "[\"a\", \"b\"]\n"},
		{req + `p Rake::FileList.new("a").to_s`, "\"a\"\n"},
		{req + `fl=Rake::FileList.new("a","b"); fl.exclude("b"); p fl.to_a`, "[\"a\"]\n"},
		{req + `fl=Rake::FileList.new("a"); fl.include("b"); p fl.to_a`, "[\"a\", \"b\"]\n"},
		{req + `fl=Rake::FileList.new("a"); fl.resolve; p fl.to_a`, "[\"a\"]\n"},
		{req + `p Rake::FileList.new("a.rb","b.rb").ext(".go").to_a`, "[\"a.go\", \"b.go\"]\n"},
		{req + `p Rake::FileList.new("a.rb").ext.to_a`, "[\"a\"]\n"},
		{req + `p Rake::FileList.new("CVS","a").to_a`, "[\"a\"]\n"},
		{req + `fl=Rake::FileList.new("CVS","a"); fl.clear_exclude; p fl.to_a`, "[\"CVS\", \"a\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRakeFileListGlob covers the Dir.glob and File.exist? seams over a real temp
// directory (glob expansion + default-ignore exclusion, and #existing).
func TestRakeFileListGlob(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "keep.bak"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")

	glob := fmt.Sprintf(req+`p Rake::FileList.new(%q).to_a`, filepath.Join(dir, "*"))
	if got, want := eval(t, glob), fmt.Sprintf("[%q, %q]\n", a, b); got != want {
		t.Errorf("glob: got=%q want=%q", got, want)
	}

	exist := fmt.Sprintf(req+`p Rake::FileList.new(%q, %q).existing.to_a`, a, filepath.Join(dir, "nope.txt"))
	if got, want := eval(t, exist), fmt.Sprintf("[%q]\n", a); got != want {
		t.Errorf("existing: got=%q want=%q", got, want)
	}
}

// TestRakeFileTaskNeeded covers FileTask#needed? against the real filesystem mtime
// seam: an up-to-date target, an out-of-date one, and a missing one.
func TestRakeFileTaskNeeded(t *testing.T) {
	dir := t.TempDir()
	pre := filepath.Join(dir, "pre")
	tgt := filepath.Join(dir, "tgt")
	write := func(p string) {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour)
	now := time.Now()
	chtime := func(p string, ts time.Time) {
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	src := fmt.Sprintf(req+`file %q => %q; p Rake::Task[%q].needed?`, tgt, pre, tgt)

	// Up-to-date: target newer than prerequisite → not needed.
	write(pre)
	write(tgt)
	chtime(pre, old)
	chtime(tgt, now)
	if got := eval(t, src); got != "false\n" {
		t.Errorf("up-to-date: got=%q want=%q", got, "false\n")
	}

	// Out-of-date: target older than prerequisite → needed.
	chtime(pre, now)
	chtime(tgt, old)
	if got := eval(t, src); got != "true\n" {
		t.Errorf("out-of-date: got=%q want=%q", got, "true\n")
	}

	// Missing target → needed (also exercises rakeStat's absent branch).
	if err := os.Remove(tgt); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, src); got != "true\n" {
		t.Errorf("missing: got=%q want=%q", got, "true\n")
	}
}

// TestRakeErrors covers every raising branch across the binding.
func TestRakeErrors(t *testing.T) {
	cases := []struct {
		src, class, msgSub string
	}{
		{req + `task`, "ArgumentError", "given 0"},
		{req + `task :a => 1, :b => 2`, "RuntimeError", "Task Argument Error"},
		{req + `Rake::Task[:nope_zzz]`, "RuntimeError", "Don't know how to build"},
		{req + `Rake::Task[]`, "ArgumentError", "given 0"},
		{req + `Rake::Task.task_defined?`, "ArgumentError", "given 0"},
		{req + `desc`, "ArgumentError", "given 0"},
		{req + `Rake.application[]`, "ArgumentError", "given 0"},
		{req + `Rake.application.lookup`, "ArgumentError", "given 0"},
		{req + `Rake.application.task_defined?`, "ArgumentError", "given 0"},
		{req + `Rake.application["nope_zzz"]`, "RuntimeError", "Don't know how to build"},
		{req + `task :a => :b; task :b => :a; Rake::Task[:a].invoke`, "RuntimeError", "Circular dependency detected"},
		{req + `task :a => :ghost_zzz; Rake::Task[:a].prerequisite_tasks`, "RuntimeError", "Don't know how to build"},
	}
	for _, c := range cases {
		class, msg := evalErr(t, c.src)
		if class != c.class || !strings.Contains(msg, c.msgSub) {
			t.Errorf("src=%q got class=%q msg=%q want class=%q msg~=%q", c.src, class, msg, c.class, c.msgSub)
		}
	}
}

// TestRakeValueProtocol covers the ToS / Inspect / Truthy arms of each wrapper.
func TestRakeValueProtocol(t *testing.T) {
	app := rake.NewApplication()
	ti := app.DefineTask(rake.PlainTask, "t", nil, nil, nil, nil)
	tv := &RakeTaskVal{t: ti}
	if tv.ToS() != "t" || tv.Inspect() != "#<Rake::Task t>" || !tv.Truthy() {
		t.Errorf("task: ToS=%q Inspect=%q Truthy=%v", tv.ToS(), tv.Inspect(), tv.Truthy())
	}
	av := &RakeApplicationVal{app: app}
	if av.ToS() != "#<Rake::Application>" || av.Inspect() != "#<Rake::Application>" || !av.Truthy() {
		t.Errorf("app: ToS=%q Inspect=%q Truthy=%v", av.ToS(), av.Inspect(), av.Truthy())
	}
	fv := &RakeFileListVal{fl: rake.NewFileList(nil, "a")}
	if fv.ToS() != "a" || fv.Inspect() != "#<Rake::FileList>" || !fv.Truthy() {
		t.Errorf("filelist: ToS=%q Inspect=%q Truthy=%v", fv.ToS(), fv.Inspect(), fv.Truthy())
	}
}

// TestRakeBaseTask covers rakeBaseTask for both a plain task and a file task.
func TestRakeBaseTask(t *testing.T) {
	app := rake.NewApplication()
	plain := app.DefineTask(rake.PlainTask, "p", nil, nil, nil, nil)
	file := app.DefineTask(rake.FileKind, "f", nil, nil, nil, nil)
	if b := rakeBaseTask(plain); b == nil || b.Name() != "p" {
		t.Errorf("plain base = %v", b)
	}
	if b := rakeBaseTask(file); b == nil || b.Name() != "f" {
		t.Errorf("file base = %v", b)
	}
}

// TestRakeSeams covers the filesystem seams directly, including their error /
// absent branches.
func TestRakeSeams(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := rakeStat(f); !ok {
		t.Error("rakeStat: existing file reported absent")
	}
	if _, ok := rakeStat(filepath.Join(dir, "missing")); ok {
		t.Error("rakeStat: missing file reported present")
	}
	if m := rakeGlob(filepath.Join(dir, "*")); len(m) != 1 || m[0] != f {
		t.Errorf("rakeGlob: %v", m)
	}
	if m := rakeGlob("["); m != nil {
		t.Errorf("rakeGlob bad pattern: %v", m)
	}
	if !rakeExists(f) || rakeExists(filepath.Join(dir, "missing")) {
		t.Error("rakeExists: wrong result")
	}
}

// TestRakeResolveArgs covers the non-raising decode forms directly (the raising
// forms are covered by TestRakeErrors through the DSL).
func TestRakeResolveArgs(t *testing.T) {
	// No-Hash form: name + flattened arg names.
	n, an, dep := rakeResolveArgs([]object.Value{object.SymVal("t"), object.SymVal("a"), object.SymVal("b")})
	if n != "t" || len(an) != 2 || an[0] != "a" || an[1] != "b" || dep != nil {
		t.Errorf("no-hash: n=%q an=%v dep=%v", n, an, dep)
	}
	// Sole-Hash form: name → deps.
	h := object.NewHash()
	h.Set(object.SymVal("t"), object.SymVal("d"))
	n, an, dep = rakeResolveArgs([]object.Value{h})
	if n != "t" || an != nil || len(dep) != 1 || dep[0] != "d" {
		t.Errorf("sole-hash: n=%q an=%v dep=%v", n, an, dep)
	}
	// Name + Hash form: arg-names key → deps value.
	h2 := object.NewHash()
	h2.Set(object.NewArrayFromSlice([]object.Value{object.SymVal("x")}), object.SymVal("d"))
	n, an, dep = rakeResolveArgs([]object.Value{object.SymVal("t"), h2})
	if n != "t" || len(an) != 1 || an[0] != "x" || len(dep) != 1 || dep[0] != "d" {
		t.Errorf("name+hash: n=%q an=%v dep=%v", n, an, dep)
	}
}
