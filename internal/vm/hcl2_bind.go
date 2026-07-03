// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"

	hcl2 "github.com/go-ruby-hcl2/hcl2"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent value model of github.com/go-ruby-hcl2/hcl2. The
// lexer/parser/evaluator live in that library; rbgo only translates the source
// String and a context Hash into a hcl2.Eval call and maps the returned value
// model (int64/float64/string/bool/nil/[]any/*Map, exactly as the TOML backend
// uses) back into the object graph.

// hcl2Eval evaluates an HCL2 document against ctx and returns a Ruby Hash. A
// syntax or evaluation error (hcl2.Diagnostics) raises HCL2::Error carrying the
// library's message.
func hcl2Eval(vm *VM, src string, ctx object.Value) object.Value {
	m, err := hcl2.Eval(src, hcl2Context(ctx))
	if err != nil {
		raise("HCL2::Error", "%s", err.Error())
	}
	return fromHCL2Map(vm, m)
}

// hcl2EvalExpr evaluates a single HCL2 expression against ctx and returns its
// Ruby value.
func hcl2EvalExpr(vm *VM, src string, ctx object.Value) object.Value {
	v, err := hcl2.EvalExpr(src, hcl2Context(ctx))
	if err != nil {
		raise("HCL2::Error", "%s", err.Error())
	}
	return fromHCL2(vm, v)
}

// hcl2Context maps a Ruby context value to a *hcl2.Context. A nil / Ruby nil
// value (or any non-Hash) yields a nil context (an empty environment). A Hash
// with a :variables (or "variables") key reads that sub-Hash as the variable
// bindings; otherwise the whole Hash is read as the variables map. Only variables
// are bridged — Ruby callables are not exposed as HCL functions, so a
// :functions key is ignored (the library's built-in functions remain available).
func hcl2Context(ctx object.Value) *hcl2.Context {
	h, ok := object.KindOK[*object.Hash](ctx)
	if !ok {
		return nil
	}
	vars := h
	if v, present := hcl2HashGet(h, "variables"); present {
		if sub, ok := object.KindOK[*object.Hash](v); ok {
			vars = sub
		}
	}
	c := hcl2.NewContext()
	for _, k := range vars.Keys {
		val, _ := vars.Get(k)
		c.Variables[hcl2Key(k)] = toHCL2(val)
	}
	return c
}

// hcl2HashGet looks up a key by its bare name, trying the Symbol and String
// spellings, so a caller may write either `variables:` or `"variables" =>`.
func hcl2HashGet(h *object.Hash, name string) (object.Value, bool) {
	if v, ok := h.Get(object.SymVal(string(object.Symbol(name)))); ok {
		return v, true
	}
	return h.Get(object.Wrap(object.NewString(name)))
}

// hcl2Key renders a Ruby Hash key as a variable name: a Symbol by its name, any
// other value by its to_s.
func hcl2Key(k object.Value) string {
	if s, ok := object.KindOK[object.Symbol](k); ok {
		return string(s)
	}
	if s, ok := object.KindOK[*object.String](k); ok {
		return s.Str()
	}
	return k.ToS()
}

// --- rbgo value -> library value (for context variables) -------------------

// toHCL2 maps a Ruby value to the hcl2 value model (the same small set the
// backend uses: bool / int64 / float64 / string / nil / []any / *Map). A Symbol
// maps to its name (HCL has string values only), and Array / Hash recurse. An
// unmapped value maps to nil.
func toHCL2(v object.Value) hcl2.Value {
	{
		__sw65 := v
		switch {
		case __sw65 == nil:
			n := __sw65
			_ = n
			return nil
		case object.IsNilObj(__sw65):
			n := object.NilObj()
			_ = n
			return nil
		case object.IsBool(__sw65):
			n := object.AsBoolV(__sw65)
			_ = n
			return bool(n)
		case object.IsInt(__sw65):
			n := object.AsInteger(__sw65)
			_ = n
			return int64(n)
		case object.IsKind[*object.Bignum](__sw65):
			n := object.Kind[*object.Bignum](__sw65)
			_ = n
			if n.I.IsInt64() {
				return n.I.Int64()
			}
			f, _ := new(big.Float).SetInt(n.I).Float64()
			return f
		case object.IsFloat(__sw65):
			n := object.AsFloatV(__sw65)
			_ = n
			return float64(n)
		case object.IsKind[*object.String](__sw65):
			n := object.Kind[*object.String](__sw65)
			_ = n
			return n.Str()
		case object.IsKind[object.Symbol](__sw65):
			n := object.Kind[object.Symbol](__sw65)
			_ = n
			return string(n)
		case object.IsKind[*object.Array](__sw65):
			n := object.Kind[*object.Array](__sw65)
			_ = n
			out := make([]hcl2.Value, len(n.Elems))
			for i, el := range n.Elems {
				out[i] = toHCL2(el)
			}
			return out
		case object.IsKind[*object.Hash](__sw65):
			n := object.Kind[*object.Hash](__sw65)
			_ = n
			m := hcl2.NewMap()
			for _, k := range n.Keys {
				val, _ := n.Get(k)
				m.Set(hcl2Key(k), toHCL2(val))
			}
			return m
		}
	}
	return nil
}

// --- library value -> rbgo value (for Eval results) ------------------------

// fromHCL2 maps a value produced by hcl2.Eval back into the rbgo object graph.
// The library narrows an integral number to int64 and a fractional one to
// float64, and *Map / []any recurse, exactly as the TOML backend does.
func fromHCL2(vm *VM, v hcl2.Value) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilVal()
	case bool:
		return object.BoolValue(bool(object.Bool(n)))
	case int64:
		return object.IntValue(n)
	case *big.Int:
		return object.NormInt(n)
	case float64:
		return object.FloatValue(float64(object.Float(n)))
	case string:
		return object.Wrap(object.NewString(n))
	case []hcl2.Value:
		arr := object.NewArrayFromSlice(make([]object.Value, len(n)))
		for i, el := range n {
			arr.Elems[i] = fromHCL2(vm, el)
		}
		return object.Wrap(arr)
	case *hcl2.Map:
		return fromHCL2Map(vm, n)
	}
	// The evaluator only ever produces the cases above; anything else is nil.
	return object.NilVal()
}

// fromHCL2Map maps a library ordered *Map to a Ruby Hash with String keys,
// preserving insertion order.
func fromHCL2Map(vm *VM, m *hcl2.Map) object.Value {
	h := object.NewHash()
	for _, p := range m.Pairs() {
		h.Set(object.Wrap(object.NewString(p.Key)), fromHCL2(vm, p.Val))
	}
	return object.Wrap(h)
}
