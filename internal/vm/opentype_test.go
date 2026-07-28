// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"

	opentype "github.com/go-ruby-opentype/opentype"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// otRecover runs fn and returns the RubyError class it raises, or "" when it
// returns without raising — the seam for asserting the TypeError / Opentype
// error branches of the marshalling helpers.
func otRecover(fn func()) (class string) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok {
				class = re.Class
				return
			}
			panic(r)
		}
	}()
	fn()
	return ""
}

// TestOpentypeShells covers ToS/Inspect/Truthy of the Font and Face wrappers.
func TestOpentypeShells(t *testing.T) {
	f := &OpentypeFont{}
	if f.ToS() != "#<Opentype::Font>" || f.Inspect() != f.ToS() || !f.Truthy() {
		t.Errorf("font shell: %q / %q / %v", f.ToS(), f.Inspect(), f.Truthy())
	}
	fc := &OpentypeFace{}
	if fc.ToS() != "#<Opentype::Face>" || fc.Inspect() != fc.ToS() || !fc.Truthy() {
		t.Errorf("face shell: %q / %q / %v", fc.ToS(), fc.Inspect(), fc.Truthy())
	}
}

// TestOpentypeRequireEndToEnd is the headline scenario: require the library,
// load the most-legible bundled font, parse and size it, shape a string into a
// non-empty positioned-glyph Array, and list the bundled families. It is the
// task's acceptance test run through the interpreter.
func TestOpentypeRequireEndToEnd(t *testing.T) {
	src := `
require "opentype"

# The default (most-legible) family flows as a binary String of TTF bytes.
blob = Opentype.most_legible
raise "no font bytes" unless blob.length > 1000

# Parse it (open_font) and via the default_font shortcut.
font = Opentype.open_font(blob)
raise "parse failed" unless font.is_a?(Opentype::Font)
raise "default_font" unless Opentype.default_font.is_a?(Opentype::Font)
raise "glyph count" unless font.num_glyphs > 100

# Size it and shape a run: an Array of positioned-glyph Hashes.
face = font.face(24)
raise "face class" unless face.is_a?(Opentype::Face)
run = Opentype.shape(face, "Hello")
raise "empty run" unless run.length > 0
first = run[0]
raise "no gid" unless first["gid"] >= 0
raise "no advance" unless first.key?("x_advance")

# The bundled families, each a Hash of metadata.
fams = Opentype.families
raise "no families" unless fams.length > 0
raise "no name" unless fams[0]["name"] == "Atkinson Hyperlegible"

puts "glyphs=#{run.length}"
puts "families=#{fams.length}"
`
	got := runSrc(t, src)
	if !strings.Contains(got, "glyphs=") || !strings.Contains(got, "families=") {
		t.Fatalf("end-to-end output = %q", got)
	}
}

