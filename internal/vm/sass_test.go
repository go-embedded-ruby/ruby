// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// bindRecover runs fn and returns the RubyError class it raises, or "" when it
// returns without raising — the seam for asserting the raise branches of the
// binding helpers a Ruby program cannot reach naturally.
func bindRecover(fn func()) (class string) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok {
				class = re.Class
				return
			}
			panic(r)
		}
	}()
	fn()
	return ""
}

// TestSassCompileEndToEnd is the headline scenario: require the library and compile
// SCSS source to CSS through Sass.compile, Sass.compile_string and the SassC::Engine
// surface, asserting the compiled CSS carries the declaration.
func TestSassCompileEndToEnd(t *testing.T) {
	src := `
require "sass"

css = Sass.compile("$c: red; a { color: $c; }")
raise "compile" unless css.include?("color: red")

css2 = Sass.compile_string("$c: red; a { color: $c; }")
raise "compile_string" unless css2.include?("color: red")

eng = SassC::Engine.new("$c: red; a { color: $c; }")
raise "sassc" unless eng.render.include?("color: red")

puts "ok"
`
	if got := runSrc(t, src); got != "ok" {
		t.Fatalf("end-to-end output = %q", got)
	}
}

// TestSassOptions drives every option branch through the interpreter: the syntax:
// values (indented/sass, css, an unknown value falling back to scss), the style:
// values (compressed and a non-compressed value), a load_paths: Array, a non-Array
// load_paths (ignored), and a non-String argument coerced via to_s.
func TestSassOptions(t *testing.T) {
	src := `
require "sass"

# style: compressed collapses whitespace.
raise "compressed" unless Sass.compile_string(".a{b:1}", style: :compressed) == ".a{b:1}\n"
# style: a non-compressed value keeps the default expanded layout.
raise "expanded" unless Sass.compile_string(".a{b:1}", style: :expanded).include?("b: 1")

# syntax: indented / sass parses the whitespace-significant grammar.
raise "indented" unless Sass.compile_string(".a\n  b: 1\n", syntax: :indented).include?("b: 1")
raise "sass" unless Sass.compile_string(".a\n  b: 1\n", syntax: :sass).include?("b: 1")
# syntax: css passes plain CSS through.
raise "css" unless Sass.compile_string(".a{b:1}", syntax: :css).include?("b: 1")
# syntax: an unknown value (a String / an Integer) falls back to scss.
raise "scss-str" unless Sass.compile_string(".a{b:1+1}", syntax: "weird").include?("b: 2")
raise "scss-int" unless Sass.compile_string(".a{b:1+1}", syntax: 7).include?("b: 2")

# load_paths: an Array is accepted (unused here); a non-Array is ignored.
raise "load_paths" unless Sass.compile_string(".a{b:1}", load_paths: ["x"]).include?("b: 1")
raise "load_paths-bad" unless Sass.compile_string(".a{b:1}", load_paths: "x").include?("b: 1")

# a non-String argument is coerced via to_s inside a rescue (":sym" is invalid scss).
bad = begin; Sass.compile(:not_scss); "no"; rescue Sass::CompileError; "raised"; end
raise "sym-arg" unless bad == "raised"

puts "ok"
`
	if got := runSrc(t, src); got != "ok" {
		t.Fatalf("options output = %q", got)
	}
}

// TestSassCompileFile compiles a .scss file on disk (Sass.compile_file) and asserts
// a missing file raises Sass::CompileError.
func TestSassCompileFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "in.scss")
	if err := os.WriteFile(p, []byte(".x { y: 2 * 3px; }"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `
require "sass"
css = Sass.compile_file(` + rubyStr(p) + `)
raise "compile_file" unless css.include?("y: 6px")
missing = begin
  Sass.compile_file("no/such/file.scss")
  "no"
rescue Sass::CompileError
  "raised"
end
puts missing
`
	if got := runSrc(t, src); got != "raised" {
		t.Fatalf("compile_file output = %q", got)
	}
}

// TestSassErrors covers the compile-error raise path and the zero-argument
// ArgumentError guards on the three module methods.
func TestSassErrors(t *testing.T) {
	src := `
require "sass"

bad = begin
  Sass.compile_string(".a{b:$undefined}")
  "no"
rescue Sass::CompileError
  "raised"
end
raise "compile error" unless bad == "raised"

# zero-arg arity guards.
raise "cs arity" unless (begin; Sass.compile_string; "no"; rescue ArgumentError; "raised"; end) == "raised"
raise "c arity" unless (begin; Sass.compile; "no"; rescue ArgumentError; "raised"; end) == "raised"
raise "cf arity" unless (begin; Sass.compile_file; "no"; rescue ArgumentError; "raised"; end) == "raised"

puts "ok"
`
	if got := runSrc(t, src); got != "ok" {
		t.Fatalf("errors output = %q", got)
	}
}

// TestSassCSurface covers the legacy SassC::Engine branches: the compressed and
// syntax options, a compile error raising SassC::SyntaxError, and the zero-argument
// ArgumentError guard.
func TestSassCSurface(t *testing.T) {
	src := `
require "sass"

raise "compressed" unless SassC::Engine.new(".a{b:1}", style: :compressed).render == ".a{b:1}\n"
raise "syntax" unless SassC::Engine.new(".a\n  b: 1\n", syntax: "sass").render.include?("b: 1")

bad = begin
  SassC::Engine.new(".a{b:$undefined}").render
  "no"
rescue SassC::SyntaxError
  "raised"
end
raise "sassc error" unless bad == "raised"

raise "arity" unless (begin; SassC::Engine.new; "no"; rescue ArgumentError; "raised"; end) == "raised"

puts "ok"
`
	if got := runSrc(t, src); got != "ok" {
		t.Fatalf("sassc output = %q", got)
	}
}

// TestSassCEngineShell covers the native handle's ToS/Inspect/Truthy and the
// uninitialized-handle raise branch that Ruby cannot reach (new always sets it).
func TestSassCEngineShell(t *testing.T) {
	h := &sassCEngine{}
	if h.ToS() != "#<SassC::Engine>" || h.Inspect() != h.ToS() || !h.Truthy() {
		t.Errorf("shell: %q / %q / %v", h.ToS(), h.Inspect(), h.Truthy())
	}
	inst := &RObject{class: newClass("X", nil), ivars: map[string]object.Value{}}
	if cls := bindRecover(func() { sassCEngineHandle(inst) }); cls != "SassC::SyntaxError" {
		t.Errorf("uninitialized handle class = %q", cls)
	}
}
