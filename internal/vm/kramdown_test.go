// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestKramdownConstants covers the Kramdown loadable module and Document class
// (require "kramdown").
func TestKramdownConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "kramdown"; p Kramdown.is_a?(Module)`, "true\n"},
		{`require "kramdown"; p defined?(Kramdown::Document)`, "\"constant\"\n"},
		// require returns true the first time, false afterwards.
		{`p require "kramdown"`, "true\n"},
		{`require "kramdown"; p require "kramdown"`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKramdownRender covers Kramdown::Document.new(...).to_html, the one-shot
// Kramdown.to_html, and the String#to_kramdown_html shortcut, including an
// options Hash.
func TestKramdownRender(t *testing.T) {
	cases := []struct{ src, want string }{
		// Document class round-trip: a header and inline emphasis.
		{`require "kramdown"; puts Kramdown::Document.new("# Title").to_html`, "<h1 id=\"title\">Title</h1>\n"},
		{`require "kramdown"; puts Kramdown::Document.new("*em*").to_html`, "<p><em>em</em></p>\n"},
		// One-shot module method.
		{`require "kramdown"; puts Kramdown.to_html("plain text")`, "<p>plain text</p>\n"},
		// String shortcut.
		{`require "kramdown"; puts "**b**".to_kramdown_html`, "<p><strong>b</strong></p>\n"},
		// Options Hash: auto_ids: false suppresses the generated header id.
		{`require "kramdown"; puts Kramdown::Document.new("# H", { auto_ids: false }).to_html`, "<h1>H</h1>\n"},
		{`require "kramdown"; puts Kramdown.to_html("# H", { auto_ids: false })`, "<h1>H</h1>\n"},
		{`require "kramdown"; puts "# H".to_kramdown_html({ auto_ids: false })`, "<h1>H</h1>\n"},
		// auto_id_prefix prepends to the generated id; footnote_nr sets numbering
		// start; the boolean options are accepted.
		{`require "kramdown"; puts Kramdown::Document.new("# H", { auto_id_prefix: "x-" }).to_html`, "<h1 id=\"x-h\">H</h1>\n"},
		{`require "kramdown"; puts Kramdown.to_html("s", { smart_quotes: false, typographic_symbols: false, hard_wrap: false, footnote_nr: 2 })`, "<p>s</p>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestKramdownArgErrors covers the wrong-argument-count guards of the module and
// Document methods.
func TestKramdownArgErrors(t *testing.T) {
	for _, src := range []string{
		`require "kramdown"; Kramdown.to_html`,
		`require "kramdown"; Kramdown::Document.new`,
	} {
		if got := eval(t, `begin; `+src+`; rescue ArgumentError => e; p e.class; end`); !strings.Contains(got, "ArgumentError") {
			t.Errorf("src=%q got=%q", src, got)
		}
	}
}

// TestKramdownNonStringSource covers kramdownSourceArg's to_s branch (a non-String
// source is coerced via to_s) and a non-Hash options value (defaults apply).
func TestKramdownNonStringSource(t *testing.T) {
	// An Integer source is rendered as its to_s ("42") inside a paragraph.
	if got := eval(t, `require "kramdown"; puts Kramdown.to_html(42)`); got != "<p>42</p>\n" {
		t.Errorf("non-string source got=%q", got)
	}
	// A non-Hash options value (a String) selects the defaults (auto id present).
	if got := eval(t, `require "kramdown"; puts Kramdown.to_html("# H", "ignored")`); got != "<h1 id=\"h\">H</h1>\n" {
		t.Errorf("non-hash opts got=%q", got)
	}
}