// TestOpentypeSurface exercises the rest of the Ruby-visible surface so every
// marshalling branch and every handle method is driven through the interpreter:
// the Font queries, the Face measurement / metrics / glyph / variation methods,
// the Bidi helpers, and the option-Hash argument shapes (String, Symbol and
// integer keys; Array, Symbol and Float values; a nil options argument).
func TestOpentypeSurface(t *testing.T) {
	src := `
require "opentype"
font = Opentype.default_font

# Font queries: an Int, a nil (a rune with no glyph), and empty variation Arrays
# on this non-variable font.
raise "glyph_index" unless font.glyph_index("A") >= 0
raise "missing glyph" unless font.glyph_index(0).nil?
raise "axes" unless font.axes == []
raise "named_instances" unless font.named_instances == []
raise "parse alias" unless font.equal?(font) && Opentype.parse(Opentype.most_legible).is_a?(Opentype::Font)

face = font.face(20)

# Scalar returns.
raise "measure" unless face.measure("AV") > 0
raise "advance" unless face.advance("A") > 0
raise "kern" unless face.kern("A", "V").is_a?(Integer)

# A Hash return with a Float member (scale).
m = face.metrics
raise "metrics" unless m["scale"].is_a?(Float) && m["ascent"].is_a?(Integer)

# A glyph-info Hash: a Bool member and a binary-String coverage mask.
gi = face.glyph_info("A", 0, 0)
raise "found" unless gi["found"] == true
raise "mask" unless gi["mask"]["pix"].bytesize > 0

# A void method returns nil.
raise "set_hinting" unless face.set_hinting(true).nil?

# set_variation echoes the numeric coords back as a Hash (a Float value arg).
v = face.set_variation({"wght" => 700.0})
raise "variation" unless v["wght"] == 700.0

# Bidi helpers: a String and an Array of Ints.
raise "visual_order" unless Opentype.visual_order("abc", "ltr") == "abc"
lv = Opentype.resolve_levels("abc", "auto")
raise "levels" unless lv.is_a?(Array) && lv[0].is_a?(Integer)

# load(default) returns bytes; load(other) returns nil.
raise "load default" unless Opentype.load("Atkinson Hyperlegible").length > 0
raise "load other" unless Opentype.load("Inter").nil?

# Option-Hash argument shapes: String key, Symbol key, integer key (to_s'd),
# an Array value (features), a Symbol value (direction), and a nil opts arg.
run = Opentype.shape(face, "AV", {"direction" => "ltr", :script => "latn", 1 => 2, "features" => ["kern"]})
raise "opts run" unless run.length > 0
raise "sym opts" unless Opentype.shape(face, "AV", {:direction => :ltr}).length > 0
raise "nil opts" unless Opentype.shape(face, "AV", nil).length > 0

puts "ok"
`
	if got := runSrc(t, src); got != "ok" {
		t.Fatalf("surface output = %q", got)
	}
}

// TestOpentypeErrors covers the raise paths: a bad font blob and an unknown
// adapter method both surface as a rescuable Opentype::Error.
func TestOpentypeErrors(t *testing.T) {
	src := `
require "opentype"
result = begin
  Opentype.open_font("not a font")
  "no-raise"
rescue Opentype::Error => e
  "raised"
end
puts result
`
	if got := runSrc(t, src); got != "raised" {
		t.Fatalf("bad-blob error = %q", got)
	}

	// An unknown method name funnels through Call's error into Opentype::Error.
	if got := otRecover(func() { New(nil).otDispatch(opentype.NewModule(), "no_such_method", nil) }); got != "Opentype::Error" {
		t.Fatalf("unknown method class = %q", got)
	}
}

// TestOpentypeValueToAny covers otValueToAny branches the Ruby surface cannot
// reach naturally: a *OpentypeFont argument (handles are results, never method
// arguments, so only a direct call exercises the unwrap) and an unmappable value
// (a Range) which raises TypeError.
func TestOpentypeValueToAny(t *testing.T) {
	mod := opentype.NewModule()
	font, err := mod.OpenFont(mod.MostLegible())
	if err != nil {
		t.Fatal(err)
	}
	if got := otValueToAny(&OpentypeFont{h: font}); got != font {
		t.Errorf("font unwrap = %#v", got)
	}

	// An object rbgo has but the adapter cannot accept raises TypeError.
	rng := object.NewRange(object.Integer(1), object.Integer(2), false)
	if cls := otRecover(func() { otValueToAny(rng) }); cls != "TypeError" {
		t.Errorf("unmappable arg class = %q", cls)
	}
}

// TestOpentypeAnyToValue covers otAnyToValue branches the adapter's own methods
// never emit: a bare int64 scalar and a value type with no Ruby peer (which
// raises TypeError). Every other branch is driven by TestOpentypeSurface.
func TestOpentypeAnyToValue(t *testing.T) {
	if v, ok := otAnyToValue(int64(7)).(object.Integer); !ok || v != 7 {
		t.Errorf("int64 -> %#v", otAnyToValue(int64(7)))
	}
	if cls := otRecover(func() { otAnyToValue(complex(1, 2)) }); cls != "TypeError" {
		t.Errorf("unmappable result class = %q", cls)
	}
}

// TestOpentypeKeyString covers otKeyString's to_s fallback for a non-String,
// non-Symbol Hash key (the String and Symbol arms are exercised by
// TestOpentypeSurface's option Hashes).
func TestOpentypeKeyString(t *testing.T) {
	if got := otKeyString(object.Integer(7)); got != "7" {
		t.Errorf("integer key -> %q", got)
	}
}
