// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"testing"

	hcl2 "github.com/go-ruby-hcl2/hcl2"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// bigOver64 returns a big.Int larger than int64 can hold, for the Bignum arms.
func bigOver64() *big.Int {
	return new(big.Int).Lsh(big.NewInt(1), 100) // 2**100
}

// TestHCL2SourceArgNonString covers hcl2SourceArg's to_s branch.
func TestHCL2SourceArgNonString(t *testing.T) {
	if got := hcl2SourceArg(object.Integer(5)); got != "5" {
		t.Errorf("non-string source -> %q", got)
	}
}

// TestHCL2KeyBridge covers hcl2Key across its Symbol, String and to_s-default
// arms.
func TestHCL2KeyBridge(t *testing.T) {
	if got := hcl2Key(object.Symbol("s")); got != "s" {
		t.Errorf("symbol key -> %q", got)
	}
	if got := hcl2Key(object.NewString("str")); got != "str" {
		t.Errorf("string key -> %q", got)
	}
	if got := hcl2Key(object.Integer(3)); got != "3" {
		t.Errorf("integer key -> %q", got)
	}
}

// TestHCL2ToBridge covers every arm of toHCL2: go-nil, object.Nil, bool, integer,
// a fitting Bignum (-> int64), an over-64-bit Bignum (-> float64), float, string,
// symbol, array, hash, and the unmapped default (-> nil).
func TestHCL2ToBridge(t *testing.T) {
	if toHCL2(nil) != nil {
		t.Error("go-nil should map to nil")
	}
	if toHCL2(object.NilV) != nil {
		t.Error("object.NilV should map to nil")
	}
	if v, ok := toHCL2(object.Bool(true)).(bool); !ok || !v {
		t.Errorf("bool -> %#v", toHCL2(object.Bool(true)))
	}
	if v, ok := toHCL2(object.Integer(7)).(int64); !ok || v != 7 {
		t.Errorf("int -> %#v", toHCL2(object.Integer(7)))
	}
	// A *Bignum whose value fits in int64 maps to int64 (the IsInt64 true arm).
	fit := &object.Bignum{I: big.NewInt(123)}
	if v, ok := toHCL2(fit).(int64); !ok || v != 123 {
		t.Errorf("fitting bignum -> %#v", toHCL2(fit))
	}
	// An over-64-bit Bignum maps to float64.
	big100 := &object.Bignum{I: bigOver64()}
	if _, ok := toHCL2(big100).(float64); !ok {
		t.Errorf("over-64 bignum -> %#v", toHCL2(big100))
	}
	if v, ok := toHCL2(object.Float(1.5)).(float64); !ok || v != 1.5 {
		t.Errorf("float -> %#v", toHCL2(object.Float(1.5)))
	}
	if v, ok := toHCL2(object.NewString("x")).(string); !ok || v != "x" {
		t.Errorf("string -> %#v", toHCL2(object.NewString("x")))
	}
	if v, ok := toHCL2(object.Symbol("s")).(string); !ok || v != "s" {
		t.Errorf("symbol -> %#v", toHCL2(object.Symbol("s")))
	}
	arr := &object.Array{Elems: []object.Value{object.Integer(1)}}
	if v, ok := toHCL2(arr).([]hcl2.Value); !ok || len(v) != 1 {
		t.Errorf("array -> %#v", toHCL2(arr))
	}
	h := object.NewHash()
	h.Set(object.NewString("k"), object.Integer(2))
	if m, ok := toHCL2(h).(*hcl2.Map); !ok || m.Len() != 1 {
		t.Errorf("hash -> %#v", toHCL2(h))
	}
	// An unmapped value maps to nil.
	if toHCL2(&Proc{}) != nil {
		t.Errorf("unmapped -> %#v", toHCL2(&Proc{}))
	}
}

// TestHCL2FromBridge covers the Go-only arms of fromHCL2 the Ruby tests do not
// reach directly: a *big.Int (-> Bignum/Integer), a nil value, and the unmapped
// default (both -> nil).
func TestHCL2FromBridge(t *testing.T) {
	vm := New(nil)
	if v := fromHCL2(vm, bigOver64()); v == nil {
		t.Error("big.Int -> nil")
	}
	if v := fromHCL2(vm, nil); v != object.NilV {
		t.Errorf("nil -> %v", v)
	}
	if v := fromHCL2(vm, struct{}{}); v != object.NilV {
		t.Errorf("unmapped -> %v", v)
	}
}

// TestHCL2ContextVariablesWrapperNonHash covers hcl2Context when the :variables
// key is present but not a Hash: the whole Hash is then read as variables.
func TestHCL2ContextVariablesWrapperNonHash(t *testing.T) {
	h := object.NewHash()
	h.Set(object.Symbol("variables"), object.Integer(1))
	c := hcl2Context(h)
	if c == nil {
		t.Fatal("nil context")
	}
	// The whole Hash is variables, so "variables" itself is a bound variable.
	if _, ok := c.Variables["variables"]; !ok {
		t.Errorf("expected variables key bound, got %#v", c.Variables)
	}
}
