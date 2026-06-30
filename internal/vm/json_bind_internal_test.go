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

// TestJSONBindGenerate covers the rbgo->library generate mapping (objSource /
// emitValue) across every arm by round-tripping each shape through jsonGenerate,
// including the scalar shapes the document tests do not all reach directly (a
// plain Go nil, a Bignum, a Symbol) and the to_s-of-unknown default.
func TestJSONBindGenerate(t *testing.T) {
	cases := []struct {
		name string
		in   object.Value
		want string
	}{
		{"go-nil", nil, "null"},
		{"nil", object.NilV, "null"},
		{"true", object.Bool(true), "true"},
		{"false", object.Bool(false), "false"},
		{"int", object.Integer(7), "7"},
		{"float", object.Float(2.5), "2.5"},
		{"string", object.NewString("hi"), `"hi"`},
		{"symbol", object.Symbol("sym"), `"sym"`},
		{"empty-array", &object.Array{}, "[]"},
		{"array", &object.Array{Elems: []object.Value{object.Integer(1)}}, "[1]"},
	}
	for _, c := range cases {
		if got := jsonGenerate(c.in); got != c.want {
			t.Errorf("%s: jsonGenerate=%q want %q", c.name, got, c.want)
		}
	}
	// A Bignum emits its full decimal form.
	bg := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
	if got := jsonGenerate(object.NormInt(bg)); got != bg.String() {
		t.Errorf("bignum -> %q", got)
	}
	// A Hash emits an ordered object with coerced keys (Symbol key -> bare name).
	h := object.NewHash()
	h.Set(object.Symbol("k"), object.Integer(2))
	if got := jsonGenerate(h); got != `{"k":2}` {
		t.Errorf("hash -> %q", got)
	}
	// An empty Hash emits "{}".
	if got := jsonGenerate(object.NewHash()); got != "{}" {
		t.Errorf("empty hash -> %q", got)
	}
	// The to_s-of-unknown default: a Range serialises by its Ruby to_s string.
	rng := &object.Range{Lo: object.Integer(1), Hi: object.Integer(2)}
	if got := jsonGenerate(rng); got != `"1..2"` {
		t.Errorf("range default -> %q", got)
	}
}

// TestJSONBindGenerateNestedError covers emitValue's error-propagation arms: a
// non-finite float nested inside an Array and inside a Hash re-raises
// JSON::GeneratorError (exercising the err returns of the Array/Object callbacks).
func TestJSONBindGenerateNestedError(t *testing.T) {
	one := object.Float(1.0)
	inf := object.Float(float64(one) / float64(object.Float(0))) // +Inf
	arr := &object.Array{Elems: []object.Value{inf}}
	if re := rubyErr(t, func() { jsonGenerate(arr) }); re.Class != "JSON::GeneratorError" {
		t.Errorf("array+inf -> class=%q", re.Class)
	}
	h := object.NewHash()
	h.Set(object.NewString("k"), inf)
	if re := rubyErr(t, func() { jsonGenerate(h) }); re.Class != "JSON::GeneratorError" {
		t.Errorf("hash+inf -> class=%q", re.Class)
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

// TestJSONBindObjBuilder covers the library->rbgo parse mapping (objBuilder)
// across every Builder method by driving the builder directly, including the
// scalar shapes (a *big.Int via Big, a symbolized Key) and the nested
// array-inside-object structure the document tests do not all reach directly.
func TestJSONBindObjBuilder(t *testing.T) {
	// Scalar arms, each as the sole top-level value.
	var bNull objBuilder
	bNull.Null()
	if bNull.Result() != object.NilV {
		t.Error("Null -> not NilV")
	}
	var bBool objBuilder
	bBool.Bool(true)
	if v, ok := bBool.Result().(object.Bool); !ok || !bool(v) {
		t.Errorf("Bool -> %#v", bBool.Result())
	}
	var bInt objBuilder
	bInt.Int(9)
	if v, ok := bInt.Result().(object.Integer); !ok || int64(v) != 9 {
		t.Errorf("Int -> %#v", bInt.Result())
	}
	bg := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
	var bBig objBuilder
	bBig.Big(bg)
	if v, ok := bBig.Result().(*object.Bignum); !ok || v.I.Cmp(bg) != 0 {
		t.Errorf("Big -> %#v", bBig.Result())
	}
	var bFloat objBuilder
	bFloat.Float(2.5)
	if v, ok := bFloat.Result().(object.Float); !ok || float64(v) != 2.5 {
		t.Errorf("Float -> %#v", bFloat.Result())
	}
	var bStr objBuilder
	bStr.Str("s")
	if v, ok := bStr.Result().(*object.String); !ok || v.Str() != "s" {
		t.Errorf("Str -> %#v", bStr.Result())
	}

	// An object containing an array value, with a symbolized key — exercises
	// BeginObject/Key(symbolize)/BeginArray/elements/EndArray/EndObject and emit
	// into both an open Hash and an open Array.
	var b objBuilder
	b.BeginObject(1)
	b.Key("k", true) // symbolize -> Symbol key
	b.BeginArray(2)
	b.Int(1)
	b.Str("x")
	b.EndArray()
	b.EndObject()
	h, ok := b.Result().(*object.Hash)
	if !ok || len(h.Keys) != 1 {
		t.Fatalf("object -> %#v", b.Result())
	}
	if _, ok := h.Keys[0].(object.Symbol); !ok {
		t.Errorf("symbolized key -> %#v", h.Keys[0])
	}
	v, _ := h.Get(object.Symbol("k"))
	a, ok := v.(*object.Array)
	if !ok || len(a.Elems) != 2 {
		t.Fatalf("array value -> %#v", v)
	}

	// A non-symbolized Key yields a String key.
	var b2 objBuilder
	b2.BeginObject(1)
	b2.Key("s", false)
	b2.Null()
	b2.EndObject()
	h2 := b2.Result().(*object.Hash)
	if _, ok := h2.Keys[0].(*object.String); !ok {
		t.Errorf("string key -> %#v", h2.Keys[0])
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
