// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	stdtime "time"

	chronic "github.com/go-ruby-chronic/chronic"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's object graph and the
// github.com/go-ruby-chronic/chronic library. The parser lives in that library;
// rbgo only translates the options hash into chronic.Options and the resulting
// Go time.Time / *Span back into rbgo's Time / [begin, end] shapes.

// chronicParse maps the options hash to chronic.Options, calls the library, and
// returns a Ruby Time (guess applied) or, when guess: :none, a two-element
// [begin, end] Array of Time. A phrase that does not parse returns nil (matching
// Chronic.parse).
func chronicParse(text string, opts *object.Hash) object.Value {
	o, span := chronicOptions(opts)
	if span {
		s, ok := chronic.ParseSpan(text, o)
		if !ok {
			return object.NilV
		}
		return object.NewArray(goTimeToRuby(s.Begin), goTimeToRuby(s.End))
	}
	t, ok := chronic.Parse(text, o)
	if !ok {
		return object.NilV
	}
	return goTimeToRuby(t)
}

// chronicOptions builds a chronic.Options from the Ruby options hash. It maps
// now: (a Time anchor), context: (:future / :past), endian_precedence: (a leading
// :little selects day/month) and guess: (:begin / :end / :middle / false-or-:none
// disables guessing, returning the whole Span). The second result reports whether
// guessing was disabled (so the caller returns a Span).
func chronicOptions(h *object.Hash) (chronic.Options, bool) {
	o := chronic.Options{}
	if h == nil {
		return o, false
	}
	if v, ok := h.Get(object.Symbol("now")); ok {
		if t, isT := object.KindOK[*Time](v); isT {
			o.Now = stdtime.Unix(t.t.ToUnix(), 0).UTC()
		}
	}
	if v, ok := h.Get(object.Symbol("context")); ok {
		if sym, isSym := object.KindOK[object.Symbol](v); isSym && string(sym) == "past" {
			o.Context = chronic.ContextPast
		}
	}
	if v, ok := h.Get(object.Symbol("endian_precedence")); ok {
		o.EndianLittle = chronicEndianLittle(v)
	}
	if v, ok := h.Get(object.Symbol("guess")); ok {
		{
			__sw36 := v
			switch {
			case object.IsBool(__sw36):
				g := object.AsBoolV(__sw36)
				_ = g
				if !bool(g) {
					return o, true
				}
			case object.IsKind[object.Symbol](__sw36):
				g := object.Kind[object.Symbol](__sw36)
				_ = g
				switch string(g) {
				case "begin":
					o.Guess = chronic.GuessBegin
				case "end":
					o.Guess = chronic.GuessEnd
				case "none":
					return o, true
				}
			}
		}
	}
	return o, false
}

// chronicEndianLittle reads the endian_precedence option: an Array whose first
// element is :little (the gem's [:little, :middle]) selects day/month parsing, as
// does the bare :little symbol.
func chronicEndianLittle(v object.Value) bool {
	{
		__sw37 := v
		switch {
		case object.IsKind[*object.Array](__sw37):
			p := object.Kind[*object.Array](__sw37)
			_ = p
			return len(p.Elems) > 0 && chronicIsLittle(p.Elems[0])
		default:
			p := __sw37
			_ = p
			return chronicIsLittle(v)
		}
	}
}

// chronicIsLittle reports whether v is the :little symbol.
func chronicIsLittle(v object.Value) bool {
	s, ok := object.KindOK[object.Symbol](v)
	return ok && string(s) == "little"
}
