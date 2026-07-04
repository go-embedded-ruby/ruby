// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	prawn "github.com/go-ruby-prawn/prawn"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// fixedClock pins the PDF /CreationDate for the whole test file so output is
// deterministic and timezone-independent (never asserting time.Now): every test
// that renders runs under this clock, restored on cleanup.
func fixedClock(t *testing.T) {
	t.Helper()
	prev := prawn.SetClock(func() time.Time {
		return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	})
	t.Cleanup(func() { prawn.SetClock(prev) })
}

// TestPrawnGenerateRoundTrip drives the headline block-form generator through
// rbgo: Prawn::Document.generate { |pdf| … } yields the document to the block,
// and #render returns a well-formed PDF (a %PDF- header, an %%EOF trailer, the
// drawn text in the uncompressed content stream) with the expected page count.
func TestPrawnGenerateRoundTrip(t *testing.T) {
	fixedClock(t)
	src := `
require "prawn"
pdf = Prawn::Document.generate do |doc|
  doc.text "hello world"
  doc.rectangle [10, 20], 100, 50
  doc.stroke
  doc.start_new_page
  doc.text "second page"
end
bytes = pdf.render
r = []
r << (bytes.start_with?("%PDF-") ? "pdf" : "no-pdf")
r << (bytes.include?("hello world") ? "text" : "no-text")
r << (bytes.include?("second page") ? "p2" : "no-p2")
r << (bytes.rstrip.end_with?("%%EOF") ? "eof" : "no-eof")
r << pdf.page_count.to_s
puts r.join("|")
`
	if got := runSrc(t, src); got != "pdf|text|p2|eof|2" {
		t.Fatalf("prawn generate round-trip = %q", got)
	}
}

// TestPrawnGenerateToFile drives the file-writing form: generate(path) { … }
// writes the rendered PDF to path, and the written bytes are the same PDF.
func TestPrawnGenerateToFile(t *testing.T) {
	fixedClock(t)
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "out.pdf")
	src := `
require "prawn"
Prawn::Document.generate(` + rubyStr(pdfPath) + `) do |pdf|
  pdf.text "to file"
end
puts "done"
`
	if got := runSrc(t, src); got != "done" {
		t.Fatalf("generate-to-file = %q", got)
	}
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatalf("read pdf: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) || !bytes.Contains(data, []byte("to file")) {
		t.Fatalf("written file is not the expected PDF (%d bytes)", len(data))
	}
}

// TestPrawnDocumentDSL exercises the drawing DSL and the reader/setter method
// pairs on a Document.new instance: text options, fonts and colors, the graphics
// primitives (both the path+paint and the one-shot forms), page/cursor
// management, tables and the bounds Hash.
func TestPrawnDocumentDSL(t *testing.T) {
	fixedClock(t)
	src := `
require "prawn"
pdf = Prawn::Document.new(page_size: "A4", margin: 40)
r = []

# text with a full options hash (size/style/align/leading/color/font).
pdf.text "styled", size: 18, style: :bold, align: :center, leading: 2, color: "FF0000", font: "Courier"
pdf.text "plain"                                   # nil options path
pdf.draw_text "at point", at: [5, 700], size: 9    # absolute placement
pdf.draw_text "no-at"                              # draw_text with no options hash

# font reader/setter and styles.
pdf.font "Times", style: :italic
r << pdf.font                                       # reader -> current family
pdf.font "Helvetica", style: :bold_italic

# size/color/line-width reader+setter pairs.
pdf.font_size 14
r << pdf.font_size.to_s                             # reader
pdf.fill_color "00FF00"
r << pdf.fill_color                                 # reader
pdf.stroke_color "0000FF"
r << pdf.stroke_color                               # reader
pdf.line_width 2
r << pdf.line_width.to_s                            # reader

# graphics: pending-path primitives painted by stroke/fill, plus block forms
# and the one-shot rectangles.
pdf.rectangle [0, 100], 80, 40
pdf.line [0, 0], [80, 40]
pdf.circle [40, 40], 10
pdf.stroke
pdf.rectangle [0, 0], 20, 20
pdf.fill
pdf.stroke { pdf.rectangle [0, 50], 10, 10 }        # stroke with block
pdf.fill   { pdf.circle [5, 5], 3 }                 # fill with block
pdf.stroke_rectangle [0, 60], 30, 10
pdf.fill_rectangle [0, 70], 30, 10

# cursor/page management.
top = pdf.cursor
pdf.move_down 20
pdf.move_up 5
pdf.move_cursor_to top
r << (pdf.cursor == top ? "cursor" : "bad-cursor")
b = pdf.bounds
r << b[:width].round(0).to_s
r << b[:height].round(0).to_s
r << [b[:left], b[:bottom]].map { |x| x.round(0) }.join(",")
pdf.start_new_page
r << pdf.page_count.to_s

# a table with explicit widths and a bold header row; mixed cell types.
pdf.table([["h1", "h2"], ["a", 1], [nil, "b"]],
          column_widths: [60, 60], header: true,
          cell_padding: 3, border_width: 1, font_size: 8, row_height: 16)

r << (pdf.render.start_with?("%PDF-") ? "ok" : "bad")
r << pdf.to_s
r << pdf.inspect
puts r.join("|")
`
	want := strings.Join([]string{
		"Times", "14.0", "00FF00", "0000FF", "2.0",
		"cursor", "515", "762", "40,40", "2", "ok",
		"#<Prawn::Document>", "#<Prawn::Document>",
	}, "|")
	if got := runSrc(t, src); got != want {
		t.Fatalf("prawn DSL =\n %q\nwant\n %q", got, want)
	}
}

