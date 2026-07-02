// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestSlimRequire covers that `require "slim"` returns true on the first load and
// false thereafter, matching MRI's provided-feature contract.
func TestSlimRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "slim"`, "true\n"},
		{"require \"slim\"\np require \"slim\"", "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSlimRender covers the compile→eval render seam: static structure, the block
// source form, embedded `=` expressions with HTML escaping, and locals binding.
func TestSlimRender(t *testing.T) {
	cases := []struct{ src, want string }{
		// Static template via the block source form (the gem's Slim::Template.new { }).
		{`require "slim"; puts Slim::Template.new { "h1 Hello" }.render`, "<h1>Hello</h1>\n"},
		// The template as a positional argument.
		{`require "slim"; puts Slim::Template.new("p body").render`, "<p>body</p>\n"},
		// An embedded `=` expression is HTML-escaped through Slim::Helpers.escape_html.
		{`require "slim"; puts Slim::Template.new { "p = '<b>'" }.render`, "<p>&lt;b&gt;</p>\n"},
		// A local passed to render is bound and resolvable by name.
		{`require "slim"; puts Slim::Template.new { "p\n  = name" }.render(nil, name: "World")`, "<p>World</p>\n"},
		// A local whose value carries HTML is escaped through the `=` output path.
		{`require "slim"; puts Slim::Template.new { "p\n  = v" }.render(nil, v: "<b>")`, "<p>&lt;b&gt;</p>\n"},
		// Slim::Engine names the same class.
		{`require "slim"; p Slim::Engine.equal?(Slim::Template)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSlimHelpers covers the prelude Slim::Helpers runtime the compiled source
// calls: escape_html, the Safe wrapper, render_attribute, and render_attributes
// (class merge, id, boolean, nil-omit, splat, Safe-passthrough, sort).
func TestSlimHelpers(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "slim"; puts Slim::Helpers.escape_html(%q{<a>&"'})`, "&lt;a&gt;&amp;&quot;&#39;\n"},
		{`require "slim"; puts Slim::Helpers.escape_html(42)`, "42\n"},
		// A Safe-wrapped value renders unescaped in an attribute.
		{`require "slim"; p Slim::Helpers.render_attribute("href", Slim::Helpers.safe("<x>"))`, "\" href=\\\"<x>\\\"\"\n"},
		// nil/false attribute value is omitted.
		{`require "slim"; p Slim::Helpers.render_attribute("a", nil)`, "\"\"\n"},
		{`require "slim"; p Slim::Helpers.render_attribute("a", false)`, "\"\"\n"},
		// true value emits a boolean attribute.
		{`require "slim"; p Slim::Helpers.render_attribute("checked", true)`, "\" checked=\\\"\\\"\"\n"},
		// A plain value is escaped.
		{`require "slim"; p Slim::Helpers.render_attribute("t", "<b>")`, "\" t=\\\"&lt;b&gt;\\\"\"\n"},
		// render_attributes: class values (Array) merge with spaces, sorted keys.
		{`require "slim"; p Slim::Helpers.render_attributes({"class" => ["a", "b"], "id" => "x"})`, "\" class=\\\"a b\\\" id=\\\"x\\\"\"\n"},
		// A splat hash merges on top of the base, class values accumulate.
		{`require "slim"; p Slim::Helpers.render_attributes({"class" => "a"}, {"class" => "b"})`, "\" class=\\\"a b\\\"\"\n"},
		// A nil-valued and true-valued attribute in render_attributes.
		{`require "slim"; p Slim::Helpers.render_attributes({"z" => nil, "checked" => true})`, "\" checked=\\\"\\\"\"\n"},
		// A Safe value in render_attributes is left unescaped.
		{`require "slim"; p Slim::Helpers.render_attributes({"href" => Slim::Helpers.safe("<u>")})`, "\" href=\\\"<u>\\\"\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSlimConstants covers the Slim module, Slim::Template class and Slim::Error
// tree resolving, and that #src exposes the compiled source.
func TestSlimConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "slim"; p Slim::Template.is_a?(Class)`, "true\n"},
		{`require "slim"; p (Slim::Error < StandardError)`, "true\n"},
		{`require "slim"; p Slim::Template.new("p x").src.is_a?(String)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
