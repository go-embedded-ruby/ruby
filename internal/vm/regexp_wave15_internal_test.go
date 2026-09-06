// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// bs is a single backslash. Test sources are written with "\x5c" rather than a
// literal backslash-u so the Go-source \u escape is never triggered.
const bs = "\x5c"

// TestSourceHasNonASCIIUnicodeEscape exercises every branch of the source scan
// that decides whether a \u escape ties a Regexp to UTF-8. Several inputs (a
// lone trailing backslash, a malformed or unterminated \u{…}, a short \uHH) can
// only be reached by calling the helper directly, since a Regexp carrying them
// would fail to compile before #encoding is ever asked.
func TestSourceHasNonASCIIUnicodeEscape(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"abc", false},                        // no backslash at all
		{"a" + bs, false},                     // trailing backslash: nothing follows
		{bs + "d+", false},                    // some other escape, not \u
		{bs + bs + "u{9879}", false},          // escaped backslash: the u is literal
		{bs + "u{9879}", true},                // brace form, non-ASCII code point
		{bs + "u{41}", false},                 // brace form, ASCII code point only
		{bs + "u{41 9879}", true},             // brace list: one member is non-ASCII
		{bs + "u{zz}", false},                 // brace body is not valid hex
		{bs + "u{9879", false},                // unterminated brace form
		{bs + "u9879", true},                  // fixed 4-hex form, non-ASCII
		{bs + "u0041", false},                 // fixed 4-hex form, ASCII
		{bs + "u12", false},                   // fewer than 4 hex digits after \u
		{bs + "u0041y" + bs + "u9879z", true}, // continues past an ASCII escape
	}
	for _, c := range cases {
		if got := sourceHasNonASCIIUnicodeEscape(c.src); got != c.want {
			t.Errorf("sourceHasNonASCIIUnicodeEscape(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}

// TestPatternHasBackrefOrCall exercises every branch of the linear-time scan.
func TestPatternHasBackrefOrCall(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"abc", false},                  // plain literal
		{"(a)" + bs + "1", true},        // numbered back-reference
		{"(?<n>a)" + bs + "k<n>", true}, // named back-reference, angle form
		{"(?<n>a)" + bs + "k'n'", true}, // named back-reference, quote form
		{"(?<n>a)" + bs + "g<n>", true}, // subexpression call, angle form
		{"(?<n>a)" + bs + "g'n'", true}, // subexpression call, quote form
		{"[" + bs + "1]", false},        // a digit escape inside a class is octal
		{"[abc]x", false},               // class open and close, then a plain char
		{"a" + bs, false},               // trailing backslash: nothing follows
		{"a" + bs + "d", false},         // an unrelated escape consumes its byte
		{bs + "ka", false},              // \k not followed by < or ' is not a ref
	}
	for _, c := range cases {
		if got := patternHasBackrefOrCall(c.src); got != c.want {
			t.Errorf("patternHasBackrefOrCall(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}

// TestRegexpQuoteEncodingHelpers covers the two small helpers that decide a
// Regexp.quote / .escape result's encoding tag.
func TestRegexpQuoteEncodingHelpers(t *testing.T) {
	if got := regexpQuoteResultEnc("abc", "UTF-8"); got != "US-ASCII" {
		t.Errorf("regexpQuoteResultEnc(ascii) = %q, want US-ASCII", got)
	}
	if got := regexpQuoteResultEnc("caf\xc3\xa9", "UTF-8"); got != "UTF-8" {
		t.Errorf("regexpQuoteResultEnc(non-ascii) = %q, want UTF-8", got)
	}
	if got := regexpQuoteResultEnc("\xff", "ASCII-8BIT"); got != "ASCII-8BIT" {
		t.Errorf("regexpQuoteResultEnc(binary) = %q, want ASCII-8BIT", got)
	}
	if got := regexpOperandEncName(object.NewStringBytesEnc([]byte("x"), "EUC-JP")); got != "EUC-JP" {
		t.Errorf("regexpOperandEncName(String) = %q, want EUC-JP", got)
	}
	if got := regexpOperandEncName(object.Symbol("sym")); got != "UTF-8" {
		t.Errorf("regexpOperandEncName(Symbol) = %q, want UTF-8", got)
	}
}

// TestRegexpLinearTimeAndFriends drives the Ruby-visible methods so every branch
// of the native closures runs. Each expectation was checked against MRI 4.0.5.
// The VM sends #warn to the same buffer as stdout, so the flags-ignored warning
// is asserted inline before the result.
func TestRegexpLinearTimeAndFriends(t *testing.T) {
	// A backslash for building Ruby source snippets without a Go \u escape.
	cases := []struct{ src, want string }{
		// linear_time?: linear unless a back-reference / subexpression call.
		{`p(Regexp.linear_time?(/a/))`, "true\n"},
		{`p(Regexp.linear_time?("a"))`, "true\n"},
		{`p(Regexp.linear_time?("a", Regexp::IGNORECASE))`, "true\n"},
		{`p(Regexp.linear_time?(/(a)` + bs + `1/))`, "false\n"},
		{`p(Regexp.linear_time?("(a)` + bs + bs + `1"))`, "false\n"},
		// A Regexp argument ignores flags and warns (warning shares stdout here).
		{`p(Regexp.linear_time?(/a/, Regexp::IGNORECASE))`, "warning: flags ignored\ntrue\n"},
		// named_captures collects every index a repeated name owns.
		{`p(/(?<a>.)(?<b>.)(?<a>.)/.named_captures)`, "{\"a\" => [1, 3], \"b\" => [2]}\n"},
		// \u escape with a non-ASCII code point fixes UTF-8 (literal source).
		{`p(/` + bs + `u{9879}/.encoding)`, "#<Encoding:UTF-8>\n"},
		{`p(/` + bs + `u{9879}/.fixed_encoding?)`, "true\n"},
		{`p(/A/.encoding)`, "#<Encoding:US-ASCII>\n"},
		// Regexp.quote / .escape result encoding follows the input.
		{`p(Regexp.quote("abc").encoding)`, "#<Encoding:US-ASCII>\n"},
		{`p(Regexp.quote("café").encoding)`, "#<Encoding:UTF-8>\n"},
		{`p(Regexp.escape(:sym).encoding)`, "#<Encoding:US-ASCII>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRegexpLinearTimeZeroArgs covers the argument-count guard.
func TestRegexpLinearTimeZeroArgs(t *testing.T) {
	class, msg := evalErr(t, `Regexp.linear_time?`)
	if class != "ArgumentError" || msg != "wrong number of arguments (given 0, expected 1..2)" {
		t.Errorf("got %s: %q", class, msg)
	}
}
