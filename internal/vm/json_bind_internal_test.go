// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"testing"

	json "github.com/go-ruby-json/json"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestJSONBindToJSON covers the rbgo->library value mapping (toJSON) across every
// arm, including the scalar shapes the document tests do not all reach directly
// (a plain Go nil, a Bignum, a Symbol) and the to_s-of-unknown default.
func TestJSONBindToJSON(t *testing.T) {
	cases := []struct {
		name string
		in   object.Value
		want json.Value
	}{
		{"go-nil", nil, nil},
		{"nil", object.NilV, nil},
		{"true", object.Bool(true), true},
		{"false", object.Bool(false), false},
		{"int", object.Integer(7), int64(7)},
		{"float", object.Float(2.5), float64(2.5)},
		{"string", object.NewString("hi"), "hi"},
		{"symbol", object.Symbol("sym"), json.Symbol("sym")},
	}
	for _, c := range cases {
		if got := toJSON(c.in); got != c.want {
			t.Errorf("%s: toJSON=%#v want %#v", c.name, got, c.want)
		}
	}
	// A Bignum maps to its *big.Int.
	bg := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
	if got, ok := toJSON(object.NormInt(bg)).(*big.Int); !ok || got.Cmp(bg) != 0 {
		t.Errorf("bignum -> %#v", toJSON(object.NormInt(bg)))
	}
	// An Array maps to []json.Value, a Hash to an ordered *Map (keys coerced).
	arr := toJSON(&object.Array{Elems: []object.Value{object.Integer(1)}})
	if s, ok := arr.([]json.Value); !ok || len(s) != 1 || s[0] != int64(1) {
		t.Errorf("array -> %#v", arr)
	}
	h := object.NewHash()
	h.Set(object.Symbol("k"), object.Integer(2))
	m, ok := toJSON(h).(*json.Map)
	if !ok || m.Len() != 1 {
		t.Fatalf("hash -> %#v", toJSON(h))
	}
	if v, ok := m.Get("k"); !ok || v != int64(2) {
		t.Errorf("hash key -> %#v present=%v", v, ok)
	}
	// The to_s-of-unknown default: a Range serialises by its Ruby to_s string.
	rng := &object.Range{Lo: object.Integer(1), Hi: object.Integer(2)}
	if got := toJSON(rng); got != "1..2" {
		t.Errorf("range default -> %#v", got)
	}
}

// TestJSONBindKeyString covers jsonKeyString's three arms (String / Symbol /
// to_s-of-anything-else).
func TestJSONBindKeyString(t *testing.T) {
	if got := jsonKeyString(object.NewString("s")); got != "s" {
		t.Errorf("string key -> %q", got)
	}
	if got := jsonKeyString(object.Symbol("y")); got != "y" {
		t.Errorf("symbol key -> %q", got)
	}
	if got := jsonKeyString(object.Integer(1)); got != "1" {
		t.Errorf("int key -> %q", got)
	}
}

// TestJSONBindFromJSON covers the library->rbgo value mapping (fromJSON) across
// every arm, including the cases the document tests do not all reach directly (a
// *big.Int, a Symbol) and the defensive default.
func TestJSONBindFromJSON(t *testing.T) {
	if fromJSON(nil) != object.NilV {
		t.Error("nil -> not NilV")
	}
	if v, ok := fromJSON(true).(object.Bool); !ok || !bool(v) {
		t.Errorf("bool -> %#v", fromJSON(true))
	}
	if v, ok := fromJSON(int64(9)).(object.Integer); !ok || int64(v) != 9 {
		t.Errorf("int64 -> %#v", fromJSON(int64(9)))
	}
	bg := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
	if v, ok := fromJSON(bg).(*object.Bignum); !ok || v.I.Cmp(bg) != 0 {
		t.Errorf("*big.Int -> %#v", fromJSON(bg))
	}
	if v, ok := fromJSON(float64(2.5)).(object.Float); !ok || float64(v) != 2.5 {
		t.Errorf("float64 -> %#v", fromJSON(float64(2.5)))
	}
	if v, ok := fromJSON("s").(*object.String); !ok || v.Str() != "s" {
		t.Errorf("string -> %#v", fromJSON("s"))
	}
	if v, ok := fromJSON(json.Symbol("y")).(object.Symbol); !ok || string(v) != "y" {
		t.Errorf("Symbol -> %#v", fromJSON(json.Symbol("y")))
	}
	arr := fromJSON([]json.Value{int64(1), "x"})
	a, ok := arr.(*object.Array)
	if !ok || len(a.Elems) != 2 {
		t.Fatalf("[]Value -> %#v", arr)
	}
	m := json.NewMap()
	m.Set("k", int64(3))
	hv := fromJSON(m)
	if hh, ok := hv.(*object.Hash); !ok || len(hh.Keys) != 1 {
		t.Errorf("*Map -> %#v", hv)
	}
	// A value the parser never produces (a bare int) maps to nil (defensive arm).
	if fromJSON(123) != object.NilV {
		t.Errorf("unmapped -> %#v", fromJSON(123))
	}
}

