// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestKramdownDocShell covers the KramdownDoc value shell's ToS/Inspect/Truthy.
func TestKramdownDocShell(t *testing.T) {
	d := &KramdownDoc{src: "x"}
	if d.ToS() != "#<Kramdown::Document>" || d.Inspect() != "#<Kramdown::Document>" || !d.Truthy() {
		t.Errorf("shell: %q / %q / %v", d.ToS(), d.Inspect(), d.Truthy())
	}
}

// TestKramdownKeyBridge covers kramdownKey's to_s-default arm (a non-Symbol,
// non-String key) and kramdownStr's to_s-default arm (a non-String value).
func TestKramdownKeyBridge(t *testing.T) {
	if got := kramdownKey(object.SymVal(string(object.Symbol("s")))); got != "s" {
		t.Errorf("symbol key -> %q", got)
	}
	if got := kramdownKey(object.Wrap(object.NewString("k"))); got != "k" {
		t.Errorf("string key -> %q", got)
	}
	if got := kramdownKey(object.IntValue(int64(object.Integer(3)))); got != "3" {
		t.Errorf("integer key -> %q", got)
	}
	if got := kramdownStr(object.Wrap(object.NewString("s"))); got != "s" {
		t.Errorf("string str -> %q", got)
	}
	if got := kramdownStr(object.IntValue(int64(object.Integer(7)))); got != "7" {
		t.Errorf("integer str -> %q", got)
	}
}

// TestKramdownOptionsFootnoteNonInt covers the footnote_nr arm being given a
// non-Integer value: the default is kept (the type switch's ok=false path).
func TestKramdownOptionsFootnoteNonInt(t *testing.T) {
	h := object.NewHash()
	h.Set(object.SymVal(string(object.Symbol("footnote_nr"))), object.Wrap(object.NewString("nope")))
	o := kramdownOptions(object.Wrap(h))
	if o == nil || o.FootnoteNr != 1 {
		t.Errorf("footnote_nr non-int should keep default 1, got %#v", o)
	}
}
