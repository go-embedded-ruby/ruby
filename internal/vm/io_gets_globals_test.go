package vm_test

import "testing"

// TestGetsLineGlobals covers the $. (line number) and $_ (last read line) globals
// that IO#gets / #readline / #each_line / #readlines maintain, verified against
// MRI Ruby 4.0.6: gets and readline set both $. and $_; the bulk readers set $.
// but leave $_ untouched; and reading past the end clears $_.
func TestGetsLineGlobals(t *testing.T) {
	cases := []struct{ src, want string }{
		// gets sets $. to the line number and $_ to the line (a StringIO too).
		{`require "stringio"; io = StringIO.new("a\nb\nc\n"); io.gets; p [$., $_]`, "[1, \"a\\n\"]\n"},
		{`require "stringio"; io = StringIO.new("a\nb\n"); io.gets; io.gets; p $.`, "2\n"},
		{`require "stringio"; io = StringIO.new("x\n"); p io.readline; p [$., $_]`, "\"x\\n\"\n[1, \"x\\n\"]\n"},
		// Reading past the last line clears $_ but keeps $. at the final number.
		{`require "stringio"; io = StringIO.new("a\n"); io.gets; io.gets; p [$., $_]`, "[1, nil]\n"},
		// readlines advances $. to the count but does not touch $_.
		{`require "stringio"; io = StringIO.new("a\nb\nc\n"); $_ = "pre"; io.readlines; p [$., $_]`, "[3, \"pre\"]\n"},
		// each_line advances $. but leaves the outer $_ alone.
		{`require "stringio"; io = StringIO.new("a\nb\n"); $_ = "pre"; io.each_line {}; p [$., $_]`, "[2, \"pre\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
