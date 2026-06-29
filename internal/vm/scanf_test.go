// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestScanf covers String#scanf (and its block form), IO#scanf and the
// top-level Kernel#scanf backed by github.com/go-ruby-scanf/scanf (the
// MRI-4.0.5 faithful port): mixed directives, integer bases, floats, %s/%c,
// character-class sets, field widths, assignment suppression, the %% literal,
// big integers, the block (group) form, and the partial/failed/empty matches
// that scanf returns rather than raising — every value asserted against MRI
// 4.0.5's stdlib scanf (`require "scanf"`).
func TestScanf(t *testing.T) {
	cases := []struct{ src, want string }{
		// Mixed directives: integer, float, string (the canonical example).
		{`require "scanf"; p "42 3.14 foo".scanf("%d %f %s")`, `[42, 3.14, "foo"]` + "\n"},
		// %% literal percent.
		{`require "scanf"; p "50%".scanf("%d%%")`, "[50]\n"},
		// Integer bases: %x hex, %i (0x detected), %o octal.
		{`require "scanf"; p "0xff".scanf("%x")`, "[255]\n"},
		{`require "scanf"; p "0x1f".scanf("%i")`, "[31]\n"},
		{`require "scanf"; p "010".scanf("%o")`, "[8]\n"},
		// Signed integers.
		{`require "scanf"; p "-5 +7".scanf("%d %d")`, "[-5, 7]\n"},
		// %s skips leading whitespace and is whitespace-delimited.
		{`require "scanf"; p "  hello world".scanf("%s %s")`, `["hello", "world"]` + "\n"},
		// %c with a field width reads exactly that many characters.
		{`require "scanf"; p "abcde".scanf("%3c")`, `["abc"]` + "\n"},
		// %g float with an exponent.
		{`require "scanf"; p "3.14e2".scanf("%g")`, "[314.0]\n"},
		// Character-class sets: positive %[...] and negated %[^...].
		{`require "scanf"; p "abc123".scanf("%[a-z]")`, `["abc"]` + "\n"},
		{`require "scanf"; p "abc123def".scanf("%[^0-9]%d%[a-z]")`, `["abc", 123, "def"]` + "\n"},
		{`require "scanf"; p "foo=bar".scanf("%[^=]=%s")`, `["foo", "bar"]` + "\n"},
		// Field widths split adjacent integers.
		{`require "scanf"; p "12345".scanf("%2d%3d")`, "[12, 345]\n"},
		// Assignment suppression (%*d) parses but does not store the value.
		{`require "scanf"; p "10 20".scanf("%*d %d")`, "[20]\n"},
		// Big integer beyond int64 (the *big.Int arm).
		{`require "scanf"; p "99999999999999999999999".scanf("%d")`,
			"[99999999999999999999999]\n"},
		// MRI has no %b directive: "%b" is the literal percent + 'b' to match.
		{`require "scanf"; p "%b literal".scanf("%%b %s")`, `["literal"]` + "\n"},
		// Partial match: the first value converts, the second directive fails, so
		// only the prefix is returned (no error).
		{`require "scanf"; p "42 abc".scanf("%d %d")`, "[42]\n"},
		// Empty input and a no-match both return an empty Array.
		{`require "scanf"; p "".scanf("%d")`, "[]\n"},
		{`require "scanf"; p "no".scanf("%d")`, "[]\n"},

		// Block form: each pass's group is yielded; with two params it auto-splats.
		{`require "scanf"; p "1 2 3 4".scanf("%d %d"){|a,b| a+b}`, "[3, 7]\n"},
		// Block form with a single param: the whole group Array is passed.
		{`require "scanf"; p "1 2 3".scanf("%d"){|x| x}`, "[[1], [2], [3]]\n"},
		{`require "scanf"; p "1 2 3".scanf("%d"){|x| x.first * 10}`, "[10, 20, 30]\n"},
		// Block form returning the block value per group across multiple columns.
		{`require "scanf"; p "1 2 3 4 5 6".scanf("%d %d %d"){|a,b,c| a*b*c}`, "[6, 120]\n"},

		// IO#scanf reads the stream's remaining buffered input.
		{`require "scanf"; require "stringio"; p StringIO.new("7 8").scanf("%d %d")`, "[7, 8]\n"},
		// IO#scanf block form over a StringIO.
		{`require "scanf"; require "stringio"; p StringIO.new("1 2 3 4").scanf("%d %d"){|a,b| a+b}`,
			"[3, 7]\n"},

		// Kernel#scanf (top-level) reads $stdin; reassigning $stdin redirects it.
		{`require "scanf"; require "stringio"; $stdin = StringIO.new("5 6 done"); p scanf("%d %d %s")`,
			`[5, 6, "done"]` + "\n"},
		{`require "scanf"; require "stringio"; $stdin = StringIO.new("9 done"); p scanf("%d %s")`,
			`[9, "done"]` + "\n"},

		// defined?/respond_to? after require; require returns true once then false.
		{`require "scanf"; p "".respond_to?(:scanf)`, "true\n"},
		{`p require("scanf"); p require("scanf")`, "true\nfalse\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestScanfLazyLoad asserts the scanf surface is absent until `require "scanf"`,
// matching MRI's lazy lib/scanf.rb load (in MRI String does not respond to
// :scanf before the require either).
func TestScanfLazyLoad(t *testing.T) {
	if got := eval(t, `p "".respond_to?(:scanf)`); got != "false\n" {
		t.Errorf("String#scanf should be absent before require, got %q", got)
	}
}

// TestScanfKernelNoStdin covers the Kernel#scanf defensive arm where $stdin is
// not an IO object (reassigned to a non-IO): the binding returns an empty Array
// rather than failing.
func TestScanfKernelNoStdin(t *testing.T) {
	if got := eval(t, `require "scanf"; $stdin = 5; p scanf("%d")`); got != "[]\n" {
		t.Errorf("Kernel#scanf with non-IO $stdin should return [], got %q", got)
	}
}
