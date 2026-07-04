// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	prawn "github.com/go-ruby-prawn/prawn"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent PDF core of github.com/go-ruby-prawn/prawn (a pure-Go,
// no-cgo reimplementation of Ruby's prawn gem over github.com/go-pdf/fpdf). It
// carries the single instance value type Prawn wraps — a Document — plus the
// conversions that turn Ruby keyword Hashes, [x, y] point Arrays and drawing
// arguments into the library's option structs and coordinates, and the error
// bridge that re-raises the library's Prawn::Errors sentinels as the matching
// Ruby exception classes. All PDF generation is delegated to go-ruby-prawn. See
// prawn.go for the module wiring.

// PrawnDocument is an instance of Prawn::Document: a PDF being built, backed by a
// go-ruby-prawn *prawn.Document. Its drawing methods thread this wrapped document
// into the library; #render yields the finished PDF bytes.
type PrawnDocument struct {
	cls *RClass
	d   *prawn.Document
}

// ToS renders the fixed Prawn::Document label, matching a default Ruby #to_s for
// an opaque wrapped object.
func (d *PrawnDocument) ToS() string { return "#<Prawn::Document>" }

// Inspect renders the same fixed label as ToS.
func (d *PrawnDocument) Inspect() string { return "#<Prawn::Document>" }

func (d *PrawnDocument) Truthy() bool { return true }

// prawnOptions maps a Prawn::Document.new / .generate keyword Hash to the
// library's Options: page_size: (a "LETTER"/:A4 name or an explicit [w, h] Array),
// page_layout: (:portrait/:landscape), margin: (a single number applied to every
// side) and margins: ([top, right, bottom, left]). Absent keys select prawn's
// defaults.
func prawnOptions(args []object.Value) prawn.Options {
	var o prawn.Options
	h := prawnOptsHash(args)
	if h == nil {
		return o
	}
	if v, ok := prawnHashGet(h, "page_size"); ok {
		switch p := v.(type) {
		case *object.String:
			o.PageSize = p.Str()
		case object.Symbol:
			o.PageSize = string(p)
		case *object.Array:
			if len(p.Elems) == 2 {
				o.PageWidth, o.PageHeight = prawnFloat(p.Elems[0]), prawnFloat(p.Elems[1])
			}
		}
	}
	if v, ok := prawnHashGet(h, "page_layout"); ok {
		o.PageLayout = prawnSymOrStr(v)
	}
	if v, ok := prawnHashGet(h, "margin"); ok {
		m := prawnFloat(v)
		o.Margin = &m
	}
	if v, ok := prawnHashGet(h, "margins"); ok {
		if arr, ok := v.(*object.Array); ok && len(arr.Elems) == 4 {
			o.Margins = &[4]float64{
				prawnFloat(arr.Elems[0]), prawnFloat(arr.Elems[1]),
				prawnFloat(arr.Elems[2]), prawnFloat(arr.Elems[3]),
			}
		}
	}
	return o
}

// prawnTextOptions maps a text/draw_text keyword tail to the library's
// *TextOptions, or nil when there is no trailing Hash.
func prawnTextOptions(rest []object.Value) *prawn.TextOptions {
	return prawnTextOptionsFromHash(prawnOptsHash(rest))
}

// prawnTextOptionsFromHash maps a per-call text Hash (size:/style:/align:/leading:/
// color:/font:) to the library's *TextOptions. A nil Hash yields nil, which the
// library reads as "keep the document's current settings".
func prawnTextOptionsFromHash(h *object.Hash) *prawn.TextOptions {
	if h == nil {
		return nil
	}
	o := &prawn.TextOptions{}
	if v, ok := prawnHashGet(h, "size"); ok {
		o.Size = prawnFloat(v)
	}
	if v, ok := prawnHashGet(h, "style"); ok {
		o.Style = prawnStyle(v)
		o.StyleSet = true
	}
	if v, ok := prawnHashGet(h, "align"); ok {
		o.Align = prawnAlign(v)
	}
	if v, ok := prawnHashGet(h, "leading"); ok {
		o.Leading = prawnFloat(v)
	}
	if v, ok := prawnHashGet(h, "color"); ok {
		o.Color = strArg(v)
	}
	if v, ok := prawnHashGet(h, "font"); ok {
		o.Font = strArg(v)
	}
	return o
}

