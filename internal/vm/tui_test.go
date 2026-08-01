// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	tui "github.com/go-ruby-widgets/tui"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestTuiShells covers ToS/Inspect/Truthy of the Widget wrapper.
func TestTuiShells(t *testing.T) {
	w := &TuiWidget{}
	if w.ToS() != "#<Tui::Widget>" || w.Inspect() != w.ToS() || !w.Truthy() {
		t.Errorf("widget shell: %q / %q / %v", w.ToS(), w.Inspect(), w.Truthy())
	}
}

// TestTuiRequireEndToEnd is the headline scenario: require the library, put a
// label in a container, lay it out, render it to an ANSI frame and to a decoded
// cell grid, and assert the label text appears in the frame — the task's
// acceptance test run through the interpreter.
func TestTuiRequireEndToEnd(t *testing.T) {
	src := `
require "tui"

lbl  = Tui.label("Hello")
cont = Tui.container
cont.set_body(lbl)
Tui.set_size(cont, 20, 3)

# render_cells: a Hash grid whose per-row "text" contains the label.
grid = Tui.render_cells(cont, 20, 3)
raise "no text row" unless grid["text"].join(" ").include?("Hello")
raise "grid cols" unless grid["cols"] == 20

# render: a self-contained ANSI string; decode_cells re-parses it.
ansi = Tui.render(cont, 20, 3)
re = Tui.decode_cells(ansi, 20, 3)
raise "redecode" unless re["text"].join(" ").include?("Hello")

# A cell Hash carries a char and optional colors.
cell = grid["cells"][0][0]
raise "cell char" unless cell.key?("char")

puts "ok"
`
	if got := runSrc(t, src); got != "ok" {
		t.Fatalf("end-to-end output = %q", got)
	}
}

// TestTuiSurface exercises the rest of the Ruby-visible surface so every
// remaining marshalling branch, constructor, widget method and callback is
// driven through the interpreter: the Bool round-trip (check_button / checked),
// the Float round-trip (progress_bar / set_fraction / fraction), the Int returns
// (list box selected / notebook active / bounds), the Array return (items), the
// Widget-handle argument (a child pane), the Symbol argument (set_align), and a
// click that fires a wired callback (a nil and a Symbol event-Hash key too).
func TestTuiSurface(t *testing.T) {
	src := `
require "tui"

# Label alignment via a Symbol argument; text round-trip.
lbl = Tui.label("hi")
lbl.set_align(:center)
lbl.set_text("bye")
raise "text" unless lbl.text == "bye"
raise "kind" unless lbl.kind == "label"

# Bool round-trip through a check button + a wired toggle.
cb = Tui.check_button("on?", true)
cb.on_toggle("toggled")
raise "checked" unless cb.checked == true
cb.set_checked(false)
raise "unchecked" unless cb.checked == false

# Float round-trip through a progress bar.
pb = Tui.progress_bar
pb.set_label("loading")
pb.set_fraction(0.5)
raise "fraction" unless (pb.fraction - 0.5).abs < 0.001

# Int returns + Array return through a list box.
lb = Tui.list_box(["a", "b", "c"])
lb.on_select("selected")
lb.set_items(["x", "y"])
lb.set_selected(1)
raise "selected" unless lb.selected == 1
raise "items" unless lb.items == ["x", "y"]

# An entry with a placeholder and a wired change callback.
en = Tui.entry("seed")
en.set_placeholder("type here")
en.on_change("changed")

# Containers and splits take Widget-handle arguments (child panes).
cont = Tui.container
cont.set_header(Tui.label("H"))
cont.set_body(lb)
cont.set_footer(Tui.label("F"))
cont.set_header_height(1)
cont.set_footer_height(1)
cont.add_overlay(Tui.label("O"))

hs = Tui.h_split
hs.set_left(Tui.label("L"))
hs.set_right(Tui.label("R"))
hs.set_left_fraction(30)

vs = Tui.v_split
vs.set_top(Tui.label("T"))
vs.set_bottom(Tui.label("B"))
vs.set_top_fraction(40)

# A notebook: tabs, active index round-trip, tab-change callback.
nb = Tui.notebook
nb.add_tab("one", Tui.label("p1"))
nb.add_tab("two", Tui.label("p2"))
nb.on_tab_changed("tab")
nb.set_active(1)
raise "active" unless nb.active == 1

# Bounds Hash after layout.
Tui.set_size(cont, 40, 10)
bn = Tui.bounds(cont)
raise "bounds" unless bn["w"] == 40 && bn["h"] == 10

# A click on a button fires its callback; the event Hash carries a Symbol key
# and a nil value (exercising those marshalling arms).
btn = Tui.button("OK")
btn.on_click("clicked")
Tui.set_size(btn, 10, 1)
res = Tui.dispatch(btn, {"kind" => "click", "x" => 1, "y" => 0, :ctrl => nil})
raise "fired" unless res["fired"] == ["clicked"]
raise "repaint" unless res["repaint"] == true

puts "ok"
`
	if got := runSrc(t, src); got != "ok" {
		t.Fatalf("surface output = %q", got)
	}
}

// TestTuiErrors covers the raise paths: a method used on the wrong widget kind, a
// nil widget handle, and an unknown adapter method all surface as a rescuable
// Tui::Error.
func TestTuiErrors(t *testing.T) {
	src := `
require "tui"

# set_fraction on a label is a wrong-kind error.
wrong = begin
  Tui.label("x").set_fraction(0.5)
  "no-raise"
rescue Tui::Error
  "raised"
end
raise "wrong-kind" unless wrong == "raised"

# render on a nil handle is a nil-widget error.
nilw = begin
  Tui.render(nil, 10, 3)
  "no-raise"
rescue Tui::Error
  "raised"
end
puts nilw
`
	if got := runSrc(t, src); got != "raised" {
		t.Fatalf("nil-widget error = %q", got)
	}

	// An unknown method name funnels through Call's error into Tui::Error.
	if got := otRecover(func() { New(nil).tuiDispatch(tui.NewModule(), "no_such_method", nil) }); got != "Tui::Error" {
		t.Fatalf("unknown method class = %q", got)
	}
}

// TestTuiValueToAny covers the one tuiValueToAny branch the Ruby surface cannot
// reach: an unmappable value (a Range) raising TypeError. Every other arm — the
// scalars, Array, Hash and the Widget-handle argument — is driven by the surface
// test.
func TestTuiValueToAny(t *testing.T) {
	rng := object.NewRange(object.Integer(1), object.Integer(2), false)
	if cls := otRecover(func() { tuiValueToAny(rng) }); cls != "TypeError" {
		t.Errorf("unmappable arg class = %q", cls)
	}
}

// TestTuiAnyToValue covers tuiAnyToValue branches the adapter's own methods never
// emit: a bare int64 scalar and a value type with no Ruby peer (which raises
// TypeError).
func TestTuiAnyToValue(t *testing.T) {
	if v, ok := tuiAnyToValue(int64(7)).(object.Integer); !ok || v != 7 {
		t.Errorf("int64 -> %#v", tuiAnyToValue(int64(7)))
	}
	if cls := otRecover(func() { tuiAnyToValue(complex(1, 2)) }); cls != "TypeError" {
		t.Errorf("unmappable result class = %q", cls)
	}
}

// TestTuiKeyString covers tuiKeyString's to_s fallback for a non-String,
// non-Symbol Hash key (the String and Symbol arms are exercised by the surface
// test's event Hash).
func TestTuiKeyString(t *testing.T) {
	if got := tuiKeyString(object.Integer(7)); got != "7" {
		t.Errorf("integer key -> %q", got)
	}
}
