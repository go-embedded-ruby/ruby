// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestAddressableConstants covers the Addressable module and its classes
// (require "addressable/uri").
func TestAddressableConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "addressable/uri"; p Addressable.is_a?(Module)`, "true\n"},
		{`p require "addressable/uri"`, "true\n"},
		{`require "addressable/uri"; p require "addressable/uri"`, "false\n"},
		{`require "addressable/uri"; p Addressable::URI.is_a?(Class)`, "true\n"},
		{`require "addressable/uri"; p Addressable::Template.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestAddressableURI covers Addressable::URI.parse and the component readers.
func TestAddressableURI(t *testing.T) {
	pre := `require "addressable/uri"; u = Addressable::URI.parse("http://user@example.com:8080/path?a=1&b=2#frag"); `
	cases := []struct{ src, want string }{
		{pre + `p u.scheme`, "\"http\"\n"},
		{pre + `p u.host`, "\"example.com\"\n"},
		{pre + `p u.path`, "\"/path\"\n"},
		{pre + `p u.port`, "8080\n"},
		{pre + `p u.userinfo`, "\"user\"\n"},
		{pre + `p u.query`, "\"a=1&b=2\"\n"},
		{pre + `p u.fragment`, "\"frag\"\n"},
		{pre + `p u.query_values`, "{\"a\" => \"1\", \"b\" => \"2\"}\n"},
		{pre + `p u.to_s`, "\"http://user@example.com:8080/path?a=1&b=2#frag\"\n"},
		{pre + `p u.class.name`, "\"Addressable::URI\"\n"},
		{pre + `p u.inspect.start_with?("#<Addressable::URI")`, "true\n"},
		// A URI with no port / query returns nil for those components.
		{`require "addressable/uri"; u = Addressable::URI.parse("http://example.com/"); p u.port`, "nil\n"},
		{`require "addressable/uri"; u = Addressable::URI.parse("http://example.com/"); p u.query`, "nil\n"},
		{`require "addressable/uri"; u = Addressable::URI.parse("http://example.com/"); p u.query_values`, "nil\n"},
		{`require "addressable/uri"; u = Addressable::URI.parse("http://example.com/"); p u.fragment`, "nil\n"},
		// parse(nil) returns nil.
		{`require "addressable/uri"; p Addressable::URI.parse(nil)`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestAddressableNormalizeJoin covers #normalize and #join.
func TestAddressableNormalizeJoin(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "addressable/uri"; p Addressable::URI.parse("HTTP://Example.COM/a/../b").normalize.to_s`, "\"http://example.com/b\"\n"},
		{`require "addressable/uri"; p Addressable::URI.parse("http://example.com/a/b").join("../c").to_s`, "\"http://example.com/c\"\n"},
		// join accepts another Addressable::URI as the reference.
		{`require "addressable/uri"; base = Addressable::URI.parse("http://example.com/a/"); ref = Addressable::URI.parse("c"); p base.join(ref).to_s`, "\"http://example.com/a/c\"\n"},
		{`require "addressable/uri"; p Addressable::URI.parse("http://example.com/").normalize.class.name`, "\"Addressable::URI\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestAddressableTemplate covers Addressable::Template#expand and #extract.
func TestAddressableTemplate(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "addressable/uri"; p Addressable::Template.new("http://example.com/{name}").pattern`, "\"http://example.com/{name}\"\n"},
		{`require "addressable/uri"; p Addressable::Template.new("http://example.com/{name}").to_s`, "\"http://example.com/{name}\"\n"},
		{`require "addressable/uri"; p Addressable::Template.new("http://example.com/{name}").expand("name" => "bob").to_s`, "\"http://example.com/bob\"\n"},
		// A Symbol key works as a variable name.
		{`require "addressable/uri"; p Addressable::Template.new("http://example.com/{name}").expand(name: "bob").to_s`, "\"http://example.com/bob\"\n"},
		// A list variable expands per RFC 6570.
		{`require "addressable/uri"; p Addressable::Template.new("http://example.com{/list*}").expand("list" => ["a", "b"]).to_s`, "\"http://example.com/a/b\"\n"},
		{`require "addressable/uri"; p Addressable::Template.new("http://example.com/{name}").extract("http://example.com/bob")`, "{\"name\" => \"bob\"}\n"},
		// A list variable extracts as an Array of Strings.
		{`require "addressable/uri"; p Addressable::Template.new("http://example.com{/list*}").extract("http://example.com/a/b")`, "{\"list\" => [\"a\", \"b\"]}\n"},
		{`require "addressable/uri"; p Addressable::Template.new("http://example.com/{name}").class.name`, "\"Addressable::Template\"\n"},
		{`require "addressable/uri"; p Addressable::Template.new("x").inspect.start_with?("#<Addressable::Template")`, "true\n"},
		// A URI that does not match the template extracts nil.
		{`require "addressable/uri"; p Addressable::Template.new("http://other.example/{name}").extract("http://example.com/bob")`, "nil\n"},
		// extract accepts an Addressable::URI as its argument.
		{`require "addressable/uri"; u = Addressable::URI.parse("http://example.com/bob"); p Addressable::Template.new("http://example.com/{name}").extract(u)`, "{\"name\" => \"bob\"}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestAddressableErrors covers the arity and type paths.
func TestAddressableErrors(t *testing.T) {
	// No-argument calls raise ArgumentError.
	for _, call := range []string{
		`Addressable::URI.parse`,
		`Addressable::Template.new`,
		`Addressable::Template.new("x").expand`,
		`Addressable::Template.new("x").extract`,
		`Addressable::URI.parse("http://x/").join`,
	} {
		src := `require "addressable/uri"
begin
  ` + call + `
rescue ArgumentError
  puts "arity"
end`
		if got := eval(t, src); !strings.Contains(got, "arity") {
			t.Errorf("%s no-arg: got %q", call, got)
		}
	}
	// expand with a non-Hash variables argument raises TypeError.
	got := eval(t, `require "addressable/uri"
begin
  Addressable::Template.new("http://x/{n}").expand("nope")
rescue TypeError
  puts "typeerr"
end`)
	if !strings.Contains(got, "typeerr") {
		t.Errorf("expand non-hash: got %q", got)
	}
}
