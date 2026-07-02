// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	xmlbuilder "github.com/go-ruby-builder/builder"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestBuilderValueOf covers the arms of builderValueOf the Ruby tests do not
// reach directly: a plain Go nil and the object.Nil singleton (both map to Go
// nil), plus the String and Symbol shapes.
func TestBuilderValueOf(t *testing.T) {
	if got := builderValueOf(nil); got != nil {
		t.Errorf("plain nil: got %#v want nil", got)
	}
	if got := builderValueOf(object.NilV); got != nil {
		t.Errorf("object.Nil: got %#v want nil", got)
	}
	if got := builderValueOf(object.NewString("s")); got != "s" {
		t.Errorf("string: got %#v want \"s\"", got)
	}
	if got := builderValueOf(object.Symbol("y")); got != "y" {
		t.Errorf("symbol: got %#v want \"y\"", got)
	}
	if got := builderValueOf(object.Integer(3)); got != "3" {
		t.Errorf("integer: got %#v want \"3\"", got)
	}
}

// TestBuilderNameAndContent covers builderName's String arm and default
// fall-through, and builderContent's empty-args path.
func TestBuilderNameAndContent(t *testing.T) {
	if got := builderName(object.NewString("k")); got != "k" {
		t.Errorf("string name: got %q want \"k\"", got)
	}
	if got := builderName(object.Integer(4)); got != "4" {
		t.Errorf("default name: got %q want \"4\"", got)
	}
	if got := builderContent(nil); got != "" {
		t.Errorf("empty content: got %q want \"\"", got)
	}
	if got := builderString(nil); got != "" {
		t.Errorf("empty string content: got %q want \"\"", got)
	}
}

// TestXmlMarkupShell covers the *XmlMarkup value shell (ToS / Inspect / Truthy).
func TestXmlMarkupShell(t *testing.T) {
	m := &XmlMarkup{x: xmlbuilder.New()}
	if m.ToS() != "#<Builder::XmlMarkup>" || m.Inspect() != "#<Builder::XmlMarkup>" || !m.Truthy() {
		t.Errorf("shell mismatch: ToS=%q Inspect=%q Truthy=%v", m.ToS(), m.Inspect(), m.Truthy())
	}
}
