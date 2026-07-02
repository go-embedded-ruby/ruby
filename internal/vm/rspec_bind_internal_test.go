// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"testing"

	rspec "github.com/go-ruby-rspec/rspec"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestRSpecFromRuby covers every arm of the Ruby-value -> rspec value map,
// including the ones the Ruby tests do not reach directly (plain Go nil, the
// Nil singleton, Bignum, Regexp, Class).
func TestRSpecFromRuby(t *testing.T) {
	vm := New(nil)
	if v := rspecFromRuby(vm, nil); v != nil {
		t.Errorf("go-nil -> %v", v)
	}
	if v := rspecFromRuby(vm, object.NilV); v != nil {
		t.Errorf("NilV -> %v", v)
	}
	if v := rspecFromRuby(vm, object.Bool(true)); v != true {
		t.Errorf("bool -> %v", v)
	}
	if v := rspecFromRuby(vm, object.Integer(5)); v != int64(5) {
		t.Errorf("int -> %v", v)
	}
	bn := &object.Bignum{I: new(big.Int).Lsh(big.NewInt(1), 70)}
	if v, ok := rspecFromRuby(vm, bn).(*big.Int); !ok || v.BitLen() != 71 {
		t.Errorf("bignum -> %T", rspecFromRuby(vm, bn))
	}
	if v := rspecFromRuby(vm, object.Float(1.5)); v != 1.5 {
		t.Errorf("float -> %v", v)
	}
	if v := rspecFromRuby(vm, object.NewString("s")); v != "s" {
		t.Errorf("string -> %v", v)
	}
	if v := rspecFromRuby(vm, object.Symbol("s")); v != rspec.Symbol("s") {
		t.Errorf("symbol -> %v", v)
	}
	arr := rspecFromRuby(vm, &object.Array{Elems: []object.Value{object.Integer(1)}})
	if a, ok := arr.([]any); !ok || len(a) != 1 {
		t.Errorf("array -> %T", arr)
	}
	h := object.NewHash()
	h.Set(object.NewString("k"), object.Integer(1))
	if _, ok := rspecFromRuby(vm, h).(*rspec.Hash); !ok {
		t.Errorf("hash -> %T", rspecFromRuby(vm, h))
	}
	rng := &object.Range{Lo: object.Integer(1), Hi: object.Integer(3)}
	if r, ok := rspecFromRuby(vm, rng).(*rspec.Range); !ok || r.Exclusive {
		t.Errorf("range -> %T", rspecFromRuby(vm, rng))
	}
	// A Class maps to rspec.Class.
	if v := rspecFromRuby(vm, vm.cString); v != rspec.Class("String") {
		t.Errorf("class -> %v", v)
	}
}

// TestRSpecFromRubyObject covers the arbitrary-object arm (an *rspec.Object with
// class name, ivars and method names).
func TestRSpecFromRubyObject(t *testing.T) {
	vm := New(nil)
	cls := newClass("Widget", vm.cObject)
	cls.define("spin", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return object.NilV })
	obj := &RObject{class: cls, ivars: map[string]object.Value{"@n": object.Integer(1)}}
	v := rspecFromRuby(vm, obj)
	ro, ok := v.(*rspec.Object)
	if !ok {
		t.Fatalf("object -> %T", v)
	}
	if ro.Class != "Widget" {
		t.Errorf("class name -> %q", ro.Class)
	}
	if _, ok := ro.IVars["@n"]; !ok {
		t.Error("ivar @n missing from snapshot")
	}
	found := false
	for _, m := range ro.RespondsTo {
		if m == "spin" {
			found = true
		}
	}
	if !found {
		t.Errorf("method spin missing from RespondsTo %v", ro.RespondsTo)
	}
}

// TestRSpecClassNameArms covers rspecClassName's String / Symbol / default arms
// and its no-arg raise.
func TestRSpecClassNameArms(t *testing.T) {
	if n := rspecClassName([]object.Value{object.NewString("Foo")}); n != "Foo" {
		t.Errorf("string -> %q", n)
	}
	if n := rspecClassName([]object.Value{object.Symbol("Bar")}); n != "Bar" {
		t.Errorf("symbol -> %q", n)
	}
	if n := rspecClassName([]object.Value{object.Integer(3)}); n != "3" {
		t.Errorf("default -> %q", n)
	}
	assertRaises(t, "ArgumentError", func() { rspecClassName(nil) })
}

// TestRSpecSymbolsArms covers rspecSymbols' Symbol / String / default arms.
func TestRSpecSymbolsArms(t *testing.T) {
	got := rspecSymbols([]object.Value{object.Symbol("a"), object.NewString("b"), object.Integer(3)})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "3" {
		t.Errorf("symbols -> %v", got)
	}
}

// TestRSpecNumericArms covers the Bignum arms and the raises of rspecFloatArg /
// rspecIntArg.
func TestRSpecNumericArms(t *testing.T) {
	bn := &object.Bignum{I: big.NewInt(42)}
	if f := rspecFloatArg([]object.Value{bn}); f != 42 {
		t.Errorf("float bignum -> %v", f)
	}
	if n := rspecIntArg([]object.Value{bn}); n != 42 {
		t.Errorf("int bignum -> %v", n)
	}
	assertRaises(t, "ArgumentError", func() { rspecFloatArg(nil) })
	assertRaises(t, "ArgumentError", func() { rspecIntArg(nil) })
	assertRaises(t, "TypeError", func() { rspecFloatArg([]object.Value{object.NewString("x")}) })
	assertRaises(t, "TypeError", func() { rspecIntArg([]object.Value{object.NewString("x")}) })
}

// TestRSpecRaiseSpecOf covers rspecRaiseSpecOf's Class / String / Regexp arms.
func TestRSpecRaiseSpecOf(t *testing.T) {
	vm := New(nil)
	cls := vm.consts["ArgumentError"].(*RClass)
	spec := rspecRaiseSpecOf([]object.Value{cls, object.NewString("msg")})
	if spec.class != "ArgumentError" || spec.message != "msg" {
		t.Errorf("class/string spec -> %+v", spec)
	}
	re := &Regexp{source: "ab", flags: ""}
	spec2 := rspecRaiseSpecOf([]object.Value{re})
	if _, ok := spec2.message.(*rspec.Regexp); !ok {
		t.Errorf("regexp spec -> %T", spec2.message)
	}
}

// TestRSpecMatcherArgNonMatcher covers rspecMatcherWrap's non-matcher raise.
func TestRSpecMatcherArgNonMatcher(t *testing.T) {
	assertRaises(t, "ArgumentError", func() { rspecMatcherArg([]object.Value{object.Integer(1)}) })
	assertRaises(t, "ArgumentError", func() { rspecMatcherWrap(nil) })
}

// TestRSpecDisplayMethods covers the ToS / Inspect / Truthy methods on the
// matcher and expectation wrappers.
func TestRSpecDisplayMethods(t *testing.T) {
	m := &RSpecMatcher{m: rspec.Eq(int64(1))}
	if m.ToS() == "" || m.Inspect() == "" || !m.Truthy() {
		t.Error("matcher display methods")
	}
	e := &RSpecExpectation{}
	if e.ToS() == "" || e.Inspect() == "" || !e.Truthy() {
		t.Error("expectation display methods")
	}
}
