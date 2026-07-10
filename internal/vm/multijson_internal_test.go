// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// mjRun runs a Ruby program with `require "multi_json"` prepended.
func mjRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"multi_json\"\n"+body)
}

// TestMultiJsonLoadPreservesOrder proves MultiJson.load parses through rbgo's
// ordered JSON: the object keeps source key order (not sorted) and dump round-trips
// it back to the same document.
func TestMultiJsonLoadPreservesOrder(t *testing.T) {
	got := mjRun(t, `
h = MultiJson.load('{"b":1,"a":2,"c":3}')
puts h.inspect
puts h.keys.inspect
puts MultiJson.dump(h)
puts MultiJson.decode('[3,1,2]').inspect
puts MultiJson.encode({"x" => 1})
`)
	want := `{"b" => 1, "a" => 2, "c" => 3}` + "\n" +
		`["b", "a", "c"]` + "\n" +
		`{"b":1,"a":2,"c":3}` + "\n" +
		`[3, 1, 2]` + "\n" +
		`{"x":1}`
	if got != want {
		t.Fatalf("order:\n got=%q\nwant=%q", got, want)
	}
}

// TestMultiJsonSymbolizeKeys covers the :symbolize_keys / :symbolize_names options
// (per-call and via default_options) yielding Symbol keys, and the :pretty dump
// layout.
func TestMultiJsonSymbolizeKeys(t *testing.T) {
	got := mjRun(t, `
puts MultiJson.load('{"a":1}', symbolize_keys: true).inspect
puts MultiJson.load('{"a":1}', symbolize_names: true).inspect
puts MultiJson.load('{"a":1}').inspect
MultiJson.default_options = {symbolize_keys: true}
puts MultiJson.default_options.inspect
puts MultiJson.load('{"z":9}').inspect
puts MultiJson.load('{"z":9}', symbolize_keys: false).inspect
puts MultiJson.dump({"a" => [1, 2]}, pretty: true)
`)
	want := "{a: 1}\n{a: 1}\n{\"a\" => 1}\n{symbolize_keys: true}\n{z: 9}\n{\"z\" => 9}\n" +
		"{\n  \"a\": [\n    1,\n    2\n  ]\n}"
	if got != want {
		t.Fatalf("symbolize:\n got=%q\nwant=%q", got, want)
	}
}

// TestMultiJsonAdapters covers the adapter surface: the default adapter name, use
// / adapter= selection over the eight registry names, with_adapter's scoped switch
// with restore, and current_adapter honouring a per-call :adapter override.
func TestMultiJsonAdapters(t *testing.T) {
	got := mjRun(t, `
puts MultiJson.adapter
MultiJson.use("oj")
puts MultiJson.adapter
MultiJson.adapter = "yajl"
puts MultiJson.adapter
puts MultiJson.current_adapter
puts MultiJson.current_adapter(adapter: "gson")
r = MultiJson.with_adapter("json_pure") { MultiJson.adapter }
puts r
puts MultiJson.adapter
`)
	want := "json_gem\noj\nyajl\nyajl\ngson\njson_pure\nyajl"
	if got != want {
		t.Fatalf("adapters:\n got=%q\nwant=%q", got, want)
	}
}

// TestMultiJsonParseError covers a malformed document raising MultiJson::ParseError
// (aliased as LoadError), with #data carrying the input and #cause the underlying
// message, and a value-with-adapter-option load still succeeding.
func TestMultiJsonParseError(t *testing.T) {
	got := mjRun(t, `
begin
  MultiJson.load("{not json")
rescue MultiJson::ParseError => e
  puts e.class
  puts e.data
  puts(e.cause.nil? ? "nil-cause" : "has-cause")
end
begin
  MultiJson.load("nope")
rescue MultiJson::LoadError
  puts "load-error"
end
puts MultiJson.load('{"k":1}', adapter: "oj").inspect
`)
	want := "MultiJson::ParseError\n{not json\nhas-cause\nload-error\n{\"k\" => 1}"
	if got != want {
		t.Fatalf("parse error:\n got=%q\nwant=%q", got, want)
	}
}

// TestMultiJsonAdapterError covers an unknown adapter name raising
// MultiJson::AdapterError (an ArgumentError) from use, adapter=, with_adapter,
// current_adapter and a per-call :adapter override on load / dump.
func TestMultiJsonAdapterError(t *testing.T) {
	cases := []string{
		`MultiJson.use("nope")`,
		`MultiJson.adapter = "nope"`,
		`MultiJson.with_adapter("nope") { 1 }`,
		`MultiJson.current_adapter(adapter: "nope")`,
		`MultiJson.load("{}", adapter: "nope")`,
		`MultiJson.dump({}, adapter: "nope")`,
	}
	for _, expr := range cases {
		got := mjRun(t, "begin; "+expr+"; rescue MultiJson::AdapterError => e; puts e.class; puts(e.is_a?(ArgumentError)); end")
		if got != "MultiJson::AdapterError\ntrue" {
			t.Fatalf("%s expected AdapterError(ArgumentError), got %q", expr, got)
		}
	}
}

// TestMultiJsonArgErrors covers the ArgumentError / TypeError guards: a missing
// argument on load / dump / use / adapter= / with_adapter / default_options=, a
// non-String load, and default_options= ignoring a non-Hash while a bare
// default_options reads an (empty) Hash.
func TestMultiJsonArgErrors(t *testing.T) {
	argErr := []string{
		"MultiJson.load",
		"MultiJson.dump",
		"MultiJson.send(:use)",
		"MultiJson.send(:adapter=)",
		"MultiJson.with_adapter { 1 }",
		"MultiJson.send(:default_options=)",
	}
	for _, expr := range argErr {
		got := mjRun(t, "begin; "+expr+"; rescue ArgumentError; puts \"arg\"; end")
		if got != "arg" {
			t.Fatalf("%s expected ArgumentError, got %q", expr, got)
		}
	}
	got := mjRun(t, `
begin
  MultiJson.load(42)
rescue TypeError
  puts "type"
end
MultiJson.default_options = 7
puts MultiJson.default_options.inspect
`)
	if got != "type\n{}" {
		t.Fatalf("guards: got=%q", got)
	}
}
