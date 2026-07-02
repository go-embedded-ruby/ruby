// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestRQRCodeConstants covers the RQRCode loadable module, its QRCode class and
// error tree (require "rqrcode").
func TestRQRCodeConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rqrcode"; p RQRCode.is_a?(Module)`, "true\n"},
		{`require "rqrcode"; p defined?(RQRCode::QRCode)`, "\"constant\"\n"},
		{`require "rqrcode"; p RQRCode::QRCodeArgumentError < StandardError`, "true\n"},
		{`require "rqrcode"; p RQRCode::QRCodeRunTimeError < StandardError`, "true\n"},
		{`p require "rqrcode"`, "true\n"},
		{`require "rqrcode"; p require "rqrcode"`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRQRCodeMatrix covers the module-matrix accessors: version, module_count,
// modules / to_a (Array of Arrays of booleans), qrcode (self), and checked?.
func TestRQRCodeMatrix(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rqrcode"; p RQRCode::QRCode.new("A", size: 2, level: :l).version`, "2\n"},
		{`require "rqrcode"; p RQRCode::QRCode.new("A", size: 2, level: :l).module_count`, "25\n"},
		// modules and to_a return a 25x25 matrix of booleans.
		{`require "rqrcode"; qr = RQRCode::QRCode.new("A", size: 2, level: :l); p qr.modules.length; p qr.modules[0].length; p qr.modules[0][0].class`, "25\n25\nTrueClass\n"},
		{`require "rqrcode"; qr = RQRCode::QRCode.new("A", size: 2, level: :l); p qr.to_a.length`, "25\n"},
		// qrcode returns the same object; a chained .modules works like the gem.
		{`require "rqrcode"; qr = RQRCode::QRCode.new("A", size: 2, level: :l); p qr.qrcode.equal?(qr)`, "true\n"},
		{`require "rqrcode"; qr = RQRCode::QRCode.new("A", size: 2, level: :l); p qr.qrcode.modules.length`, "25\n"},
		// checked?(row, col) reads a single module (the finder pattern corner is dark).
		{`require "rqrcode"; p RQRCode::QRCode.new("A", size: 2, level: :l).checked?(0, 0)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRQRCodeRenderers covers each renderer: as_html, as_svg (with and without
// options), as_ansi (default and with options), and to_s (default and with
// options).
func TestRQRCodeRenderers(t *testing.T) {
	// as_html produces an HTML table.
	if got := eval(t, `require "rqrcode"; puts RQRCode::QRCode.new("A", size: 2, level: :l).as_html.start_with?("<table>")`); got != "true\n" {
		t.Errorf("as_html got=%q", got)
	}
	// as_svg with default options is a standalone SVG document.
	if got := eval(t, `require "rqrcode"; puts RQRCode::QRCode.new("A", size: 2, level: :l).as_svg.include?("<svg")`); got != "true\n" {
		t.Errorf("as_svg got=%q", got)
	}
	// as_svg honours module_size, color, viewbox, use_path options.
	if got := eval(t, `require "rqrcode"; svg = RQRCode::QRCode.new("A", size: 2, level: :l).as_svg({ module_size: 4, color: "f00", viewbox: true, use_path: true, offset: 2, fill: "fff", shape_rendering: "auto" }); puts svg.include?("viewBox")`); got != "true\n" {
		t.Errorf("as_svg opts got=%q", got)
	}
	// as_ansi default renders (non-empty) and honours options.
	if got := eval(t, `require "rqrcode"; puts(RQRCode::QRCode.new("A", size: 2, level: :l).as_ansi.length > 0)`); got != "true\n" {
		t.Errorf("as_ansi default got=%q", got)
	}
	if got := eval(t, `require "rqrcode"; puts(RQRCode::QRCode.new("A", size: 2, level: :l).as_ansi({ quiet_zone_size: 0, light: "L", dark: "D", fill_character: "." }).length > 0)`); got != "true\n" {
		t.Errorf("as_ansi opts got=%q", got)
	}
	// to_s default uses "x" for dark; a custom dark/light/quiet renders too.
	if got := eval(t, `require "rqrcode"; puts RQRCode::QRCode.new("A", size: 2, level: :l).to_s.lines.first.include?("x")`); got != "true\n" {
		t.Errorf("to_s got=%q", got)
	}
	if got := eval(t, `require "rqrcode"; puts RQRCode::QRCode.new("A", size: 2, level: :l).to_s({ dark: "#", light: ".", quiet_zone_size: 1 }).include?("#")`); got != "true\n" {
		t.Errorf("to_s opts got=%q", got)
	}
}

// TestRQRCodeModeAndLevelForms covers accepting a level/mode as both a Symbol and
// a String, and forcing an encoding mode.
func TestRQRCodeModeAndLevelForms(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rqrcode"; p RQRCode::QRCode.new("hi", level: "q").version > 0`, "true\n"},
		{`require "rqrcode"; p RQRCode::QRCode.new("123", mode: :number).version > 0`, "true\n"},
		{`require "rqrcode"; p RQRCode::QRCode.new("123", mode: "number").version > 0`, "true\n"},
		// A non-String data argument is coerced via to_s (an integer's digits are
		// numeric-mode data).
		{`require "rqrcode"; p RQRCode::QRCode.new(12345).version > 0`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRQRCodeErrors covers the error tree: a size beyond 40 and a bad forced mode
// raise QRCodeArgumentError, an over-capacity request raises QRCodeRunTimeError,
// an out-of-range checked? raises QRCodeRunTimeError, and the argument-count
// guards raise ArgumentError.
func TestRQRCodeErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "rqrcode"; begin; RQRCode::QRCode.new("1", size: 60); rescue => e; p e.class; end`, "RQRCode::QRCodeArgumentError\n"},
		{`require "rqrcode"; begin; RQRCode::QRCode.new("abc", mode: :number); rescue => e; p e.class; end`, "RQRCode::QRCodeArgumentError\n"},
		{`require "rqrcode"; begin; RQRCode::QRCode.new("A" * 5000, max_size: 1); rescue => e; p e.class; end`, "RQRCode::QRCodeRunTimeError\n"},
		{`require "rqrcode"; begin; RQRCode::QRCode.new("A", size: 2, level: :l).checked?(999, 0); rescue => e; p e.class; end`, "RQRCode::QRCodeRunTimeError\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Argument-count guards.
	for _, src := range []string{
		`require "rqrcode"; RQRCode::QRCode.new`,
		`require "rqrcode"; RQRCode::QRCode.new("A", size: 2, level: :l).checked?(0)`,
	} {
		if got := eval(t, `begin; `+src+`; rescue ArgumentError => e; p e.class; end`); !strings.Contains(got, "ArgumentError") {
			t.Errorf("src=%q got=%q", src, got)
		}
	}
}
