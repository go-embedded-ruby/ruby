// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"testing"

	racc "github.com/go-ruby-racc/racc"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestRaccInt covers raccInt's non-Integer arm (a nil table cell decodes to 0).
func TestRaccInt(t *testing.T) {
	if got := raccInt(object.Integer(5)); got != 5 {
		t.Errorf("int arm got=%d", got)
	}
	if got := raccInt(object.NilV); got != 0 {
		t.Errorf("nil arm got=%d", got)
	}
}

// TestRaccIntSlice covers the nil-cell element arm and the non-Array arm.
func TestRaccIntSlice(t *testing.T) {
	got := raccIntSlice(object.NewArrayFromSlice([]object.Value{object.Integer(3), object.NilV}))
	if len(got) != 2 || got[0] != 3 || got[1] != raccNilCell {
		t.Errorf("slice got=%v", got)
	}
	if raccIntSlice(object.NewString("x")) != nil {
		t.Error("non-array should decode to nil")
	}
}

// TestRaccTokenKey covers every arm of the token-key mapper.
func TestRaccTokenKey(t *testing.T) {
	if got := raccTokenKey(object.Symbol("NUMBER")); got != racc.Symbol("NUMBER") {
		t.Errorf("symbol arm got=%v", got)
	}
	if got := raccTokenKey(object.NewString("+")); got != "+" {
		t.Errorf("string arm got=%v", got)
	}
	if got := raccTokenKey(object.Bool(false)); got != nil {
		t.Errorf("bool arm got=%v", got)
	}
	if got := raccTokenKey(object.NilV); got != nil {
		t.Errorf("nil arm got=%v", got)
	}
	if got := raccTokenKey(object.Integer(1)); got != nil {
		t.Errorf("default arm got=%v", got)
	}
}

// TestRaccSymName covers the Symbol, String and default arms.
func TestRaccSymName(t *testing.T) {
	if got := raccSymName(object.Symbol("_reduce_1")); got != "_reduce_1" {
		t.Errorf("symbol arm got=%q", got)
	}
	if got := raccSymName(object.NewString("_reduce_1")); got != "_reduce_1" {
		t.Errorf("string arm got=%q", got)
	}
	if got := raccSymName(object.Integer(1)); got != "" {
		t.Errorf("default arm got=%q", got)
	}
}

// TestRaccToVal covers the Ruby-value passthrough and the Go-nil arm.
func TestRaccToVal(t *testing.T) {
	s := object.NewString("x")
	if raccToVal(s) != s {
		t.Error("passthrough arm")
	}
	if raccToVal(nil) != object.NilV {
		t.Error("nil arm")
	}
	if raccToVal(42) != object.NilV {
		t.Error("non-value arm")
	}
}

// TestRaccDecodeToken covers the EOF arms (non-Array and short Array) and the
// well-formed [sym, val] arm.
func TestRaccDecodeToken(t *testing.T) {
	k, v := raccDecodeToken(object.NewArrayFromSlice([]object.Value{object.Symbol("A"), object.Integer(1)}))
	if k != racc.Symbol("A") || v != object.Integer(1) {
		t.Errorf("pair arm k=%v v=%v", k, v)
	}
	if k, _ := raccDecodeToken(object.NilV); k != nil {
		t.Error("non-array should be EOF")
	}
	if k, _ := raccDecodeToken(object.NewArrayFromSlice([]object.Value{object.Symbol("A")})); k != nil {
		t.Error("short array should be EOF")
	}
}

// TestRaccDecodeYield covers the single-Array-yield arm, the positional arm and
// the EOF arm.
func TestRaccDecodeYield(t *testing.T) {
	k, v := raccDecodeYield([]object.Value{object.NewArrayFromSlice([]object.Value{object.Symbol("A"), object.Integer(1)})})
	if k != racc.Symbol("A") || v != object.Integer(1) {
		t.Errorf("single-array arm k=%v v=%v", k, v)
	}
	k, v = raccDecodeYield([]object.Value{object.Symbol("B"), object.Integer(2)})
	if k != racc.Symbol("B") || v != object.Integer(2) {
		t.Errorf("positional arm k=%v v=%v", k, v)
	}
	if k, _ := raccDecodeYield(nil); k != nil {
		t.Error("empty should be EOF")
	}
	// A single non-Array argument is not a pair, so it is EOF.
	if k, _ := raccDecodeYield([]object.Value{object.Integer(9)}); k != nil {
		t.Error("single scalar should be EOF")
	}
}

// TestRaccBuildTablesMalformed covers raccBuildTables' rejection arms: a
// non-Array, a short Array, a non-Array reduce table, and a non-Hash token table.
func TestRaccBuildTablesMalformed(t *testing.T) {
	if _, _, ok := raccBuildTables(object.NewString("x")); ok {
		t.Error("non-array should be rejected")
	}
	if _, _, ok := raccBuildTables(object.NewArrayFromSlice([]object.Value{object.Integer(1)})); ok {
		t.Error("short array should be rejected")
	}
	// 14 elements but element 9 (reduce table) is not an Array.
	e := make([]object.Value, 14)
	for i := range e {
		e[i] = object.NilV
	}
	e[9] = object.NewString("notarray")
	if _, _, ok := raccBuildTables(object.NewArrayFromSlice(append([]object.Value{}, e...))); ok {
		t.Error("non-array reduce table should be rejected")
	}
	// reduce table an Array but element 10 (token table) not a Hash.
	e[9] = object.NewArrayFromSlice([]object.Value{object.Integer(1), object.Integer(2), object.Symbol("_reduce_1")})
	e[10] = object.NewString("nothash")
	if _, _, ok := raccBuildTables(object.NewArrayFromSlice(append([]object.Value{}, e...))); ok {
		t.Error("non-hash token table should be rejected")
	}
}

// TestRaccTokenTableNonHash covers raccTokenTable's non-Hash rejection arm.
func TestRaccTokenTableNonHash(t *testing.T) {
	if _, ok := raccTokenTable(object.NewString("x")); ok {
		t.Error("non-hash should be rejected")
	}
}

// TestRaccFinishError covers raccFinish's error arm — an engine-level failure
// that surfaces without a seam raising becomes a Racc::ParseError. It is the
// belt-and-braces path (the default on_error raises before the engine returns).
func TestRaccFinishError(t *testing.T) {
	defer func() {
		r := recover()
		re, ok := r.(RubyError)
		if !ok || re.Class != "Racc::ParseError" || re.Message != "boom" {
			t.Errorf("raccFinish error arm recovered %v", r)
		}
	}()
	raccFinish(nil, errors.New("boom"))
	t.Fatal("raccFinish should have raised")
}