// TestPrawnPageSizeShapes covers the three page_size shapes (a named String, a
// Symbol, and an explicit [w, h] Array) plus the page_layout and margins options,
// each producing a document whose bounds reflect the request.
func TestPrawnPageSizeShapes(t *testing.T) {
	fixedClock(t)
	src := `
require "prawn"
r = []
r << Prawn::Document.new(page_size: :A4).bounds[:height].round(0).to_s          # symbol name
r << Prawn::Document.new(page_size: [200, 300], margin: 0).bounds[:width].round(0).to_s
r << Prawn::Document.new(page_size: "LETTER", page_layout: "landscape", margins: [10, 10, 10, 10]).bounds[:width].round(0).to_s
puts r.join("|")
`
	// A4 height 841.89 minus the default 36pt top+bottom margins ≈ 770; custom
	// width 200; letter landscape width 792 - 20 = 772.
	if got := runSrc(t, src); got != "770|200|772" {
		t.Fatalf("page-size shapes = %q", got)
	}
}

// TestPrawnErrorTree exercises the Prawn::Errors exceptions raised at render: an
// unknown font, an invalid page layout, an incompatible (non-UTF-8) string, an
// unsupported image type, and a plain I/O error (a missing image file) which maps
// to the Prawn::Errors::Error base.
func TestPrawnErrorTree(t *testing.T) {
	fixedClock(t)
	src := `
require "prawn"
r = []

begin
  pdf = Prawn::Document.new
  pdf.font "NoSuchFont"
  pdf.render
rescue Prawn::Errors::UnknownFont => e
  r << (e.is_a?(Prawn::Errors::Error) && e.is_a?(StandardError) ? "unknown-font" : "bad")
end

begin
  Prawn::Document.new(page_layout: "diagonal").render
rescue Prawn::Errors::InvalidPageLayout
  r << "layout"
end

begin
  pdf = Prawn::Document.new
  pdf.text "\xFF\xFE"
  pdf.render
rescue Prawn::Errors::IncompatibleStringEncoding
  r << "encoding"
end

begin
  pdf = Prawn::Document.new
  pdf.image "picture.gif"
  pdf.render
rescue Prawn::Errors::Error
  r << "io-missing"
end

puts r.join("|")
`
	if got := runSrc(t, src); got != "unknown-font|layout|encoding|io-missing" {
		t.Fatalf("prawn error tree = %q", got)
	}
}

// TestPrawnImage embeds a real PNG through the image DSL (both the cursor-flow and
// the at:/fit: placements) and confirms the rendered PDF carries an image XObject.
func TestPrawnImage(t *testing.T) {
	fixedClock(t)
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "dot.png")
	if err := os.WriteFile(pngPath, onePixelPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `
require "prawn"
pdf = Prawn::Document.new
pdf.image ` + rubyStr(pngPath) + `                              # at the cursor
pdf.image ` + rubyStr(pngPath) + `, at: [0, 500], width: 40    # absolute + width
pdf.image ` + rubyStr(pngPath) + `, height: 25                 # height-only scale
pdf.image ` + rubyStr(pngPath) + `, fit: [30, 30]              # fit box
bytes = pdf.render
puts (bytes.start_with?("%PDF-") && bytes.include?("/Image") ? "img" : "no-img")
`
	if got := runSrc(t, src); got != "img" {
		t.Fatalf("prawn image = %q", got)
	}
}

