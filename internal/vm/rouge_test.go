// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestRougeRequire covers that `require "rouge"` returns true on the first load
// and false thereafter, matching MRI's provided-feature contract.
func TestRougeRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "rouge"`, "true\n"},
		{"require \"rouge\"\np require \"rouge\"", "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRougeHighlight covers Rouge.highlight over the lexer/formatter names,
// including the defaults and Symbol name arguments.
func TestRougeHighlight(t *testing.T) {
	// A Ruby snippet highlighted with the ruby lexer and html formatter carries the
	// gem's CSS short-codes.
	if got := eval(t, `require "rouge"; puts Rouge.highlight("puts 1", "ruby", "html")`); !strings.Contains(got, `class="`) {
		t.Errorf("highlight ruby/html got=%q", got)
	}
	// Symbol lexer/formatter names resolve the same way.
	if got := eval(t, `require "rouge"; puts Rouge.highlight("puts 1", :ruby, :html)`); !strings.Contains(got, `class="`) {
		t.Errorf("highlight symbol names got=%q", got)
	}
	// The lexer/formatter default to text/html when omitted.
	if got := eval(t, `require "rouge"; p Rouge.highlight("plain")`); !strings.Contains(got, "plain") {
		t.Errorf("highlight defaults got=%q", got)
	}
	// Only the lexer given (formatter defaults to html).
	if got := eval(t, `require "rouge"; p Rouge.highlight("puts 1", "ruby").class`); got != "String\n" {
		t.Errorf("highlight one-name got=%q", got)
	}
	// A non-String source is coerced with to_s.
	if got := eval(t, `require "rouge"; p Rouge.highlight(42, "text", "html").class`); got != "String\n" {
		t.Errorf("highlight to_s source got=%q", got)
	}
}

// TestRougeHighlightUnknown covers Rouge.highlight raising Rouge::Error on an
// unknown lexer or formatter name.
func TestRougeHighlightUnknown(t *testing.T) {
	unknown := `require "rouge"
begin
  Rouge.highlight("x", "no_such_lexer_xyz", "html")
  puts "no-raise"
rescue Rouge::Error
  puts "err"
end`
	if got := eval(t, unknown); got != "err\n" {
		t.Errorf("unknown lexer got=%q want=%q", got, "err\n")
	}
	badFmt := `require "rouge"
begin
  Rouge.highlight("x", "text", "no_such_formatter_xyz")
  puts "no-raise"
rescue Rouge::Error
  puts "err"
end`
	if got := eval(t, badFmt); got != "err\n" {
		t.Errorf("unknown formatter got=%q want=%q", got, "err\n")
	}
	// highlight with no argument raises ArgumentError.
	noArg := `require "rouge"
begin
  Rouge.highlight
  puts "no-raise"
rescue ArgumentError
  puts "arity"
end`
	if got := eval(t, noArg); got != "arity\n" {
		t.Errorf("highlight no-arg got=%q", got)
	}
}

// TestRougeLexerFind covers Rouge::Lexer.find returning a lexer whose #tag /
// #title / #aliases read back the found lexer's metadata, and nil for an unknown
// name.
func TestRougeLexerFind(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rouge"; p Rouge::Lexer.find("ruby").tag`, "\"ruby\"\n"},
		{`require "rouge"; p Rouge::Lexer.find("ruby").title.is_a?(String)`, "true\n"},
		{`require "rouge"; p Rouge::Lexer.find("ruby").aliases.is_a?(Array)`, "true\n"},
		{`require "rouge"; p Rouge::Lexer.find("no_such_lexer_xyz")`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Lexer.find with no argument raises ArgumentError.
	noArg := `require "rouge"
begin
  Rouge::Lexer.find
  puts "no-raise"
rescue ArgumentError
  puts "arity"
end`
	if got := eval(t, noArg); got != "arity\n" {
		t.Errorf("find no-arg got=%q", got)
	}
}

// TestRougeFormatterFind covers Rouge::Formatter.find returning the tag of a known
// formatter and nil for an unknown one.
func TestRougeFormatterFind(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rouge"; p Rouge::Formatter.find("html")`, "\"html\"\n"},
		{`require "rouge"; p Rouge::Formatter.find("no_such_formatter_xyz")`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Formatter.find with no argument raises ArgumentError.
	noArg := `require "rouge"
begin
  Rouge::Formatter.find
  puts "no-raise"
rescue ArgumentError
  puts "arity"
end`
	if got := eval(t, noArg); got != "arity\n" {
		t.Errorf("find no-arg got=%q", got)
	}
}

// TestRougeConstants covers that the Rouge module, its nested classes and the
// Rouge::Error tree resolve.
func TestRougeConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rouge"; p Rouge::Lexer.is_a?(Class)`, "true\n"},
		{`require "rouge"; p Rouge::Formatter.is_a?(Class)`, "true\n"},
		{`require "rouge"; p (Rouge::Error < StandardError)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
