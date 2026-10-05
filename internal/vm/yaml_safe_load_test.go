// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// --- YAML.safe_load permitted-class restriction (issue #775) ----------------
//
// safe_load used to instantiate any class a document named. These tests pin the
// refusal, and pin it against real MRI rather than against a reading of the
// docs: safeLoadOracleWant below is the VERBATIM stdout of safeLoadOracleSrc
// under ruby 4.0.5 (2026-05-20) with its bundled Psych, captured by running the
// identical program. Any line that drifts from MRI fails here.
//
// The default permitted set is the interesting part. psych.rb documents
// TrueClass / FalseClass / NilClass / Integer / Float / String / Array / Hash as
// permitted by default (psych.rb:278-287), but
// Psych::ClassLoader::Restricted#initialize (psych/class_loader.rb:78-82) seeds
// @classes from permitted_classes ALONE and holds none of those names. They load
// because Psych::Visitors::ToRuby builds them without consulting the class
// loader at all -- so the restricted default set is EMPTY, and everything that
// does reach the class loader (Symbol, Time, Date, Range, Regexp, Struct,
// BigDecimal, and every !ruby/object: tag) is refused until named. The rows
// below measure exactly that.

// safeLoadOracleSrc is run under both MRI and rbgo; see safeLoadOracleWant.
const safeLoadOracleSrc = `require "yaml"
class Gadget; attr_accessor :foo; end
class Allowed; end
class A; end
class B; end
Y = "--- !ruby/object:Gadget\nfoo: bar\n"
def t(label)
  r = yield
  puts "#{label} => #{r.class}"
rescue Psych::DisallowedClass => e
  puts "#{label} => DisallowedClass: #{e.message}"
rescue => e
  puts "#{label} => #{e.class}: #{e.message}"
end
t("bare")              { YAML.safe_load(Y) }
t("empty")             { YAML.safe_load(Y, permitted_classes: []) }
t("other")             { YAML.safe_load(Y, permitted_classes: [Allowed]) }
t("exact")             { YAML.safe_load(Y, permitted_classes: [Gadget]) }
t("exact-string")      { YAML.safe_load(Y, permitted_classes: ["Gadget"]) }
t("symbol")            { YAML.safe_load(":foo") }
t("symbol-ok")         { YAML.safe_load(":foo", permitted_classes: [Symbol]) }
t("symbol-key")        { YAML.safe_load(":a: 1") }
t("symbol-key-ok")     { YAML.safe_load(":a: 1", permitted_classes: [Symbol]) }
t("symbol-seq")        { YAML.safe_load("- :a\n") }
t("symbol-tag")        { YAML.safe_load("--- !ruby/symbol foo\n") }
t("psym-only")         { YAML.safe_load(":foo", permitted_symbols: [:foo]) }
t("psym-allowed")      { YAML.safe_load(":foo", permitted_classes: [Symbol], permitted_symbols: [:foo]) }
t("psym-refused")      { YAML.safe_load(":bar", permitted_classes: [Symbol], permitted_symbols: [:foo]) }
t("psym-empty")        { YAML.safe_load(":zz", permitted_classes: [Symbol], permitted_symbols: []) }
t("time")              { YAML.safe_load("--- 2001-12-14 21:59:43 -05:00\n") }
t("time-ok")           { YAML.safe_load("--- 2001-12-14 21:59:43 -05:00\n", permitted_classes: [Time]) }
t("time-in-map")       { YAML.safe_load("a: 2001-12-14 21:59:43 -05:00\n") }
t("regexp")            { YAML.safe_load("--- !ruby/regexp /ab/i\n") }
t("regexp-ok")         { YAML.safe_load("--- !ruby/regexp /ab/i\n", permitted_classes: [Regexp]) }
t("range")             { YAML.safe_load("--- !ruby/range\nbegin: 1\nend: 5\nexcl: false\n") }
t("range-ok")          { YAML.safe_load("--- !ruby/range\nbegin: 1\nend: 5\nexcl: false\n", permitted_classes: [Range]) }
t("range-inner")       { YAML.safe_load("--- !ruby/range\nbegin: :s\nend: 5\nexcl: false\n", permitted_classes: [Range]) }
t("class-tag")         { YAML.safe_load("--- !ruby/class 'String'\n") }
t("class-tag-ok")      { YAML.safe_load("--- !ruby/class 'String'\n", permitted_classes: [String]) }
t("nested-outer-wins") { YAML.safe_load("--- !ruby/object:A\nx: !ruby/object:B\n  y: 1\n") }
t("nested-inner")      { YAML.safe_load("--- !ruby/object:A\nx: !ruby/object:B\n  y: 1\n", permitted_classes: ["A"]) }
t("plain")             { YAML.safe_load("a: [1, 2.5, true, null, 'x']\n") }
t("unknown-tag")       { YAML.safe_load("--- !foo\nfoo: bar\n") }
t("empty-doc")         { YAML.safe_load("") }
t("rescued")           { begin; YAML.safe_load(Y); rescue Psych::DisallowedClass; "RESCUE-FIRED"; end }
`