// TestJSONBindRaiseError covers raiseJSONError, asserting each typed library
// error re-raises as the Ruby exception named by its RubyClass().
func TestJSONBindRaiseError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&json.ParserError{Message: "p"}, "JSON::ParserError"},
		{&json.NestingError{Message: "n"}, "JSON::NestingError"},
		{&json.GeneratorError{Message: "g"}, "JSON::GeneratorError"},
		{&json.TypeError{Message: "t"}, "TypeError"},
	}
	for _, c := range cases {
		re := rubyErr(t, func() { raiseJSONError(c.err) })
		if re.Class != c.want {
			t.Errorf("%v -> class=%q message=%q", c.err, re.Class, re.Message)
		}
	}
}

// TestJSONBindPrettyError covers jsonPrettyGenerate's error arm: a non-finite
// float re-raises as JSON::GeneratorError.
func TestJSONBindPrettyError(t *testing.T) {
	inf := object.Float(1.0)
	inf = object.Float(float64(inf) / 0) // +Inf
	re := rubyErr(t, func() { jsonPrettyGenerate(inf) })
	if re.Class != "JSON::GeneratorError" {
		t.Errorf("class=%q message=%q", re.Class, re.Message)
	}
}

// TestJSONOptsHash covers jsonOptsHash's three arms: no trailing arg, a trailing
// non-Hash, and a trailing Hash.
func TestJSONOptsHash(t *testing.T) {
	if jsonOptsHash(nil) != nil {
		t.Error("no args -> non-nil")
	}
	if jsonOptsHash([]object.Value{object.Integer(1)}) != nil {
		t.Error("non-hash trailing -> non-nil")
	}
	h := object.NewHash()
	if jsonOptsHash([]object.Value{h}) != h {
		t.Error("hash trailing -> not the hash")
	}
}

// TestJSONParseOpts covers jsonParseOpts: no options, symbolize_names, and the
// shared max_nesting / allow_nan keywords.
func TestJSONParseOpts(t *testing.T) {
	if jsonParseOpts(nil) != nil {
		t.Error("no options -> non-nil")
	}
	h := object.NewHash()
	h.Set(object.Symbol("symbolize_names"), object.Bool(true))
	h.Set(object.Symbol("max_nesting"), object.Integer(2))
	h.Set(object.Symbol("allow_nan"), object.Bool(true))
	if got := jsonParseOpts([]object.Value{h}); len(got) != 3 {
		t.Errorf("populated parse opts -> %d", len(got))
	}
}

// TestJSONGenerateOpts covers jsonGenerateOpts including every string keyword,
// the max_nesting integer and false arms, and allow_nan; and the no-options path.
func TestJSONGenerateOpts(t *testing.T) {
	if jsonGenerateOpts(nil) != nil {
		t.Error("no options -> non-nil")
	}
	h := object.NewHash()
	for _, k := range []string{"indent", "space", "space_before", "object_nl", "array_nl"} {
		h.Set(object.Symbol(k), object.NewString(" "))
	}
	// A non-String value for a string keyword is ignored (the isStr guard).
	h.Set(object.Symbol("indent"), object.NewString("  "))
	h.Set(object.Symbol("max_nesting"), object.Bool(false)) // disables the limit
	h.Set(object.Symbol("allow_nan"), object.Bool(true))
	if got := jsonGenerateOpts([]object.Value{h}); len(got) != 7 {
		t.Errorf("populated generate opts -> %d", len(got))
	}
	// max_nesting as an Integer, and a non-String string-keyword value (ignored).
	h2 := object.NewHash()
	h2.Set(object.Symbol("indent"), object.Integer(2)) // ignored: not a String
	h2.Set(object.Symbol("max_nesting"), object.Integer(5))
	if got := jsonGenerateOpts([]object.Value{h2}); len(got) != 1 {
		t.Errorf("int max_nesting opts -> %d", len(got))
	}
}
