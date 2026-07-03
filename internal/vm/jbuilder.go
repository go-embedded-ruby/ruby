// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	jbuilder "github.com/go-ruby-jbuilder/jbuilder"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Jbuilder is the Ruby wrapper around a *jbuilder.Jbuilder builder. The gem
// drives the builder through method_missing on a `json` receiver — json.name "x"
// sets a key, json.name { … } nests an object, json.array! maps a collection —
// and this shell reproduces that: method_missing routes onto the library's
// Set/Block driver methods, and the bang methods (array!, set!, merge!, extract!,
// nil!, null!, child!, key_format!, ignore_nil!, target!) map onto their library
// counterparts. The JSON assembly and byte-exact encoding live entirely in the
// github.com/go-ruby-jbuilder/jbuilder library (see jbuilder_bind.go); this file
// is the thin wiring plus rbgo's method_missing → Set/Block and block evaluation.
type Jbuilder struct {
	b *jbuilder.Jbuilder
}

func (j *Jbuilder) ToS() string     { return "#<Jbuilder>" }
func (j *Jbuilder) Inspect() string { return "#<Jbuilder>" }
func (j *Jbuilder) Truthy() bool    { return true }

// registerJbuilder installs the Jbuilder class (require "jbuilder"):
// Jbuilder.encode { |json| … } and Jbuilder.new plus the instance DSL. The class
// is the receiver Rails passes as `json`, so its instances answer method_missing
// (json.<name>) as well as the explicit bang methods.
func (vm *VM) registerJbuilder() {
	cls := newClass("Jbuilder", vm.cObject)
	vm.consts["Jbuilder"] = object.Wrap(cls)

	// Jbuilder.encode { |json| … } builds a fresh builder, yields it, and returns
	// the compact JSON string — the common `render json: Jbuilder.encode { … }`.
	cls.smethods["encode"] = &Method{name: "encode", owner: cls,
		native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
			if blk == nil {
				raise("LocalJumpError", "no block given (yield)")
			}
			j := &Jbuilder{b: jbuilder.New()}
			vm.callBlock(blk, []object.Value{object.Wrap(j)})
			return object.Wrap(object.NewString(j.b.Encode()))
		}}

	// Jbuilder.new { |json| … } builds a builder, optionally yielding it (the gem
	// yields self to a block passed to new). The instance is returned so callers
	// can keep driving it and read target!.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
			j := &Jbuilder{b: jbuilder.New()}
			if blk != nil {
				vm.callBlock(blk, []object.Value{object.Wrap(j)})
			}
			return object.Wrap(j)
		}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *Jbuilder { return object.Kind[*Jbuilder](v) }

	// method_missing(name, *args, &blk) is the heart of the DSL: json.name "x"
	// sets key -> value, json.name { … } nests a block-built object, and
	// json.name collection { |x| … } maps a nested array (the gem's
	// method_missing collection form). name is the (unformatted) key.
	d("method_missing", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		// The dispatcher always calls method_missing with the missing name as
		// args[0], so args is non-empty here.
		j := self(v)
		key := jbuilderName(args[0])
		rest := args[1:]
		switch {
		case blk != nil && len(rest) == 1:
			// json.comments @post.comments do |comment| … end — nested array.
			jbuilderArray(vm, j, rest[0], key, blk)
		case blk != nil:
			// json.author do … end — nested object under key.
			j.b.Block(key, jbuilderBlockFn(vm, j, blk))
		case len(rest) == 0:
			// json.name with no value sets an empty nested object (gem behaviour).
			j.b.Block(key, func(*jbuilder.Jbuilder) {})
		case len(rest) == 1:
			// json.name value.
			j.b.Set(key, toJbuilder(vm, rest[0]))
		default:
			// json.name(obj, :a, :b) — extract! shorthand.
			j.b.Set(key, toJbuilder(vm, rest[0]))
		}
		return object.NilVal()
	})

	// respond_to_missing? — the builder answers every name dynamically.
	d("respond_to_missing?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(true)))
	})

	// set!(key, value = nil, &blk): explicit form of json.<key>.
	d("set!", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		j := self(v)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		key := jbuilderName(args[0])
		switch {
		case blk != nil && len(args) >= 2:
			jbuilderArray(vm, j, args[1], key, blk)
		case blk != nil:
			j.b.Block(key, jbuilderBlockFn(vm, j, blk))
		case len(args) >= 2:
			j.b.Set(key, toJbuilder(vm, args[1]))
		default:
			j.b.Block(key, func(*jbuilder.Jbuilder) {})
		}
		return object.NilVal()
	})

	// array!(collection) { |x| … } turns the builder into a JSON array by mapping
	// the block over the collection; with no block the elements pass through.
	d("array!", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		j := self(v)
		var coll object.Value = object.NilVal()
		if len(args) > 0 {
			coll = args[0]
		}
		jbuilderArray(vm, j, coll, "", blk)
		return object.NilVal()
	})

	// child! { … } appends one block-built element to the builder's array.
	d("child!", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		j := self(v)
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		j.b.Child(jbuilderBlockFn(vm, j, blk))
		return object.NilVal()
	})

	// merge!(hash_or_array) folds a Hash's pairs (or an Array's elements) into the
	// current target, preserving order.
	d("merge!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		j := self(v)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		{
			__sw73 := args[0]
			switch {
			case object.IsKind[*object.Array](__sw73):
				n := object.Kind[*object.Array](__sw73)
				_ = n
				elems := make([]any, len(n.Elems))
				for i, el := range n.Elems {
					elems[i] = toJbuilder(vm, el)
				}
				j.b.MergeArray(elems)
			case object.IsKind[*object.Hash](__sw73):
				n := object.Kind[*object.Hash](__sw73)
				_ = n
				j.b.Merge(jbuilderPairs(vm, n))
			default:
				n := __sw73
				_ = n
				raise("TypeError", "no implicit conversion into Hash or Array")
			}
		}
		return object.NilVal()
	})

	// extract!(object, *keys): copy object.key / object[key] for each key into the
	// current object. rbgo resolves each attribute here (via [] or the reader) so
	// the library only records the ordered pairs.
	d("extract!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		j := self(v)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		j.b.Extract(jbuilderExtract(vm, args[0], args[1:]))
		return object.NilVal()
	})

	// nil!/null! set the whole target to JSON null.
	nilFn := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).b.Nil()
		return object.NilVal()
	}
	d("nil!", nilFn)
	d("null!", nilFn)

	// key_format!(op: arg, …) sets the active key transform (camelize/dasherize/
	// underscore). With no arguments it clears formatting.
	d("key_format!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).b.KeyFormat(jbuilderKeyOps(vm, args)...)
		return object.NilVal()
	})

	// ignore_nil!(on = true) drops keys whose value is nil.
	d("ignore_nil!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			self(v).b.IgnoreNil()
		} else {
			self(v).b.IgnoreNil(args[0].Truthy())
		}
		return object.NilVal()
	})

	// target!/encode! render the builder to its compact JSON string.
	target := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).b.Encode()))
	}
	d("target!", target)
	d("encode!", target)
	d("to_json", target)
	d("to_s", target)
}
