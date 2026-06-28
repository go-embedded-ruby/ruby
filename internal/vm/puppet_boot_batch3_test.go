// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"path/filepath"
	"testing"
)

// TestClassEvalString covers the string form of class_eval / module_eval
// (class_eval("def m; end", file, line)), the mechanism racc's runtime uses to
// graft do_parse/yyparse onto a generated parser. Asserted against MRI 4.0.5.
func TestClassEvalString(t *testing.T) {
	cases := []struct{ src, want string }{
		// a def in the source becomes an instance method of the class.
		{"class C; end\nC.class_eval(\"def x; 1; end\")\np C.new.x", "1\n"},
		// the optional file/line trailing arguments are accepted and ignored.
		{"class C; end\nC.class_eval(\"def y; 2; end\", \"f.rb\", 10)\np C.new.y", "2\n"},
		// module_eval string form behaves identically.
		{"module M; end\nM.module_eval(\"def z; 3; end\")\nclass D; include M; end\np D.new.z", "3\n"},
		// constants/expressions in the source run with the class as self.
		{"class C; end\nC.class_eval(\"K = 7\")\np C::K", "7\n"},
		// no block and no string still raises LocalJumpError, as MRI does.
		{"class C; end\nbegin\n  C.class_eval\nrescue LocalJumpError => e\n  puts \"caught\"\nend", "caught\n"},
		// a syntax error in the source raises SyntaxError.
		{"class C; end\nbegin\n  C.class_eval(\"def (\")\nrescue SyntaxError\n  puts \"syn\"\nend", "syn\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestLineKeyword covers Kernel#__LINE__. This VM does not track per-instruction
// source lines (backtraces and #caller report line 0), so __LINE__ is 0 — enough
// to feed a line offset to class_eval(str, file, line).
func TestLineKeyword(t *testing.T) {
	cases := []struct{ src, want string }{
		{"p __LINE__", "0\n"},
		{"class C\n  p __LINE__\nend", "0\n"},
		{"p(__LINE__ + 1)", "1\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestArrayCollectBang covers Array#collect!, the classic alias of map!.
func TestArrayCollectBang(t *testing.T) {
	cases := []struct{ src, want string }{
		{"a = [1, 2, 3]\na.collect! { |x| x * 10 }\np a", "[10, 20, 30]\n"},
		// returns the receiver, mutating in place.
		{"a = [1, 2]\np a.collect! { |x| x + 1 }.equal?(a)", "true\n"},
		// no block yields an Enumerator, like map!.
		{"p [1, 2].collect!.class", "Enumerator\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestStringScanner covers the pure-Ruby strscan StringScanner against MRI's
// behavior on the surface Puppet's lexer relies on.
func TestStringScanner(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "strscan"; p require("strscan")`, "false\n"},
		// scan advances and records the match; a failed scan returns nil and holds.
		{"require \"strscan\"\ns = StringScanner.new(\"hello world\")\np s.scan(/\\w+/)\np s.pos\np s.scan(/\\w+/)", "\"hello\"\n5\nnil\n"},
		// skip returns the match length and advances.
		{"require \"strscan\"\ns = StringScanner.new(\"  ab\")\np s.skip(/\\s+/)\np s.pos", "2\n2\n"},
		// peek does not advance; match? returns the match length without advancing.
		{"require \"strscan\"\ns = StringScanner.new(\"abcd\")\np s.peek(2)\np s.match?(/ab/)\np s.pos", "\"ab\"\n2\n0\n"},
		// scan_until consumes through the next match; matched is just the match.
		{"require \"strscan\"\ns = StringScanner.new(\"xx99\")\np s.scan_until(/\\d+/)\np s.matched\np s.pos", "\"xx99\"\n\"99\"\n4\n"},
		// scan_until miss returns nil and leaves the position.
		{"require \"strscan\"\ns = StringScanner.new(\"abc\")\np s.scan_until(/z/)\np s.pos", "nil\n0\n"},
		// getch returns one character and advances; nil at eos.
		{"require \"strscan\"\ns = StringScanner.new(\"ab\")\np s.getch\np s.getch\np s.getch", "\"a\"\n\"b\"\nnil\n"},
		// eos?, rest, string, charpos.
		{"require \"strscan\"\ns = StringScanner.new(\"hi\")\ns.scan(/h/)\np s.rest\np s.string\np s.charpos\np s.eos?", "\"i\"\n\"hi\"\n1\nfalse\n"},
		// empty scanner is at eos immediately.
		{"require \"strscan\"\np StringScanner.new(\"\").eos?", "true\n"},
		// pos= moves the position, including a negative index from the end.
		{"require \"strscan\"\ns = StringScanner.new(\"abcd\")\ns.pos = 2\np s.rest\ns.pos = -1\np s.rest", "\"cd\"\n\"d\"\n"},
		// terminate jumps to the end; reset returns to the start.
		{"require \"strscan\"\ns = StringScanner.new(\"abc\")\ns.terminate\np s.eos?\ns.reset\np s.pos", "true\n0\n"},
		// peek past the end clamps to what remains.
		{"require \"strscan\"\np StringScanner.new(\"ab\").peek(9)", "\"ab\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFind covers the pure-Ruby Find module (require "find") against MRI: the
// traversal order, the no-block Enumerator path, Find.prune, and the missing-path
// error. A real temporary tree is built with forward-slashed paths so the Ruby
// source is Windows-safe (no backslash corruption).
func TestFind(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	// Build: <dir>/a.txt, <dir>/sub/, <dir>/sub/b.txt via Ruby itself.
	setup := `require "find"
root = "` + dir + `"
require "fileutils"
FileUtils.mkdir_p(File.join(root, "sub"))
File.write(File.join(root, "a.txt"), "x")
File.write(File.join(root, "sub", "b.txt"), "y")
`
	cases := []struct{ src, want string }{
		// require returns true first, false after.
		{`p require "find"`, "true\n"},
		// full traversal, depth-first with sorted children.
		{setup + `found = []
Find.find(root) { |p| found << p.sub(root, "R") }
p found`, "[\"R\", \"R/a.txt\", \"R/sub\", \"R/sub/b.txt\"]\n"},
		// prune (thrown before recording) skips descending into the sub directory.
		{setup + `pr = []
Find.find(root) { |p| Find.prune if p.end_with?("sub"); pr << p.sub(root, "R") }
p pr`, "[\"R\", \"R/a.txt\"]\n"},
		// no block returns an Enumerator.
		{setup + `p Find.find(root).class`, "Enumerator\n"},
		// a missing top-level path raises Errno::ENOENT.
		{`require "find"
begin
  Find.find("` + dir + `/nope") { |p| }
rescue Errno::ENOENT
  puts "enoent"
end`, "enoent\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
