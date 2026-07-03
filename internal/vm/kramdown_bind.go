// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	kramdown "github.com/go-ruby-kramdown/kramdown"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-kramdown/kramdown renderer. The
// parser and HTML renderer live in that library; rbgo only maps the Markdown
// source String and an options Hash to a single kramdown.ToHTML call, so the
// kramdown-faithful HTML the Kramdown module relies on is preserved by
// construction.

// kramdownRender renders Markdown source to HTML by mapping the options value
// into a *kramdown.Options and calling kramdown.ToHTML.
func kramdownRender(src string, opt object.Value) string {
	return kramdown.ToHTML(src, kramdownOptions(opt))
}

// kramdownOptions maps a Ruby options value to a *kramdown.Options. A nil / Ruby
// nil value (or any non-Hash) selects kramdown's defaults (returned as nil so the
// library applies DefaultOptions). A Hash maps recognised keys (as Symbol or
// String) onto the option fields: the booleans (:auto_ids, :smart_quotes,
// :typographic_symbols, :hard_wrap) take the key's truthiness, :auto_id_prefix
// takes a String, and :footnote_nr an Integer. Unrecognised keys are ignored.
func kramdownOptions(opt object.Value) *kramdown.Options {
	h, ok := object.KindOK[*object.Hash](opt)
	if !ok {
		return nil
	}
	o := kramdown.DefaultOptions()
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		switch kramdownKey(k) {
		case "auto_ids":
			o.AutoIds = val.Truthy()
		case "auto_id_prefix":
			o.AutoIdPrefix = kramdownStr(val)
		case "smart_quotes":
			o.SmartQuotes = val.Truthy()
		case "typographic_symbols":
			o.Typographic = val.Truthy()
		case "hard_wrap":
			o.HardWrap = val.Truthy()
		case "footnote_nr":
			if n, ok := object.AsIntegerOK(val); ok {
				o.FootnoteNr = int(n)
			}
		}
	}
	return &o
}

// kramdownKey renders an option key (a Symbol or String) as its bare name; any
// other value falls back to its to_s.
func kramdownKey(k object.Value) string {
	{
		__sw83 := k
		switch {
		case object.IsKind[object.Symbol](__sw83):
			n := object.Kind[object.Symbol](__sw83)
			_ = n
			return string(n)
		case object.IsKind[*object.String](__sw83):
			n := object.Kind[*object.String](__sw83)
			_ = n
			return n.Str()
		}
	}
	return k.ToS()
}

// kramdownStr renders an option value as a string: a String yields its contents,
// any other value its to_s.
func kramdownStr(v object.Value) string {
	if s, ok := object.KindOK[*object.String](v); ok {
		return s.Str()
	}
	return v.ToS()
}
