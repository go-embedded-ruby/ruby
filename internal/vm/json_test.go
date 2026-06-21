package vm_test

import (
	"strings"
	"testing"
)

// TestJSON covers the JSON module (generate/dump/pretty_generate/parse) and
// Object#to_json, asserted against MRI Ruby 4.0.5 (with `require "json"`).
func TestJSON(t *testing.T) {
	cases := []struct{ src, want string }{
		// generate.
		{`p JSON.generate({"a" => 1, "b" => [1, 2, 3]})`, "\"{\\\"a\\\":1,\\\"b\\\":[1,2,3]}\"\n"},
		{`p JSON.generate([1, 2.5, true, false, nil, "x"])`, "\"[1,2.5,true,false,null,\\\"x\\\"]\"\n"},
		{`p JSON.generate(42)`, "\"42\"\n"},
		{`p JSON.generate(2.5)`, "\"2.5\"\n"},
		{`p JSON.dump({"k" => "v"})`, "\"{\\\"k\\\":\\\"v\\\"}\"\n"},
		{`p JSON.generate(9999999999999999999999)`, "\"9999999999999999999999\"\n"},
		// Object#to_json over the value types (symbol/integer hash keys, ranges).
		{`p [1, {"x" => "y"}].to_json`, "\"[1,{\\\"x\\\":\\\"y\\\"}]\"\n"},
		{`p({a: 1, b: 2}.to_json)`, "\"{\\\"a\\\":1,\\\"b\\\":2}\"\n"},
		{`p({1 => 2}.to_json)`, "\"{\\\"1\\\":2}\"\n"},
		{`p :sym.to_json`, "\"\\\"sym\\\"\"\n"},
		{`p((1..2).to_json)`, "\"\\\"1..2\\\"\"\n"}, // fallback: to_s of an unknown value
		// String escaping (real control bytes via chr, sidestepping string-literal
		// escapes): backspace/formfeed and a low control char.
		{`p JSON.generate("a\"b\\c\nd\re\tf")`, "\"\\\"a\\\\\\\"b\\\\\\\\c\\\\nd\\\\re\\\\tf\\\"\"\n"},
		{`p JSON.generate(8.chr + 12.chr)`, "\"\\\"\\\\b\\\\f\\\"\"\n"},
		{`p JSON.generate(1.chr)`, "\"\\\"\\\\u0001\\\"\"\n"},
		// parse: types, nesting, key order, int-vs-float.
		{`p JSON.parse("{\"a\":1,\"b\":[true,null,2.5]}")`, "{\"a\" => 1, \"b\" => [true, nil, 2.5]}\n"},
		{`p JSON.parse("[1, 2.5, 1e3]")`, "[1, 2.5, 1000.0]\n"},
		{`p JSON.parse("{\"b\":1,\"a\":2}")`, "{\"b\" => 1, \"a\" => 2}\n"}, // order preserved
		{`p JSON.parse("\"hello\"")`, "\"hello\"\n"},
		{`p JSON.parse("true")`, "true\n"},
		{`p JSON.parse("null")`, "nil\n"},
		{`p JSON.parse("[]")`, "[]\n"},
		{`p JSON.parse("{}")`, "{}\n"},
		// Round-trip.
		{`p JSON.parse(JSON.generate({"n" => [1, 2], "s" => "hi"}))`, "{\"n\" => [1, 2], \"s\" => \"hi\"}\n"},
		// pretty_generate.
		{`puts JSON.pretty_generate({"a" => 1, "b" => [1, 2]})`, "{\n  \"a\": 1,\n  \"b\": [\n    1,\n    2\n  ]\n}\n"},
		{`puts JSON.pretty_generate([])`, "[]\n"},
		{`puts JSON.pretty_generate({})`, "{}\n"},
		{`puts JSON.pretty_generate(5)`, "5\n"},
	}
	for _, c := range cases {
		if got := eval(t, "require \"json\"\n"+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Errors: malformed input and non-String argument, and Infinity/NaN.
	errs := []struct{ src, want string }{
		{`JSON.parse("]")`, "unexpected token"},
		{`JSON.parse("{1:2}")`, "unexpected token"},
		{`JSON.parse("")`, "unexpected token"},
		{`JSON.parse("1 2")`, "unexpected token"},
		{`JSON.parse(123)`, "no implicit conversion"},
		{`JSON.generate(1.0 / 0)`, "not allowed in JSON"},
	}
	for _, c := range errs {
		if err := runErr(t, "require \"json\"\n"+c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
