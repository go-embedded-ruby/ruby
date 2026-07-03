// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
	ostruct "github.com/go-ruby-ostruct/ostruct"
)

// TestRegisterOstructNoClass covers the guard in registerOstruct for a host that
// stripped the prelude-defined OpenStruct class: there is nothing to back, so it
// must return without panicking (and without recreating the class). The prelude
// always defines OpenStruct, so this branch is only reachable by deleting the
// constant first.
func TestRegisterOstructNoClass(t *testing.T) {
	vm := New(&bytes.Buffer{})
	delete(vm.consts, "OpenStruct")
	vm.registerOstruct() // must be a no-op, not a panic
	if _, ok := vm.consts["OpenStruct"]; ok {
		t.Fatal("registerOstruct re-created OpenStruct when the class was absent")
	}
}

// TestOstructFromHashNonHash covers the defensive !ok branch of ostructFromHash:
// a non-Hash table argument (never produced by the prelude, which always passes
// @table) yields an empty OpenStruct rather than panicking.
func TestOstructFromHashNonHash(t *testing.T) {
	vm := New(&bytes.Buffer{})
	if got := vm.ostructFromHash(object.NilV).Len(); got != 0 {
		t.Fatalf("non-Hash table: Len = %d, want 0", got)
	}
}

// TestSymKey covers symKey's three coercions: a Symbol passes through, a String
// is interned by content, and any other value (the default branch) is interned
// via its #to_s.
func TestSymKey(t *testing.T) {
	if got := symKey(object.Symbol("a")); got != ostruct.Symbol("a") {
		t.Fatalf("Symbol key: got %q", got)
	}
	if got := symKey(object.NewString("b")); got != ostruct.Symbol("b") {
		t.Fatalf("String key: got %q", got)
	}
	if got := symKey(object.Integer(7)); got != ostruct.Symbol("7") {
		t.Fatalf("default (Integer) key: got %q", got)
	}
}

// TestRaiseOstructErr covers raiseOstructErr's mapping of each library error type
// to the matching Ruby exception class, including the default (non-library
// error) branch that maps to RuntimeError. The raise unwinds via panic, so each
// case is recovered.
func TestRaiseOstructErr(t *testing.T) {
	vm := New(&bytes.Buffer{})
	cases := []struct {
		err  error
		want string
	}{
		{&ostruct.NameError{Message: "n"}, "NameError"},
		{&ostruct.TypeError{Message: "t"}, "TypeError"},
		{&ostruct.ArgumentError{Message: "a"}, "ArgumentError"},
		{errors.New("boom"), "RuntimeError"},
	}
	for _, c := range cases {
		got := recoverRaisedClass(t, vm, c.err)
		if got != c.want {
			t.Errorf("err=%T raised %s, want %s", c.err, got, c.want)
		}
	}
}

// recoverRaisedClass runs vm.raiseOstructErr(err) and returns the Ruby class name
// of the exception it raises (raise unwinds by panicking with a RubyError).
func recoverRaisedClass(t *testing.T, vm *VM, err error) (cls string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("raiseOstructErr(%T) did not raise", err)
		}
		re, ok := r.(RubyError)
		if !ok {
			t.Fatalf("raiseOstructErr(%T) panicked with %T, not RubyError", err, r)
		}
		cls = re.Class
	}()
	vm.raiseOstructErr(err)
	return ""
}

// TestUnwrapVMValue covers unwrapVMValue's branches: a bare Go nil, a wrapped
// non-dig value, a wrapped dig value, a plain library Symbol, and the default
// (unexpected payload) branch — all mapping back to a Ruby value.
func TestUnwrapVMValue(t *testing.T) {
	vm := New(&bytes.Buffer{})
	// bare nil -> Ruby nil
	if _, ok := object.AsNilOK(unwrapVMValue(nil)); !ok {
		t.Fatal("nil did not unwrap to Ruby nil")
	}
	// plain vmValue (a non-dig value: an Integer) -> the wrapped value
	plain := vm.wrapVMValue(object.Integer(3))
	if _, ok := plain.(vmValue); !ok {
		t.Fatalf("Integer wrapped as %T, want vmValue", plain)
	}
	if got := unwrapVMValue(plain); got.ToS() != "3" {
		t.Fatalf("vmValue unwrapped to %q", got.ToS())
	}
	// vmDigValue (a dig-responder: an Array) -> the wrapped value
	dig := vm.wrapVMValue(&object.Array{Elems: []object.Value{object.Integer(1)}})
	if _, ok := dig.(vmDigValue); !ok {
		t.Fatalf("Array wrapped as %T, want vmDigValue", dig)
	}
	if got := unwrapVMValue(dig); got.ToS() != "[1]" {
		t.Fatalf("vmDigValue unwrapped to %q", got.ToS())
	}
	// a library Symbol -> a Ruby Symbol
	if got := unwrapVMValue(ostruct.Symbol("k")); got != object.Value(object.Symbol("k")) {
		t.Fatalf("library Symbol unwrapped to %#v", got)
	}
	// default (unexpected payload) -> Ruby nil
	if _, ok := object.AsNilOK(unwrapVMValue(42)); !ok {
		t.Fatal("unexpected payload did not unwrap to Ruby nil")
	}
}

// TestWrapVMValueNil covers wrapVMValue's nil normalisation: both a Go-nil value
// (an absent table entry) and a Ruby nil map to a bare Go nil, so the library's
// nil handling (rendering and the dig short-circuit) applies unchanged.
func TestWrapVMValueNil(t *testing.T) {
	vm := New(&bytes.Buffer{})
	if got := vm.wrapVMValue(nil); got != nil {
		t.Fatalf("Go-nil wrapped to %#v, want nil", got)
	}
	if got := vm.wrapVMValue(object.NilV); got != nil {
		t.Fatalf("Ruby nil wrapped to %#v, want nil", got)
	}
}

// TestVMValueInspectAndClass covers vmValue's Inspector (Inspect) and Classer
// (RubyClassName) adapters directly, the renderers the library calls for each
// stored value.
func TestVMValueInspectAndClass(t *testing.T) {
	vm := New(&bytes.Buffer{})
	w := vmValue{vm: vm, v: object.NewString("x")}
	if got := w.Inspect(); got != `"x"` {
		t.Fatalf("Inspect = %q, want %q", got, `"x"`)
	}
	if got := w.RubyClassName(); got != "String" {
		t.Fatalf("RubyClassName = %q, want String", got)
	}
}
