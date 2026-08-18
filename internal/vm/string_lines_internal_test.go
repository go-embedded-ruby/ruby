// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringLines covers String#lines / #each_line: the default newline
// separator, a custom separator, a nil separator (whole string), paragraph mode
// (an "" separator, LF and CRLF aware), the chomp: keyword, the block/no-block
// forms and encoding preservation. Cases involving a bare CR are asserted through
// Ruby == to avoid emitting a raw carriage return. Verified against ruby 4.0.6.
func TestStringLines(t *testing.T) {
	cases := []struct{ src, want string }{
		// Default separator.
		{`p "one\ntwo\nthree".lines`, `["one\n", "two\n", "three"]`},
		{`p "".lines`, `[]`},
		{`p "abc".lines`, `["abc"]`},
		{`p "a\nb\n".lines`, `["a\n", "b\n"]`}, // trailing separator, no empty tail
		// Custom separator (kept, then chomped).
		{`p "axbxc".lines("x")`, `["ax", "bx", "c"]`},
		{`p "axbx".lines("x", chomp: true)`, `["a", "b"]`},
		// nil separator: the whole string is one line.
		{`p "a\nb".lines(nil)`, `["a\nb"]`},
		// chomp with the default separator strips "\n" and "\r\n".
		{`p "a\nb\n".lines(chomp: true)`, `["a", "b"]`},
		{`p ("a\r\nb\r\n".lines(chomp: true) == ["a", "b"])`, `true`},
		// A lone CR is kept by chomp (not a record separator).
		{`p ("x\r".lines(nil, chomp: true) == ["x\r"])`, `true`},
		// Paragraph mode: each paragraph keeps its terminating blank line; extra
		// newlines are skipped; a final paragraph without a blank line is whole.
		{`p "a\n\nb\n\nc".lines("")`, `["a\n\n", "b\n\n", "c"]`},
		{`p "a\n\n\n\nb".lines("")`, `["a\n\n", "b"]`},
		{`p "a\nb".lines("")`, `["a\nb"]`},
		// Leading blank lines form their own paragraph (not dropped).
		{`p "\n\nb\n\nc".lines("")`, `["\n\n", "b\n\n", "c"]`},
		{`p "\n\n\n".lines("")`, `["\n\n"]`},
		{`p "a\n\nb\n\n".lines("", chomp: true)`, `["a", "b"]`},
		// Paragraph mode is CRLF-aware (a blank line may be "\r\n\r\n").
		{`p ("a\r\n\r\nb".lines("") == ["a\r\n\r\n", "b"])`, `true`},
		// #lines with a block yields each line and returns self.
		{`s = "a\nb"; a = []; r = s.lines { |x| a << x }; p a; p r.equal?(s)`, `["a\n", "b"]` + "\n" + `true`},
		// Encoding is preserved on each line.
		{`p "a\nb".b.lines.map(&:encoding).map(&:to_s)`, `["ASCII-8BIT", "ASCII-8BIT"]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestStringEachLine covers String#each_line's block and no-block (Enumerator)
// forms, including that the Enumerator honours the separator argument and that
// the block form returns self. Verified against ruby 4.0.6.
func TestStringEachLine(t *testing.T) {
	cases := []struct{ src, want string }{
		{`a = []; "one\ntwo".each_line { |s| a << s }; p a`, `["one\n", "two"]`},
		{`s = "a\nb"; p s.each_line { |x| }.equal?(s)`, `true`},
		{`a = []; "".each_line { |s| a << s }; p a`, `[]`},
		// The no-block Enumerator carries the separator argument.
		{`p "axbxc".each_line("x").to_a`, `["ax", "bx", "c"]`},
		{`p "a\nb".each_line.class`, `Enumerator`},
		// Custom separator with a block.
		{`a = []; "a\nb\nc".each_line("\n", chomp: true) { |s| a << s }; p a`, `["a", "b", "c"]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
