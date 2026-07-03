// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	jbuilder "github.com/go-ruby-jbuilder/jbuilder"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-jbuilder/jbuilder builder. The JSON
// assembly and byte-exact encoding live in that library; rbgo maps Ruby values
// into its value model (nil / bool / integers / floats / String / Symbol / []any
// / Hash / nested *jbuilder.Jbuilder) and drives its Set/Block/Array/Merge/Extract
// methods, evaluating Ruby blocks against a child builder as it goes.

// toJbuilder maps a Ruby value into the jbuilder value model for a scalar Set. A
// Hash becomes a Go map[string]any keyed by its stringified keys — jbuilder emits
// it sorted, matching the gem handing a Hash to the JSON encoder — an Array
// recurses to []any, and a nested Jbuilder wrapper contributes its underlying
// builder. Everything else passes through as its Go-typed value.
func toJbuilder(vm *VM, v object.Value) any {
	{
		__sw74 := v
		switch {
		case __sw74 == nil:
			n := __sw74
			_ = n
			return nil
		case object.IsNilObj(__sw74):
			n := object.NilObj()
			_ = n
			return nil
		case object.IsBool(__sw74):
			n := object.AsBoolV(__sw74)
			_ = n
			return bool(n)
		case object.IsInt(__sw74):
			n := object.AsInteger(__sw74)
			_ = n
			return int64(n)
		case object.IsKind[*object.Bignum](__sw74):
			n := object.Kind[*object.Bignum](__sw74)
			_ = n
			return n.I
		case object.IsFloat(__sw74):
			n := object.AsFloatV(__sw74)
			_ = n
			return float64(n)
		case object.IsKind[*object.String](__sw74):
			n := object.Kind[*object.String](__sw74)
			_ = n
			return n.Str()
		case object.IsKind[object.Symbol](__sw74):
			n := object.Kind[object.Symbol](__sw74)
			_ = n
			return jbuilder.Symbol(string(n))
		case object.IsKind[*object.Array](__sw74):
			n := object.Kind[*object.Array](__sw74)
			_ = n
			out := make([]any, len(n.Elems))
			for i, el := range n.Elems {
				out[i] = toJbuilder(vm, el)
			}
			return out
		case object.IsKind[*object.Hash](__sw74):
			n := object.Kind[*object.Hash](__sw74)
			_ = n
			m := map[string]any{}
			for _, k := range n.Keys {
				val, _ := n.Get(k)
				m[jbuilderName(k)] = toJbuilder(vm, val)
			}
			return m
		case object.IsKind[*Jbuilder](__sw74):
			n := object.Kind[*Jbuilder](__sw74)
			_ = n
			return n.b
		}
	}
	// A Ruby object with no direct shape degrades to its #to_s text.
	return object.Kind[*object.String](vm.send(v, "to_s", nil, nil)).Str()
}

// jbuilderName renders a key (a Symbol, a String, or any value) as its bare
// string name — jbuilder applies its own key_format! transform afterwards.
func jbuilderName(v object.Value) string {
	{
		__sw75 := v
		switch {
		case object.IsKind[object.Symbol](__sw75):
			n := object.Kind[object.Symbol](__sw75)
			_ = n
			return string(n)
		case object.IsKind[*object.String](__sw75):
			n := object.Kind[*object.String](__sw75)
			_ = n
			return n.Str()
		}
	}
	return v.ToS()
}

// jbuilderBlockFn adapts a Ruby block to the library's func(*jbuilder.Jbuilder)
// child-builder callback. The gem reuses the same `json` receiver inside a nested
// block (blocks take no parameter and drive `json` directly), so the wrapper's
// underlying builder is temporarily swapped to the child for the block's duration
// and restored afterwards — reproducing the gem's in-place @attributes swap. The
// block is still passed the wrapper as its argument so `do |json| … end` forms
// also work.
func jbuilderBlockFn(vm *VM, j *Jbuilder, blk *Proc) func(*jbuilder.Jbuilder) {
	return func(child *jbuilder.Jbuilder) {
		prev := j.b
		j.b = child
		defer func() { j.b = prev }()
		vm.callBlock(blk, []object.Value{object.Wrap(j)})
	}
}

