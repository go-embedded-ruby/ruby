// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	jbuilder "github.com/go-ruby-jbuilder/jbuilder"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestJbuilderToBridge covers the Go-only arms of toJbuilder the Ruby tests do
// not reach directly: a plain Go nil (mapped to nil) and a nested *Jbuilder
// wrapper (mapped to its underlying builder).
func TestJbuilderToBridge(t *testing.T) {
	vm := New(nil)
	if got := toJbuilder(vm, nil); got != nil {
		t.Errorf("plain nil: got %#v want nil", got)
	}
	inner := &Jbuilder{b: jbuilder.New()}
	if got := toJbuilder(vm, inner); got != inner.b {
		t.Errorf("nested Jbuilder: got %#v want the inner builder", got)
	}
	if got := toJbuilder(vm, object.NilV); got != nil {
		t.Errorf("object.Nil: got %#v want nil", got)
	}
}

// TestJbuilderShell covers the *Jbuilder value shell (ToS / Inspect / Truthy).
func TestJbuilderShell(t *testing.T) {
	j := &Jbuilder{b: jbuilder.New()}
	if j.ToS() != "#<Jbuilder>" || j.Inspect() != "#<Jbuilder>" || !j.Truthy() {
		t.Errorf("shell mismatch: ToS=%q Inspect=%q Truthy=%v", j.ToS(), j.Inspect(), j.Truthy())
	}
}

// TestJbuilderNameDefault covers jbuilderName's fall-through (a non-Symbol,
// non-String key rendered via to_s).
func TestJbuilderNameDefault(t *testing.T) {
	if got := jbuilderName(object.Integer(5)); got != "5" {
		t.Errorf("integer key: got %q want \"5\"", got)
	}
}
