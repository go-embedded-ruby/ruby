// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestBoundInspectDelegates checks the object.Value#Inspect() adapters on
// BoundMethod / UnboundMethod (which the interface requires but Ruby dispatch,
// routing through the #inspect→#to_s alias, never calls) render the same string
// as #ToS().
func TestBoundInspectDelegates(t *testing.T) {
	vm := New(nil)
	cls := newClass("K", vm.cObject)
	m := &Method{name: "f", native: func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return self }, owner: cls}
	obj := &RObject{class: cls}

	b := vm.newBoundMethod(obj, "f", m)
	if b.Inspect() != b.ToS() {
		t.Errorf("BoundMethod.Inspect()=%q ToS()=%q", b.Inspect(), b.ToS())
	}
	u := &UnboundMethod{name: "f", owner: cls, m: m, vm: vm}
	if u.Inspect() != u.ToS() {
		t.Errorf("UnboundMethod.Inspect()=%q ToS()=%q", u.Inspect(), u.ToS())
	}
}

// TestLocalNameOutOfRange covers localName's guard: a slot outside the iseq's
// recorded locals yields the empty string rather than panicking.
func TestLocalNameOutOfRange(t *testing.T) {
	is := &bytecode.ISeq{Locals: []string{"a"}}
	if got := localName(is, 5); got != "" {
		t.Errorf("out-of-range localName = %q, want empty", got)
	}
	if got := localName(is, 0); got != "a" {
		t.Errorf("in-range localName = %q, want %q", got, "a")
	}
}