// jbuilderArray maps a Ruby collection into a JSON array via the library. With a
// block, the wrapper's builder is swapped to each element's child for the block's
// duration (so `json` drives that element); with no block the elements are emitted
// verbatim. key == "" turns the whole builder into the array (json.array!); a
// non-empty key nests the array under that key.
func jbuilderArray(vm *VM, j *Jbuilder, coll object.Value, key string, blk *Proc) {
	b := j.b
	items := jbuilderItems(vm, coll)
	build := func(target *jbuilder.Jbuilder) {
		if blk == nil {
			// No block: emit each element as its coerced value-model form, since
			// the library would otherwise store the raw object.Value verbatim.
			out := make([]any, len(items))
			for i, it := range items {
				out[i] = toJbuilder(vm, it.(object.Value))
			}
			target.Array(out, nil)
			return
		}
		target.Array(items, func(child *jbuilder.Jbuilder, it any) {
			// The gem's array! block takes only the element (`|comment|`) and drives
			// the shared `json`, so swap the wrapper's builder to this element's
			// child for the block and pass just the element.
			prev := j.b
			j.b = child
			defer func() { j.b = prev }()
			vm.callBlock(blk, []object.Value{it.(object.Value)})
		})
	}
	if key == "" {
		build(b)
		return
	}
	b.Block(key, build)
}

// jbuilderItems extracts a Ruby collection's elements as a []any of raw
// object.Value elements (the block sees the original Ruby object). A non-Array is
// treated as empty. The elements are held as object.Value so the mapping block
// receives real Ruby values; a no-block array! coerces them at emit time.
func jbuilderItems(vm *VM, coll object.Value) []any {
	arr, ok := object.KindOK[*object.Array](coll)
	if !ok {
		return nil
	}
	items := make([]any, len(arr.Elems))
	for i, el := range arr.Elems {
		items[i] = any(el)
	}
	return items
}

// jbuilderPairs maps a Ruby Hash to the library's ordered []Pair for merge!,
// preserving insertion order and stringifying keys.
func jbuilderPairs(vm *VM, h *object.Hash) []jbuilder.Pair {
	pairs := make([]jbuilder.Pair, 0, h.Len())
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		pairs = append(pairs, jbuilder.Pair{Key: jbuilderName(k), Value: toJbuilder(vm, val)})
	}
	return pairs
}

// jbuilderExtract resolves object.<key> / object[key] for each key into ordered
// pairs for the library's Extract. A Hash source reads with [], any other object
// with its #<key> reader (falling back to [] when it responds), so
// json.extract!(hash_or_obj, :a, :b) works for both.
func jbuilderExtract(vm *VM, src object.Value, keys []object.Value) []jbuilder.Pair {
	pairs := make([]jbuilder.Pair, 0, len(keys))
	for _, k := range keys {
		name := jbuilderName(k)
		var val object.Value
		if h, ok := object.KindOK[*object.Hash](src); ok {
			if v, present := h.Get(object.SymVal(string(object.Symbol(name)))); present {
				val = v
			} else if v, present := h.Get(object.Wrap(object.NewString(name))); present {
				val = v
			} else {
				val = object.NilVal()
			}
		} else if vm.respondsTo(src, name) {
			val = vm.send(src, name, nil, nil)
		} else {
			val = vm.send(src, "[]", []object.Value{k}, nil)
		}
		pairs = append(pairs, jbuilder.Pair{Key: name, Value: toJbuilder(vm, val)})
	}
	return pairs
}

// jbuilderKeyOps maps a key_format! argument list to the library's KeyOp chain.
// Arguments are Symbols (:camelize/:dasherize/:underscore) or a Hash
// ({camelize: :lower}); an unknown op is ignored. No arguments yields an empty
// chain, which clears formatting.
func jbuilderKeyOps(vm *VM, args []object.Value) []jbuilder.KeyOp {
	var ops []jbuilder.KeyOp
	add := func(name string, arg object.Value) {
		switch name {
		case "camelize":
			ops = append(ops, jbuilder.Camelize(jbuilderCamelUpper(arg)))
		case "dasherize":
			ops = append(ops, jbuilder.Dasherize())
		case "underscore":
			ops = append(ops, jbuilder.Underscore())
		}
	}
	for _, a := range args {
		{
			__sw76 := a
			switch {
			case object.IsKind[object.Symbol](__sw76):
				n := object.Kind[object.Symbol](__sw76)
				_ = n
				add(string(n), object.NilVal())
			case object.IsKind[*object.String](__sw76):
				n := object.Kind[*object.String](__sw76)
				_ = n
				add(n.Str(), object.NilVal())
			case object.IsKind[*object.Hash](__sw76):
				n := object.Kind[*object.Hash](__sw76)
				_ = n
				for _, k := range n.Keys {
					val, _ := n.Get(k)
					add(jbuilderName(k), val)
				}
			}
		}
	}
	return ops
}

// jbuilderCamelUpper reads the camelize option's argument: :upper selects
// UpperCamelCase, anything else (:lower / nil) lowerCamelCase.
func jbuilderCamelUpper(arg object.Value) bool {
	{
		__sw77 := arg
		switch {
		case object.IsKind[object.Symbol](__sw77):
			n := object.Kind[object.Symbol](__sw77)
			_ = n
			return string(n) == "upper"
		case object.IsKind[*object.String](__sw77):
			n := object.Kind[*object.String](__sw77)
			_ = n
			return n.Str() == "upper"
		}
	}
	return false
}
