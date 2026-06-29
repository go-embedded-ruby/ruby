// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestUnicodeNormalize covers the unicode_normalize standard library — the
// String core extensions String#unicode_normalize and String#unicode_normalized?
// (backed by github.com/go-ruby-unicode-normalize/unicode-normalize, the
// MRI-4.0.5-faithful port). These methods are always available in MRI without a
// require, so they are asserted with no require here. Every value is asserted
// against MRI 4.0.5's stdlib output.
func TestUnicodeNormalize(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		// NFC composes the combining sequence (e + combining acute) into the
		// precomposed "é"; the precomposed form round-trips through NFC unchanged.
		{`puts "é".unicode_normalize(:nfc)`, "é\n"},
		{`puts "é".unicode_normalize(:nfc)`, "é\n"},
		// The default form (no argument) is :nfc.
		{`puts "é".unicode_normalize`, "é\n"},
		// NFD decomposes "é" into e + combining acute (2 codepoints).
		{`puts "é".unicode_normalize(:nfd).length`, "2\n"},
		{`puts "é".unicode_normalize(:nfd) == "é"`, "true\n"},
		// NFKC applies compatibility composition: the "ﬁ" ligature becomes "fi".
		{`puts "ﬁ".unicode_normalize(:nfkc)`, "fi\n"},
		// NFKD applies compatibility decomposition: the ligature splits to "fi" too.
		{`puts "ﬁ".unicode_normalize(:nfkd)`, "fi\n"},
		// A pure-ASCII string is unchanged by every form.
		{`puts "abc".unicode_normalize(:nfc)`, "abc\n"},

		// unicode_normalized? — true for the canonical form, false otherwise.
		{`puts "é".unicode_normalized?(:nfc)`, "true\n"},
		{`puts "é".unicode_normalized?(:nfd)`, "false\n"},
		{`puts "é".unicode_normalized?(:nfd)`, "true\n"},
		// Default form for the predicate is :nfc too.
		{`puts "é".unicode_normalized?`, "true\n"},
		// Compatibility forms.
		{`puts "ﬁ".unicode_normalized?(:nfkc)`, "false\n"},
		{`puts "fi".unicode_normalized?(:nfkc)`, "true\n"},
		{`puts "ﬁ".unicode_normalized?(:nfkd)`, "false\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestUnicodeNormalizeErrors covers the ArgumentError raises that match MRI
// 4.0.5: an unrecognised form symbol, a non-symbol form argument and an invalid
// UTF-8 receiver — for both #unicode_normalize and #unicode_normalized?.
func TestUnicodeNormalizeErrors(t *testing.T) {
	for _, c := range []struct{ src, class, msg string }{
		// Unknown form symbol -> "Invalid normalization form <to_s>." (the symbol's
		// to_s, exactly as MRI formats it).
		{`"x".unicode_normalize(:bogus)`, "ArgumentError", "Invalid normalization form bogus."},
		{`"x".unicode_normalized?(:bogus)`, "ArgumentError", "Invalid normalization form bogus."},
		// MRI only accepts the four bare symbols; the equivalent string is rejected,
		// with the string's contents in the message.
		{`"x".unicode_normalize("nfc")`, "ArgumentError", "Invalid normalization form nfc."},
		// A non-string, non-symbol argument is rendered via its to_s.
		{`"x".unicode_normalize(123)`, "ArgumentError", "Invalid normalization form 123."},
		{`"x".unicode_normalize(nil)`, "ArgumentError", "Invalid normalization form ."},
		// Invalid UTF-8 receiver -> "invalid byte sequence in UTF-8" (a lone 0xFF byte
		// tagged UTF-8, the same construction MRI rejects with this message).
		{`255.chr.force_encoding("UTF-8").unicode_normalize(:nfc)`, "ArgumentError", "invalid byte sequence in UTF-8"},
		{`255.chr.force_encoding("UTF-8").unicode_normalized?(:nfc)`, "ArgumentError", "invalid byte sequence in UTF-8"},
	} {
		err := runErr(t, c.src)
		if err == nil {
			t.Errorf("src=%q: expected error, got nil", c.src)
			continue
		}
		if !strings.Contains(err.Error(), c.class) || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("src=%q got=%v want class=%q msg=%q", c.src, err, c.class, c.msg)
		}
	}
}

// TestUnicodeNormalizeProvidedFeature proves "unicode_normalize" is a registered
// provided feature: the first require reports it as newly loaded (true) and a
// second require reports it as already loaded (false), the standard providedFeatures
// contract. The String core extensions are installed at startup regardless.
func TestUnicodeNormalizeProvidedFeature(t *testing.T) {
	got := eval(t, `puts(require "unicode_normalize"); puts(require "unicode_normalize")`)
	if got != "true\nfalse\n" {
		t.Errorf(`require "unicode_normalize" got=%q want "true\nfalse\n"`, got)
	}
}
