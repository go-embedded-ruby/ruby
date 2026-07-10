// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	multijson "github.com/go-ruby-multi-json/multi-json"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the MultiJson module and its Ruby-facing API (require
// "multi_json"). The adapter-name registry (the eight accepted adapter names and
// the unknown-name → MultiJson::AdapterError rule) and the ParseError / options
// semantics come from github.com/go-ruby-multi-json/multi-json, a pure-Go port of
// the multi_json gem. The actual JSON parse and generate are *not* taken from
// that library: its encoding/json backend keys objects with a Go map, which loses
// Ruby Hash key order and the symbol-vs-string key distinction. rbgo instead
// routes load/dump through its own ordered JSON engine (the go-ruby-json backend
// used by the JSON module, see multijson_bind.go), so `MultiJson.load` preserves
// insertion order and `symbolize_keys` yields real Symbol keys. On a CGO=0 target
// every adapter name resolves to that one engine, so the choice of adapter is
// observable only through MultiJson.adapter — faithful to the gem, whose adapters
// all produce the same document.
//
// The mutable facade state (the selected adapter and the default-options Hash) is
// held in closures captured by registerMultiJson so it never leaks across
// interpreters, mirroring the per-VM isolation of the other stateful bindings.

// registerMultiJson installs the MultiJson module (require "multi_json"):
// load/decode and dump/encode, the adapter selection surface
// (adapter/adapter=/use/with_adapter/current_adapter) and the default-options
// accessors, plus the MultiJson::ParseError / AdapterError tree.
func (vm *VM) registerMultiJson() {
	mod := newClass("MultiJson", nil)
	mod.isModule = true
	vm.consts["MultiJson"] = mod
	vm.registerMultiJsonErrors(mod)

	// Per-VM facade state, captured by the method closures below: the selected
	// adapter name and the default options Hash merged (per-call keys winning) into
	// every load / dump.
	adapter := multijson.DefaultAdapter
	defaultOpts := object.NewHash()

	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// resolve validates name against the library's adapter registry, raising
	// MultiJson::AdapterError for an unknown name, and returns its normalised form.
	resolve := func(name string) string {
		ad, err := multijson.CurrentAdapter(map[string]any{multijson.OptAdapter: name})
		if err != nil {
			raise("MultiJson::AdapterError", "%s", err.(*multijson.AdapterError).Error())
		}
		return ad.Name()
	}

	// MultiJson.adapter reports the selected adapter name; MultiJson.current_adapter
	// resolves it honouring a per-call :adapter override.
	def("adapter", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(adapter)
	})
	def("current_adapter", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(resolve(mjAdapterName(adapter, jsonOptsHash(args))))
	})

	// MultiJson.adapter= / .use select the adapter (validated through resolve).
	setAdapter := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		adapter = resolve(mjArg(args).ToS())
		return object.NewString(adapter)
	}
	def("adapter=", setAdapter)
	def("use", setAdapter)

	// MultiJson.with_adapter(name){ ... } runs the block with name selected, then
	// restores the previous adapter (even when the block raises).
	def("with_adapter", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		prev := adapter
		adapter = resolve(mjArg(args).ToS())
		defer func() { adapter = prev }()
		return vm.callBlock(blk, nil)
	})

	// MultiJson.default_options / .default_options= read and replace the options
	// Hash merged into every load / dump. A non-Hash assignment is ignored (the
	// gem stores whatever it is given; only Hash options are ever consulted).
	def("default_options", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return defaultOpts
	})
	def("default_options=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		v := mjArg(args)
		if h, ok := v.(*object.Hash); ok {
			defaultOpts = h
		}
		return v
	})

	// MultiJson.load(str[, opts]) / .decode parse a document through rbgo's ordered
	// JSON so Hash key order and symbol-vs-string keys survive; :symbolize_keys /
	// :symbolize_names (per-call over default_options) key objects with Symbols.
	load := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s, ok := mjArg(args).(*object.String)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into String", classNameOf(args[0]))
		}
		opts := jsonOptsHash(args[1:])
		resolve(mjAdapterName(adapter, opts)) // validate a per-call :adapter override
		sym := mjMergedTruthy(defaultOpts, opts, "symbolize_keys", "symbolize_names")
		return vm.mjParse(s.Str(), sym)
	}
	def("load", load)
	def("decode", load)

	// MultiJson.dump(obj[, opts]) / .encode render a document; :pretty (per-call
	// over default_options) selects the indented layout.
	dump := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		obj := mjArg(args)
		opts := jsonOptsHash(args[1:])
		resolve(mjAdapterName(adapter, opts)) // validate a per-call :adapter override
		if mjMergedTruthy(defaultOpts, opts, "pretty") {
			return object.NewString(jsonPrettyGenerate(obj))
		}
		return object.NewString(jsonGenerate(obj))
	}
	def("dump", dump)
	def("encode", dump)
}

// registerMultiJsonErrors installs the MultiJson exception tree: ParseError <
// StandardError (with the gem's LoadError / DecodeError aliases) and AdapterError
// < ArgumentError. Each class is registered both as a nested constant of MultiJson
// and under its qualified name in the top-level table, so a re-raised error
// rescues as the right Ruby class exactly as the JSON:: and Errno:: classes do.
// ParseError#data / #cause expose the offending input and the underlying parse
// error, matching the gem's readers (nil until set on a raised instance).
func (vm *VM) registerMultiJsonErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	arg := vm.consts["ArgumentError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	parseErr := reg("ParseError", "MultiJson::ParseError", std)
	// LoadError / DecodeError are the gem's aliases of ParseError (same class).
	mod.consts["LoadError"] = parseErr
	vm.consts["MultiJson::LoadError"] = parseErr
	mod.consts["DecodeError"] = parseErr
	vm.consts["MultiJson::DecodeError"] = parseErr
	reg("AdapterError", "MultiJson::AdapterError", arg)

	parseErr.define("data", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@data")
	})
	parseErr.define("cause", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@cause")
	})
}
