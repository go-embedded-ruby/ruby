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
		{`p YAML.dump(false)`, "\"--- false\\n\"\n"},
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
		// A nil mapping value renders as "key:" with nothing after the colon.
		{`puts YAML.dump({"k" => nil})`, "---\nk:\n"},
		// Nested sequences and mappings within a sequence (Psych's inline-first form).
		{`puts YAML.dump([[1, 2], [3]])`, "---\n- - 1\n  - 2\n- - 3\n"},
		{`puts YAML.dump([{"a" => 1}, {"b" => 2}])`, "---\n- a: 1\n- b: 2\n"},
		// A multi-key mapping under a sequence dash (inline first key, rest indented).
		{`puts YAML.dump([{"a" => 1, "b" => 2}])`, "---\n- a: 1\n  b: 2\n"},
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

// TestYAMLDumpScalarQuoting covers Psych's scalar plain/single/double-quote
// selection. Each expected form is the exact Psych.dump output in MRI.
func TestYAMLDumpScalarQuoting(t *testing.T) {
	cases := []struct{ src, want string }{
		// Plain (unquoted): ordinary words, mid-string "#", a resource ref.
		{`p YAML.dump("a#b")`, "\"--- a#b\\n\"\n"},
		{`p YAML.dump("hello world")`, "\"--- hello world\\n\"\n"},
		{`p YAML.dump("File[/tmp/x]")`, "\"--- File[/tmp/x]\\n\"\n"},
		// Single-quoted: reserved words, numeric-looking, a "key: " or trailing ":".
		{`p YAML.dump("no")`, "\"--- 'no'\\n\"\n"},
		{`p YAML.dump("yes")`, "\"--- 'yes'\\n\"\n"},
		{`p YAML.dump("123")`, "\"--- '123'\\n\"\n"},
		{`p YAML.dump("with: colon")`, "\"--- 'with: colon'\\n\"\n"},
		{`p YAML.dump("trailing:")`, "\"--- 'trailing:'\\n\"\n"},
		{`p YAML.dump("2026-06-21")`, "\"--- '2026-06-21'\\n\"\n"},
		{`p YAML.dump("00:30")`, "\"--- '00:30'\\n\"\n"},
		{`p YAML.dump("0x1A")`, "\"--- '0x1A'\\n\"\n"},
		{`p YAML.dump("a #b")`, "\"--- 'a #b'\\n\"\n"},
		// Double-quoted: leading indicator characters and control characters.
		{`p YAML.dump("-leading")`, "\"--- \\\"-leading\\\"\\n\"\n"},
		{`p YAML.dump("@at")`, "\"--- \\\"@at\\\"\\n\"\n"},
		{`p YAML.dump("#hash")`, "\"--- \\\"#hash\\\"\\n\"\n"},
		{`p YAML.dump("@x\ry")`, "\"--- \\\"@x\\\\ry\\\"\\n\"\n"},
		{`p YAML.dump("@x\ty")`, "\"--- \\\"@x\\\\ty\\\"\\n\"\n"},
		// Control characters escape as \0 / \xNN in the double-quoted form (built
		// via chr to avoid the source-level escape).
		{`p YAML.dump("@x" + 0.chr + "y")`, "\"--- \\\"@x\\\\0y\\\"\\n\"\n"},
		{`p YAML.dump("@x" + 1.chr + "y")`, "\"--- \\\"@x\\\\x01y\\\"\\n\"\n"},
		// A multi-line string is a literal block scalar even with a leading "@".
		{`p YAML.dump("@x\nz")`, "\"--- |-\\n  @x\\n  z\\n\"\n"},
		// A symbol whose name carries a newline is escaped via the quoted form.
		{`p YAML.dump(:"a\nb")`, "\"--- :\\\"a\\\\nb\\\"\\n\"\n"},
		// A leading-indicator string carrying a quote/backslash double-quotes,
		// escaping them. (Psych would single-quote the quote case; double-quoting is
		// also valid YAML and these shapes do not occur in Puppet persistence.)
		{`p YAML.dump("@a\"b")`, "\"--- \\\"@a\\\\\\\"b\\\"\\n\"\n"},
		{`p YAML.dump("@a\\b")`, "\"--- \\\"@a\\\\\\\\b\\\"\\n\"\n"},
		// Symbols, including names with a space or dash (bare ":name" in Psych).
		{`p YAML.dump(:checked)`, "\"--- :checked\\n\"\n"},
		{"p YAML.dump(:\"a b\")", "\"--- :a b\\n\"\n"},
		{"p YAML.dump(:\"a-b\")", "\"--- :a-b\\n\"\n"},
		// Floats with a fractional / integral / exponent form.
		{`p YAML.dump(1.0)`, "\"--- 1.0\\n\"\n"},
		{`p YAML.dump(100.0)`, "\"--- 100.0\\n\"\n"},
		{`p YAML.dump(-1.5)`, "\"--- -1.5\\n\"\n"},
		{`p YAML.dump(0.0/0.0)`, "\"--- .nan\\n\"\n"},
		// Bignum.
		{`p YAML.dump(10 ** 30)`, "\"--- 1000000000000000000000000000000\\n\"\n"},
		// A string with mid-string quote/backslash stays plain (Psych leaves it).
		{`p YAML.dump("a\"b\\c")`, "\"--- a\\\"b\\\\c\\n\"\n"},
		// A block scalar whose content ends in a newline (clip chomp "|").
		{`puts YAML.dump({"x" => "a\nb\n"})`, "---\nx: |\n  a\n  b\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestYAMLDumpTime covers dumping a Time value (Psych's ISO-8601 timestamp).
func TestYAMLDumpTime(t *testing.T) {
	src := `p YAML.dump(Time.at(0).utc)`
	want := "\"--- 1970-01-01 00:00:00.000000000 Z\\n\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestYAMLDumpNestedSeqInSeqEmpty covers an empty sequence/mapping under a
// sequence dash ("- []" / "- {}").
func TestYAMLDumpNestedEmptyUnderDash(t *testing.T) {
	cases := []struct{ src, want string }{
		{`puts YAML.dump([[], {}])`, "---\n- []\n- {}\n"},
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
