// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestPackUuencode covers Array#pack's 'u' directive: the length character and
// 1/2/3-byte group encoding, the count modifier selecting bytes-per-line (45 by
// default, rounded down to a multiple of three otherwise), an empty input, one
// element per directive, a US-ASCII result, and the TypeError for a non-String
// element. Expectations use \x60 for the backtick (value-zero) character.
// Verified against ruby 4.0.6.
func TestPackUuencode(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p ["a"].pack("u") == "!80\x60\x60\n"`, `true`},
		{`p ["ab"].pack("u") == "\"86(\x60\n"`, `true`},
		{`p ["abc"].pack("u") == "#86)C\n"`, `true`},
		{`p ["hello"].pack("u") == "%:&5L;&\\\x60\n"`, `true`},
		{`p [""].pack("u") == ""`, `true`},
		// Count modifiers <= 2 (and * and none) mean 45 bytes per line.
		{`p ["a"].pack("u0") == ["a"].pack("u")`, `true`},
		{`p ["a"].pack("u*") == ["a"].pack("u")`, `true`},
		{`p ["a"].pack("u2") == ["a"].pack("u")`, `true`},
		// A count >= 3 is rounded down to a multiple of three.
		{`p ["abcdefg"].pack("u3") == "#86)C\n#9&5F\n!9P\x60\x60\n"`, `true`},
		{`p ["abcdefghijklm"].pack("u7") == "&86)C9&5F\n&9VAI:FML\n!;0\x60\x60\n"`, `true`},
		// One element per directive.
		{`p ["abc", "DEF"].pack("uu") == "#86)C\n#1$5&\n"`, `true`},
		// The result is US-ASCII.
		{`p ["abcd"].pack("u").encoding.name`, `"US-ASCII"`},
		// A non-String element (Integer, nil) raises TypeError.
		{`begin; [0].pack("u"); rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; [nil].pack("u"); rescue TypeError => e; p e.class; end`, `TypeError`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestUnpackUudecode covers String#unpack's 'u' directive: decoding across
// newline-separated lines with a single directive, empty strings for directives
// beyond the input, the count/'*' modifier being ignored, an empty input, and
// the BINARY result encoding. Verified against ruby 4.0.6.
func TestUnpackUudecode(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "#86)C\n#1$5&\n".unpack("u")`, `["abcDEF"]`},
		{`p "#86)C\n#1$5&\n".unpack("uuu")`, `["abcDEF", "", ""]`},
		{`p "#86)C\n#1$5&\n".unpack("u*")`, `["abcDEF"]`},
		{`p "#86)C\n#1$5&\n".unpack("u238")`, `["abcDEF"]`},
		{`p "".unpack("u")`, `[""]`},
		{`p "".unpack("u")[0].encoding.name`, `"ASCII-8BIT"`},
		{`p "!80\x60\x60\n".unpack("u")`, `["a"]`},
		// CR?LF line endings are consumed; other trailing characters stop decoding.
		{`p "#86)C\r\n#1$5&\r\n".unpack("u")`, `["abcDEF"]`},
		{`p "#86)C   junk\n#1$5&\n".unpack("u")`, `["abc"]`},
		// A round trip is the identity for every byte value.
		{`s = (0..255).to_a.pack("C*"); p [s].pack("u").unpack("u")[0].bytes == s.bytes`, `true`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
