// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Wave-34 encoding conformance tests.
//
// TestEconvErrorAttributes, TestEncodeSameEncoding,
// TestValidInEncodingUsesTheScanner and TestUnicodeNormalizeEncodingDispatch are
// WITNESSES: each was run against the unfixed product (the behaviour reverted while
// the new symbols stayed, so the mutation still compiled) and each failed. The
// remaining tests in this file cover helpers that only exist with the fix, so they
// are GUARDS against future drift rather than witnesses of it.

package vm

import (
	"io"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestEconvErrorAttributes covers the transcoding-error attributes MRI's
// make_econv_exception (transcode.c) attaches to the exception it builds, and the
// fact that rb_econv_check_error raises that very object — so the rescued
// exception and Encoding::Converter#last_error are one object with one set of
// attributes.
//
// A/B: every case here fails before the fix with NoMethodError, because the
// accessors did not exist and #convert raised a second, attribute-less exception.
func TestEconvErrorAttributes(t *testing.T) {
	cases := []struct{ src, want string }{
		// undefined_conversion: @error_char is the failing hop's bytes tagged with
		// that hop's SOURCE encoding, and the names/objects are the hop's, not the
		// converter's endpoints.
		{`ec = Encoding::Converter.new("utf-8", "ascii")
begin; ec.convert("\u{8765}"); rescue Encoding::UndefinedConversionError => e
  p [e.source_encoding, e.source_encoding_name, e.destination_encoding, e.destination_encoding_name]
  p [e.error_char, e.error_char.encoding]
end`, `[#<Encoding:UTF-8>, "UTF-8", #<Encoding:US-ASCII>, "US-ASCII"]
["蝥", #<Encoding:UTF-8>]`},
		// A multi-hop path reports the hop that failed (ISO-8859-1 -> UTF-8 -> EUC-JP
		// fails on the UTF-8 -> EUC-JP leg), which is the case the spec's comment
		// singles out.
		{`ec = Encoding::Converter.new("ISO-8859-1", "EUC-JP")
begin; ec.convert("\xA0"); rescue Encoding::UndefinedConversionError => e
  p [e.source_encoding_name, e.destination_encoding_name, e.error_char.size]
end`, `["UTF-8", "EUC-JP", 1]`},
		// invalid_byte_sequence: @error_bytes and @readagain_bytes are BINARY, and
		// @incomplete_input is false.
		{`ec = Encoding::Converter.new("utf-8", "iso-8859-1")
begin; ec.convert("\xf1abcd"); rescue Encoding::InvalidByteSequenceError => e
  p [e.error_bytes, e.error_bytes.encoding, e.readagain_bytes, e.readagain_bytes.encoding]
  p [e.incomplete_input?, e.source_encoding_name, e.destination_encoding_name]
end`, `["\xF1", #<Encoding:BINARY (ASCII-8BIT)>, "a", #<Encoding:BINARY (ASCII-8BIT)>]
[false, "UTF-8", "ISO-8859-1"]`},
		// incomplete_input reaches the same builder through #finish, where there are
		// no read-again bytes, so @readagain_bytes is nil rather than an empty String.
		{`ec = Encoding::Converter.new("EUC-JP", "ISO-8859-1")
ec.convert("abc\xA1")
begin; ec.finish; rescue Encoding::InvalidByteSequenceError => e
  p [e.incomplete_input?, e.error_bytes, e.readagain_bytes]
end`, `[true, "\xA1", nil]`},
		// The raised object IS #last_error (rb_econv_check_error passes
		// make_econv_exception's result straight to rb_exc_raise).
		{`ec = Encoding::Converter.new("utf-8", "ascii")
begin; ec.convert("\u{8765}"); rescue => e; p e.equal?(ec.last_error); end`, `true`},
		// Each accessor is rb_attr_get of one ivar, so an exception built any other
		// way answers nil instead of raising.
		{`e = Encoding::InvalidByteSequenceError.new
p [e.incomplete_input?, e.error_bytes, e.readagain_bytes, e.source_encoding, e.destination_encoding_name]`,
			`[nil, nil, nil, nil, nil]`},
		// UndefinedConversionError has no #error_bytes and InvalidByteSequenceError no
		// #error_char: transcode.c defines each only on its own class.
		{`p Encoding::UndefinedConversionError.new.respond_to?(:error_bytes)
p Encoding::InvalidByteSequenceError.new.respond_to?(:error_char)
p Encoding::UndefinedConversionError.new.respond_to?(:error_char)`, `false
false
true`},
	}
	for _, c := range cases {
		if got := strings.TrimRight(eval(t, c.src), "\n"); got != c.want {
			t.Errorf("src:\n%s\ngot:\n%s\nwant:\n%s", c.src, got, c.want)
		}
	}
}

// TestSetEconvErrorAttrsUnknownStatus covers setEconvErrorAttrs's guard directly: a
// status that is not one of the three transcoding failures attaches nothing, so the
// encoding names are not stamped onto an exception that has no error to describe.
// make_econv_exception returns Qnil in that case rather than building anything.
func TestSetEconvErrorAttrsUnknownStatus(t *testing.T) {
	vm := New(io.Discard)
	exc := vm.buildException("RuntimeError", "not a transcoding error")
	vm.setEconvErrorAttrs(exc, "source_buffer_empty", []byte("x"), nil, "UTF-8", "EUC-JP")
	if got := getIvar(exc, "@source_encoding_name"); got != object.NilV {
		t.Errorf("@source_encoding_name = %v, want nil for a non-error status", got.Inspect())
	}
}

// TestSetEconvErrorAttrsUnregisteredEncoding covers the set_encs branch that MRI
// guards with `if (0 <= idx)`: an encoding NAME is always recorded, but the Encoding
// OBJECT only when the name resolves to a registered encoding.
func TestSetEconvErrorAttrsUnregisteredEncoding(t *testing.T) {
	vm := New(io.Discard)
	vm.registerEncodingErrors()
	exc := vm.buildException("Encoding::UndefinedConversionError", "synthetic")
	vm.setEconvErrorAttrs(exc, "undefined_conversion", []byte("x"), nil, "X-NOT-AN-ENCODING", "Y-NOR-THIS")
	if got := getIvar(exc, "@source_encoding_name").ToS(); got != "X-NOT-AN-ENCODING" {
		t.Errorf("@source_encoding_name = %q, want the name as given", got)
	}
	if got := getIvar(exc, "@source_encoding"); got != object.NilV {
		t.Errorf("@source_encoding = %v, want nil for an unregistered name", got.Inspect())
	}
	if got := getIvar(exc, "@destination_encoding"); got != object.NilV {
		t.Errorf("@destination_encoding = %v, want nil for an unregistered name", got.Inspect())
	}
	// The error char keeps the receiver's default tag when the source encoding is
	// not one rbgo knows, rather than being tagged with a name nothing can resolve.
	if ec := getIvar(exc, "@error_char"); ec.ToS() != "x" {
		t.Errorf("@error_char = %q, want %q", ec.ToS(), "x")
	}
}

// TestCanonicalEncName covers the registry resolution the transcoding path needs
// because rbgo tags a String with an encoding NAME where MRI stores an index.
func TestCanonicalEncName(t *testing.T) {
	vm := New(io.Discard)
	cases := map[string]string{
		"iso-8859-9":        "ISO-8859-9",
		"UTF-8":             "UTF-8",
		"binary":            "ASCII-8BIT",
		"eucJP":             "EUC-JP",
		"X-NOT-AN-ENCODING": "X-NOT-AN-ENCODING", // left as written
	}
	for in, want := range cases {
		if got := vm.canonicalEncName(in); got != want {
			t.Errorf("canonicalEncName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTranscodeOptsHasDecorator covers each decorator str_transcode0 gates its
// same-encoding short-circuits on.
func TestTranscodeOptsHasDecorator(t *testing.T) {
	if (transcodeOpts{}).hasDecorator() {
		t.Error("plain options report a decorator")
	}
	for name, o := range map[string]transcodeOpts{
		"cr_newline":        {crNewline: true},
		"crlf_newline":      {crlfNewline: true},
		"universal_newline": {universalNewline: true},
		"xml":               {xml: "text"},
	} {
		if !o.hasDecorator() {
			t.Errorf("%s: hasDecorator() = false", name)
		}
	}
}

// TestEncodeSameEncoding covers the str_transcode0 short-circuits: with matching
// source and destination encodings MRI never looks up a converter, so an explicit
// `invalid:` is a scrub in that one encoding and everything else passes the bytes
// through — even for an encoding rbgo cannot transcode.
//
// A/B: before the fix the first two cases raised
// Encoding::ConverterNotFoundError(Emacs-Mule to Emacs-Mule).
func TestEncodeSameEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [0x80].pack("C").force_encoding("Emacs-Mule").encode(invalid: :replace)`, `"?"`},
		{`p [0x80].pack("C").force_encoding("Emacs-Mule").encode(invalid: :replace, replace: "!")`, `"!"`},
		// Without `invalid:` the bytes are returned unchanged, ill-formed and all.
		{`p [0x80].pack("C").force_encoding("Emacs-Mule").encode.bytes`, `[128]`},
		{`p "abc".force_encoding("Emacs-Mule").encode.encoding`, `#<Encoding:Emacs-Mule>`},
		// A decorator suppresses the short-circuit, so the conversion still runs.
		{`p "a\nb".encode("UTF-8", crlf_newline: true).bytes`, `[97, 13, 10, 98]`},
		// An encoding WITH a scanner scrubs on the same path.
		{"p \"a\\xE2\".dup.force_encoding(\"UTF-8\").encode(invalid: :replace)", "\"a\uFFFD\""},
	}
	for _, c := range cases {
		if got := strings.TrimRight(eval(t, c.src), "\n"); got != c.want {
			t.Errorf("%s\ngot %s want %s", c.src, got, c.want)
		}
	}
}

// TestScrubBytesInWithoutScanner covers scrubBytesIn's early return for an encoding
// rbgo has no character automaton for: the bytes are handed back untouched, which is
// how String#scrub already treats such an encoding.
func TestScrubBytesInWithoutScanner(t *testing.T) {
	in := []byte{0x80, 0x41}
	got := scrubBytesIn(in, "Big5", transcodeOpts{})
	if string(got) != string(in) {
		t.Errorf("scrubBytesIn = %q, want the input unchanged", got)
	}
}

// TestEmacsMuleAutomaton covers the character automaton transcribed from the
// `trans` table of enc/emacs_mule.c, state by state. The three secondary leads take
// different extension ranges, which is the part the grammar comment alone does not
// give.
func TestEmacsMuleAutomaton(t *testing.T) {
	cases := []struct {
		bytes []byte
		n     int
		valid bool
		why   string
	}{
		{[]byte{0x41}, 1, true, "ASCII is a one-byte character"},
		{[]byte{0x80}, 1, false, "0x80 begins no character"},
		{[]byte{0x9E}, 1, false, "0x9E is neither a lead nor a character"},
		{[]byte{0xA0}, 1, false, "a C byte cannot start a character"},
		{[]byte{0x8F, 0xA0}, 2, true, "PRIMARY_CHAR_1: 0x81..0x8F + one C byte"},
		{[]byte{0x8F, 0x41}, 1, false, "a byte below 0xA0 cannot be C1"},
		{[]byte{0x8F}, 1, false, "input ends mid-character"},
		{[]byte{0x90, 0xA0, 0xA0}, 3, true, "PRIMARY_CHAR_2: 0x90..0x99 + two C bytes"},
		{[]byte{0x90, 0xA0, 0x41}, 2, false, "the maximal ill-formed subpart is what was consumed"},
		{[]byte{0x90, 0x41}, 1, false, "C1 must be a C byte"},
		{[]byte{0x9A, 0xE0, 0xA0}, 3, true, "0x9A/0x9B take an extension in 0xE0..0xEF"},
		{[]byte{0x9A, 0xF0, 0xA0}, 1, false, "0xF0 is out of 0x9A's extension range"},
		{[]byte{0x9C, 0xF0, 0xA0, 0xA0}, 4, true, "0x9C takes an extension in 0xF0..0xF4 and two C bytes"},
		{[]byte{0x9C, 0xF5, 0xA0, 0xA0}, 1, false, "0xF5 is out of 0x9C's extension range"},
		{[]byte{0x9D, 0xF5, 0xA0, 0xA0}, 4, true, "0x9D takes an extension in 0xF5..0xFE"},
		{[]byte{0x9D, 0xFF, 0xA0, 0xA0}, 1, false, "0xFF is out of 0x9D's extension range"},
		{[]byte{0x9C, 0xF0, 0xA0}, 3, false, "a secondary character cut off by the end of input"},
	}
	for _, c := range cases {
		n, valid := scanEmacsMuleToken(c.bytes)
		if n != c.n || valid != c.valid {
			t.Errorf("scanEmacsMuleToken(% X) = (%d, %v), want (%d, %v) — %s",
				c.bytes, n, valid, c.n, c.valid, c.why)
		}
	}
}

// TestValidInEncodingUsesTheScanner covers validInEncoding agreeing with the
// scrubber: rb_enc_str_coderange asks the encoding's own automaton whether the
// string is BROKEN, so the two cannot hold separate opinions.
//
// A/B: before the fix Emacs-Mule had no automaton here and every byte string was
// reported valid.
func TestValidInEncodingUsesTheScanner(t *testing.T) {
	cases := []struct {
		src string
		ok  bool
	}{
		{`p "\x8F\xA0".dup.force_encoding("Emacs-Mule").valid_encoding?`, true},
		{`p "\x8FA".dup.force_encoding("Emacs-Mule").valid_encoding?`, false},
		{`p "\x9C\xF0\xA0".dup.force_encoding("Emacs-Mule").valid_encoding?`, false},
		{`p "abc".force_encoding("Emacs-Mule").valid_encoding?`, true},
	}
	for _, c := range cases {
		want := "false\n"
		if c.ok {
			want = "true\n"
		}
		if got := eval(t, c.src); got != want {
			t.Errorf("%s: got %q want %q", c.src, got, want)
		}
	}
	// An encoding with neither an automaton nor an x/text codec is treated as always
	// valid, which is the pre-existing fallback and is asserted here as a guard.
	if !validInEncoding([]byte{0xFF}, "X-NOT-AN-ENCODING") {
		t.Error("an encoding with no scanner and no codec should read as valid")
	}
}

// TestUnicodeNormalizeEncodingDispatch covers the encoding dispatch at the head of
// UnicodeNormalize.normalize and .normalized?
// (lib/unicode_normalize/normalize.rb). The encoding decides before the bytes are
// looked at, which is why a non-Unicode encoding reports the incompatibility rather
// than "invalid byte sequence in UTF-8".
//
// A/B: before the fix the first three cases returned a value (normalizing the bytes
// as if they were UTF-8) instead of raising.
func TestUnicodeNormalizeEncodingDispatch(t *testing.T) {
	cases := []struct{ src, want string }{
		{`begin; [0xE0].pack("C").force_encoding("ISO-8859-1").unicode_normalize(:nfd)
rescue => e; p [e.class, e.message]; end`,
			`[Encoding::CompatibilityError, "Unicode Normalization not appropriate for ISO-8859-1"]`},
		{`begin; [0xE0].pack("C").force_encoding("ISO-8859-1").unicode_normalize!
rescue => e; p e.class; end`, `Encoding::CompatibilityError`},
		{`begin; "abc".dup.force_encoding("ISO-8859-1").unicode_normalized?
rescue => e; p e.class; end`, `Encoding::CompatibilityError`},
		// US-ASCII is normalized in every form, and is returned untouched — the bytes
		// are never examined, so a high byte does not raise.
		{`s = [0xE0].pack("C").force_encoding("US-ASCII")
p [s.unicode_normalized?, s.unicode_normalize.bytes, s.unicode_normalize.encoding]`,
			`[true, [224], #<Encoding:US-ASCII>]`},
		{`s = +"abc"; s.force_encoding("US-ASCII"); s.unicode_normalize!; p [s, s.encoding]`,
			`["abc", #<Encoding:US-ASCII>]`},
		// The other Unicode encodings round-trip through UTF-8 and come back in the
		// receiver's encoding.
		{`s = "ẛ̣".encode("UTF-16LE")
n = s.unicode_normalize(:nfc)
p [n.encoding, n.encode("UTF-8") == "ẛ̣".unicode_normalize(:nfc)]`,
			`[#<Encoding:UTF-16LE>, true]`},
		{`p "ẛ̣".encode("UTF-32BE").unicode_normalized?(:nfd)`, `false`},
		{`s = "ẛ̣".encode("UTF-16BE").dup
s.unicode_normalize!(:nfd)
p [s.encoding, s.encode("UTF-8") == "ẛ̣".unicode_normalize(:nfd)]`,
			`[#<Encoding:UTF-16BE>, true]`},
		// The UTF-8 branch still reports an ill-formed receiver as MRI's gsub does.
		{`begin; "a\xE2".dup.unicode_normalize; rescue => e; p [e.class, e.message]; end`,
			`[ArgumentError, "invalid byte sequence in UTF-8"]`},
		// GB18030 is in UNICODE_ENCODINGS even though it is not a UTF, so it is
		// normalizable rather than incompatible.
		{`p "abc".dup.force_encoding("GB18030").unicode_normalized?`, `true`},
	}
	for _, c := range cases {
		if got := strings.TrimRight(eval(t, c.src), "\n"); got != c.want {
			t.Errorf("src:\n%s\ngot:\n%s\nwant:\n%s", c.src, got, c.want)
		}
	}
}

// TestUnicodeNormalizeVia covers the UNICODE_ENCODINGS membership test on both
// sides, including the aliases (UCS-2BE, UCS-4BE) that reach it under their
// canonical names.
func TestUnicodeNormalizeVia(t *testing.T) {
	vm := New(io.Discard)
	for _, name := range []string{"UTF-16BE", "UTF-16LE", "UTF-32BE", "UTF-32LE", "GB18030", "UCS-2BE", "UCS-4BE"} {
		if !unicodeNormalizeVia(vm.canonicalEncName(name)) {
			t.Errorf("%s should normalize through UTF-8", name)
		}
	}
	for _, name := range []string{"ISO-8859-1", "Shift_JIS", "Big5", "EUC-JP", "ASCII-8BIT"} {
		if unicodeNormalizeVia(name) {
			t.Errorf("%s should not normalize through UTF-8", name)
		}
	}
}
