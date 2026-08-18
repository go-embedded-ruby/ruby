// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringInspectEscaping covers String#inspect's escaping: named control
// escapes, \uXXXX for other UTF-8 control characters and the line/paragraph
// separators, \xXX for binary/US-ASCII bytes and invalid UTF-8, and verbatim
// output for printable, format and non-ASCII-space characters. Verified against
// ruby 4.0.6.
func TestStringInspectEscaping(t *testing.T) {
	// Cases whose inspect output is pure ASCII are compared directly (via puts, so
	// the escaped text -- never a raw control byte -- reaches stdout).
	asciiCases := []struct{ src, want string }{
		// Named escapes plus quote and backslash.
		{`"\a\b\t\n\v\f\r\e".inspect`, `"\a\b\t\n\v\f\r\e"`},
		{`"\"".inspect`, `"\""`},
		{`"\\".inspect`, `"\\"`},
		// # is escaped only before an interpolation sigil.
		{`"\#{".inspect`, `"\#{"`},
		{`"\#$".inspect`, `"\#$"`},
		{`"\#@".inspect`, `"\#@"`},
		{`"#a".inspect`, `"#a"`},
		{`"#".inspect`, `"#"`},
		// Other ASCII control characters (and DEL) use \uXXXX in a UTF-8 string.
		{`"\x00\x01\x1f\x7f".inspect`, `"\u0000\u0001\u001F\u007F"`},
		{`"\u{85}\u{2028}\u{2029}".inspect`, `"\u0085\u2028\u2029"`},
		// Invalid UTF-8 bytes escape as \xXX (uppercase).
		{`"\xFF".inspect`, `"\xFF"`},
		{`"\xF0\x9F".inspect`, `"\xF0\x9F"`},
		// A binary string escapes every non-printable byte as \xXX.
		{`"\x00\x80\xff".b.inspect`, `"\x00\x80\xFF"`},
		// A US-ASCII string escapes non-ASCII bytes as \xXX.
		{`"\xC2\xA9".dup.force_encoding("US-ASCII").inspect`, `"\xC2\xA9"`},
	}
	for _, c := range asciiCases {
		if got := eval(t, "puts "+c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}

	// Printable, format and non-ASCII-space characters stay verbatim: assert
	// through Ruby == so the multibyte bytes never need matching in Go.
	verbatim := []string{
		`"A\u{e9}\u{1f600}".inspect == "\"A\u{e9}\u{1f600}\""`,
		`"\u{202e}".inspect == "\"\u{202e}\""`,
		`"\u{a0}".inspect == "\"\u{a0}\""`,
	}
	for _, src := range verbatim {
		if got := eval(t, "p ("+src+")"); got != "true\n" {
			t.Errorf("src=%q got=%q want true", src, got)
		}
	}
}
