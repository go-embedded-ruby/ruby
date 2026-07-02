// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"math/big"
	"strings"
	"testing"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	drytypes "github.com/go-ruby-dry-types/dry-types"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestDryTypesConstants covers the module wiring: the Dry / Dry::Types modules,
// require idempotence, the namespace constants and the error tree.
func TestDryTypesConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "dry/types"; p Dry::Types.is_a?(Module)`, "true\n"},
		{`p require "dry/types"`, "true\n"},
		{`require "dry/types"; p require "dry/types"`, "false\n"},
		{`require "dry-types"; p defined?(Dry::Types) ? true : false`, "true\n"},
		// Namespace constants resolve to a Type.
		{`require "dry/types"; p Dry::Types::Strict::Integer.call(3)`, "3\n"},
		{`require "dry/types"; p Dry::Types::Coercible::Integer.call("7")`, "7\n"},
		{`require "dry/types"; p Dry::Types::Params::Integer.call("9")`, "9\n"},
		// Namespace method spelling.
		{`require "dry/types"; p Dry::Types::Strict.Integer.call(4)`, "4\n"},
		// Error tree.
		{`require "dry/types"; p Dry::Types::CoercionError < StandardError`, "true\n"},
		{`require "dry/types"; p Dry::Types::ConstraintError < Dry::Types::CoercionError`, "true\n"},
		{`require "dry/types"; p Dry::Types::SchemaError < Dry::Types::CoercionError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDryTypesLookup covers Dry::Types[...] name resolution: dotted names, a bare
