package object

import (
	"math"
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
		{String("hi"), "hi", `"hi"`, true},
		{Symbol("hi"), "hi", ":hi", true},
		{&Array{}, "[]", "[]", true},
		{&Array{Elems: []Value{Integer(1), String("x"), Symbol("y")}}, `[1, "x", :y]`, `[1, "x", :y]`, true},
		{NewHash(), "{}", "{}", true},
		{String("a\"b\\c\nd\te"), "a\"b\\c\nd\te", `"a\"b\\c\nd\te"`, true},
		{Bool(true), "true", "true", true},
		{Bool(false), "false", "false", false},
		{Nil{}, "", "nil", false},
		{Main{}, "main", "main", true},
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
	h.Set(String("b"), Integer(2))
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
	if h.Inspect() != `{:a=>9, "b"=>2}` {
		t.Fatalf("inspect = %q", h.Inspect())
	}
}

func TestSingletons(t *testing.T) {
	if !True.Truthy() || False.Truthy() || NilV.Truthy() {
		t.Fatal("singleton truthiness wrong")
	}
}