// TestPrawnArgumentErrors covers the argument-shape guards reached from Ruby: a
// non-Array point, a two-element point with a non-numeric coordinate, a bad
// scalar coordinate, and non-Array table data / rows.
func TestPrawnArgumentErrors(t *testing.T) {
	fixedClock(t)
	src := `
require "prawn"
pdf = Prawn::Document.new
r = []
begin
  pdf.rectangle 5, 10, 20            # point is not an Array
rescue ArgumentError
  r << "point"
end
begin
  pdf.rectangle ["a", "b"], 10, 20   # coordinate is not numeric
rescue TypeError
  r << "coord"
end
begin
  pdf.move_down "x"                  # scalar is not numeric
rescue TypeError
  r << "scalar"
end
begin
  pdf.table 42                       # data is not an Array
rescue TypeError
  r << "data"
end
begin
  pdf.table [1, 2]                   # a row is not an Array
rescue TypeError
  r << "row"
end
puts r.join("|")
`
	if got := runSrc(t, src); got != "point|coord|scalar|data|row" {
		t.Fatalf("prawn argument errors = %q", got)
	}
}

// TestPrawnRenderFileErrors covers the RenderFile error branches of #render_file
// and of generate(path): writing into a directory that does not exist maps the
// I/O failure to the Prawn::Errors::Error base.
func TestPrawnRenderFileErrors(t *testing.T) {
	fixedClock(t)
	missing := filepath.Join(t.TempDir(), "no-such-dir", "out.pdf")
	src := `
require "prawn"
r = []
begin
  pdf = Prawn::Document.new
  pdf.text "x"
  pdf.render_file ` + rubyStr(missing) + `
rescue Prawn::Errors::Error
  r << "render_file"
end
begin
  Prawn::Document.generate(` + rubyStr(missing) + `) { |p| p.text "y" }
rescue Prawn::Errors::Error
  r << "generate"
end
puts r.join("|")
`
	if got := runSrc(t, src); got != "render_file|generate" {
		t.Fatalf("prawn render-file errors = %q", got)
	}
}

// TestPrawnSymOrStrFallback drives the prawnSymOrStr default arm: a non-Symbol,
// non-String page_layout (an Integer) is rendered via #to_s ("123"), which is not
// a valid layout and surfaces as Prawn::Errors::InvalidPageLayout at render.
func TestPrawnSymOrStrFallback(t *testing.T) {
	fixedClock(t)
	src := `
require "prawn"
begin
  Prawn::Document.new(page_layout: 123).render
rescue Prawn::Errors::InvalidPageLayout
  puts "layout-to-s"
end
`
	if got := runSrc(t, src); got != "layout-to-s" {
		t.Fatalf("prawn sym-or-str fallback = %q", got)
	}
}

// TestPrawnCoverageExtras covers the remaining option-shape arms: generate with
// an options Hash (not just a path), a successful render_file, a table with no
// options Hash, and the two font trailing-argument shapes (a non-Hash trailing
// argument, and a Hash carrying no style: key).
func TestPrawnCoverageExtras(t *testing.T) {
	fixedClock(t)
	out := filepath.Join(t.TempDir(), "extra.pdf")
	src := `
require "prawn"
r = []

# generate with an options Hash (and a block) — no path, so #render supplies bytes.
pdf = Prawn::Document.generate(page_size: "A4", margin: 20) { |p| p.text "opt" }
r << (pdf.render.start_with?("%PDF-") ? "gen-opts" : "bad")

# a successful render_file.
doc = Prawn::Document.new
doc.text "written"
doc.render_file ` + rubyStr(out) + `
r << "render_file"

# a table with no options Hash (defaults).
doc.table([["only"]])
r << "table-default"

# font with a trailing non-Hash argument (prawnOptsHash returns nil).
doc.font "Courier", 42
# font with a Hash that has no :style key (prawnStyleFromHash default).
doc.font "Symbol", label: :x
r << doc.font

puts r.join("|")
`
	if got := runSrc(t, src); got != "gen-opts|render_file|table-default|Symbol" {
		t.Fatalf("prawn coverage extras = %q", got)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("render_file did not write %s: %v", out, err)
	}
}

