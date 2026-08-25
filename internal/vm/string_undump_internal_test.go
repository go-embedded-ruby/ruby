// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStringUndump covers String#undump parsing a String#dump literal back into
// the original string: the named escapes, \" \\ \#, \xHH, \uXXXX and \u{…}
// (single, multiple, and empty), printable passthrough, the .force_encoding
// suffix, and a String#dump round trip — including a non-ASCII-compatible source.
// Verified against ruby 4.0.6.
func TestStringUndump(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p '"foo"'.undump`, `"foo"`},
		{`p '""'.undump`, `""`},
		// Named escapes decode to their control byte.
		{`p '"\a\b\t\n\v\f\r\e"'.undump.bytes`, `[7, 8, 9, 10, 11, 12, 13, 27]`},
		// \" \\ and \# unescape.
		{`p "\"\\\"\\\\\"".undump`, `"\"\\"`},
		{`p '"\#{a}"'.undump`, `"\#{a}"`},
		// # is literal when not part of an escape.
		{`p '"#1 # #a"'.undump`, `"#1 # #a"`},
		// \xHH (either case) becomes the byte.
		{`p '"\x41\x7F"'.undump`, `"A\u007F"`},
		{`p '"\xab"'.undump.bytes`, `[171]`},
		// \uXXXX and \u{…} become UTF-8 code points.
		{`p '"A"'.undump`, `"A"`},
		{`p '"\u00E9"'.undump`, `"é"`},
		{`p '"\u{80}"'.undump.bytes`, `[194, 128]`},
		{`p '"\u{41 42 43}"'.undump`, `"ABC"`},
		{`p '"\u{}"'.undump`, `""`},
		// An unrecognized escape keeps its backslash.
		{`p '"\q"'.undump`, `"\\q"`},
		// A String#dump round trip is the identity.
		{`p "café\t\"x\" ok".dump.undump == "café\t\"x\" ok"`, `true`},
		// Round trip through a non-ASCII-compatible encoding.
		{`s = "\u{876}".encode('utf-16be'); p s.dump.undump == s`, `true`},
		{`p '"\bv".force_encoding("UTF-16BE")'.undump == "ࡶ".encode('utf-16be')`, `true`},
		// The result keeps the receiver's (ASCII-compatible) encoding.
		{`p '"foo"'.encode("ISO-8859-1").undump.encoding.name`, `"ISO-8859-1"`},
		// dump ignores frozen and undump returns a plain String instance.
		{`p '"foo"'.freeze.undump.frozen?`, `false`},
		{`p '"foo"'.undump.instance_of?(String)`, `true`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestStringUndumpErrors covers every RuntimeError (and the CompatibilityError)
// String#undump raises: a missing or excess wrapping quote, a malformed \x or \u
// escape, an out-of-range code point, a raw NUL or non-ASCII byte, a trailing
// backslash, and a malformed or unknown .force_encoding suffix. Verified against
// ruby 4.0.6.
func TestStringUndumpErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`begin; 'foo'.undump; rescue => e; p e.message; end`, `"invalid dumped string; not wrapped with '\"' nor '\"...\".force_encoding(\"...\")' form"`},
		{`begin; '"foo'.undump; rescue => e; p e.message; end`, `"unterminated dumped string"`},
		{`begin; 'foo"'.undump; rescue => e; p e.message; end`, `"invalid dumped string; not wrapped with '\"' nor '\"...\".force_encoding(\"...\")' form"`},
		{`begin; '" "" "'.undump; rescue => e; p e.message; end`, `"invalid dumped string; not wrapped with '\"' nor '\"...\".force_encoding(\"...\")' form"`},
		{`begin; '"\x"'.undump; rescue => e; p e.message; end`, `"invalid hex escape"`},
		{`begin; '"\x3y"'.undump; rescue => e; p e.message; end`, `"invalid hex escape"`},
		{`begin; '"\u"'.undump; rescue => e; p e.message; end`, `"invalid Unicode escape"`},
		{`begin; '"\u{"'.undump; rescue => e; p e.message; end`, `"invalid Unicode escape"`},
		{`begin; '"\u{3042"'.undump; rescue => e; p e.message; end`, `"invalid Unicode escape"`},
		{`begin; '"\u{zz}"'.undump; rescue => e; p e.message; end`, `"invalid Unicode escape"`},
		{`begin; '"\u{110000}"'.undump; rescue => e; p e.message; end`, `"invalid Unicode codepoint (too large)"`},
		{`begin; '"\z'.undump; rescue => e; p e.message; end`, `"unterminated dumped string"`},
		{`begin; s = %Q{"foo\0"}; s.undump; rescue => e; p e.message; end`, `"string contains null byte"`},
		{`begin; s = %Q{"あ"}; s.undump; rescue => e; p e.message; end`, `"non-ASCII character detected"`},
		{`begin; '"".force_encoding("Unknown")'.undump; rescue => e; p e.message; end`, `"dumped string has unknown encoding name"`},
		{`begin; '"".force_encoding("BINARY"'.undump; rescue => e; p e.message; end`, `"invalid dumped string; not wrapped with '\"' nor '\"...\".force_encoding(\"...\")' form"`},
		{`begin; '"".force_encoding()'.undump; rescue => e; p e.message; end`, `"invalid dumped string; not wrapped with '\"' nor '\"...\".force_encoding(\"...\")' form"`},
		// A trailing backslash with no following byte is an invalid escape.
		{`begin; s = %Q{"\\}; s.undump; rescue => e; p e.message; end`, `"invalid escape"`},
		// A non-ASCII-compatible receiver cannot be undumped.
		{`begin; '"foo"'.encode("utf-16le").undump; rescue => e; p e.class; end`, `Encoding::CompatibilityError`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
