// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	widgets "github.com/go-ruby-widgets/widgets"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestWidgetsRequireEndToEnd is the headline scenario: require the library,
// build a button in a vertical box, lay the tree out, render it to an RGBA pixel
// buffer, and dispatch a click that fires the button's callback — the task's
// acceptance test run through the interpreter. It drives the []byte (pixels),
// []any (fired) and Hash (render/dispatch) result branches at once.
func TestWidgetsRequireEndToEnd(t *testing.T) {
	src := `
require "widgets"

b = Widgets.button("Hi", "hit")
root = Widgets.v_box
Widgets.add_widget(root, b)
Widgets.layout(root, 100, 40)

px = Widgets.render(root, 100, 40)
raise "no pixels" unless px["pixels"].bytesize > 0
raise "stride" unless px["stride"] == 400
raise "size" unless px["w"] == 100 && px["h"] == 40

# A click on the button fires its callback and asks for a repaint.
Widgets.layout(b, 100, 40)
res = Widgets.dispatch(b, {"kind" => "click", "x" => 5, "y" => 5})
raise "not fired" unless res["fired"] == ["hit"]
raise "no repaint" unless res["repaint"] == true

puts "bytes=#{px["pixels"].bytesize}"
puts "fired=#{res["fired"].length}"
`
	got := runSrc(t, src)
	if got == "" {
		t.Fatalf("end-to-end produced no output")
	}
}

// TestWidgetsSurface exercises the rest of the Ruby-visible surface so every
// remaining marshalling branch and constructor/mutator/composition verb is
// driven through the interpreter: the other constructors, the Array- and
// Hash-shaped arguments, the Symbol argument (set_style), the Bool round-trip
// (check_button / checked), the String round-trip (text) and the Hash returns
// (bounds).
func TestWidgetsSurface(t *testing.T) {
	src := `
require "widgets"

# Text round-trip through a Label (String arg + String return).
l = Widgets.label("hello")
Widgets.set_text(l, "world")
raise "text" unless Widgets.text(l) == "world"

# Bool round-trip through a CheckButton (Bool arg + Bool return).
c = Widgets.check_button("on?", true, "toggled")
raise "checked" unless Widgets.checked(c) == true
Widgets.set_checked(c, false)
raise "unchecked" unless Widgets.checked(c) == false

# Symbol argument (set_style) + more mutators.
btn = Widgets.button("Go", nil)
Widgets.set_style(btn, :prominent)
Widgets.set_theme("dark")

# Array-shaped arguments: drop-down, list box, menu (Array of Hashes).
dd = Widgets.drop_down(["a", "b", "c"], 0, "picked")
Widgets.select(dd, 2)
lb = Widgets.list_box(["x", "y"], nil)
Widgets.select(lb, 1)
menu = Widgets.menu([{"label" => "Open", "shortcut" => "Ctrl+O", "action" => "open"},
                     {"separator" => true}, "skip-me"])
bar = Widgets.menu_bar
Widgets.add_menu(bar, "File", menu)

# Grid + attach; an HBox with fixed/flex children; spacing.
g = Widgets.grid(2, 1)
Widgets.attach(g, Widgets.label("g0"), 0, 0)
Widgets.attach(g, Widgets.label("g1"), 1, 0)
hb = Widgets.h_box
Widgets.set_spacing(hb, 4)
Widgets.add_fixed(hb, Widgets.label("fix"), 20)
Widgets.add_flex(hb, Widgets.label("flex"), 1)

# A config-driven container with a border layout + Add options Hash (region,
# flex, size — String, Symbol and integer values covered).
cont = Widgets.container("border")
Widgets.add(cont, Widgets.label("N"), {"region" => "north", "size" => 10})
# Symbol Hash keys (exercise the Symbol key arm) alongside a Symbol value.
Widgets.add(cont, Widgets.label("C"), {:region => :center, :flex => 1})

# A card container: set_active + set_layout.
card = Widgets.container("card")
Widgets.add_widget(card, Widgets.label("p0"))
Widgets.add_widget(card, Widgets.label("p1"))
Widgets.set_active(card, 1)
Widgets.set_layout(card, "vbox")

# A frame around a child, a dock, a bare border, a text view, an entry.
fr = Widgets.frame(Widgets.label("framed"))
dk = Widgets.dock(Widgets.label("body"))
Widgets.dock_at(dk, Widgets.label("top"), "top", 8)
bd = Widgets.border
Widgets.set_region(bd, Widgets.label("w"), "west", 6)
tv = Widgets.text_view("multi\nline")
en = Widgets.entry("typed", "changed")

# Layout the whole tree, then read a Hash of bounds.
Widgets.layout(cont, 200, 120)
bnds = Widgets.bounds(cont)
raise "bounds" unless bnds["w"] == 200 && bnds["h"] == 120

puts "ok"
`
	if got := runSrc(t, src); got != "ok" {
		t.Fatalf("surface output = %q", got)
	}
}

// TestWidgetsErrors covers the raise paths: an unknown handle and an unknown
// adapter method both surface as a rescuable Widgets::Error.
func TestWidgetsErrors(t *testing.T) {
	src := `
require "widgets"
result = begin
  Widgets.render(999999, 10, 10)
  "no-raise"
rescue Widgets::Error => e
  "raised"
end
puts result
`
	if got := runSrc(t, src); got != "raised" {
		t.Fatalf("unknown-handle error = %q", got)
	}

	// An unknown method name funnels through Call's error into Widgets::Error.
	if got := otRecover(func() { New(nil).wdgDispatch(widgets.NewModule(), "no_such_method", nil) }); got != "Widgets::Error" {
		t.Fatalf("unknown method class = %q", got)
	}
}

// TestWidgetsValueToAny covers wdgValueToAny branches the Ruby surface cannot
// reach naturally: a Float argument (no Widgets method takes a float) and an
// unmappable value (a Range) which raises TypeError.
func TestWidgetsValueToAny(t *testing.T) {
	if got := wdgValueToAny(object.Float(1.5)); got != 1.5 {
		t.Errorf("float -> %#v", got)
	}
	rng := object.NewRange(object.Integer(1), object.Integer(2), false)
	if cls := otRecover(func() { wdgValueToAny(rng) }); cls != "TypeError" {
		t.Errorf("unmappable arg class = %q", cls)
	}
}

// TestWidgetsAnyToValue covers wdgAnyToValue branches the adapter's own methods
// never emit: a bare int64 and float64 scalar, and a value type with no Ruby
// peer (which raises TypeError). Every other branch is driven by the surface
// tests.
func TestWidgetsAnyToValue(t *testing.T) {
	if v, ok := wdgAnyToValue(int64(7)).(object.Integer); !ok || v != 7 {
		t.Errorf("int64 -> %#v", wdgAnyToValue(int64(7)))
	}
	if v, ok := wdgAnyToValue(float64(2.5)).(object.Float); !ok || v != 2.5 {
		t.Errorf("float64 -> %#v", wdgAnyToValue(float64(2.5)))
	}
	if cls := otRecover(func() { wdgAnyToValue(complex(1, 2)) }); cls != "TypeError" {
		t.Errorf("unmappable result class = %q", cls)
	}
}

// TestWidgetsKeyString covers wdgKeyString's to_s fallback for a non-String,
// non-Symbol Hash key (the String and Symbol arms are exercised by the surface
// test's option Hashes).
func TestWidgetsKeyString(t *testing.T) {
	if got := wdgKeyString(object.Integer(7)); got != "7" {
		t.Errorf("integer key -> %q", got)
	}
}
