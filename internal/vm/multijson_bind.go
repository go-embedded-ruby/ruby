// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	json "github.com/go-ruby-json/json"
	multijson "github.com/go-ruby-multi-json/multi-json"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the value bridge for the MultiJson module (see multijson.go). It
// parses through rbgo's ordered JSON engine — the same json.ParseInto + objBuilder
// path the JSON module uses (json_bind.go) — so a decoded object keeps its source
// key order and, under symbolize, real Symbol keys. A malformed document is
// re-raised as MultiJson::ParseError carrying the offending input (#data) and the
// underlying parse error's message (#cause), wrapped through the library's
// *multijson.ParseError for a gem-faithful message.

// mjParse parses src into a Ruby value through the ordered JSON builder. symbolize
// keys objects with Symbols. A parse failure raises MultiJson::ParseError.
func (vm *VM) mjParse(src string, symbolize bool) object.Value {
	var b objBuilder
	var opts []json.Option
	if symbolize {
		opts = append(opts, json.WithSymbolizeNames(true))
	}
	if err := json.ParseInto(src, &b, opts...); err != nil {
		vm.raiseMultiJsonParseError(src, err)
	}
	return b.Result().(object.Value)
}

// raiseMultiJsonParseError raises MultiJson::ParseError for a failed parse of src,
// stamping the offending input on @data and the underlying error's message on
// @cause so ParseError#data / #cause read them back. The message is shaped by the
// library's *multijson.ParseError (which forwards the wrapped cause's message).
func (vm *VM) raiseMultiJsonParseError(src string, cause error) {
	pe := &multijson.ParseError{Data: src, Cause: cause}
	cls := vm.consts["MultiJson::ParseError"].(*RClass)
	obj := &RObject{class: cls, ivars: map[string]object.Value{
		"@message": object.NewString(pe.Error()),
		"@data":    object.NewString(src),
		"@cause":   object.NewString(cause.Error()),
	}}
	panic(RubyError{Class: "MultiJson::ParseError", Message: pe.Error(), Obj: obj})
}

// mjArg reads the single leading argument, raising ArgumentError when it is
// missing.
func mjArg(args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return args[0]
}

// mjAdapterName returns the adapter name for a call: the trailing options'
// :adapter key overrides current when present, mirroring the gem's per-call
// :adapter option.
func mjAdapterName(current string, opts *object.Hash) string {
	if v, ok := mjOptGet(opts, "adapter"); ok {
		return v.ToS()
	}
	return current
}

// mjOptGet reads an option by symbol or string key from an options Hash (nil
// Hash, or an absent key, reports false).
func mjOptGet(h *object.Hash, key string) (object.Value, bool) {
	if h == nil {
		return nil, false
	}
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}

// mjMergedTruthy reports whether any of keys is truthy in the merged options: the
// per-call Hash wins over the defaults, and any of the given aliases (e.g.
// symbolize_keys / symbolize_names) satisfies it.
func mjMergedTruthy(def, call *object.Hash, keys ...string) bool {
	for _, k := range keys {
		if v, ok := mjOptGet(call, k); ok {
			return v.Truthy()
		}
	}
	for _, k := range keys {
		if v, ok := mjOptGet(def, k); ok {
			return v.Truthy()
		}
	}
	return false
}
