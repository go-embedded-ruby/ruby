package object

import (
	"math"
	"math/big"
	"testing"
)

func TestToSAndInspect(t *testing.T) {
	tests := []struct {
		v            Value
		toS, inspect string
		truthy       bool
	}{
		{Integer(42), "42", "42", true},
		{Integer(-7), "-7", "-7", true},
		{Float(1.0), "1.0", "1.0", true},
		{Float(3.5), "3.5", "3.5", true},
		{Float(math.Inf(1)), "Infinity", "Infinity", true},
		{Float(math.Inf(-1)), "-Infinity", "-Infinity", true},
		{Float(math.NaN()), "NaN", "NaN", true},
		{NewString("hi"), "hi", `"hi"`, true},
		{Symbol("hi"), "hi", ":hi", true},
		{&Array{}, "[]", "[]", true},
		{&Array{Elems: []Value{Integer(1), NewString("x"), Symbol("y")}}, `[1, "x", :y]`, `[1, "x", :y]`, true},
		{NewHash(), "{}", "{}", true},
		{&Range{Lo: Integer(1), Hi: Integer(5)}, "1..5", "1..5", true},
		{&Range{Lo: Integer(1), Hi: Integer(5), Exclusive: true}, "1...5", "1...5", true},
		{&Range{Lo: NewString("a"), Hi: NewString("c")}, "a..c", `"a".."c"`, true},
		{NewString("a\"b\\c\nd\te"), "a\"b\\c\nd\te", `"a\"b\\c\nd\te"`, true},
		{Bool(true), "true", "true", true},
		{Bool(false), "false", "false", false},
		{Nil{}, "", "nil", false},
		{NewMain(), "main", "main", true},
	}
	for _, tc := range tests {
		if got := tc.v.ToS(); got != tc.toS {
			t.Errorf("%#v ToS = %q, want %q", tc.v, got, tc.toS)
		}
		if got := tc.v.Inspect(); got != tc.inspect {
			t.Errorf("%#v Inspect = %q, want %q", tc.v, got, tc.inspect)
		}
		if got := tc.v.Truthy(); got != tc.truthy {
			t.Errorf("%#v Truthy = %v, want %v", tc.v, got, tc.truthy)
		}
	}
}

func TestHashOps(t *testing.T) {
	h := NewHash()
	if h.Len() != 0 || h.repr() != "{}" {
		t.Fatal("empty hash")
	}
	h.Set(Symbol("a"), Integer(1))
	h.Set(NewString("b"), Integer(2))
	h.Set(Symbol("a"), Integer(9)) // update keeps order, no new key
	if h.Len() != 2 {
		t.Fatalf("len = %d want 2", h.Len())
	}
	if v, ok := h.Get(Symbol("a")); !ok || v != Integer(9) {
		t.Fatalf("get a = %v,%v", v, ok)
	}
	if _, ok := h.Get(Symbol("z")); ok {
		t.Fatal("missing key should be absent")
	}
	if h.Inspect() != `{a: 9, "b" => 2}` {
		t.Fatalf("inspect = %q", h.Inspect())
	}
}

// TestHashContentKeysAndClear covers content-addressed Array/Hash/Bignum keys,
// Clear, Delete, and (with no CustomKeyHook installed) the identity fallback for a
// plain reference key.
func TestHashContentKeysAndClear(t *testing.T) {
	h := NewHash()
	// Array key by content.
	h.Set(&Array{Elems: []Value{Integer(1), Integer(2)}}, NewString("a"))
	if v, ok := h.Get(&Array{Elems: []Value{Integer(1), Integer(2)}}); !ok || v.(*String).Str() != "a" {
		t.Fatalf("array key get = %v,%v", v, ok)
	}
	// Nested Hash key by content (exercises valKey on the value side).
	inner := NewHash()
	inner.Set(Symbol("x"), &Array{Elems: []Value{Integer(3)}})
	hk := NewHash()
	hk.Set(Symbol("x"), &Array{Elems: []Value{Integer(3)}})
	h.Set(inner, NewString("b"))
	if v, ok := h.Get(hk); !ok || v.(*String).Str() != "b" {
		t.Fatalf("hash key get = %v,%v", v, ok)
	}
	// Bignum key by content.
	big1, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	big2, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	h.Set(&Bignum{I: big1}, Integer(7))
	if v, ok := h.Get(&Bignum{I: big2}); !ok || v != Integer(7) {
		t.Fatalf("bignum key get = %v,%v", v, ok)
	}
	// A plain reference key (no hook) is identity-keyed: a distinct Range misses.
	r := &Range{Lo: Integer(1), Hi: Integer(2)}
	h.Set(r, NewString("rng"))
	if v, ok := h.Get(r); !ok || v.(*String).Str() != "rng" {
		t.Fatalf("identity key get = %v,%v", v, ok)
	}
	if _, ok := h.Get(&Range{Lo: Integer(1), Hi: Integer(2)}); ok {
		t.Fatal("distinct reference key should miss without a hook")
	}
	// Delete an Array key by content.
	if _, ok := h.Delete(&Array{Elems: []Value{Integer(1), Integer(2)}}); !ok {
		t.Fatal("delete array key")
	}
	if _, ok := h.Get(&Array{Elems: []Value{Integer(1), Integer(2)}}); ok {
		t.Fatal("array key still present after delete")
	}
	// Clear empties everything.
	h.Clear()
	if h.Len() != 0 {
		t.Fatalf("len after clear = %d", h.Len())
	}
	if _, ok := h.Get(hk); ok {
		t.Fatal("hash key present after clear")
	}
}

func TestSingletons(t *testing.T) {
	if !True.Truthy() || False.Truthy() || NilV.Truthy() {
		t.Fatal("singleton truthiness wrong")
	}
}