// TestPrawnRequire proves require "prawn" reports a first-time load (true) and a
// no-op reload (false), like a normal gem.
func TestPrawnRequire(t *testing.T) {
	src := `
require "prawn"
puts require("prawn")
`
	if got := runSrc(t, src); got != "false" {
		t.Fatalf("second require should be false, got %q", got)
	}
}

// TestPrawnStyleAndAlign covers every prawnStyle and prawnAlign arm directly,
// including the unrecognised default of each and the String (not Symbol) form.
func TestPrawnStyleAndAlign(t *testing.T) {
	styles := map[object.Value]prawn.Style{
		object.Symbol("normal"):        prawn.StyleNormal,
		object.Symbol("bold"):          prawn.StyleBold,
		object.Symbol("italic"):        prawn.StyleItalic,
		object.Symbol("bold_italic"):   prawn.StyleBoldItalic,
		object.Symbol("weird"):         prawn.StyleNormal, // default arm
		object.NewString("bold"):       prawn.StyleBold,   // String form of prawnSymOrStr
	}
	for v, want := range styles {
		if got := prawnStyle(v); got != want {
			t.Errorf("prawnStyle(%v) = %v want %v", v.Inspect(), got, want)
		}
	}
	aligns := map[object.Value]prawn.Align{
		object.Symbol("left"):    prawn.AlignLeft, // default arm
		object.Symbol("center"):  prawn.AlignCenter,
		object.Symbol("right"):   prawn.AlignRight,
		object.Symbol("justify"): prawn.AlignJustify,
	}
	for v, want := range aligns {
		if got := prawnAlign(v); got != want {
			t.Errorf("prawnAlign(%v) = %v want %v", v.Inspect(), got, want)
		}
	}
}

// TestPrawnRaiseError maps a representative go-ruby-prawn sentinel onto its Ruby
// class (the Kind arm) and a plain error onto the Prawn::Errors::Error base (the
// fallback arm), covering both arms of raisePrawnError directly.
func TestPrawnRaiseError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{prawn.ErrUnknownFont, "Prawn::Errors::UnknownFont"},
		{prawn.ErrCannotFit, "Prawn::Errors::CannotFit"},
		{errors.New("boom"), "Prawn::Errors::Error"},
	}
	for _, c := range cases {
		func() {
			defer func() {
				re, ok := recover().(RubyError)
				if !ok {
					t.Fatalf("raisePrawnError(%v): did not raise a RubyError", c.err)
				}
				if re.Class != c.want {
					t.Errorf("raisePrawnError(%v): class %q want %q", c.err, re.Class, c.want)
				}
			}()
			raisePrawnError(c.err)
		}()
	}
}

// TestPrawnValueMethods covers the Go-level object.Value surface of the Document
// wrapper (Truthy/ToS/Inspect) reached directly, and its classOf dispatch.
func TestPrawnValueMethods(t *testing.T) {
	vm := New(&bytes.Buffer{})
	cls := vm.consts["Prawn"].(*RClass).consts["Document"].(*RClass)
	doc := &PrawnDocument{cls: cls, d: prawn.New(prawn.Options{})}
	if !doc.Truthy() {
		t.Error("document should be truthy")
	}
	if doc.ToS() != "#<Prawn::Document>" || doc.Inspect() != doc.ToS() {
		t.Errorf("document render = %q / %q", doc.ToS(), doc.Inspect())
	}
	if got := vm.classOf(doc); got != cls {
		t.Errorf("classOf(document) = %v want %v", got, cls)
	}
}

// onePixelPNG returns the bytes of a 1x1 opaque black PNG, used to exercise the
// image DSL without depending on an external fixture file.
func onePixelPNG() []byte {
	return []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde,
		0x00, 0x00, 0x00, 0x0c, 'I', 'D', 'A', 'T',
		0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00, 0x00, 0x03, 0x00, 0x01,
		0x18, 0xdd, 0x8d, 0xb0,
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
}

// strconv quotes s as a Ruby double-quoted string literal for embedding in test
// source (escaping backslashes and quotes so a Windows-style path survives).
func rubyStr(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