// prawnImageOptions maps an image keyword Hash (at:/width:/height:/fit:) to the
// library's ImageOptions. An absent Hash selects prawn's defaults (natural size at
// the cursor).
func prawnImageOptions(h *object.Hash) prawn.ImageOptions {
	var o prawn.ImageOptions
	if h == nil {
		return o
	}
	if v, ok := prawnHashGet(h, "at"); ok {
		o.AtX, o.AtY = prawnPoint(v)
		o.AtSet = true
	}
	if v, ok := prawnHashGet(h, "width"); ok {
		o.Width = prawnFloat(v)
	}
	if v, ok := prawnHashGet(h, "height"); ok {
		o.Height = prawnFloat(v)
	}
	if v, ok := prawnHashGet(h, "fit"); ok {
		if arr, ok := v.(*object.Array); ok && len(arr.Elems) == 2 {
			o.FitW, o.FitH = prawnFloat(arr.Elems[0]), prawnFloat(arr.Elems[1])
		}
	}
	return o
}

// prawnTableData converts a Ruby table data Array (an Array of row Arrays) to the
// [][]string the library draws, coercing each cell to a String (a String
// verbatim, nil to the empty string, any other value via #to_s). A non-Array data
// value or row raises TypeError.
func prawnTableData(v object.Value) [][]string {
	arr, ok := v.(*object.Array)
	if !ok {
		raise("TypeError", "table data must be an Array of rows, got %s", v.Inspect())
	}
	out := make([][]string, len(arr.Elems))
	for i, row := range arr.Elems {
		r, ok := row.(*object.Array)
		if !ok {
			raise("TypeError", "table row must be an Array, got %s", row.Inspect())
		}
		cells := make([]string, len(r.Elems))
		for j, c := range r.Elems {
			cells[j] = prawnCell(c)
		}
		out[i] = cells
	}
	return out
}

// prawnCell renders a table cell to its string form: a String verbatim, nil as
// the empty string, and any other value via its Ruby #to_s.
func prawnCell(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	if object.IsNil(v) {
		return ""
	}
	return v.ToS()
}

// prawnTableOptions maps a table keyword Hash to the library's TableOptions:
// column_widths: (an Array of point widths), header: (a truthy flag), and the
// cell_padding:/border_width:/font_size:/row_height: point values. An absent Hash
// or key selects prawn's defaults.
func prawnTableOptions(h *object.Hash) prawn.TableOptions {
	var o prawn.TableOptions
	if h == nil {
		return o
	}
	if v, ok := prawnHashGet(h, "column_widths"); ok {
		if arr, ok := v.(*object.Array); ok {
			o.ColumnWidths = make([]float64, len(arr.Elems))
			for i, e := range arr.Elems {
				o.ColumnWidths[i] = prawnFloat(e)
			}
		}
	}
	if v, ok := prawnHashGet(h, "header"); ok {
		o.Header = v.Truthy()
	}
	if v, ok := prawnHashGet(h, "cell_padding"); ok {
		o.CellPadding = prawnFloat(v)
	}
	if v, ok := prawnHashGet(h, "border_width"); ok {
		o.BorderWidth = prawnFloat(v)
	}
	if v, ok := prawnHashGet(h, "font_size"); ok {
		o.FontSize = prawnFloat(v)
	}
	if v, ok := prawnHashGet(h, "row_height"); ok {
		o.RowHeight = prawnFloat(v)
	}
	return o
}

