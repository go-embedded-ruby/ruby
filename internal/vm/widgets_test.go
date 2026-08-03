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

# A Backdrop ground: explicit hex fill + grid, plus a theme-default one
# (empty colour strings). Render it to prove the new constructor dispatches
# and paints through the normal render path.
back = Widgets.backdrop("#11131a", "#171a24", 40)
Widgets.layout(back, 64, 48)
bpx = Widgets.render(back, 64, 48)
raise "backdrop pixels" unless bpx["pixels"].bytesize == 64 * 48 * 4
themed = Widgets.backdrop("", "", 0)
raise "themed backdrop" unless themed.is_a?(Integer)

# Opt into anti-aliased, shaped OpenType text: window titles, menus, HUD,
# desktop and frame decorations repaint against the bundled vector face.
# Both the default-size and explicit-size forms are auto-registered from the
# adapter's method surface, so respond_to? and the call both resolve.
raise "respond use_opentype_text" unless Widgets.respond_to?(:use_opentype_text)
raise "respond use_opentype_text_size" unless Widgets.respond_to?(:use_opentype_text_size)
Widgets.use_opentype_text
Widgets.use_opentype_text_size(18)
# It repaints through the normal render path (no exception, real pixels).
lbl = Widgets.label("Title")
Widgets.layout(lbl, 80, 20)
lpx = Widgets.render(lbl, 80, 20)
raise "aa label pixels" unless lpx["pixels"].bytesize == 80 * 20 * 4

# --- v0.7.0 desktop-environment overlay + chrome widgets -------------------
# The compositor's DE-feature spine: notifications/toast, tray, wallpaper,
# applet chrome. Each new adapter method is auto-registered from the method
# surface, so it resolves through the same Call shim with no Go change. A 4x4
# RGBA blit is passed base64-encoded (the binding base64-decodes String pixel
# arguments): 64 zero bytes == "AAAA"*21 + "AA==".
px4 = "AAAA" * 21 + "AA=="

# Transient overlays: a Notification and a Toast, shown, anchored, ticked.
note = Widgets.notification("Build finished")
raise "note handle" unless note.is_a?(Integer)
Widgets.set_life(note, 2)
Widgets.set_visible(note, true)
raise "note visible" unless Widgets.visible(note) == true
Widgets.anchor_in(note, 0, 0, 200, 120, "top_right")
Widgets.tick(note)

toast = Widgets.toast("Saved", "success", "Undo", "undo_save")
Widgets.set_kind(toast, "info")
Widgets.anchor_in(toast, 0, 0, 200, 120, "bottom_center")
Widgets.set_visible(toast, true)
raise "toast visible" unless Widgets.visible(toast) == true

# Count/indicator chrome.
badge = Widgets.badge("12", "#c0392b", "#ffffff")
raise "badge" unless badge.is_a?(Integer)
lvl = Widgets.level_bar(5)
Widgets.set_value(lvl, 3)

# Pop-ups: a context menu over an existing Menu, a popover, a command palette.
ctx = Widgets.context_menu(menu)
Widgets.popup(ctx, 12, 20)
Widgets.set_visible(ctx, true)
raise "ctx visible" unless Widgets.visible(ctx) == true
pop = Widgets.popover(Widgets.label("body"), "Details")
Widgets.set_visible(pop, true)
raise "pop visible" unless Widgets.visible(pop) == true
palette = Widgets.command_palette([{"label" => "Open File", "action" => "open"},
                                   {"label" => "Quit", "action" => "quit"}, "skip"])
Widgets.set_visible(palette, true)
raise "palette visible" unless Widgets.visible(palette) == true

# Compact affordances.
ib = Widgets.icon_button("+", "add")
tip = Widgets.tooltip("Add a track", "above")
Widgets.set_visible(tip, true)
av = Widgets.avatar("DD", "#2c3e50")
raise "chrome handles" unless [ib, tip, av].all? { |h| h.is_a?(Integer) }

# An RGBA blit through the image constructor, rendered to real pixels.
img = Widgets.image(px4, 4, 4, "fit")
Widgets.layout(img, 16, 16)
ipx = Widgets.render(img, 16, 16)
raise "image pixels" unless ipx["pixels"].bytesize == 16 * 16 * 4

