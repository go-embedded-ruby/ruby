// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	binpkg "encoding/binary"
	"unicode/utf8"
)

// decodeCharFrom decodes the next character of in, interpreted in encoding
// `from`, returning its rune, the number of bytes it occupies, the number of
// trailing "read-again" bytes that were consumed past an error, and a status.
// For stepInvalid, in[:n] are the erroneous bytes and in[n:n+readLen] the
// read-again bytes; for stepIncomplete the whole remaining slice is the
// incomplete tail. It is the decode half of the Encoding::Converter engine.
func (vm *VM) decodeCharFrom(in []byte, from string) (r rune, n, readLen int, st stepStatus) {
	switch from {
	case "UTF-8":
		return utf8Step(in)
	case "US-ASCII":
		if in[0] < 0x80 {
			return rune(in[0]), 1, 0, stepOK
		}
		return 0, 1, 0, stepInvalid
	case "ASCII-8BIT", "ISO-8859-1":
		return rune(in[0]), 1, 0, stepOK // every byte is a code point
	case "UTF-16LE":
		return utf16Step(in, false)
	case "UTF-16BE":
		return utf16Step(in, true)
	case "UTF-32LE":
		return utf32Step(in, false)
	case "UTF-32BE":
		return utf32Step(in, true)
	case "EUC-JP":
		return eucjpStep(in)
	}
	return xtextRuneStep(in, from)
}

// utf8Step decodes one UTF-8 character, distinguishing an invalid sequence
// (with its maximal valid subpart and the read-again byte that broke it) from an
// incomplete one truncated at the end of the buffer.
func utf8Step(in []byte) (rune, int, int, stepStatus) {
	b0 := in[0]
	if b0 < 0x80 {
		return rune(b0), 1, 0, stepOK
	}
	var length int
	var lo, hi byte // allowed range of the second byte
	switch {
	case b0 >= 0xC2 && b0 <= 0xDF:
		length, lo, hi = 2, 0x80, 0xBF
	case b0 == 0xE0:
		length, lo, hi = 3, 0xA0, 0xBF
	case b0 >= 0xE1 && b0 <= 0xEC:
		length, lo, hi = 3, 0x80, 0xBF
	case b0 == 0xED:
		length, lo, hi = 3, 0x80, 0x9F
	case b0 >= 0xEE && b0 <= 0xEF:
		length, lo, hi = 3, 0x80, 0xBF
	case b0 == 0xF0:
		length, lo, hi = 4, 0x90, 0xBF
	case b0 >= 0xF1 && b0 <= 0xF3:
		length, lo, hi = 4, 0x80, 0xBF
	case b0 == 0xF4:
		length, lo, hi = 4, 0x80, 0x8F
	default: // 0x80–0xC1, 0xF5–0xFF: never a valid lead
		return 0, 1, 0, stepInvalid
	}
	for i := 1; i < length; i++ {
		if i >= len(in) {
			return 0, 0, 0, stepIncomplete // valid prefix, truncated
		}
		clo, chi := byte(0x80), byte(0xBF)
		if i == 1 {
			clo, chi = lo, hi
		}
		if in[i] < clo || in[i] > chi {
			return 0, i, 1, stepInvalid // subpart in[:i], read-again in[i]
		}
	}
	r, _ := utf8.DecodeRune(in[:length])
	return r, length, 0, stepOK
}

// utf16Step decodes one UTF-16 code point (a BMP unit or a surrogate pair),
// reporting a lone / unpaired surrogate as invalid with the following unit as the
// read-again bytes, and a truncated unit as incomplete.
func utf16Step(in []byte, be bool) (rune, int, int, stepStatus) {
	order := binOrder(be)
	if len(in) < 2 {
		return 0, 0, 0, stepIncomplete
	}
	u := order.Uint16(in[:2])
	switch {
	case u >= 0xD800 && u <= 0xDBFF: // high surrogate: needs a low surrogate next
		if len(in) < 4 {
			return 0, 0, 0, stepIncomplete
		}
		lowU := order.Uint16(in[2:4])
		if lowU >= 0xDC00 && lowU <= 0xDFFF {
			r := 0x10000 + (rune(u-0xD800)<<10 | rune(lowU-0xDC00))
			return r, 4, 0, stepOK
		}
		return 0, 2, 2, stepInvalid // high surrogate, next unit read again
	case u >= 0xDC00 && u <= 0xDFFF: // lone low surrogate
		return 0, 2, 0, stepInvalid
	}
	return rune(u), 2, 0, stepOK
}

