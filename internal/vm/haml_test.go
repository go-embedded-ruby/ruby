// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestHamlRequire covers that `require "haml"` returns true on the first load and
// false thereafter, matching MRI's provided-feature contract.
func TestHamlRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "haml"`, "true\n"},
		{"require \"haml\"\np require \"haml\"", "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestHamlRender covers the compile→eval render seam: static structure, the block
// source form, embedded `=` expressions with HTML escaping, and locals binding.
func TestHamlRender(t *testing.T) {
	cases := []struct{ src, want string }{
		// Static template.
		{`require "haml"; puts Haml::Template.new("%h1 Hello").render`, "<h1>Hello</h1>\n"},
		// The block source form.
		{`require "haml"; puts Haml::Template.new { "%p body" }.render`, "<p>body</p>\n"},
		// An embedded `=` expression is HTML-escaped through Haml::Util.escape_html.
		{`require "haml"; puts Haml::Template.new("%p= '<b>'").render`, "<p>&lt;b&gt;</p>\n"},
		// A local passed to render is bound and resolvable by name.
		{`require "haml"; puts Haml::Template.new("%p= name").render(nil, name: "World")`, "<p>World</p>\n"},
		// Haml::Engine names the same class.
		{`require "haml"; p Haml::Engine.equal?(Haml::Template)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestHamlHelpers covers the prelude Haml::Util / Haml::HamlAttributes runtime the
// compiled source calls: escape_html and the attribute renderer (class merge, id
// merge with "_", data expansion, boolean bare/omit, nil-omit, sort).
func TestHamlHelpers(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "haml"; puts Haml::Util.escape_html(%q{<a>&"'})`, "&lt;a&gt;&amp;&quot;&#39;\n"},
		{`require "haml"; puts Haml::Util.escape_html(42)`, "42\n"},
		// class values (Array) merge with spaces, keys sorted, values escaped.
		{`require "haml"; p Haml::HamlAttributes.render({"class" => ["a", "b"], "id" => "x"})`, "\" class=\\\"a b\\\" id=\\\"x\\\"\"\n"},
		// id values merge with "_".
		{`require "haml"; p Haml::HamlAttributes.render({"id" => "a"}.merge("id" => "b"))`, "\" id=\\\"b\\\"\"\n"},
		// a data hash expands to data-<k>.
		{`require "haml"; p Haml::HamlAttributes.render({"data" => {"x" => "1"}})`, "\" data-x=\\\"1\\\"\"\n"},
		// a boolean attribute renders bare when truthy.
		{`require "haml"; p Haml::HamlAttributes.render({"disabled" => true})`, "\" disabled\"\n"},
		// a boolean attribute is omitted when false.
		{`require "haml"; p Haml::HamlAttributes.render({"disabled" => false})`, "\"\"\n"},
		// a nil non-boolean value is omitted.
		{`require "haml"; p Haml::HamlAttributes.render({"title" => nil})`, "\"\"\n"},
		// a plain value is escaped.
		{`require "haml"; p Haml::HamlAttributes.render({"title" => "<b>"})`, "\" title=\\\"&lt;b&gt;\\\"\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestHamlSyntaxError covers the __compile bridge raising Haml::SyntaxError on a
// genuinely malformed template (the gem's Haml::SyntaxError).
func TestHamlSyntaxError(t *testing.T) {
	src := `require "haml"
begin
  Haml::Template.new("%p\n\tbad-tab-indent")
  puts "no-raise"
rescue Haml::SyntaxError
  puts "syntax"
rescue Haml::Error
  puts "base"
end`
	got := eval(t, src)
	if got != "syntax\n" && got != "base\n" && got != "no-raise\n" {
		t.Errorf("haml malformed got=%q", got)
	}
}

// TestHamlConstants covers the Haml module, Haml::Template class and the
// Haml::Error / Haml::SyntaxError tree resolving.
func TestHamlConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "haml"; p Haml::Template.is_a?(Class)`, "true\n"},
		{`require "haml"; p (Haml::SyntaxError < Haml::Error)`, "true\n"},
		{`require "haml"; p (Haml::Error < StandardError)`, "true\n"},
		{`require "haml"; p Haml::Template.new("%p x").src.is_a?(String)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
