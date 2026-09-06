// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestRegexpEncodingNegotiationHelpers exercises the small pure helpers that back
// the wave-17 Regexp encoding work directly, so every branch runs even for inputs
// a compiled Regexp could never reach.
func TestRegexpEncodingNegotiationHelpers(t *testing.T) {
	// encIsASCIICompat: a registered ASCII-compatible name, a registered
	// non-ASCII-compatible one, and an unknown name (conservatively compatible).
	if !encIsASCIICompat("UTF-8") {
		t.Error("encIsASCIICompat(UTF-8) = false, want true")
	}
	if encIsASCIICompat("UTF-16LE") {
		t.Error("encIsASCIICompat(UTF-16LE) = true, want false")
	}
	if !encIsASCIICompat("no-such-encoding") {
		t.Error("encIsASCIICompat(unknown) = false, want true (default)")
	}

	// mapRegexpEngineError rewrites only the two pinned Onigmo wordings; anything
	// else passes through unchanged.
	for _, c := range []struct{ in, want string }{
		{"syntax error: trailing backslash", "too short escape sequence"},
		{"syntax error: missing closing ] in character class", "premature end of char-class"},
		{"syntax error: missing closing )", "syntax error: missing closing )"},
	} {
		if got := mapRegexpEngineError(c.in); got != c.want {
			t.Errorf("mapRegexpEngineError(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// allHex: empty is never hex; a mixed-case hex run is; a run with any non-hex
	// digit is not.
	if allHex("") {
		t.Error("allHex(empty) = true, want false")
	}
	if !allHex("0aF9") {
		t.Error("allHex(0aF9) = false, want true")
	}
	if allHex("abcX") {
		t.Error("allHex(abcX) = true, want false")
	}

	// inUnicodeRange: at/below U+10FFFF is in range, above is not.
	if !inUnicodeRange("10FFFF") {
		t.Error("inUnicodeRange(10FFFF) = false, want true")
	}
	if inUnicodeRange("110000") {
		t.Error("inUnicodeRange(110000) = true, want false")
	}

	// regexpQuoteResultEnc: ASCII content in an ASCII-compatible encoding is
	// US-ASCII, but a non-ASCII-compatible encoding is kept even for ASCII content.
	if got := regexpQuoteResultEnc("a", "UTF-8"); got != "US-ASCII" {
		t.Errorf("regexpQuoteResultEnc(ascii, UTF-8) = %q, want US-ASCII", got)
	}
	if got := regexpQuoteResultEnc("a", "UTF-16LE"); got != "UTF-16LE" {
		t.Errorf("regexpQuoteResultEnc(ascii, UTF-16LE) = %q, want UTF-16LE", got)
	}
}

// TestRegexpWave17Values drives the Ruby-visible behavior whose result is a
// value: union encoding negotiation, the Regexp encoding / source / fixed_encoding?
// helpers, the option-flag legacy forms, the subject-encoding no-op path, and the
// Regexp::TimeoutError constant. Every expectation was checked against MRI 4.0.5.
func TestRegexpWave17Values(t *testing.T) {
	cases := []struct{ src, want string }{
		// Regexp.union structural forms.
		{`p(Regexp.union == /(?!)/)`, "true\n"},
		{`p(Regexp.union("a.") == /a\./)`, "true\n"},
		{`p(Regexp.union(:foo) == /foo/)`, "true\n"},
		{`p(Regexp.union(/foo/i) == /foo/i)`, "true\n"},
		{`p(Regexp.union(["skiing", "sledding"]) == /skiing|sledding/)`, "true\n"},
		{`p(Regexp.union("n", ".") == /n|\./)`, "true\n"},

		// Union encoding negotiation: all-ASCII → US-ASCII; ASCII-incompatible
		// operands carry through; ASCII-compatible non-ASCII operands fix their
		// encoding; a Regexp operand is negotiated just like a String.
		{`p(Regexp.union("a", "b").encoding)`, "#<Encoding:US-ASCII>\n"},
		{`p(Regexp.union(/a/, /b/).encoding)`, "#<Encoding:US-ASCII>\n"},
		{`p(Regexp.union("a".encode("UTF-16LE"), "b".encode("UTF-16LE")).encoding)`, "#<Encoding:UTF-16LE>\n"},
		{`p(Regexp.union(Regexp.new("a".encode("UTF-16LE")), Regexp.new("b".encode("UTF-16LE"))).encoding)`, "#<Encoding:UTF-16LE>\n"},
		{`p(Regexp.union("©".encode("ISO-8859-1"), "°".encode("ISO-8859-1")).encoding)`, "#<Encoding:ISO-8859-1>\n"},

		// A Regexp built from a non-ASCII-compatible String reports and is fixed to
		// that encoding even though its bytes are ASCII; FIXEDENCODING pins an
		// ASCII-only pattern to its source String's encoding.
		{`p(Regexp.new("a".encode("UTF-16LE")).encoding)`, "#<Encoding:UTF-16LE>\n"},
		{`p(Regexp.new("a".encode("UTF-16LE")).fixed_encoding?)`, "true\n"},
		{`p(Regexp.new("a".encode("UTF-8"), Regexp::FIXEDENCODING).encoding)`, "#<Encoding:UTF-8>\n"},
		{`p(Regexp.new("b".encode("US-ASCII"), Regexp::FIXEDENCODING).encoding)`, "#<Encoding:US-ASCII>\n"},

		// Regexp#source carries the pattern's own encoding.
		{`p(Regexp.new("abc").source.encoding)`, "#<Encoding:US-ASCII>\n"},
		{`p(Regexp.new("` + bs + bs + `u{ff}").source.encoding)`, "#<Encoding:UTF-8>\n"},

		// Second-argument legacy forms: true/false select IGNORECASE/nothing, and
		// any other object selects IGNORECASE (with a verbose-only warning that is
		// silent here, $VERBOSE being unset).
		{`p(Regexp.new("a", true).options)`, "1\n"},
		{`p(Regexp.new("a", false).options)`, "0\n"},
		{`p(Regexp.new("a", Object.new).options)`, "1\n"},

		// A malformed \u whose braces stay balanced but empty/short compiles fine
		// when well-formed; a non-\u escape and an escaped backslash are literal.
		{`p(Regexp.new("` + bs + bs + `u{41}").source)`, `"` + bs + bs + `u{41}"` + "\n"},
		{`p(Regexp.new("` + bs + bs + `d").source)`, `"` + bs + bs + `d"` + "\n"},

		// checkSubjectEncoding leaves a non-String subject to coercion (a Symbol
		// still matches), and a valid String matches normally.
		{`p(/a/.match(:abc)[0])`, `"a"` + "\n"},
		{`p(/a/.match?("abc"))`, "true\n"},

		// Regexp::TimeoutError is a real class under RegexpError (raising it is not
		// possible with the pure-Go engine, but the constant exists for parity).
		{`p(Regexp::TimeoutError < RegexpError)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRegexpWave17Errors drives the wave-17 paths that raise: malformed \u
// escapes (the Ruby-specific messages), the engine wordings remapped to MRI's,
// the Regexp.union encoding conflicts, and the invalid-subject-encoding guard on
// #match / #match?. Every message was checked against MRI 4.0.5.
func TestRegexpWave17Errors(t *testing.T) {
	cases := []struct{ src, class, msg string }{
		// Malformed \u escapes: list / range / escape, plus the value-range path.
		{`Regexp.new("` + bs + bs + `u{}")`, "RegexpError", `invalid Unicode list: /` + bs + `u{}/`},
		{`Regexp.new("` + bs + bs + `u{abcX}")`, "RegexpError", `invalid Unicode list: /` + bs + `u{abcX}/`},
		{`Regexp.new("` + bs + bs + `u{0ffffff}")`, "RegexpError", `invalid Unicode range: /` + bs + `u{0ffffff}/`},
		{`Regexp.new("` + bs + bs + `u{110000}")`, "RegexpError", `invalid Unicode range: /` + bs + `u{110000}/`},
		{`Regexp.new("` + bs + bs + `u304")`, "RegexpError", `invalid Unicode escape: /` + bs + `u304/`},
		// An unterminated \u{ is left for the engine (translateUnicodeEscapes copies
		// it through rather than raising a Unicode-list error).
		{`Regexp.new("` + bs + bs + `u{")`, "RegexpError", `syntax error: unsupported escape ` + bs + `u: /` + bs + `u{/`},
		// Engine wordings remapped to MRI's, and one passed through unchanged.
		{`Regexp.new("a` + bs + bs + `")`, "RegexpError", `too short escape sequence: /a` + bs + `/`},
		{`Regexp.new("^[$")`, "RegexpError", `premature end of char-class: /^[$/`},
		{`Regexp.new("(")`, "RegexpError", `syntax error: missing closing ): /(/`},
		// Regexp.union encoding conflicts (each raises in argument order).
		{`Regexp.union("a".encode("UTF-16LE"), "b".encode("UTF-16BE"))`, "ArgumentError", "incompatible encodings: UTF-16LE and UTF-16BE"},
		{`Regexp.union("a".encode("UTF-16LE"), "b")`, "ArgumentError", "ASCII incompatible encoding: UTF-16LE"},
		{`Regexp.union("a".encode("UTF-16LE"), "©".encode("ISO-8859-1"))`, "ArgumentError", "incompatible encodings: UTF-16LE and ISO-8859-1"},
		{`Regexp.union(Regexp.new("a".encode("UTF-8"), Regexp::FIXEDENCODING), "b".encode("UTF-16LE"))`, "ArgumentError", "incompatible encodings: UTF-16LE and UTF-8"},
		{`Regexp.union(Regexp.new("a".encode("UTF-8"), Regexp::FIXEDENCODING), Regexp.new("b".encode("US-ASCII"), Regexp::FIXEDENCODING))`, "ArgumentError", "incompatible encodings: UTF-8 and US-ASCII"},
		// A Symbol has no #to_str in the multi-pattern form.
		{`Regexp.union("a", :b)`, "TypeError", "no implicit conversion of Symbol into String"},
		// An invalid-encoding subject cannot be matched.
		{`x = [150].pack("C").force_encoding("utf-8"); /a/.match(x)`, "ArgumentError", "invalid byte sequence in UTF-8"},
		{`x = [150].pack("C").force_encoding("utf-8"); /a/.match?(x)`, "ArgumentError", "invalid byte sequence in UTF-8"},
	}
	for _, c := range cases {
		class, msg := evalErr(t, c.src)
		if class != c.class || msg != c.msg {
			t.Errorf("src=%q\n got %s: %q\nwant %s: %q", c.src, class, msg, c.class, c.msg)
		}
	}
}
