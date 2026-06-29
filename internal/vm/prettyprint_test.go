// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestPrettyPrint covers the Ruby PrettyPrint binding (backed by
// github.com/go-ruby-prettyprint/prettyprint, the MRI-4.0.5-faithful port of the
// Wadler/Lindig layout engine): the format / singleline_format class methods, the
// new constructor with its (output, maxwidth, newline) defaults, and the
// document-builder instance methods (text, breakable, group, nest, fill_breakable,
// flush, current_group, break_outmost_groups) plus the output/maxwidth/newline/
// indent accessors. Every expected value is asserted against MRI 4.0.5's stdlib
// `prettyprint`.
func TestPrettyPrint(t *testing.T) {
	const req = `require "prettyprint"` + "\n"
	// requireCases exercise the require side directly (no auto-prefix).
	requireCases := []struct{ src, want string }{
		{`p require "prettyprint"`, "true\n"},
		{`require "prettyprint"; p require "prettyprint"`, "false\n"},
	}
	for _, c := range requireCases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	cases := []struct{ src, want string }{
		// format: a group that overflows maxwidth breaks at every breakable; one that
		// fits stays on a single line.
		{`p PrettyPrint.format("", 10) {|q| q.group { q.text "abc"; q.breakable; q.text "def"; q.breakable; q.text "ghi" } }`,
			"\"abc\\ndef\\nghi\"\n"},
		{`p PrettyPrint.format("", 79) {|q| q.group { q.text "abc"; q.breakable; q.text "def" } }`,
			"\"abc def\"\n"},
		// format default maxwidth (79) when only output is given, and full defaults.
		{`p PrettyPrint.format("") {|q| q.text "a"; q.breakable; q.text "b" }`, "\"a b\"\n"},
		{`p PrettyPrint.format {|q| q.text "a"; q.breakable; q.text "b" }`, "\"a b\"\n"},
		// format seeds the output object with the leading string.
		{`p PrettyPrint.format("PRE", 79) {|q| q.text "x"; q.breakable; q.text "y" }`, "\"PREx y\"\n"},
		// nil maxwidth falls back to the default 79 (the arg-default branch).
		{`p PrettyPrint.format("", nil) {|q| q.text "a"; q.breakable; q.text "b" }`, "\"a b\"\n"},
		// custom newline string is used at every break.
		{`p PrettyPrint.format("", 5, "<NL>") {|q| q.group { q.text "aaa"; q.breakable; q.text "bbb" } }`,
			"\"aaa<NL>bbb\"\n"},

		// group(indent, open_obj, close_obj): the brackets are emitted around the body
		// and the indent nests the broken lines.
		{`p PrettyPrint.format("", 10) {|q| q.group(1, "[", "]") { q.text "1"; q.breakable; q.text "2"; q.breakable; q.text "3" } }`,
			"\"[1 2 3]\"\n"},
		{`p PrettyPrint.format("", 4) {|q| q.group(1, "[", "]") { q.text "1"; q.breakable; q.text "22" } }`,
			"\"[1\\n 22]\"\n"},

		// nest increases the left margin for the breaks inside the block.
		{`p PrettyPrint.format("", 10) {|q| q.text "begin"; q.nest(2) { q.breakable; q.text "s1"; q.breakable; q.text "s2" }; q.breakable; q.text "end" }`,
			"\"begin\\n  s1\\n  s2\\nend\"\n"},

		// fill_breakable decides each break individually (a < b joins, b ... c breaks).
		{`p PrettyPrint.format("", 6) {|q| q.text "a"; q.fill_breakable; q.text "bb"; q.fill_breakable; q.text "ccc"; q.fill_breakable; q.text "d" }`,
			"\"a bb\\nccc d\"\n"},
		// fill_breakable with an explicit separator string.
		{`p PrettyPrint.format("", 79) {|q| q.text "a"; q.fill_breakable(", "); q.text "b" }`, "\"a, b\"\n"},

		// text with an explicit width (the width arg overrides obj.length); custom
		// breakable separator + width.
		{`p PrettyPrint.format("", 5) {|q| q.text("x", 10); q.breakable; q.text "y" }`, "\"x\\ny\"\n"},
		{`p PrettyPrint.format("", 79) {|q| q.text "a"; q.breakable(", "); q.text "b" }`, "\"a, b\"\n"},
		{`p PrettyPrint.format("", 79) {|q| q.text "a"; q.breakable(",", 1); q.text "b" }`, "\"a,b\"\n"},

		// text coerces a non-String obj via #to_s (MRI's @output << obj path).
		{`p PrettyPrint.format("") {|q| q.text 42 }`, "\"42\"\n"},

		// current_group.depth reports the nesting depth set by group_sub.
		{`p PrettyPrint.format("") {|q| q.group { q.text q.current_group.depth.to_s; q.group { q.text q.current_group.depth.to_s } } }`,
			"\"12\"\n"},
		{`p PrettyPrint.format("") {|q| q.text q.current_group.depth.to_s }`, "\"0\"\n"},

		// new + flush + the read-only accessors (defaults 79 / "\n", output, indent).
		{`q = PrettyPrint.new; q.text "hello"; q.breakable; q.text "world"; q.flush; p q.output`, "\"hello world\"\n"},
		{`q = PrettyPrint.new; p [q.maxwidth, q.newline, q.indent]`, "[79, \"\\n\", 0]\n"},
		{`q = PrettyPrint.new("", 40, "|"); p [q.maxwidth, q.newline]`, "[40, \"|\"]\n"},
		{`p PrettyPrint.format("", 5, "|") {|q| q.group { q.text "aaa"; q.breakable; q.text "bbb" } }`, "\"aaa|bbb\"\n"},
		{`q = PrettyPrint.new("seed"); q.text "x"; q.flush; p q.output`, "\"seedx\"\n"},
		// indent reflects the engine's current margin inside a nest block.
		{`p PrettyPrint.format("") {|q| q.nest(3) { q.text q.indent.to_s } }`, "\"3\"\n"},

		// break_outmost_groups is callable directly (it lays out buffered groups
		// against maxwidth); after a flush the output is the joined text.
		{`q = PrettyPrint.new("", 3); q.group { q.text "aa"; q.breakable; q.text "bb" }; q.break_outmost_groups; q.flush; p q.output`,
			"\"aa\\nbb\"\n"},

		// to_s / inspect render the opaque #<PrettyPrint> tag.
		{`q = PrettyPrint.new; p q.to_s`, "\"#<PrettyPrint>\"\n"},
		{`q = PrettyPrint.new; p q.inspect`, "\"#<PrettyPrint>\"\n"},
		{`q = PrettyPrint.new; p q`, "#<PrettyPrint>\n"},
		// class identity and the nested Group constant.
		{`p PrettyPrint.new.class`, "PrettyPrint\n"},
		{`p PrettyPrint.format("") {|q| p q.current_group.class }`, "PrettyPrint::Group\n\"\"\n"},
		{`p PrettyPrint.singleline_format {|q| p q.class }`, "PrettyPrint::SingleLine\n\"\"\n"},

		// singleline_format: never breaks — breakables become their separator text,
		// groups/nests are transparent, and open/close brackets still print.
		{`p PrettyPrint.singleline_format {|q| q.group { q.text "a"; q.breakable; q.text "b"; q.breakable; q.text "c" } }`,
			"\"a b c\"\n"},
		{`p PrettyPrint.singleline_format {|q| q.group(1, "[", "]") { q.text "1"; q.breakable; q.text "2" } }`,
			"\"[1 2]\"\n"},
		{`p PrettyPrint.singleline_format {|q| q.text "a"; q.breakable(", "); q.text "b" }`, "\"a, b\"\n"},
		{`p PrettyPrint.singleline_format {|q| q.nest(2) { q.text "x" } }`, "\"x\"\n"},
		// singleline output is seeded too; flush is a no-op; to_s coercion works.
		{`p PrettyPrint.singleline_format("PRE") {|q| q.text "x" }`, "\"PREx\"\n"},
		{`p PrettyPrint.singleline_format {|q| q.text 7; q.flush }`, "\"7\"\n"},
		// the SingleLine accessors: output / to_s / inspect / first?.
		{`PrettyPrint.singleline_format {|q| q.text "a"; p q.output }`, "\"a\"\n"},
		{`PrettyPrint.singleline_format {|q| p q.to_s }`, "\"#<PrettyPrint::SingleLine>\"\n"},
		{`PrettyPrint.singleline_format {|q| p q.inspect }`, "\"#<PrettyPrint::SingleLine>\"\n"},
		// first? is true on the first query of the innermost group, then false.
		{`PrettyPrint.singleline_format {|q| q.group { p q.first?; p q.first? } }`, "true\nfalse\n"},

		// Truthy: every wrapper is truthy in a boolean context (MRI: only nil/false
		// are falsy). current_group, the buffer and the SingleLine all qualify.
		{`q = PrettyPrint.new; p(q ? 1 : 2)`, "1\n"},
		{`p PrettyPrint.format("") {|q| q.text(q.current_group ? "y" : "n") }`, "\"y\"\n"},
		{`PrettyPrint.singleline_format {|q| p(q ? 1 : 2) }`, "1\n"},
		// the opaque PrettyPrint::Group render (MRI adds a pointer; rbgo uses the
		// stable wrapper tag) — exercised through to_s and inspect / p.
		{`PrettyPrint.format("") {|q| p q.current_group.to_s }`, "\"#<PrettyPrint::Group>\"\n"},
		{`PrettyPrint.format("") {|q| p q.current_group.inspect }`, "\"#<PrettyPrint::Group>\"\n"},
		{`PrettyPrint.format("") {|q| p q.current_group }`, "#<PrettyPrint::Group>\n"},

		// text of an object whose #to_s does not return a String degrades to empty
		// (rbgo guards the conversion rather than calling #length like MRI).
		{`class Odd; def to_s; 7; end; end; p PrettyPrint.format("") {|q| q.text(Odd.new) }`, "\"\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPrettyPrintErrors covers the LocalJumpError raised when a block-taking
// method is called without a block, matching MRI's "no block given".
func TestPrettyPrintErrors(t *testing.T) {
	const req = `require "prettyprint"` + "\n"
	cases := []struct{ src, want string }{
		{`PrettyPrint.format("")`, "LocalJumpError"},
		{`PrettyPrint.singleline_format("")`, "LocalJumpError"},
		{`q = PrettyPrint.new; q.group`, "LocalJumpError"},
		{`q = PrettyPrint.new; q.nest(2)`, "LocalJumpError"},
		{`PrettyPrint.singleline_format {|q| q.group }`, "LocalJumpError"},
		{`PrettyPrint.singleline_format {|q| q.nest(2) }`, "LocalJumpError"},
	}
	for _, c := range cases {
		if err := runErr(t, req+c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