# Desktop: wallpaper (image + gradient), tray with stock + image icons, thumbnail.
wall = Widgets.wallpaper(px4, 4, 4, "fill")
raise "wallpaper" unless wall.is_a?(Integer)
grad = Widgets.wallpaper_gradient("#203050", "#081020")
Widgets.layout(grad, 32, 24)
gpx = Widgets.render(grad, 32, 24)
raise "gradient pixels" unless gpx["pixels"].bytesize == 32 * 24 * 4

tray = Widgets.status_area
si = Widgets.status_icon("settings", "Settings", "open_settings", "settings_menu")
sii = Widgets.status_icon_image(px4, 4, 4, "Battery", "", "")
Widgets.add_widget(tray, si)
Widgets.add_widget(tray, sii)
Widgets.layout(tray, 64, 24)
tpx = Widgets.render(tray, 64, 24)
raise "tray pixels" unless tpx["pixels"].bytesize == 64 * 24 * 4

thumb = Widgets.thumbnail(px4, 4, 4, "Window 1", "focus_window")
Widgets.set_selected(thumb, true)
Widgets.set_hover(thumb, false)

# --- v0.8.0 (toolkit v0.86.0) DE-widget refinements ------------------------
# The compositor adopts these for the calendar applet, the big-clock font, the
# toast icons and the Spotlight. Each is auto-registered from the method surface.
# Toast: a leading stock-glyph icon, a multi-line body, multi-action buttons.
Widgets.set_toast_icon(toast, "save", 0, 0)
Widgets.set_toast_lines(toast, ["Saved", "3 files written"])
Widgets.set_toast_actions(toast, [{"label" => "Undo", "callback" => "undo_save"}])

# A big-font Label (the desktop's clock face).
clock = Widgets.label("12:00")
Widgets.set_font_size(clock, 48)
Widgets.layout(clock, 160, 60)
raise "clock pixels" unless Widgets.render(clock, 160, 60)["pixels"].bytesize == 160 * 60 * 4

# A Calendar month grid, driven by prev/next + the shared selection accessors.
cal = Widgets.calendar(2026, 8, 3)
raise "calendar" unless cal.is_a?(Integer)
Widgets.on_select(cal, "day_picked")
Widgets.on_month_change(cal, "month_changed")
Widgets.next_month(cal)
Widgets.prev_month(cal)
Widgets.set_selected(cal, 15)
raise "cal day" unless Widgets.selected(cal) == 15

# A LevelBar with a caption + value-band thresholds.
batt = Widgets.level_bar(10, "Battery",
  [{"min" => 0, "color_hex" => "#e00000"},
   {"min" => 8, "color_hex" => "#00a000"}])
Widgets.set_value(batt, 9)

# The CommandPalette host-driven as a Spotlight: query, filter, select, key.
Widgets.set_query(palette, "open")
raise "query" unless Widgets.query(palette) == "open"
raise "filtered" unless Widgets.filtered_commands(palette).length == 1
Widgets.set_selected(palette, 0)
raise "palette sel" unless Widgets.selected(palette) == 0
Widgets.move_selection(palette, 1)
Widgets.handle_key(palette, {"kind" => "keydown", "code" => "Escape"})

# A window decoration built from an explicit spec Hash, rendered.
deco = Widgets.decoration({
  "title" => "untitled.txt",
  "title_color" => "#2b2b2b",
  "title_ink" => "#e0e0e0",
  "titlebar" => [0, 0, 240, 28],
  "border" => [0, 0, 240, 160],
  "border_color" => "#101010",
  "buttons" => [{"rect" => [8, 6, 16, 16], "shape" => "circle",
                 "face" => "#e74c3c", "glyph" => "close", "glyph_ink" => "#000000"}]
})
raise "decoration" unless deco.is_a?(Integer)
Widgets.layout(deco, 240, 160)
dpx = Widgets.render(deco, 240, 160)
raise "deco pixels" unless dpx["pixels"].bytesize == 240 * 160 * 4

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
