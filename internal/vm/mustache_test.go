// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestMustacheModule covers the Mustache loadable class (require "mustache") and
// its error tree.
func TestMustacheModule(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "mustache"; p Mustache.is_a?(Class)`, "true\n"},
		{`p require "mustache"`, "true\n"},
		{`require "mustache"; p require "mustache"`, "false\n"},
		{`require "mustache"; p Mustache::Error < StandardError`, "true\n"},
		{`require "mustache"; p Mustache::ParseError < Mustache::Error`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMustacheRender covers Mustache.render across the context value shapes the
// binding maps into the library model: String/Symbol-keyed Hashes, Arrays
// (sections), booleans, integers, floats, nil and a fallback object #to_s.
func TestMustacheRender(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "mustache"; print Mustache.render("Hi {{name}}", {"name" => "Al"})`, "Hi Al"},
		// A Symbol-keyed Hash resolves by name too.
		{`require "mustache"; print Mustache.render("Hi {{name}}", {name: "Al"})`, "Hi Al"},
		// A section over an Array iterates.
		{`require "mustache"; print Mustache.render("{{#xs}}{{.}}{{/xs}}", {xs: [1,2,3]})`, "123"},
		// A truthy/falsey section.
		{`require "mustache"; print Mustache.render("{{#on}}Y{{/on}}{{^on}}N{{/on}}", {on: true})`, "Y"},
		{`require "mustache"; print Mustache.render("{{#on}}Y{{/on}}{{^on}}N{{/on}}", {on: false})`, "N"},
		// An integer / float / nil value stringifies with Ruby to_s.
		{`require "mustache"; print Mustache.render("{{n}}", {n: 42})`, "42"},
		{`require "mustache"; print Mustache.render("{{n}}", {n: 2.5})`, "2.5"},
		{`require "mustache"; print Mustache.render("[{{n}}]", {n: nil})`, "[]"},
		// A big integer (Bignum) value.
		{`require "mustache"; print Mustache.render("{{n}}", {n: 10**20})`, "100000000000000000000"},
		// A nested Hash resolves dotted-section access.
		{`require "mustache"; print Mustache.render("{{#a}}{{b}}{{/a}}", {a: {b: "x"}})`, "x"},
		// No context (nil) renders literal text and empty interpolations.
		{`require "mustache"; print Mustache.render("hi {{x}}")`, "hi "},
		// An arbitrary object value falls back to its #to_s.
		{`require "mustache"
			class Widget; def to_s; "W"; end; end
			print Mustache.render("{{w}}", {w: Widget.new})`, "W"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMustacheLambda covers a Proc context value bound as a Mustache section
// lambda: the block receives the unrendered section body and its result is
// rendered.
func TestMustacheLambda(t *testing.T) {
	src := `require "mustache"
	print Mustache.render("{{#wrap}}x{{/wrap}}", {wrap: ->(body) { "[" + body + "]" }})`
	if got := eval(t, src); got != "[x]" {
		t.Errorf("got=%q want=[x]", got)
	}
}

// TestMustacheInstance covers the class-based view API: Mustache.new(template),
// #template / #template=, and #render (explicit template and against the
// instance's own ivars).
func TestMustacheInstance(t *testing.T) {
	cases := []struct{ src, want string }{
		// new(template) then render(nil, context).
		{`require "mustache"
			m = Mustache.new("Hi {{name}}")
			print m.render(nil, {name: "Al"})`, "Hi Al"},
		// template accessor round-trips.
		{`require "mustache"
			m = Mustache.new("A")
			print m.template`, "A"},
		// template= updates the source; render with no args uses it.
		{`require "mustache"
			m = Mustache.new
			m.template = "lit"
			print m.render`, "lit"},
		// template is nil before it is set.
		{`require "mustache"; p Mustache.new.template`, "nil\n"},
		// render against the instance's own ivars (a Mustache subclass view).
		{`require "mustache"
			m = Mustache.new("Hi {{@who}}")
			m.instance_variable_set(:@who, "Bo")
			print m.render("Hi {{who}}", {who: "Bo"})`, "Hi Bo"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMustacheStringArg covers a non-String template argument (coerced via to_s)
// and an Integer Hash key (coerced via to_s in mustacheKey).
func TestMustacheStringArg(t *testing.T) {
	cases := []struct{ src, want string }{
		// A Symbol template renders as its literal name text.
		{`require "mustache"; print Mustache.render(:literal, {})`, "literal"},
		// An Integer Hash key stringifies; the name resolves against it.
		{`require "mustache"; print Mustache.render("{{1}}", {1 => "one"})`, "one"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMustacheParseError covers a malformed template raising Mustache::ParseError.
func TestMustacheParseError(t *testing.T) {
	src := `require "mustache"
begin
  Mustache.render("{{#open}}no close", {})
rescue Mustache::ParseError
  print "parseerror"
end`
	if got := eval(t, src); got != "parseerror" {
		t.Errorf("got=%q want=parseerror", got)
	}
}

// TestMustacheArity covers the wrong-number-of-arguments guard on Mustache.render.
func TestMustacheArity(t *testing.T) {
	src := `require "mustache"
begin
  Mustache.render
rescue ArgumentError
  print "argerror"
end`
	if got := eval(t, src); got != "argerror" {
		t.Errorf("got=%q want=argerror", got)
	}
}
