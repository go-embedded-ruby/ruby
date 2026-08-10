// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
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
