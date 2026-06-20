package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// These cover the helpers the AOT-compiled method bodies call (aot_runtime.go),
// including the error paths the end-to-end differential suite cannot reach
// (a missing constant, a yield with no block, a splat of a non-array).

func TestAOTConst(t *testing.T) {
	vm := New(io.Discard)
	vm.consts["Answer"] = object.Integer(42)
	if got := vm.aotConst("Answer"); got != object.Integer(42) {
		t.Errorf("aotConst hit = %v, want 42", got)
	}
	wantRaise(t, "NameError", func() { vm.aotConst("Nope") })
}

func TestAOTYield(t *testing.T) {
	vm := New(io.Discard)
	var seen []object.Value
	block := &Proc{native: func(_ *VM, args []object.Value) object.Value {
		seen = args
		return object.Integer(7)
	}}
	if got := vm.aotYield(block, []object.Value{object.Integer(1)}); got != object.Integer(7) {
		t.Errorf("aotYield result = %v, want 7", got)
	}
	if len(seen) != 1 || seen[0] != object.Integer(1) {
		t.Errorf("block received %v, want [1]", seen)
	}
	wantRaise(t, "LocalJumpError", func() { vm.aotYield(nil, nil) })
}

func TestAOTConcat(t *testing.T) {
	a := &object.Array{Elems: []object.Value{object.Integer(1), object.Integer(2)}}
	b := &object.Array{Elems: []object.Value{object.Integer(3)}}
	got := aotConcat(a, b).(*object.Array)
	if got.Inspect() != "[1, 2, 3]" {
		t.Errorf("aotConcat = %s, want [1, 2, 3]", got.Inspect())
	}
}

func TestAOTSplat(t *testing.T) {
	arr := &object.Array{Elems: []object.Value{object.Integer(1)}}
	if got := aotSplat(arr); got != arr {
		t.Errorf("aotSplat(array) should pass the array through, got %v", got)
	}
	wrapped := aotSplat(object.Integer(9)).(*object.Array)
	if wrapped.Inspect() != "[9]" {
		t.Errorf("aotSplat(scalar) = %s, want [9]", wrapped.Inspect())
	}
}
