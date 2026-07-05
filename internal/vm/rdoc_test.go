// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestRDocFeature covers the require "rdoc" feature probe and the module/error
// tree + class shape.
func TestRDocFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "rdoc"`, "true\n"},
		{`require "rdoc"; p require "rdoc"`, "false\n"},
		{`p require "rdoc/markup"`, "true\n"},
		{`require "rdoc"; p RDoc.is_a?(Module)`, "true\n"},
		{`require "rdoc"; p RDoc::Error < StandardError`, "true\n"},
		{`require "rdoc"; p RDoc::Markup.is_a?(Class)`, "true\n"},
		{`require "rdoc"; p RDoc::Markup::ToHtml.is_a?(Class)`, "true\n"},
		{`require "rdoc"; p RDoc::Markup::ToMarkdown.is_a?(Class)`, "true\n"},
		{`require "rdoc"; p RDoc::Markup::ToRdoc.is_a?(Class)`, "true\n"},
		{`require "rdoc"; p RDoc::Markup.new.class`, "RDoc::Markup\n"},
		{`require "rdoc"; p RDoc::Markup::ToHtml.new.class`, "RDoc::Markup::ToHtml\n"},
		{`require "rdoc"; p RDoc::Markup::ToMarkdown.new.class`, "RDoc::Markup::ToMarkdown\n"},
		{`require "rdoc"; p RDoc::Markup::ToRdoc.new.class`, "RDoc::Markup::ToRdoc\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRDocFormatterConvert covers each formatter's #convert(text) — the primary
// markup -> output path (RDoc::Markup::ToHtml.new.convert(text), etc).
func TestRDocFormatterConvert(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rdoc"; puts RDoc::Markup::ToHtml.new.convert("Hello *world*")`, "\n<p>Hello <strong>world</strong></p>\n"},
		{`require "rdoc"; puts RDoc::Markup::ToHtml.new(nil).convert("Hello *world*")`, "\n<p>Hello <strong>world</strong></p>\n"},
		{`require "rdoc"; puts RDoc::Markup::ToMarkdown.new.convert("= Title\n\nHello")`, "# Title\n\nHello\n"},
		{`require "rdoc"; puts RDoc::Markup::ToRdoc.new.convert("Hello there")`, "Hello there\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRDocMarkupConvert covers RDoc::Markup#convert(text, formatter): the driver
// renders parsed markup through the formatter its class selects.
func TestRDocMarkupConvert(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rdoc"; puts RDoc::Markup.new.convert("Hello *world*", RDoc::Markup::ToHtml.new)`, "\n<p>Hello <strong>world</strong></p>\n"},
		{`require "rdoc"; puts RDoc::Markup.new.convert("= Title\n\nHello", RDoc::Markup::ToMarkdown.new)`, "# Title\n\nHello\n"},
		{`require "rdoc"; puts RDoc::Markup.new.convert("Hello there", RDoc::Markup::ToRdoc.new)`, "Hello there\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRDocErrors covers the argument-count and type guards on the convert
// methods.
func TestRDocErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rdoc"
begin
  RDoc::Markup.new.convert("x")
rescue ArgumentError => e
  puts e.message
end`, "wrong number of arguments (given 1, expected 2)\n"},
		{`require "rdoc"
begin
  RDoc::Markup.new.convert("x", "notaformatter")
rescue TypeError => e
  puts e.message
end`, "wrong argument type String (expected RDoc::Markup formatter)\n"},
		{`require "rdoc"
begin
  RDoc::Markup::ToHtml.new.convert
rescue ArgumentError => e
  puts e.message
end`, "wrong number of arguments (given 0, expected 1)\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
