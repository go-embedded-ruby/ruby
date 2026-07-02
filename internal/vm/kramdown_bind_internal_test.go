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
	if got := kramdownKey(object.Integer(3)); got != "3" {
		t.Errorf("integer key -> %q", got)
	}
	if got := kramdownStr(object.Integer(7)); got != "7" {
		t.Errorf("integer str -> %q", got)
	}
}

// TestKramdownOptionsFootnoteNonInt covers the footnote_nr arm being given a
// non-Integer value: the default is kept (the type switch's ok=false path).
func TestKramdownOptionsFootnoteNonInt(t *testing.T) {
	h := object.NewHash()
	h.Set(object.Symbol("footnote_nr"), object.NewString("nope"))
	o := kramdownOptions(h)
	if o == nil || o.FootnoteNr != 1 {
		t.Errorf("footnote_nr non-int should keep default 1, got %#v", o)
	}
}
