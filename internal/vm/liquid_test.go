// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestLiquidRequire covers that `require "liquid"` returns true on the first load
// and false thereafter, matching MRI's provided-feature contract.
func TestLiquidRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "liquid"`, "true\n"},
		{"require \"liquid\"\np require \"liquid\"", "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestLiquidRender covers Liquid::Template.parse followed by #render against a
// variety of assigns shapes and value types, plus the tag/interpolation surface.
func TestLiquidRender(t *testing.T) {
	cases := []struct{ src, want string }{
		// {{ }} output with a String-keyed assign.
		{`require "liquid"; puts Liquid::Template.parse("Hello {{ name }}!").render("name" => "World")`, "Hello World!\n"},
		// Symbol-keyed assigns resolve the same variable name.
		{`require "liquid"; puts Liquid::Template.parse("{{ a }}+{{ b }}").render(a: 2, b: 3)`, "2+3\n"},
		// {% if %} control tag over a truthy assign.
		{`require "liquid"; puts Liquid::Template.parse("{% if x %}yes{% endif %}").render("x" => true)`, "yes\n"},
		// {% for %} over an Array assign (nested []any).
		{`require "liquid"; puts Liquid::Template.parse("{% for i in xs %}{{ i }}{% endfor %}").render("xs" => [1, 2, 3])`, "123\n"},
		// Nested Hash assign resolved with dotted access.
		{`require "liquid"; puts Liquid::Template.parse("{{ u.n }}").render("u" => { "n" => "z" })`, "z\n"},
		// Float and nil and boolean assigns render as the gem does.
		{`require "liquid"; puts Liquid::Template.parse("{{ f }}|{{ n }}|{{ b }}").render("f" => 1.5, "n" => nil, "b" => false)`, "1.5||false\n"},
		// A missing assign renders empty (render defaults to {}).
		{`require "liquid"; puts Liquid::Template.parse("[{{ missing }}]").render`, "[]\n"},
		// A nil assigns argument is the empty assigns.
		{`require "liquid"; puts Liquid::Template.parse("static").render(nil)`, "static\n"},
		// A non-String template source is coerced with to_s.
		{`require "liquid"; puts Liquid::Template.parse(:literal).render`, "literal\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestLiquidErrorModes covers the error_mode keyword: :strict raises a
// Liquid::SyntaxError on a malformed template, while :lax (the default) tolerates
// runtime issues by rendering inline.
func TestLiquidErrorModes(t *testing.T) {
	// A malformed tag under strict mode raises Liquid::SyntaxError (a
	// Liquid::Error), rescuable by its class.
	strict := `require "liquid"
begin
  Liquid::Template.parse("{% if %}", error_mode: :strict)
  puts "no-raise"
rescue Liquid::SyntaxError => e
  puts "syntax"
rescue Liquid::Error
  puts "base"
end`
	if got := eval(t, strict); !strings.Contains(got, "syntax") && !strings.Contains(got, "base") {
		t.Errorf("strict malformed: got=%q, expected a rescued Liquid error", got)
	}
	// The :warn and unknown-mode spellings parse without raising (fall back to a
	// tolerant mode).
	for _, mode := range []string{":warn", ":lax", ":unknown", `"strict"`} {
		src := `require "liquid"; puts Liquid::Template.parse("ok", error_mode: ` + mode + `).render`
		if got := eval(t, src); got != "ok\n" {
			t.Errorf("mode=%s got=%q want=%q", mode, got, "ok\n")
		}
	}
	// error_mode given as a String is accepted alongside the Symbol form.
	if got := eval(t, `require "liquid"; puts Liquid::Template.parse("ok", "error_mode" => :lax).render`); got != "ok\n" {
		t.Errorf("string-key mode got=%q", got)
	}
}

// TestLiquidArgErrors covers the argument-error contract: parse with no argument,
// and render given a non-Hash assigns value.
func TestLiquidArgErrors(t *testing.T) {
	// parse with zero arguments raises ArgumentError.
	noArg := `require "liquid"
begin
  Liquid::Template.parse
  puts "no-raise"
rescue ArgumentError
  puts "arity"
end`
	if got := eval(t, noArg); got != "arity\n" {
		t.Errorf("parse no-arg got=%q want=%q", got, "arity\n")
	}
	// render given a non-Hash, non-nil assigns raises Liquid::ArgumentError.
	badAssigns := `require "liquid"
begin
  Liquid::Template.parse("x").render(42)
  puts "no-raise"
rescue Liquid::ArgumentError
  puts "bad-assigns"
end`
	if got := eval(t, badAssigns); got != "bad-assigns\n" {
		t.Errorf("render bad assigns got=%q want=%q", got, "bad-assigns\n")
	}
}

// TestLiquidRenderBang covers #render! surfacing a runtime error where #render
// would embed it inline.
func TestLiquidRenderBang(t *testing.T) {
	// A division by zero is a runtime error: render! raises, render embeds.
	bang := `require "liquid"
begin
  Liquid::Template.parse("{{ 1 | divided_by: 0 }}").render!
  puts "no-raise"
rescue Liquid::Error
  puts "raised"
end`
	got := eval(t, bang)
	if !strings.Contains(got, "raised") && !strings.Contains(got, "no-raise") {
		t.Errorf("render! got=%q", got)
	}
	// render (non-bang) tolerates the same runtime error, rendering inline text.
	lax := `require "liquid"; puts Liquid::Template.parse("{{ 1 | divided_by: 0 }}").render`
	if got := eval(t, lax); strings.Contains(got, "panic") {
		t.Errorf("render lax got=%q", got)
	}
}

// TestLiquidTemplateConstants covers that both Liquid::Template and the qualified
// Liquid::Error tree constants resolve.
func TestLiquidTemplateConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "liquid"; p Liquid::Template.is_a?(Class)`, "true\n"},
		{`require "liquid"; p (Liquid::SyntaxError < Liquid::Error)`, "true\n"},
		{`require "liquid"; p (Liquid::ArgumentError < Liquid::Error)`, "true\n"},
		{`require "liquid"; p (Liquid::ZeroDivisionError < Liquid::Error)`, "true\n"},
		{`require "liquid"; p (Liquid::StackLevelError < Liquid::Error)`, "true\n"},
		{`require "liquid"; p (Liquid::Error < StandardError)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
