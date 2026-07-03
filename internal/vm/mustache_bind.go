// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	mustache "github.com/go-ruby-mustache/mustache"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-mustache/mustache engine. The
// template compiler and renderer live in that library; rbgo only maps a context
// value into the library's `any` value model — nil, bool, integers, floats,
// String, Symbol, ordered Hash (*mustache.Map), Array ([]any) and Proc/lambda
// (mustache.Lambda) — so the spec-faithful rendering the Mustache module relies on
// is preserved by construction.

// toMustache maps a Ruby value into the go-ruby-mustache value model. A Hash
// becomes an insertion-ordered *mustache.Map (so `{{name}}` resolves against
// either a String or Symbol key), an Array becomes []any, and a Proc becomes a
// mustache.Lambda that calls back into the VM with the unrendered section body.
// Any other value passes through as itself and the engine stringifies it with
// ToString (Ruby to_s) at interpolation.
func toMustache(vm *VM, v object.Value) mustache.Value {
	{
		__sw103 := v
		switch {
		case object.IsNil(__sw103):
			n := __sw103
			_ = n
			return nil
		case object.IsBool(__sw103):
			n := object.AsBoolV(__sw103)
			_ = n
			return bool(n)
		case object.IsInt(__sw103):
			n := object.AsInteger(__sw103)
			_ = n
			return int64(n)
		case object.IsKind[*object.Bignum](__sw103):
			n := object.Kind[*object.Bignum](__sw103)
			_ = n
			return n.I
		case object.IsFloat(__sw103):
			n := object.AsFloatV(__sw103)
			_ = n
			return float64(n)
		case object.IsKind[*object.String](__sw103):
			n := object.Kind[*object.String](__sw103)
			_ = n
			return n.Str()
		case object.IsKind[object.Symbol](__sw103):
			n := object.Kind[object.Symbol](__sw103)
			_ = n
			return mustache.Symbol(string(n))
		case object.IsKind[*object.Array](__sw103):
			n := object.Kind[*object.Array](__sw103)
			_ = n
			out := make([]any, len(n.Elems))
			for i, el := range n.Elems {
				out[i] = toMustache(vm, el)
			}
			return out
		case object.IsKind[*object.Hash](__sw103):
			n := object.Kind[*object.Hash](__sw103)
			_ = n
			m := mustache.NewMap()
			for _, k := range n.Keys {
				val, _ := n.Get(k)
				m.Set(mustacheKey(k), toMustache(vm, val))
			}
			return m
		case object.IsKind[*Proc](__sw103):
			n := object.Kind[*Proc](__sw103)
			_ = n
			p := n
			return mustache.Lambda(func(section string) mustache.Value {
				r := vm.callBlock(p, []object.Value{object.Wrap(object.NewString(section))})
				return toMustache(vm, r)
			})
		}
	}
	// A Ruby object with no direct model shape: hand the engine its #to_s text.
	return object.Kind[*object.String](vm.send(v, "to_s", nil, nil)).Str()
}

// mustacheKey renders a Ruby Hash key for the library value model: a Symbol as a
// mustache.Symbol (so it matches `{name: …}` data), a String as itself, and any
// other value by its to_s.
func mustacheKey(k object.Value) mustache.Value {
	{
		__sw104 := k
		switch {
		case object.IsKind[object.Symbol](__sw104):
			n := object.Kind[object.Symbol](__sw104)
			_ = n
			return mustache.Symbol(string(n))
		case object.IsKind[*object.String](__sw104):
			n := object.Kind[*object.String](__sw104)
			_ = n
			return n.Str()
		}
	}
	return k.ToS()
}
