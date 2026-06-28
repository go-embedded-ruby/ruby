// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestYAMLConstants covers the YAML/Psych loadable shell's constant and error
// tree (require "yaml"). YAML is an alias of Psych, matching MRI.
func TestYAMLConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		// YAML is the very same module object as Psych.
		{`require "yaml"; p YAML.equal?(Psych)`, "true\n"},
		{`require "yaml"; p Psych::VERSION`, "\"5.0.0\"\n"},
		// Psych::Nodes is a module.
		{`require "yaml"; p Psych::Nodes.is_a?(Module)`, "true\n"},
		// Error tree: SyntaxError and DisallowedClass descend from Psych::Exception,
		// which descends from StandardError.
		{`require "yaml"; p Psych::Exception < StandardError`, "true\n"},
		{`require "yaml"; p Psych::SyntaxError < Psych::Exception`, "true\n"},
		{`require "yaml"; p Psych::DisallowedClass < Psych::Exception`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLNotImplemented covers the Psych/YAML methods that still raise
// NotImplementedError until a real pure-Go YAML parser lands. dump is now
// implemented (see TestYAMLDump) and so is excluded here.
func TestYAMLNotImplemented(t *testing.T) {
	// load/safe_load/parse/parse_stream/dump_tags take args; load_file takes a
	// path. Each must raise NotImplementedError naming the method.
	calls := map[string]string{
		"load":         `YAML.load("a")`,
		"safe_load":    `YAML.safe_load("a")`,
		"load_file":    `YAML.load_file("x.yml")`,
		"parse":        `YAML.parse("a")`,
		"parse_stream": `YAML.parse_stream("a")`,
		"dump_tags":    `YAML.dump_tags`,
	}
	for name, call := range calls {
		src := `require "yaml"; ` + call
		err := runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "NotImplementedError") {
			t.Errorf("%s: expected NotImplementedError, got %v", name, err)
		}
		if err != nil && !strings.Contains(err.Error(), name) {
			t.Errorf("%s: message should name the method, got %v", name, err)
		}
	}
}

// TestYAMLDump covers YAML.dump for the plain-value shapes Puppet's local
// persistence writes (run summary / agent state). Each expected document is the
// exact Psych.dump output of the same value in MRI Ruby.
func TestYAMLDump(t *testing.T) {
	cases := []struct{ src, want string }{
		// Scalars on the document line.
		{`p YAML.dump(1)`, "\"--- 1\\n\"\n"},
		{`p YAML.dump(1.5)`, "\"--- 1.5\\n\"\n"},
		{`p YAML.dump(true)`, "\"--- true\\n\"\n"},
		{`p YAML.dump(nil)`, "\"--- \\n\"\n"},
		{`p YAML.dump(:sym)`, "\"--- :sym\\n\"\n"},
		{`p YAML.dump("hi")`, "\"--- hi\\n\"\n"},
		// Strings that round-trip as non-strings are quoted.
		{`p YAML.dump("123")`, "\"--- '123'\\n\"\n"},
		{`p YAML.dump("true")`, "\"--- 'true'\\n\"\n"},
		{`p YAML.dump("")`, "\"--- ''\\n\"\n"},
		// Float specials.
		{`p YAML.dump(1.0/0)`, "\"--- .inf\\n\"\n"},
		{`p YAML.dump(-1.0/0)`, "\"--- -.inf\\n\"\n"},
		// Collections render as block mappings/sequences. (puts does not add a
		// second newline because the document already ends in one.)
		{`puts YAML.dump({"a" => 1, "b" => [2, 3]})`, "---\na: 1\nb:\n- 2\n- 3\n"},
		{`puts YAML.dump([1, "two", :three])`, "---\n- 1\n- two\n- :three\n"},
		{`puts YAML.dump({"k" => {"nested" => true}})`, "---\nk:\n  nested: true\n"},
		// Symbol keys (as the agent state file uses).
		{`puts YAML.dump({:checked => 5})`, "---\n:checked: 5\n"},
		// Nested sequences and mappings within a sequence (Psych's inline-first form).
		{`puts YAML.dump([[1, 2], [3]])`, "---\n- - 1\n  - 2\n- - 3\n"},
		{`puts YAML.dump([{"a" => 1}, {"b" => 2}])`, "---\n- a: 1\n- b: 2\n"},
		{`puts YAML.dump({"a" => {"b" => [1, 2]}})`, "---\na:\n  b:\n  - 1\n  - 2\n"},
		// Empty collections.
		{`p YAML.dump([])`, "\"--- []\\n\"\n"},
		{`p YAML.dump({})`, "\"--- {}\\n\"\n"},
		// A nested empty collection.
		{`puts YAML.dump({"x" => []})`, "---\nx: []\n"},
		{`puts YAML.dump({"x" => {}})`, "---\nx: {}\n"},
		// A multi-line string becomes a literal block scalar.
		{`puts YAML.dump({"x" => "a\nb"})`, "---\nx: |-\n  a\n  b\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLDumpToIO covers YAML.dump(obj, io): the document is written to the IO
// (a StringIO here) and the IO is returned.
func TestYAMLDumpToIO(t *testing.T) {
	src := `require "stringio"
io = StringIO.new
r = YAML.dump({"a" => 1}, io)
print io.string
print "/"
p r.equal?(io)`
	want := "---\na: 1\n/true\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestYAMLDumpUnsupported covers the TypeError raised for a value outside the
// supported plain-value shapes, which the report YAML indirector rescues.
func TestYAMLDumpUnsupported(t *testing.T) {
	src := `class Foo; end; YAML.dump(Foo.new)`
	err := runErr(t, src)
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("expected TypeError, got %v", err)
	}
}

// TestYAMLDumpArity covers the zero-argument error.
func TestYAMLDumpArity(t *testing.T) {
	err := runErr(t, `YAML.dump`)
	if err == nil || !strings.Contains(err.Error(), "wrong number of arguments") {
		t.Errorf("expected ArgumentError, got %v", err)
	}
}