// prawnStyle maps a prawn font-style symbol/string (:normal/:bold/:italic/
// :bold_italic) to the library's Style; an unrecognised value is the upright
// StyleNormal.
func prawnStyle(v object.Value) prawn.Style {
	switch prawnSymOrStr(v) {
	case "bold":
		return prawn.StyleBold
	case "italic":
		return prawn.StyleItalic
	case "bold_italic":
		return prawn.StyleBoldItalic
	default:
		return prawn.StyleNormal
	}
}

// prawnStyleFromHash reads the style: option of a font() call, defaulting to
// StyleNormal when the Hash is absent or has no style: key.
func prawnStyleFromHash(h *object.Hash) prawn.Style {
	if h == nil {
		return prawn.StyleNormal
	}
	if v, ok := prawnHashGet(h, "style"); ok {
		return prawnStyle(v)
	}
	return prawn.StyleNormal
}

// prawnAlign maps a prawn alignment symbol/string (:left/:center/:right/:justify)
// to the library's Align; an unrecognised value is the default AlignLeft.
func prawnAlign(v object.Value) prawn.Align {
	switch prawnSymOrStr(v) {
	case "center":
		return prawn.AlignCenter
	case "right":
		return prawn.AlignRight
	case "justify":
		return prawn.AlignJustify
	default:
		return prawn.AlignLeft
	}
}

// prawnPoint reads a [x, y] point Array as two float64 coordinates, raising
// ArgumentError for anything but a two-element Array.
func prawnPoint(v object.Value) (float64, float64) {
	arr, ok := v.(*object.Array)
	if !ok || len(arr.Elems) != 2 {
		raise("ArgumentError", "expected a [x, y] point, got %s", v.Inspect())
	}
	return prawnFloat(arr.Elems[0]), prawnFloat(arr.Elems[1])
}

// prawnPointOpt reads a point-valued option (e.g. at:) from an options Hash,
// returning (0, 0) when the Hash is absent or the key is missing.
func prawnPointOpt(h *object.Hash, key string) (float64, float64) {
	if v, ok := prawnHashGet(h, key); ok {
		return prawnPoint(v)
	}
	return 0, 0
}

// prawnFloat coerces a Ruby numeric (Integer/Float/Bignum/Rational) to float64,
// raising TypeError otherwise — prawn coordinates and sizes are all points.
func prawnFloat(v object.Value) float64 {
	f, ok := toFloat(v)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Float", v.Inspect())
	}
	return f
}

// prawnSymOrStr renders a Symbol or String option value as its bare string (a
// Symbol's name, a String's contents), falling back to the value's #to_s.
func prawnSymOrStr(v object.Value) string {
	switch s := v.(type) {
	case object.Symbol:
		return string(s)
	case *object.String:
		return s.Str()
	}
	return v.ToS()
}

// prawnOptsHash returns the trailing keyword Hash of a Prawn call (the drawing
// options), or nil when the last argument is not a Hash.
func prawnOptsHash(rest []object.Value) *object.Hash {
	if len(rest) == 0 {
		return nil
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	if !ok {
		return nil
	}
	return h
}

// prawnHashGet fetches a symbol-keyed option from an options Hash, reporting
// ok=false when the Hash is absent or the key is missing.
func prawnHashGet(h *object.Hash, key string) (object.Value, bool) {
	if h == nil {
		return object.NilV, false
	}
	return h.Get(object.Symbol(key))
}

// raisePrawnError re-raises a go-ruby-prawn error as the matching Prawn::Errors
// exception: an error carrying a Kind() (the library's Prawn::Errors sentinels,
// including wrapped ones) maps to Prawn::Errors::<Kind>, and any other error (an
// I/O failure surfaced through the sticky document) to the Prawn::Errors::Error
// base. raisePrawnError never returns (raise panics); it is typed to return any so
// a caller can write `return raisePrawnError(err)` in a value position.
func raisePrawnError(err error) any {
	var k interface{ Kind() string }
	if errors.As(err, &k) {
		return raise("Prawn::Errors::"+k.Kind(), "%s", err.Error())
	}
	return raise("Prawn::Errors::Error", "%s", err.Error())
}
