// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io"
	"math/big"
	"testing"

	liquid "github.com/go-ruby-liquid/liquid"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestLiquidHandleValue covers the object.Value methods of the opaque template
// handle: they exist only so the handle can live in an ivar and are never reached
// from Ruby.
func TestLiquidHandleValue(t *testing.T) {
	h := &liquidTemplate{}
	if h.ToS() != "#<Liquid::Template>" {
		t.Errorf("ToS = %q", h.ToS())
	}
	if h.Inspect() != h.ToS() {
		t.Errorf("Inspect = %q", h.Inspect())
	}
	if !h.Truthy() {
		t.Error("handle should be truthy")
	}
}

// TestLiquidHandleUnparsed covers liquidHandle's raise arm: a Liquid::Template
// instance with no stored handle (never parsed) raises Liquid::Error. A Ruby
// program cannot build such an instance, so the arm is exercised here directly.
func TestLiquidHandleUnparsed(t *testing.T) {
	vm := New(io.Discard)
	cls := object.Kind[*RClass](vm.consts["Liquid::Template"])
	inst := &RObject{class: cls, ivars: map[string]object.Value{}}
	defer func() {
		re, ok := recover().(RubyError)
		if !ok || re.Class != "Liquid::Error" {
			t.Fatalf("want Liquid::Error, got %v", recover())
		}
	}()
	liquidHandle(object.Wrap(inst))
}

// TestLiquidErrorClass covers every arm of liquidErrorClass, including the *Error
// type switch and the non-*Error fallback.
func TestLiquidErrorClass(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&liquid.Error{Type: "SyntaxError"}, "Liquid::SyntaxError"},
		{&liquid.Error{Type: "ArgumentError"}, "Liquid::ArgumentError"},
		{&liquid.Error{Type: "ZeroDivisionError"}, "Liquid::ZeroDivisionError"},
		{&liquid.Error{Type: "StackLevelError"}, "Liquid::StackLevelError"},
		{&liquid.Error{Type: "SomethingElse"}, "Liquid::Error"},
		{errors.New("plain"), "Liquid::Error"},
	}
	for _, c := range cases {
		if got := liquidErrorClass(c.err); got != c.want {
			t.Errorf("liquidErrorClass(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

// TestLiquidModeAndKey covers the to_s fall-through arms of liquidModeName and
// liquidKey, which a Symbol/String argument never reaches.
func TestLiquidModeAndKey(t *testing.T) {
	if got := liquidModeName(object.IntValue(int64(object.Integer(1)))); got != "1" {
		t.Errorf("liquidModeName(int) = %q", got)
	}
	if got := liquidKey(object.IntValue(int64(object.Integer(2)))); got != "2" {
		t.Errorf("liquidKey(int) = %q", got)
	}
}

// TestToLiquidGoArms covers the toLiquid arms a typical Ruby assigns Hash does not
// reach: a plain Go nil, the object.Nil singleton, a Bignum, and the #to_s
// fall-through for an object with no direct model shape.
func TestToLiquidGoArms(t *testing.T) {
	vm := New(io.Discard)
	if toLiquid(vm, nil) != nil {
		t.Error("go-nil should map to nil")
	}
	if toLiquid(vm, object.NilVal()) != nil {
		t.Error("object.NilV should map to nil")
	}
	bn := object.NormInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil))
	if _, ok := toLiquid(vm, bn).(*big.Int); !ok {
		t.Errorf("bignum -> %T", toLiquid(vm, bn))
	}
	// A String maps to its contents.
	if got := toLiquid(vm, object.Wrap(object.NewString("s"))); got != "s" {
		t.Errorf("string -> %v", got)
	}
	// An object with no direct model shape is handed its #to_s text: an RObject of
	// a bare class stringifies to its "#<Class …>" inspect-style to_s.
	obj := &RObject{class: vm.cObject, ivars: map[string]object.Value{}}
	if got, ok := toLiquid(vm, object.Wrap(obj)).(string); !ok {
		t.Errorf("object -> %T (want string via to_s)", got)
	}
	// A Symbol value maps to its bare name string.
	if got := toLiquid(vm, object.SymVal(string(object.Symbol("sym")))); got != "sym" {
		t.Errorf("symbol -> %v", got)
	}
	// A truthy/false Bool and Integer/Float map straight through.
	if got := toLiquid(vm, object.BoolValue(bool(object.Bool(true)))); got != true {
		t.Errorf("bool -> %v", got)
	}
	if got := toLiquid(vm, object.IntValue(int64(object.Integer(7)))); got != int64(7) {
		t.Errorf("int -> %v", got)
	}
	if got := toLiquid(vm, object.FloatValue(float64(object.Float(1.5)))); got != 1.5 {
		t.Errorf("float -> %v", got)
	}
	// An Array recurses into []any.
	if got, ok := toLiquid(vm, object.Wrap(&object.Array{Elems: []object.Value{object.IntValue(int64(object.Integer(1)))}})).([]any); !ok || len(got) != 1 {
		t.Errorf("array -> %T", toLiquid(vm, object.Wrap(&object.Array{})))
	}
	// A Hash recurses into map[string]any.
	h := object.NewHash()
	h.Set(object.Wrap(object.NewString("k")), object.IntValue(int64(object.Integer(9))))
	if got, ok := toLiquid(vm, object.Wrap(h)).(map[string]any); !ok || got["k"] != int64(9) {
		t.Errorf("hash -> %T", toLiquid(vm, object.Wrap(h)))
	}
}

// TestLiquidRenderLaxErrorSeam covers liquidRender's lax error arm, which the real
// Template.Render never triggers (it embeds "Liquid error: …" inline). Swapping the
// liquidRenderFn seam makes the defensive raise reachable.
func TestLiquidRenderLaxErrorSeam(t *testing.T) {
	orig := liquidRenderFn
	defer func() { liquidRenderFn = orig }()
	liquidRenderFn = func(_ *liquid.Template, _ map[string]any) (string, error) {
		return "", errors.New("injected lax failure")
	}
	defer func() {
		re, ok := recover().(RubyError)
		if !ok || re.Class != "Liquid::Error" {
			t.Fatalf("want Liquid::Error, got %v", recover())
		}
	}()
	_ = liquidRender(New(io.Discard), &liquidTemplate{t: &liquid.Template{}}, map[string]any{}, false)
}