// safeLoadOracleWant is MRI 4.0.5's verbatim stdout for safeLoadOracleSrc.
const safeLoadOracleWant = `bare => DisallowedClass: Tried to load unspecified class: Gadget
empty => DisallowedClass: Tried to load unspecified class: Gadget
other => DisallowedClass: Tried to load unspecified class: Gadget
exact => Gadget
exact-string => Gadget
symbol => DisallowedClass: Tried to load unspecified class: Symbol
symbol-ok => Symbol
symbol-key => DisallowedClass: Tried to load unspecified class: Symbol
symbol-key-ok => Hash
symbol-seq => DisallowedClass: Tried to load unspecified class: Symbol
symbol-tag => DisallowedClass: Tried to load unspecified class: Symbol
psym-only => DisallowedClass: Tried to load unspecified class: Symbol
psym-allowed => Symbol
psym-refused => DisallowedClass: Tried to load unspecified class: Symbol
psym-empty => Symbol
time => DisallowedClass: Tried to load unspecified class: Time
time-ok => Time
time-in-map => DisallowedClass: Tried to load unspecified class: Time
regexp => DisallowedClass: Tried to load unspecified class: Regexp
regexp-ok => Regexp
range => DisallowedClass: Tried to load unspecified class: Range
range-ok => Range
range-inner => DisallowedClass: Tried to load unspecified class: Symbol
class-tag => DisallowedClass: Tried to load unspecified class: String
class-tag-ok => Class
nested-outer-wins => DisallowedClass: Tried to load unspecified class: A
nested-inner => DisallowedClass: Tried to load unspecified class: B
plain => Hash
unknown-tag => Hash
empty-doc => NilClass
rescued => String
`

// TestYAMLSafeLoadMatchesMRI compares rbgo's safe_load to MRI byte for byte
// over the whole restriction surface: the permitted_classes allow-list, Symbol
// (in scalar, key and sequence position, and via !ruby/symbol),
// permitted_symbols, Time, Regexp, Range, !ruby/class, and nesting order.
func TestYAMLSafeLoadMatchesMRI(t *testing.T) {
	got := eval(t, safeLoadOracleSrc)
	if got != safeLoadOracleWant {
		t.Errorf("safe_load diverges from MRI 4.0.5\n--- got ---\n%s--- want ---\n%s", got, safeLoadOracleWant)
		gl, wl := strings.Split(got, "\n"), strings.Split(safeLoadOracleWant, "\n")
		for i := 0; i < len(gl) && i < len(wl); i++ {
			if gl[i] != wl[i] {
				t.Errorf("line %d:\n got  %q\n want %q", i+1, gl[i], wl[i])
			}
		}
	}
}

