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
	switch n := v.(type) {
	case nil:
		return nil
	case object.Nil:
		return nil
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int64(n)
	case *object.Bignum:
		return n.I
	case object.Float:
		return float64(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return jbuilder.Symbol(string(n))
	case *object.Array:
		out := make([]any, len(n.Elems))
		for i, el := range n.Elems {
			out[i] = toJbuilder(vm, el)
		}
		return out
	case *object.Hash:
		m := map[string]any{}
		for _, k := range n.Keys {
			val, _ := n.Get(k)
			m[jbuilderName(k)] = toJbuilder(vm, val)
		}
		return m
	case *Jbuilder:
		return n.b
	}
	// A Ruby object with no direct shape degrades to its #to_s text.
	return vm.send(v, "to_s", nil, nil).(*object.String).Str()
}

// jbuilderName renders a key (a Symbol, a String, or any value) as its bare
// string name — jbuilder applies its own key_format! transform afterwards.
func jbuilderName(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
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
		vm.callBlock(blk, []object.Value{j})
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
	arr, ok := coll.(*object.Array)
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
		if h, ok := src.(*object.Hash); ok {
			if v, present := h.Get(object.Symbol(name)); present {
				val = v
			} else if v, present := h.Get(object.NewString(name)); present {
				val = v
			} else {
				val = object.NilV
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
		switch n := a.(type) {
		case object.Symbol:
			add(string(n), object.NilV)
		case *object.String:
			add(n.Str(), object.NilV)
		case *object.Hash:
			for _, k := range n.Keys {
				val, _ := n.Get(k)
				add(jbuilderName(k), val)
			}
		}
	}
	return ops
}

// jbuilderCamelUpper reads the camelize option's argument: :upper selects
// UpperCamelCase, anything else (:lower / nil) lowerCamelCase.
func jbuilderCamelUpper(arg object.Value) bool {
	switch n := arg.(type) {
	case object.Symbol:
		return string(n) == "upper"
	case *object.String:
		return n.Str() == "upper"
	}
	return false
}
