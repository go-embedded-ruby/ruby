// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// gcbPropOf returns the Grapheme_Cluster_Break property of r, or gcbOther when r
// carries no explicit GCB value. It binary-searches the generated gcbRanges.
func gcbPropOf(r rune) gcbProp {
	lo, hi := 0, len(gcbRanges)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		switch {
		case r < gcbRanges[mid].lo:
			hi = mid
		case r > gcbRanges[mid].hi:
			lo = mid + 1
		default:
			return gcbRanges[mid].prop
		}
	}
	return gcbOther
}

// isExtendedPictographic reports whether r has the Extended_Pictographic
// property (needed for the GB11 emoji-ZWJ grapheme rule).
func isExtendedPictographic(r rune) bool {
	lo, hi := 0, len(extPictRanges)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		switch {
		case r < extPictRanges[mid][0]:
			hi = mid
		case r > extPictRanges[mid][1]:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// graphemeBoundaries returns the byte offsets at which each extended grapheme
// cluster of s begins, per UAX#29 (rules GB1–GB13/GB999). The result always
// starts with 0 for a non-empty s and is empty for an empty s.
func graphemeBoundaries(s string) []int {
	if len(s) == 0 {
		return nil
	}
	var starts []int
	// State carried across the scan.
	var prev gcbProp
	riRun := 0    // consecutive Regional_Indicator count ending at prev
	emoji := 0    // 0: none, 1: ExtPict Extend*, 2: ExtPict Extend* ZWJ
	first := true // sot: force a boundary before the first cluster
	for i := 0; i < len(s); {
		r, sz := utf8.DecodeRuneInString(s[i:])
		cur := gcbPropOf(r)
		pict := isExtendedPictographic(r)
		if first || graphemeBreakBetween(prev, cur, riRun, emoji, pict) {
			starts = append(starts, i)
		}
		// Advance auxiliary state to reflect cur becoming the new prev.
		if cur == gcbRegionalIndicator {
			if prev == gcbRegionalIndicator {
				riRun++
			} else {
				riRun = 1
			}
		} else {
			riRun = 0
		}
		switch {
		case pict:
			emoji = 1
		case emoji == 1 && cur == gcbExtend:
			emoji = 1
		case emoji == 1 && cur == gcbZWJ:
			emoji = 2
		default:
			emoji = 0
		}
		prev = cur
		first = false
		i += sz
	}
	return starts
}

// graphemeBreakBetween reports whether UAX#29 places a cluster boundary between a
// left character with property prev and a right character with property cur.
// riRun is the count of consecutive Regional_Indicators ending at prev; emoji is
// the ExtPict/Extend/ZWJ state as of prev; curPict is Extended_Pictographic(cur).
func graphemeBreakBetween(prev, cur gcbProp, riRun, emoji int, curPict bool) bool {
	switch {
	case prev == gcbCR && cur == gcbLF: // GB3
		return false
	case prev == gcbControl || prev == gcbCR || prev == gcbLF: // GB4
		return true
	case cur == gcbControl || cur == gcbCR || cur == gcbLF: // GB5
		return true
	case prev == gcbL && (cur == gcbL || cur == gcbV || cur == gcbLV || cur == gcbLVT): // GB6
		return false
	case (prev == gcbLV || prev == gcbV) && (cur == gcbV || cur == gcbT): // GB7
		return false
	case (prev == gcbLVT || prev == gcbT) && cur == gcbT: // GB8
		return false
	case cur == gcbExtend || cur == gcbZWJ: // GB9
		return false
	case cur == gcbSpacingMark: // GB9a
		return false
	case prev == gcbPrepend: // GB9b
		return false
	case emoji == 2 && curPict: // GB11: ExtPict Extend* ZWJ × ExtPict
		return false
	case prev == gcbRegionalIndicator && cur == gcbRegionalIndicator && riRun%2 == 1: // GB12/GB13
		return false
	default: // GB999
		return true
	}
}

// graphemePiece wraps a grapheme-cluster substring as a copy-on-write String
// carrying the receiver's encoding tag.
func graphemePiece(sub, enc string) *object.String {
	p := object.NewStringView(sub)
	p.Enc = enc
	return p
}

// stringGraphemePieces returns the receiver's grapheme clusters as byte strings
// in the receiver's OWN encoding, reproducing string.c v3_4_0
// rb_str_enumerate_grapheme_clusters:
//
//	rb_encoding *enc = get_encoding(str);
//	if (!rb_enc_unicode_p(enc)) return rb_str_enumerate_chars(str, ary);
//	...
//	while (ptr < end) {
//	    OnigPosition len = onig_match(reg_grapheme_cluster, …);
//	    if (len <= 0) break;
//	    ENUM_ELEM(ary, rb_str_subseq(str, ptr-ptr0, len));
//	    ptr += len;
//	}
//
// Two things follow. A non-Unicode encoding — every single-byte set, ASCII-8BIT,
// and the DUMMY UTF-16/UTF-32 (rb_enc_unicode_p reads the Oniguruma flags a
// dummy encoding does not carry) — is enumerated by CHARACTER instead; rbgo
// clustered the bytes as UTF-8 whatever they were, so a Latin-1 "é" came back as
// one two-byte cluster and a BINARY string as one cluster per valid UTF-8
// sequence. And a real UTF-16/UTF-32 string is clustered in ITS bytes: the
// boundaries are found on the decoded text and mapped back through the per-
// character byte widths, so each returned piece is the original's own bytes.
// The walk stops at the first byte run that is not a character, which is where
// onig_match returns <= 0 and MRI breaks.
func (vm *VM) stringGraphemePieces(s *object.String) []string {
	enc := s.EncName()
	switch enc {
	case "", "UTF-8", "UTF8-MAC":
		return graphemeClusters(s.Str())
	case "UTF-16LE", "UTF-16BE", "UTF-32LE", "UTF-32BE":
	default:
		return strCharPieces(s.Str(), s.Enc)
	}
	// Decode character by character, keeping each character's width in BOTH
	// representations so a cluster boundary in the UTF-8 text names a byte range
	// in the source.
	src := s.Str()
	var u strings.Builder
	u8At, srcAt := []int{}, []int{}
	i := 0
	for i < len(src) {
		n := encCharLen(src[i:], enc)
		r := decodeFixedWidthRune(src[i:i+n], enc)
		if r < 0 {
			break // not a character: onig_match's `len <= 0`
		}
		u8At = append(u8At, u.Len())
		srcAt = append(srcAt, i)
		u.WriteRune(r)
		i += n
	}
	// The sentinel is where the walk STOPPED, not the end of the string: bytes
	// after the break are dropped, as they are when MRI leaves its while loop.
	u8At = append(u8At, u.Len())
	srcAt = append(srcAt, i)
	back := make(map[int]int, len(u8At))
	for k, off := range u8At {
		back[off] = srcAt[k]
	}
	starts := graphemeBoundaries(u.String())
	out := make([]string, 0, len(starts))
	for i, st := range starts {
		endU8 := u.Len()
		if i+1 < len(starts) {
			endU8 = starts[i+1]
		}
		out = append(out, src[back[st]:back[endU8]])
	}
	return out
}

// decodeFixedWidthRune decodes the single character in b, which is exactly one
// character of the fixed-width Unicode encoding enc (as encCharLen measured it).
// It returns -1 when the bytes are not a character — an unpaired surrogate, a
// code point above U+10FFFF, or a truncated unit — which is the condition MRI's
// onig_match reports by returning a non-positive length.
func decodeFixedWidthRune(b, enc string) rune {
	switch enc {
	case "UTF-16LE", "UTF-16BE":
		unit := func(i int) rune {
			if enc == "UTF-16BE" {
				return rune(b[i])<<8 | rune(b[i+1])
			}
			return rune(b[i+1])<<8 | rune(b[i])
		}
		if len(b) == 2 {
			c := unit(0)
			if c >= 0xD800 && c <= 0xDFFF {
				return -1
			}
			return c
		}
		if len(b) != 4 {
			return -1
		}
		hi, lo := unit(0), unit(2)
		if lo < 0xDC00 || lo > 0xDFFF {
			return -1
		}
		return 0x10000 + (hi-0xD800)<<10 + (lo - 0xDC00)
	case "UTF-32LE", "UTF-32BE":
		if len(b) != 4 {
			return -1
		}
		var c rune
		if enc == "UTF-32BE" {
			c = rune(b[0])<<24 | rune(b[1])<<16 | rune(b[2])<<8 | rune(b[3])
		} else {
			c = rune(b[3])<<24 | rune(b[2])<<16 | rune(b[1])<<8 | rune(b[0])
		}
		if c < 0 || c > 0x10FFFF || (c >= 0xD800 && c <= 0xDFFF) {
			return -1
		}
		return c
	}
	return -1
}

// graphemeClusters splits s into its extended grapheme clusters. Each substring
// is a slice of s (no copy).
func graphemeClusters(s string) []string {
	starts := graphemeBoundaries(s)
	if len(starts) == 0 {
		return nil
	}
	out := make([]string, len(starts))
	for i, st := range starts {
		end := len(s)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		out[i] = s[st:end]
	}
	return out
}
