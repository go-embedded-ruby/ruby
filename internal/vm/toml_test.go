// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestTOMLConstants covers the TOML/TomlRB loadable module and its error tree
// (require "toml"). TOML and TomlRB name the same module object.
func TestTOMLConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "toml"; p TOML.equal?(TomlRB)`, "true\n"},
		{`require "toml"; p TomlRB.is_a?(Module)`, "true\n"},
		{`p require "toml"`, "true\n"},
		{`require "toml"; p require "toml"`, "false\n"},
		{`require "toml"; p TomlRB::ParseError < StandardError`, "true\n"},
		{`require "toml"; p TomlRB::ValueOverwriteError < StandardError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTOMLParse covers TOML.parse across every scalar and structural shape the
// binding maps back into the rbgo object graph: strings, integers, floats,
// booleans, arrays, nested tables (Hash with String keys), and the four TOML
// datetime shapes collapsing onto Ruby Time.
func TestTOMLParse(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "toml"; p TOML.parse('s = "hi"')`, "{\"s\" => \"hi\"}\n"},
		{`require "toml"; p TOML.parse("n = 42")`, "{\"n\" => 42}\n"},
		{`require "toml"; p TOML.parse("f = 2.5")`, "{\"f\" => 2.5}\n"},
		{`require "toml"; p TOML.parse("b = true")`, "{\"b\" => true}\n"},
		{`require "toml"; p TOML.parse("a = [1, 2, 3]")`, "{\"a\" => [1, 2, 3]}\n"},
		// A nested table parses to a Hash with String keys.
		{`require "toml"; p TOML.parse("[srv]\nip = \"1.2.3.4\"")`, "{\"srv\" => {\"ip\" => \"1.2.3.4\"}}\n"},
		// TomlRB.parse is the same entry point.
		{`require "toml"; p TomlRB.parse("n = 1")`, "{\"n\" => 1}\n"},
		// Offset date-time -> Time.
		{`require "toml"; p TOML.parse("d = 1979-05-27T07:32:00Z")["d"].class`, "Time\n"},
		{`require "toml"; p TOML.parse("d = 1979-05-27T07:32:00Z")["d"].to_i`, "296638320\n"},
		// Local date-time -> Time (materialised UTC).
		{`require "toml"; p TOML.parse("d = 1979-05-27T07:32:00")["d"].class`, "Time\n"},
		// Local date -> Time at 00:00 UTC.
		{`require "toml"; p TOML.parse("d = 1979-05-27")["d"].class`, "Time\n"},
		// Local time -> Time on the epoch date.
		{`require "toml"; p TOML.parse("d = 07:32:00")["d"].class`, "Time\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTOMLDumpRoundTrip covers TOML.dump by round-tripping a value through
// parse(dump(x)), exercising the rbgo->library mapping for every scalar and
// structural shape (String, Symbol-as-value/key, Integer, Bignum, Float, bool,
// Array, nested Hash, Time).
func TestTOMLDumpRoundTrip(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "toml"; p TOML.parse(TOML.dump({"s" => "hi"}))`, "{\"s\" => \"hi\"}\n"},
		{`require "toml"; p TOML.parse(TOML.dump({"n" => 42}))`, "{\"n\" => 42}\n"},
		{`require "toml"; p TOML.parse(TOML.dump({"f" => 2.5}))`, "{\"f\" => 2.5}\n"},
		{`require "toml"; p TOML.parse(TOML.dump({"b" => false}))`, "{\"b\" => false}\n"},
		{`require "toml"; p TOML.parse(TOML.dump({"a" => [1, 2]}))`, "{\"a\" => [1, 2]}\n"},
		// A Symbol key and a Symbol value both render as their name.
		{`require "toml"; p TOML.parse(TOML.dump({sym: "v"}))`, "{\"sym\" => \"v\"}\n"},
		{`require "toml"; p TOML.parse(TOML.dump({"k" => :val}))`, "{\"k\" => \"val\"}\n"},
		// A Bignum still within TOML's integer range round-trips.
		{`require "toml"; n = 2 ** 62; p TOML.parse(TOML.dump({"n" => n}))["n"] == n`, "true\n"},
		// A nested Hash round-trips as a table.
		{`require "toml"; p TOML.parse(TOML.dump({"srv" => {"port" => 80}}))`, "{\"srv\" => {\"port\" => 80}}\n"},
		// TomlRB.dump is the same entry point.
		{`require "toml"; p TomlRB.parse(TomlRB.dump({"n" => 1}))`, "{\"n\" => 1}\n"},
		// A Time dumps and parses back to a Time with the same instant.
		{`require "toml"; t = Time.at(296638320); p TOML.parse(TOML.dump({"d" => t}))["d"].to_i`, "296638320\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTOMLErrors covers the error paths: a malformed document and an
// unrepresentable dump value both raise, as does calling with no argument.
func TestTOMLErrors(t *testing.T) {
	// A syntax error raises ArgumentError (toml-rb's ParseError family).
	got := eval(t, `require "toml"
begin
  TOML.parse("= bad")
rescue ArgumentError
  puts "parseerr"
end`)
	if !strings.Contains(got, "parseerr") {
		t.Errorf("parse of bad doc: got %q", got)
	}
	// Dumping a value with no TOML representation (a Proc) raises ArgumentError.
	got = eval(t, `require "toml"
begin
  TOML.dump({"k" => proc {}})
rescue ArgumentError
  puts "dumperr"
end`)
	if !strings.Contains(got, "dumperr") {
		t.Errorf("dump of Proc: got %q", got)
	}
	// No argument raises ArgumentError for each entry point.
	for _, m := range []string{"parse", "dump", "load_file"} {
		src := `require "toml"
begin
  TOML.` + m + `
rescue ArgumentError
  puts "arity"
end`
		if got := eval(t, src); !strings.Contains(got, "arity") {
			t.Errorf("%s no-arg: got %q", m, got)
		}
	}
	// load_file on a missing path raises Errno::ENOENT.
	got = eval(t, `require "toml"
begin
  TOML.load_file("/no/such/toml/file.toml")
rescue Errno::ENOENT
  puts "enoent"
end`)
	if !strings.Contains(got, "enoent") {
		t.Errorf("load_file missing: got %q", got)
	}
}

// TestTOMLLoadFile covers TOML.load_file reading and parsing a real file.
func TestTOMLLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/c.toml"
	if err := os.WriteFile(path, []byte("title = \"x\"\n[server]\nport = 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `require "toml"
m = TOML.load_file(` + strconv.Quote(path) + `)
p m["title"]
p m["server"]["port"]`
	want := "\"x\"\n8080\n"
	if got := eval(t, src); got != want {
		t.Errorf("load_file: got %q want %q", got, want)
	}
}
