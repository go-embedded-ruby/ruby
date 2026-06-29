// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// tmpDirSlash returns a forward-slash temp dir path so file-path tests behave
// identically on Windows (the coverage gate runs on windows-latest too).
func tmpDirSlash(t *testing.T) string {
	t.Helper()
	return filepath.ToSlash(t.TempDir())
}

// writeFileT writes content to path, failing the test on error.
func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.FromSlash(path), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// rubyStr renders a Go string as a double-quoted Ruby string literal (paths are
// plain ASCII here, so only the quotes need escaping).
func rubyStr(s string) string { return strconv.Quote(s) }

// TestYAMLLoadScalars covers YAML.load of the implicit scalar grammar: null,
// booleans, integers (decimal / hex / underscore / bignum), floats (incl. .inf /
// .nan), symbols, quoted strings and timestamps. Each expected output is the
// inspect of the same value loaded by MRI's Psych.
func TestYAMLLoadScalars(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p YAML.load("--- \n")`, "nil\n"},
		{`p YAML.load("--- ~\n")`, "nil\n"},
		{`p YAML.load("--- null\n")`, "nil\n"},
		{`p YAML.load("--- true\n")`, "true\n"},
		{`p YAML.load("--- false\n")`, "false\n"},
		{`p YAML.load("--- 42\n")`, "42\n"},
		{`p YAML.load("--- -7\n")`, "-7\n"},
		{`p YAML.load("--- 1_000\n")`, "1000\n"},
		{`p YAML.load("--- 0x1A\n")`, "26\n"},
		{`p YAML.load("--- -0xff\n")`, "-255\n"},
		{`p YAML.load("--- 1.5\n")`, "1.5\n"},
		{`p YAML.load("--- 100.0\n")`, "100.0\n"},
		{`p YAML.load("--- .inf\n")`, "Infinity\n"},
		{`p YAML.load("--- -.inf\n")`, "-Infinity\n"},
		{`p YAML.load("--- .nan\n")`, "NaN\n"},
		{`p YAML.load("--- :sym\n")`, ":sym\n"},
		{`p YAML.load("--- hello\n")`, "\"hello\"\n"},
		{`p YAML.load("--- '123'\n")`, "\"123\"\n"},
		{`p YAML.load(%q{--- "a\tb"} + "\n")`, "\"a\\tb\"\n"},
		// A 1000000000000000000000000000000 bignum scalar.
		{`p YAML.load("--- 1000000000000000000000000000000\n")`, "1000000000000000000000000000000\n"},
		// An empty input loads as nil; a lone "---" marker loads as nil.
		{`p YAML.load("")`, "nil\n"},
		{`p YAML.load("---")`, "nil\n"},
		// Whole-line comments and the document-end marker are ignored.
		{"p YAML.load(\"# a comment\\n--- 5\\n\")", "5\n"},
		{"p YAML.load(\"--- 5\\n...\\n\")", "5\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLLoadCollections covers block / flow mappings and sequences and their
// nesting, including the same-indent block-sequence value Psych writes under a
// mapping key.
func TestYAMLLoadCollections(t *testing.T) {
	cases := []struct{ src, want string }{
		{"p YAML.load(\"---\\na: 1\\nb: 2\\n\")", "{\"a\" => 1, \"b\" => 2}\n"},
		{"p YAML.load(\"---\\n- 1\\n- 2\\n\")", "[1, 2]\n"},
		// A block-sequence value aligned under its key.
		{"p YAML.load(\"---\\nb:\\n- 2\\n- 3\\n\")", "{\"b\" => [2, 3]}\n"},
		// A nested mapping indented two deeper.
		{"p YAML.load(\"---\\nk:\\n  n: true\\n\")", "{\"k\" => {\"n\" => true}}\n"},
		// A nil mapping value.
		{"p YAML.load(\"---\\nk:\\n\")", "{\"k\" => nil}\n"},
		// Flow collections.
		{`p YAML.load("--- [1, 2, 3]\n")`, "[1, 2, 3]\n"},
		{`p YAML.load("--- []\n")`, "[]\n"},
		{`p YAML.load("--- {}\n")`, "{}\n"},
		{`p YAML.load("--- {a: 1, b: 2}\n")`, "{\"a\" => 1, \"b\" => 2}\n"},
		// A flow sequence nested in a flow sequence (splitFlow bracket depth).
		{`p YAML.load("--- [[1, 2], [3]]\n")`, "[[1, 2], [3]]\n"},
		// A sequence of mappings.
		{"p YAML.load(\"---\\n- a: 1\\n- b: 2\\n\")", "[{\"a\" => 1}, {\"b\" => 2}]\n"},
		// A multi-key mapping under a dash.
		{"p YAML.load(\"---\\n- a: 1\\n  b: 2\\n\")", "[{\"a\" => 1, \"b\" => 2}]\n"},
		// A nested sequence under a dash.
		{"p YAML.load(\"---\\n- - 1\\n  - 2\\n\")", "[[1, 2]]\n"},
		// A "-" dash whose value is the indented block below it.
		{"p YAML.load(\"---\\n-\\n  a: 1\\n\")", "[{\"a\" => 1}]\n"},
		// An empty collection under a dash / a key.
		{"p YAML.load(\"---\\n- []\\n- {}\\n\")", "[[], {}]\n"},
		{"p YAML.load(\"---\\nx: []\\n\")", "{\"x\" => []}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLLoadBlockScalar covers literal / folded block scalars with each chomp
// indicator, both at the document level and under a mapping key / sequence dash.
func TestYAMLLoadBlockScalar(t *testing.T) {
	cases := []struct{ src, want string }{
		// Document-level literal block, strip chomp.
		{"p YAML.load(\"--- |-\\n  a\\n  b\\n\")", "\"a\\nb\"\n"},
		// Clip chomp keeps one trailing newline.
		{"p YAML.load(\"--- |\\n  a\\n  b\\n\")", "\"a\\nb\\n\"\n"},
		// Keep chomp.
		{"p YAML.load(\"--- |+\\n  a\\n\")", "\"a\\n\"\n"},
		// Folded scalar joins lines with spaces.
		{"p YAML.load(\"--- >-\\n  a\\n  b\\n\")", "\"a b\"\n"},
		// Under a mapping key.
		{"p YAML.load(\"---\\nx: |-\\n  a\\n  b\\n\")", "{\"x\" => \"a\\nb\"}\n"},
		// Under a sequence dash.
		{"p YAML.load(\"---\\n- |-\\n  a\\n  b\\n\")", "[\"a\\nb\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLLoadTime covers the ISO-8601 timestamp the emitter writes loading back
// to a Time, in both the "Z" and numeric-offset forms.
func TestYAMLLoadTime(t *testing.T) {
	cases := []struct{ src, want string }{
		{`t = YAML.load("--- 1970-01-01 00:00:00.000000000 Z\n"); p t.class; p t.to_i`, "Time\n0\n"},
		{`t = YAML.load("--- 1970-01-01 01:00:00.000000000 +01:00\n"); p t.to_i`, "0\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLLoadStateRoundTrip covers the exact shape Puppet's state.yaml uses (a
// String -> {Symbol -> Time} mapping), which must round-trip through dump/load.
func TestYAMLLoadStateRoundTrip(t *testing.T) {
	src := `require "yaml"
h = {"File[/x]" => {:checked => Time.at(0).utc, :synced => Time.at(0).utc}}
r = YAML.load(YAML.dump(h))
p r.keys
p r["File[/x]"].keys
p r["File[/x]"][:checked].to_i`
	want := "[\"File[/x]\"]\n[:checked, :synced]\n0\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestYAMLLoadRubyObject covers loading a !ruby/object: mapping back into an
// instance of the named class, with the mapping entries as instance variables —
// including a class the program already defines and one it does not (which the
// loader registers as a placeholder).
func TestYAMLLoadRubyObject(t *testing.T) {
	cases := []struct{ src, want string }{
		// Known class: ivars are restored.
		{`class Foo; attr_reader :a, :b; end
o = YAML.load("--- !ruby/object:Foo\na: 1\nb: hi\n")
p o.class
p o.a
p o.b`, "Foo\n1\n\"hi\"\n"},
		// Bare !ruby/object loads as an Object instance.
		{`o = YAML.load("--- !ruby/object\na: 1\n")
p o.instance_variable_get(:@a)`, "1\n"},
		// Unknown class: a placeholder class of that name is created.
		{`o = YAML.load("--- !ruby/object:Quux\nx: 5\n")
p o.class.name
p o.instance_variable_get(:@x)`, "\"Quux\"\n5\n"},
		// A qualified unknown class name.
		{`o = YAML.load("--- !ruby/object:A::B\nx: 1\n")
p o.class.name`, "\"A::B\"\n"},
		// An empty-bodied object (the "tag {}" inline form).
		{`class Empty; end
o = YAML.load("--- !ruby/object:Empty {}\n")
p o.class`, "Empty\n"},
		// A symbol-keyed object mapping (Psych object ivars).
		{`o = YAML.load("--- !ruby/object:Quux2\n:x: 1\n")
p o.instance_variable_get(:@x)`, "1\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLLoadAnchors covers &anchor / *alias: a shared object referenced twice
// loads as the very same instance.
func TestYAMLLoadAnchors(t *testing.T) {
	src := `class Node; attr_reader :n; end
doc = "---\n- &1 !ruby/object:Node\n  n: 7\n- *1\n"
a = YAML.load(doc)
p a[0].equal?(a[1])
p a[1].n`
	want := "true\n7\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestYAMLLoadSafeAndFile covers the safe_load / unsafe_load aliases (same
// implementation) and load_file via a temp file.
func TestYAMLLoadSafeAndFile(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p YAML.safe_load("--- 7\n")`, "7\n"},
		{`p YAML.unsafe_load("--- 7\n")`, "7\n"},
		// Extra positional / keyword args Psych accepts are tolerated and ignored.
		{`p YAML.safe_load("--- 7\n", permitted_classes: [Symbol])`, "7\n"},
		{`p YAML.load("--- 7\n", symbolize_names: true)`, "7\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLLoadFile covers YAML.load_file / safe_load_file reading from disk and
// the missing-file error.
func TestYAMLLoadFile(t *testing.T) {
	dir := tmpDirSlash(t)
	path := dir + "/doc.yaml"
	writeFileT(t, path, "---\na: 1\nb: 2\n")
	src := `p YAML.load_file(` + rubyStr(path) + `)`
	if got := eval(t, src); got != "{\"a\" => 1, \"b\" => 2}\n" {
		t.Errorf("load_file got=%q", got)
	}
	src2 := `p YAML.safe_load_file(` + rubyStr(path) + `)`
	if got := eval(t, src2); got != "{\"a\" => 1, \"b\" => 2}\n" {
		t.Errorf("safe_load_file got=%q", got)
	}
	// A missing file raises Errno::ENOENT.
	err := runErr(t, `YAML.load_file("/no/such/yaml/file.yaml")`)
	if err == nil || !strings.Contains(err.Error(), "ENOENT") {
		t.Errorf("missing file: expected ENOENT, got %v", err)
	}
}

// TestYAMLLoadArity covers the zero-argument errors of load / load_file.
func TestYAMLLoadArity(t *testing.T) {
	for _, call := range []string{`YAML.load`, `YAML.load_file`} {
		err := runErr(t, call)
		if err == nil || !strings.Contains(err.Error(), "wrong number of arguments") {
			t.Errorf("%s: expected ArgumentError, got %v", call, err)
		}
	}
}