// utf32Step decodes one UTF-32 scalar, rejecting out-of-range and surrogate code
// points as invalid and a truncated unit as incomplete.
func utf32Step(in []byte, be bool) (rune, int, int, stepStatus) {
	if len(in) < 4 {
		return 0, 0, 0, stepIncomplete
	}
	r := rune(binOrder(be).Uint32(in[:4]))
	if r < 0 || r > utf8.MaxRune || (r >= 0xD800 && r <= 0xDFFF) {
		return 0, 4, 0, stepInvalid
	}
	return r, 4, 0, stepOK
}

// eucjpStep decodes one EUC-JP character: ASCII (1 byte), JIS X 0201 kana
// (0x8E + 1), JIS X 0212 (0x8F + 2), or JIS X 0208 (a 0xA1–0xFE lead + 1). A
// lead with an out-of-range trailing byte is invalid (with that byte read again);
// a lead cut off at the buffer end is incomplete.
func eucjpStep(in []byte) (rune, int, int, stepStatus) {
	b0 := in[0]
	switch {
	case b0 < 0x80:
		return rune(b0), 1, 0, stepOK
	case b0 == 0x8E:
		return eucjpMulti(in, 2, [][2]byte{{0xA1, 0xDF}})
	case b0 == 0x8F:
		return eucjpMulti(in, 3, [][2]byte{{0xA1, 0xFE}, {0xA1, 0xFE}})
	case b0 >= 0xA1 && b0 <= 0xFE:
		return eucjpMulti(in, 2, [][2]byte{{0xA1, 0xFE}})
	}
	return 0, 1, 0, stepInvalid
}

// eucjpMulti validates a multi-byte EUC-JP sequence of the given length whose
// trailing bytes must each fall in their allowed range, then decodes it via the
// x/text EUC-JP codec.
func eucjpMulti(in []byte, length int, ranges [][2]byte) (rune, int, int, stepStatus) {
	for i := 1; i < length; i++ {
		if i >= len(in) {
			return 0, 0, 0, stepIncomplete
		}
		if in[i] < ranges[i-1][0] || in[i] > ranges[i-1][1] {
			return 0, i, 1, stepInvalid
		}
	}
	s, _ := xtextDecode(in[:length], "EUC-JP")
	r, _ := utf8.DecodeRuneInString(s)
	return r, length, 0, stepOK
}

// xtextRuneStep is the fallback decoder for a multi-byte source encoding with an
// x/text codec: an ASCII byte is itself, otherwise the shortest byte prefix that
// the codec maps to a single non-replacement rune is that character. A prefix
// that never resolves before the buffer ends is incomplete; one that cannot
// resolve at all is invalid.
func xtextRuneStep(in []byte, from string) (rune, int, int, stepStatus) {
	if in[0] < 0x80 {
		return rune(in[0]), 1, 0, stepOK
	}
	max := 4
	if len(in) < max {
		max = len(in)
	}
	for length := 1; length <= max; length++ {
		s, ok := xtextDecode(in[:length], from)
		if !ok {
			return 0, 1, 0, stepInvalid // no codec for this source
		}
		if r, size := utf8.DecodeRuneInString(s); r != utf8.RuneError && size == len(s) {
			return r, length, 0, stepOK
		}
	}
	if len(in) < 4 {
		return 0, 0, 0, stepIncomplete
	}
	return 0, 1, 0, stepInvalid
}

// binOrder returns the byte order for a UTF-16/32 codec.
func binOrder(be bool) binpkg.ByteOrder {
	if be {
		return binpkg.BigEndian
	}
	return binpkg.LittleEndian
}

// encodeCharTo encodes a single rune into encoding `to`, returning the bytes and
// whether the character is representable there (false drives an undefined
// conversion). It is the encode half of the Encoding::Converter engine and
// mirrors String#encode's per-encoding rules.
func (vm *VM) encodeCharTo(r rune, to string) ([]byte, bool) {
	switch to {
	case "UTF-8":
		return utf8.AppendRune(nil, r), true
	case "US-ASCII", "ASCII-8BIT":
		if r < 0x80 {
			return []byte{byte(r)}, true
		}
		return nil, false
	case "ISO-8859-1":
		if r < 0x100 {
			return []byte{byte(r)}, true
		}
		return nil, false
	case "UTF-16LE":
		return encodeUTF16(string(r), false), true
	case "UTF-16BE":
		return encodeUTF16(string(r), true), true
	case "UTF-32LE":
		return encodeUTF32(string(r), false), true
	case "UTF-32BE":
		return encodeUTF32(string(r), true), true
	}
	enc, ok := xtextEncodings[to]
	if !ok {
		return nil, false
	}
	out, err := enc.NewEncoder().Bytes([]byte(string(r)))
	if err != nil {
		return nil, false
	}
	return out, true
}