// TestYAMLSafeLoadEmptyPermittedClassesRaises is the case both of the binding's
// causes got wrong and the one nobody tries by hand: permitted_classes: [] is
// the most restrictive setting the API can express, and it used to permit
// everything. It is kept apart from the oracle table so that a regression here
// names itself instead of appearing as one line of a diff.
//
// Both spellings must refuse. They were ONE bug in MRI's terms -- absent and
// empty are the same policy there (psych.rb:323) -- but two in rbgo's: the
// kwarg reader returned nil for "absent", and the loader call dropped the
// option for "empty" (`if len(permitted) > 0`), so fixing either alone would
// have left the other spelling wide open.
func TestYAMLSafeLoadEmptyPermittedClassesRaises(t *testing.T) {
	const gadget = `--- !ruby/object:Gadget\nfoo: bar\n`
	for _, call := range []struct{ name, args string }{
		{"absent", ""},
		{"empty", ", permitted_classes: []"},
		{"empty with symbols", ", permitted_classes: [], permitted_symbols: [:foo]"},
		{"other class only", ", permitted_classes: [Allowed]"},
	} {
		src := `require "yaml"
class Gadget; attr_accessor :foo; end
class Allowed; end
begin
  v = YAML.safe_load("` + gadget + `"` + call.args + `)
  puts "NO RAISE: #{v.class}"
rescue Psych::DisallowedClass => e
  puts "refused: #{e.message}"
end`
		if got := eval(t, src); got != "refused: Tried to load unspecified class: Gadget\n" {
			t.Errorf("%s: got %q", call.name, got)
		}
	}
}

