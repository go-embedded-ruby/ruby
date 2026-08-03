// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"strings"
	"testing"
)

// TestConverterInternals drives the Encoding::Converter primitives directly, for
// the branches the Ruby-level tests cannot reach: the object.Value witnesses, the
// hop encodings that never error through the public surface, and the byte-level
// stepper edge cases (truncated / malformed multi-byte sequences).
func TestConverterInternals(t *testing.T) {
	c := &converterObj{src: "UTF-8", dst: "UTF-16LE"}
	if c.ToS() != "#<Encoding::Converter: UTF-8 to UTF-16LE>" || c.Inspect() != c.ToS() {
		t.Errorf("ToS/Inspect = %q", c.Inspect())
	}
	if !c.Truthy() {
		t.Error("Truthy should be true")
	}

	// decodeHop / encodeHop collapse the UTF-8 pivot at either endpoint.
	if f, to := (&converterObj{src: "UTF-8", dst: "ISO-8859-1"}).decodeHop(); f != "UTF-8" || to != "ISO-8859-1" {
		t.Errorf("decodeHop UTF-8 src = %s,%s", f, to)
	}
	if f, to := (&converterObj{src: "EUC-JP", dst: "UTF-8"}).encodeHop(); f != "EUC-JP" || to != "UTF-8" {
		t.Errorf("encodeHop UTF-8 dst = %s,%s", f, to)
	}

	// undefMessage: the multi-hop path form (source → UTF-8 → dest).
	msg := (&converterObj{src: "EUC-JP", dst: "US-ASCII"}).undefMessage([]byte("香"))
	if !strings.Contains(msg, "in conversion from EUC-JP to UTF-8 to US-ASCII") {
		t.Errorf("multi-hop undefMessage = %q", msg)
	}

	// appendCapped: appends under an unlimited / sufficient cap, drops at the edge.
	if got := appendCapped([]byte("a"), []byte("b"), -1); string(got) != "ab" {
		t.Errorf("appendCapped unlimited = %q", got)
	}
	if got := appendCapped([]byte("a"), []byte("bb"), 2); string(got) != "a" {
		t.Errorf("appendCapped over cap = %q", got)
	}

	vm := New(io.Discard)

	// decodeCharFrom over every source encoding, including the ones the Ruby tests
	// do not exercise as a Converter source.
	decodeCases := []struct {
		in   string
		from string
		want stepStatus
	}{
		{"A", "ASCII-8BIT", stepOK},
		{"\xE9", "ISO-8859-1", stepOK},
		{"A", "US-ASCII", stepOK},
		{"\x80", "US-ASCII", stepInvalid},
		{"\x00\x00\x00A", "UTF-32BE", stepOK},
		{"\x00\x00\x00", "UTF-32BE", stepIncomplete},
		{"\x00\xD8\x00\x00", "UTF-32BE", stepInvalid}, // surrogate scalar
	}
	for _, tc := range decodeCases {
		if _, _, _, st := vm.decodeCharFrom([]byte(tc.in), tc.from); st != tc.want {
			t.Errorf("decodeCharFrom(%q,%s) status = %d, want %d", tc.in, tc.from, st, tc.want)
		}
	}

	// utf8Step: a broken continuation past the lead, and a multi-byte sequence
	// truncated at the buffer end.
	if _, n, rl, st := utf8Step([]byte("\xE2\x82X")); st != stepInvalid || n != 2 || rl != 1 {
		t.Errorf("utf8Step E2 82 X = n%d rl%d st%d", n, rl, st)
	}
	if _, _, _, st := utf8Step([]byte("\xE2\x82")); st != stepIncomplete {
		t.Errorf("utf8Step E2 82 truncated st%d", st)
	}
	if _, _, _, st := utf8Step([]byte("\xC3")); st != stepIncomplete {
		t.Errorf("utf8Step C3 truncated st%d", st)
	}

	// utf16Step: a high surrogate with no room for its low half is incomplete.
	if _, _, _, st := utf16Step([]byte("\x00\xD8"), false); st != stepIncomplete {
		t.Errorf("utf16Step lone high surrogate st%d", st)
	}
	if _, n, _, st := utf16Step([]byte("\xD8\x00\xDC\x00"), true); st != stepOK || n != 4 {
		t.Errorf("utf16Step BE surrogate pair n%d st%d", n, st)
	}
}