// primitive resolving to strict, and the Dry.Types() module method.
func TestDryTypesLookup(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "dry/types"; p Dry::Types["strict.integer"].call(1)`, "1\n"},
		{`require "dry/types"; p Dry::Types["coercible.string"].call(5)`, "\"5\"\n"},
		{`require "dry/types"; p Dry::Types[:integer].call(2)`, "2\n"},
		{`require "dry/types"; p Dry.Types().equal?(Dry::Types)`, "true\n"},
		{`require "dry/types"; p Dry::Types["params.bool"].call("true")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDryTypesApply covers call / [] / valid? / try and the error mapping.
func TestDryTypesApply(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "dry/types"; p Dry::Types["strict.integer"][8]`, "8\n"},
		{`require "dry/types"; p Dry::Types["strict.integer"].valid?(3)`, "true\n"},
		{`require "dry/types"; p Dry::Types["strict.integer"].valid?("x")`, "false\n"},
		{`require "dry/types"; r = Dry::Types["strict.integer"].try("x"); p r.success?`, "false\n"},
		{`require "dry/types"; r = Dry::Types["strict.integer"].try(3); p r.success?`, "true\n"},
		{`require "dry/types"; r = Dry::Types["strict.integer"].try(3); p r.failure?`, "false\n"},
		{`require "dry/types"; r = Dry::Types["strict.integer"].try(3); p r.input`, "3\n"},
		{`require "dry/types"; r = Dry::Types["strict.integer"].try("x"); p r.error.nil?`, "false\n"},
		{`require "dry/types"; r = Dry::Types["strict.integer"].try(3); p r.error`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDryTypesCombinators covers optional / default / constrained / enum / | /
// constructor / meta.
func TestDryTypesCombinators(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "dry/types"; p Dry::Types["strict.integer"].optional.call(nil)`, "nil\n"},
		{`require "dry/types"; p Dry::Types["strict.integer"].default(0).class`, "Dry::Types::Type\n"},
		{`require "dry/types"; p Dry::Types["strict.integer"].default { 99 }.class`, "Dry::Types::Type\n"},
		{`require "dry/types"; p Dry::Types["strict.integer"].constrained(gt: 5).call(6)`, "6\n"},
		{`require "dry/types"; p Dry::Types["strict.string"].enum("a", "b").call("a")`, "\"a\"\n"},
		{`require "dry/types"; t = Dry::Types["strict.integer"] | Dry::Types["strict.string"]; p t.call("x")`, "\"x\"\n"},
		{`require "dry/types"; t = Dry::Types["strict.string"].constructor { |v| v.to_s }; p t.call(5)`, "\"5\"\n"},
		{`require "dry/types"; t = Dry::Types["strict.integer"].meta(foo: 1); p t.meta`, "{foo: 1}\n"},
		// constrained failure raises ConstraintError.
		{`require "dry/types"
begin
  Dry::Types["strict.integer"].constrained(gt: 5).call(1)
rescue Dry::Types::ConstraintError
  puts "cons"
end`, "cons\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDryTypesArrayAndSchema covers ArrayOf and the Schema hash builder.
func TestDryTypesArrayAndSchema(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "dry/types"; t = Dry::Types.ArrayOf(Dry::Types["coercible.integer"]); p t.call(["1", "2"])`, "[1, 2]\n"},
		{`require "dry/types"; t = Dry::Types.Schema(name: Dry::Types["strict.string"]); p t.call({name: "x"})`, "{name: \"x\"}\n"},
		// Optional key ("?") may be absent.
		{`require "dry/types"; t = Dry::Types.Schema("age?": Dry::Types["strict.integer"]); p t.call({})`, "{}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestDryTypesArgErrors covers the arity / type guards on the public methods that
// raise from Ruby.
func TestDryTypesArgErrors(t *testing.T) {
	raises := []struct{ src, class string }{
		{`require "dry/types"; Dry::Types[]`, "ArgumentError"},
		{`require "dry/types"; Dry::Types["nope.nope"]`, "ArgumentError"},
		{`require "dry/types"; Dry::Types["strict.integer"].call`, "ArgumentError"},
		{`require "dry/types"; Dry::Types["strict.integer"].valid?`, "ArgumentError"},
		{`require "dry/types"; Dry::Types["strict.integer"].try`, "ArgumentError"},
		{`require "dry/types"; Dry::Types["strict.integer"].default`, "ArgumentError"},
		{`require "dry/types"; Dry::Types["strict.integer"].constructor`, "ArgumentError"},
		{`require "dry/types"; Dry::Types["strict.integer"].meta(5)`, "TypeError"},
		{`require "dry/types"; Dry::Types["strict.integer"] | 5`, "TypeError"},
		{`require "dry/types"; Dry::Types["strict.integer"].send(:|)`, "ArgumentError"},
		{`require "dry/types"; Dry::Types.ArrayOf`, "ArgumentError"},
		{`require "dry/types"; Dry::Types.ArrayOf(5)`, "TypeError"},
		{`require "dry/types"; Dry::Types.Schema`, "ArgumentError"},
		{`require "dry/types"; Dry::Types.Schema(5)`, "TypeError"},
	}
	for _, c := range raises {
		if class, _ := evalErr(t, c.src); class != c.class {
			t.Errorf("src=%q class=%q want=%q", c.src, class, c.class)
		}
	}
}

// TestDryToGo covers the rbgo->library value mapping, including the shapes not
// exercised through the surface above (Undefined via a nil, Bignum, Float, Symbol,
// Time, and the pass-through for an unmapped value).
func TestDryToGo(t *testing.T) {
	if dryToGo(nil) != drytypes.Undefined {
		t.Error("nil should map to Undefined")
	}
	if dryToGo(object.NilV) != nil {
		t.Error("Ruby nil should map to Go nil")
	}
	if dryToGo(object.Bool(true)) != true {
		t.Error("bool")
	}
	if dryToGo(object.Integer(5)).(int64) != 5 {
		t.Error("int")
	}
	bn := &object.Bignum{I: big.NewInt(7)}
	if dryToGo(bn).(*big.Int).Int64() != 7 {
		t.Error("bignum")
	}
	if dryToGo(object.Float(1.5)).(float64) != 1.5 {
		t.Error("float")
	}
	if dryToGo(object.NewString("x")).(string) != "x" {
		t.Error("string")
	}
	if dryToGo(object.Symbol("s")) != drytypes.Symbol("s") {
		t.Error("symbol")
	}
	arr := dryToGo(&object.Array{Elems: []object.Value{object.Integer(1)}}).([]any)
	if arr[0].(int64) != 1 {
		t.Error("array")
	}
	h := object.NewHash()
	h.Set(object.NewString("k"), object.Integer(2))
	m := dryToGo(h).(*drytypes.Map)
	if v, _ := m.Get("k"); v.(int64) != 2 {
		t.Error("hash")
	}
	// A Time maps to a Go time.Time.
	tm := &Time{t: gotime.FromUnix(1_700_000_000)}
	if _, ok := dryToGo(tm).(stdtime.Time); !ok {
		t.Error("time")
	}
	// An unmapped value passes through as-is.
	p := &Proc{}
	if dryToGo(p) != object.Value(p) {
		t.Error("passthrough")
	}
}

// TestDryFromGo covers the library->rbgo value mapping across every case,
// including the ones the surface does not directly return (int, big.Int, Symbol,
// Date, Time, Undefined, and the default nil).
func TestDryFromGo(t *testing.T) {
	vm := New(io.Discard)
	vm.registerDryTypes()
	check := func(in any, want string) {
		if got := dryFromGo(vm, in).Inspect(); got != want {
			t.Errorf("dryFromGo(%v) = %q want %q", in, got, want)
		}
	}
	check(nil, "nil")
	check(true, "true")
	check(int(3), "3")
	check(int64(4), "4")
	check(big.NewInt(5), "5")
	check(1.5, "1.5")
	check("x", "\"x\"")
	check(drytypes.Symbol("s"), ":s")
	check([]any{int64(1)}, "[1]")
	check(drytypes.Date{Year: 2026, Month: 6, Day: 30}, "\"2026-06-30\"")
	check(stdtime.Unix(1_700_000_000, 0), "2023-11-14 22:13:20 +0000")
	check(drytypes.Undefined, "nil")
	// An unmapped value falls through to nil.
	check(struct{}{}, "nil")
	// A *drytypes.Map maps back to a Hash.
	m := drytypes.NewMap()
	m.Set("a", int64(1))
	if got := dryFromGo(vm, m).Inspect(); !strings.Contains(got, "\"a\" => 1") {
		t.Errorf("map: %q", got)
	}
}

// TestDryTypesValueObjects covers the DryType / DryResult value-object display
// methods and the branches only reachable via a non-Symbol/String key or
// non-DryType value (the to_s fallbacks and skip guards).
func TestDryTypesValueObjects(t *testing.T) {
	dt := &DryType{t: drytypes.StrictInteger()}
	if dt.ToS() == "" || dt.Inspect() == "" || !dt.Truthy() {
		t.Error("DryType display")
	}
	dr := &DryResult{r: drytypes.Try(drytypes.StrictInteger(), 3)}
	if dr.ToS() == "" || dr.Inspect() == "" || !dr.Truthy() {
		t.Error("DryResult display")
	}
	if dryTypeName(object.Integer(5)) != "5" {
		t.Error("dryTypeName fallback")
	}
	if dryKeyName(object.Integer(6)) != "6" {
		t.Error("dryKeyName fallback")
	}
	if cs := dryConstraints([]object.Value{object.Integer(1)}); cs != nil {
		t.Errorf("dryConstraints non-hash: %v", cs)
	}
	h := object.NewHash()
	h.Set(object.Symbol("bad"), object.Integer(1))
	if sc := drySchema(h); sc == nil {
		t.Error("drySchema nil")
	}
	if c := dryErrorClass(&drytypes.CoercionError{Message: "x"}); c != "Dry::Types::CoercionError" {
		t.Errorf("coercion class %q", c)
	}
	if c := dryErrorClass(&drytypes.SchemaError{Message: "x"}); c != "Dry::Types::SchemaError" {
		t.Errorf("schema class %q", c)
	}
	if c := dryErrorClass(&drytypes.MissingKeyError{Message: "x"}); c != "Dry::Types::SchemaError" {
		t.Errorf("missing-key class %q", c)
	}
}

// TestDryTypesMetaGetter covers the no-argument #meta getter path and a
// String-keyed meta setter (exercising dryKeyName's String branch).
func TestDryTypesMetaGetter(t *testing.T) {
	if got := eval(t, `require "dry/types"; p Dry::Types["strict.integer"].meta`); got != "{}\n" {
		t.Errorf("empty meta: %q", got)
	}
	if got := eval(t, `require "dry/types"; p Dry::Types["strict.integer"].meta("k" => 1).meta`); got != "{k: 1}\n" {
		t.Errorf("string-keyed meta: %q", got)
	}
}

// TestDryTypesDefaultFn covers the DefaultFn (block-default) substitution
// closure. It fires only on an Undefined input, which the Ruby surface cannot
// produce (Ruby nil maps to Go nil, not Undefined), so the registered #default
// block method is invoked directly and its result applied to Undefined — driving
// the very closure #default installs.
func TestDryTypesDefaultFn(t *testing.T) {
	vm := New(io.Discard)
	cls := vm.consts["Dry::Types::Type"].(*RClass)
	m := cls.methods["default"]
	if m == nil || m.native == nil {
		t.Fatal("default method missing")
	}
	base := &DryType{t: drytypes.StrictInteger()}
	blk := &Proc{native: func(_ *VM, _ []object.Value) object.Value { return object.Integer(99) }}
	res := m.native(vm, base, nil, blk).(*DryType)
	out, err := res.t.Call(drytypes.Undefined)
	if err != nil {
		t.Fatalf("default fn call: %v", err)
	}
	if out.(int64) != 99 {
		t.Errorf("default block = %v want 99", out)
	}
}