// TestYAMLSafeLoadDisallowedClassIsRescuable guards the constant itself.
// Psych::DisallowedClass existed in the constant tree and was raised nowhere,
// so `rescue Psych::DisallowedClass` was decorative. It stayed decorative even
// once it WAS raised, because exceptionObject resolved an internal raise's class
// name through the flat vm.consts table and a namespaced name is not there --
// the exception arrived as a bare StandardError. Both halves are checked here:
// the specific rescue fires, and the object's own class and ancestry agree.
func TestYAMLSafeLoadDisallowedClassIsRescuable(t *testing.T) {
	src := `require "yaml"
begin
  YAML.safe_load("--- !ruby/object:Gadget\nfoo: bar\n")
  puts "NO RAISE"
rescue Psych::DisallowedClass => e
  puts "rescued"
  puts e.class
  puts e.class.ancestors.include?(Psych::Exception)
  puts e.is_a?(StandardError)
  puts e.message
end`
	want := "rescued\nPsych::DisallowedClass\ntrue\ntrue\nTried to load unspecified class: Gadget\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestYAMLSafeLoadPsychSyntaxErrorIsRescuable covers the other half of the
// namespaced-name fix. rbgo has raised Psych::SyntaxError for a malformed
// document all along, and `rescue Psych::SyntaxError` could not catch it for the
// same reason -- so a caller's only option was a bare `rescue`. This is the
// positive control for the errorClass walk: if that walk regressed, the oracle
// test above would still pass (its rows go through a bare rescue), and this
// would not.
func TestYAMLSafeLoadPsychSyntaxErrorIsRescuable(t *testing.T) {
	src := `require "yaml"
begin
  YAML.load("a:\n\t- 1\n")
  puts "NO RAISE"
rescue Psych::SyntaxError => e
  puts "rescued #{e.class}"
end`
	if got := eval(t, src); !strings.HasPrefix(got, "rescued Psych::SyntaxError") {
		t.Errorf("got %q", got)
	}
}

// TestYAMLSafeLoadNamesTheFirstOffender pins WHICH class the message names when
// a document has several. An *Object's ivars live in a Go map, so walking them
// in map order would make the message depend on Go's randomised iteration and
// the test flake; yamlIVarOrder sorts them. Two disallowed ivars, repeated, must
// report the same one every time.
//
// It reports "Zed", the FIRST in document order, which is MRI's answer too: the
// engine fills Object.Order from the document, and yamlIVarOrder honours it
// before falling back to a lexicographic sort (a lexicographic walk would have
// said "Able").
func TestYAMLSafeLoadNamesTheFirstOffender(t *testing.T) {
	src := `require "yaml"
class Keeper; end
20.times do
  begin
    YAML.safe_load("--- !ruby/object:Keeper\nzz: !ruby/object:Zed\n  a: 1\naa: !ruby/object:Able\n  b: 2\n",
                   permitted_classes: [Keeper])
  rescue Psych::DisallowedClass => e
    print e.message.split(": ").last, " "
  end
end
puts`
	got := eval(t, src)
	for _, f := range strings.Fields(got) {
		if f != "Zed" {
			t.Fatalf("ivar walk is not deterministic: %q", got)
		}
	}
	if len(strings.Fields(got)) != 20 {
		t.Errorf("got %q", got)
	}
}

// TestYAMLSafeLoadFileRestricts checks safe_load_file shares the restriction:
// it is the same binding, and a file is the likelier place for a document a
// caller did not write.
func TestYAMLSafeLoadFileRestricts(t *testing.T) {
	path := tmpDirSlash(t) + "/doc.yaml"
	writeFileT(t, path, "--- !ruby/object:Gadget\nfoo: bar\n")
	src := `require "yaml"
class Gadget; end
begin
  puts YAML.safe_load_file(` + rubyStr(path) + `).class
rescue Psych::DisallowedClass => e
  puts "refused: #{e.message}"
end
puts YAML.safe_load_file(` + rubyStr(path) + `, permitted_classes: [Gadget]).class
puts YAML.load_file(` + rubyStr(path) + `).class`
	want := "refused: Tried to load unspecified class: Gadget\nGadget\nGadget\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestYAMLLoadStaysUnrestricted checks the fix did not leak into the unsafe
// entry points. load / unsafe_load are documented as unsafe and Puppet's
// persistence calls them; a restriction there would be a different bug.
func TestYAMLLoadStaysUnrestricted(t *testing.T) {
	src := `require "yaml"
class Gadget; attr_accessor :foo; end
puts YAML.load("--- !ruby/object:Gadget\nfoo: bar\n").class
puts YAML.unsafe_load("--- !ruby/object:Gadget\nfoo: bar\n").foo
puts YAML.load(":sym").class
puts YAML.load("--- !ruby/regexp /ab/i\n").class`
	want := "Gadget\nbar\nSymbol\nRegexp\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestYAMLSafeLoadRefusalDefinesNoConstant guards a second effect of the same
// defect, which the issue did not mention. yamlResolveClass REGISTERS a
// placeholder class when a `!ruby/object:` names one the program never defined
// -- so before the restriction was enforced, loading an untrusted document
// injected attacker-named constants into the global namespace as a side effect,
// on top of instantiating them.
//
// Running the permission check before fromYAML is what prevents it: no Ruby
// class is created, and no Ruby method runs, for a document that is refused.
// MRI defines nothing either (measured).
func TestYAMLSafeLoadRefusalDefinesNoConstant(t *testing.T) {
	src := `require "yaml"
begin; YAML.safe_load("--- !ruby/object:NeverHeardOfIt\nx: 1\n"); rescue Psych::DisallowedClass; end
begin; YAML.safe_load("--- !ruby/class 'AlsoUnheardOf'\n"); rescue Psych::DisallowedClass; end
begin; YAML.safe_load("--- !ruby/module 'StillUnheardOf'\n"); rescue Psych::DisallowedClass; end
puts Object.const_defined?(:NeverHeardOfIt)
puts Object.const_defined?(:AlsoUnheardOf)
puts Object.const_defined?(:StillUnheardOf)`
	if got := eval(t, src); got != "false\nfalse\nfalse\n" {
		t.Errorf("a refused document defined a constant: got %q", got)
	}
}

// TestYAMLSafeLoadNamelessObjectIsObject covers checkObject's empty-class arm.
// A `!ruby/object:` tag with no class name after the colon loads as an object
// whose Class is "", and it is gated as "Object" -- which is what Psych reports:
// ClassLoader#load returns nil for an empty name (class_loader.rb:27) and
// to_ruby.rb:244 falls back to `class_loader.object`.
//
// The coverage gate named this arm, so the first question was whether anything
// can reach it rather than how to cover it. It can: the engine's loader yields
// Class:"" for the bare-colon spelling (and "Object" for `!ruby/object` with no
// colon at all), so both spellings are checked here. Measured against MRI
// 4.0.5, which answers "Tried to load unspecified class: Object" for both.
func TestYAMLSafeLoadNamelessObjectIsObject(t *testing.T) {
	for _, tag := range []string{`--- !ruby/object\nfoo: 1\n`, `--- !ruby/object:\nfoo: 1\n`} {
		src := `require "yaml"
begin
  puts YAML.safe_load("` + tag + `").class
rescue Psych::DisallowedClass => e
  puts "refused: #{e.message}"
end
puts YAML.safe_load("` + tag + `", permitted_classes: [Object]).class`
		want := "refused: Tried to load unspecified class: Object\nObject\n"
		if got := eval(t, src); got != want {
			t.Errorf("tag %q: got %q want %q", tag, got, want)
		}
	}
}
