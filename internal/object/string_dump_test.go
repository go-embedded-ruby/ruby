// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package object

import "testing"

// TestStringDump exercises (*String).Dump at the object layer, covering the
// branches the VM-level String#dump cannot yet reach because rbgo has no way to
// build a non-ASCII-compatible string: such an encoding escapes its high bytes
// as \xHH and appends a .force_encoding("NAME") suffix, while an ASCII-compatible
// encoding does not. The ASCII-compatible cases here match ruby 4.0.6; the
// non-compatible cases assert Dump's own byte-wise contract (rbgo cannot yet
// produce a real UTF-16/UTF-32 string to compare against MRI).
func TestStringDump(t *testing.T) {
	cases := []struct {
		in   *String
		want string
	}{
		// Default (UTF-8): a multibyte character escapes to \u, no suffix.
		{NewString("é"), `"\u00E9"`},
		// ASCII-compatible non-default encoding: no suffix.
		{NewStringBytesEnc([]byte("foo"), "ISO-8859-1"), `"foo"`},
		// Non-ASCII-compatible encoding: high bytes escape as \xHH, ASCII bytes
		// stay verbatim, and the literal carries a .force_encoding suffix.
		{NewStringBytesEnc([]byte{0x80, 'v'}, "UTF-16BE"), `"\x80v".force_encoding("UTF-16BE")`},
		{NewStringBytesEnc([]byte{0xC0, 'A'}, "UTF-32BE"), `"\xC0A".force_encoding("UTF-32BE")`},
	}
	for _, c := range cases {
		if got := c.in.Dump(); got != c.want {
			t.Errorf("Dump(%q enc=%q) = %q, want %q", c.in.Str(), c.in.EncName(), got, c.want)
		}
	}

	if !asciiCompatibleEnc("UTF-8") || !asciiCompatibleEnc("US-ASCII") {
		t.Error("UTF-8/US-ASCII must be ASCII-compatible")
	}
	if asciiCompatibleEnc("UTF-16LE") || asciiCompatibleEnc("UTF-32LE") {
		t.Error("UTF-16/UTF-32 must not be ASCII-compatible")
	}
}
