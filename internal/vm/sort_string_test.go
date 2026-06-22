package vm_test

import (
	"strings"
	"testing"
)

// TestSortBlockAndStringIndex covers Array#sort/#sort! with a comparator block
// (previously ignored), String#[substr] / #slice(substr), the String#index start
// offset (previously ignored) and String#rindex. Asserted against MRI Ruby 4.0.5.
func TestSortBlockAndStringIndex(t *testing.T) {
	cases := []struct{ src, want string }{
		// sort / sort! with a comparator block.
		{`p [3, 1, 2].sort { |a, b| b <=> a }`, "[3, 2, 1]\n"},
		{`p ["bb", "a", "ccc"].sort { |a, b| a.length <=> b.length }`, "[\"a\", \"bb\", \"ccc\"]\n"},
		{`a = [3, 1, 2]; a.sort! { |x, y| y <=> x }; p a`, "[3, 2, 1]\n"},
		{`p [3, 1, 2].sort`, "[1, 2, 3]\n"}, // no block still works
		// String#[substr] / #slice(substr): the substring if present, else nil.
		{`p "hello world"["world"]`, "\"world\"\n"},
		{`p "hello"["xyz"]`, "nil\n"},
		{`p "hello".slice("ell")`, "\"ell\"\n"},
		// String#index with a start offset (character-based, negative from the end).
		{`p ["abcabc".index("bc"), "abcabc".index("bc", 2), "abcabc".index("z")]`, "[1, 4, nil]\n"},
		{`p "hello".index("l", -2)`, "3\n"},
		{`p "hello".index("l", 99)`, "nil\n"},  // offset past the end
		{`p "hello".index("h", -99)`, "nil\n"}, // offset before the start
		{`p "café".index("é")`, "3\n"},         // multibyte
		// String#rindex.
		{`p ["hello".rindex("l"), "hello".rindex("z"), "hello".rindex("h")]`, "[3, nil, 0]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// A comparator block returning a non-Integer fails like MRI ("... with 0 failed").
	if err := runErr(t, `[1, 2].sort { |a, b| "x" }`); err == nil || !strings.Contains(err.Error(), "comparison of String with 0 failed") {
		t.Errorf("sort bad-comparator err=%v, want \"comparison of String with 0 failed\"", err)
	}
}
