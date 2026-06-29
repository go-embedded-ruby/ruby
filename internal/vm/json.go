// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	json "github.com/go-ruby-json/json"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerJSON installs the JSON module (require "json"): JSON.generate/dump,
// JSON.pretty_generate, JSON.parse and Object#to_json. The parser and generator
// live in the github.com/go-ruby-json/json library; this module is the thin
// wiring that maps rbgo's object graph to and from the library's value model (see
// json_bind.go) and translates the Ruby keyword options to the library's
// functional options. The error tree (JSON::JSONError and its subclasses) is
// registered so a re-raised library error rescues as the right Ruby class.
func (vm *VM) registerJSON() {
	mod := newClass("JSON", nil)
	mod.isModule = true
	vm.consts["JSON"] = mod
	vm.registerJSONErrors(mod)

	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// JSON.generate(obj[, opts]) / JSON.dump(obj) render a compact document.
	generate := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(jsonGenerate(args[0], jsonGenerateOpts(args[1:])...))
	}
	def("generate", generate)
	def("dump", generate)

	// JSON.pretty_generate(obj[, opts]) renders the two-space-indented layout.
	def("pretty_generate", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(jsonPrettyGenerate(args[0], jsonGenerateOpts(args[1:])...))
	})

	// JSON.parse(str[, opts]) parses a document; the symbolize_names: keyword
	// returns object keys as Symbols.
	def("parse", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s, ok := args[0].(*object.String)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into String", classNameOf(args[0]))
		}
		return jsonParse(s.Str(), jsonParseOpts(args[1:])...)
	})

	// Object#to_json serialises any value (Array / Hash / … included) via generate.
	vm.cObject.define("to_json", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(jsonGenerate(self))
	})
}

// registerJSONErrors installs the JSON::JSONError exception tree mirroring MRI
// (JSONError < StandardError; ParserError / GeneratorError < JSONError;
// NestingError < ParserError). Each class is registered both as a nested constant
// of JSON (so Ruby `JSON::ParserError` resolves it) and under its qualified name
// in the top-level table (so a re-raised library error's exceptionObject lookup
// finds the very same class), exactly as Errno:: classes are.
func (vm *VM) registerJSONErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	jsonErr := reg("JSONError", "JSON::JSONError", std)
	parserErr := reg("ParserError", "JSON::ParserError", jsonErr)
	reg("NestingError", "JSON::NestingError", parserErr)
	reg("GeneratorError", "JSON::GeneratorError", jsonErr)
}

// jsonParseOpts translates a JSON.parse options hash to the library's functional
// options: symbolize_names (object keys as Symbols), max_nesting and allow_nan.
// An absent keyword leaves the library default in place.
func jsonParseOpts(rest []object.Value) []json.Option {
	h := jsonOptsHash(rest)
	if h == nil {
		return nil
	}
	var opts []json.Option
	if v, ok := h.Get(object.Symbol("symbolize_names")); ok {
		opts = append(opts, json.WithSymbolizeNames(v.Truthy()))
	}
	opts = append(opts, jsonSharedOpts(h)...)
	return opts
}

// jsonGenerateOpts translates a JSON.generate / pretty_generate options hash to
// the library's functional options: indent / space / space_before / object_nl /
// array_nl (string formatting) plus the shared max_nesting / allow_nan. An absent
// keyword leaves the library default in place.
func jsonGenerateOpts(rest []object.Value) []json.Option {
	h := jsonOptsHash(rest)
	if h == nil {
		return nil
	}
	var opts []json.Option
	str := func(key string, with func(string) json.Option) {
		if v, ok := h.Get(object.Symbol(key)); ok {
			if s, isStr := v.(*object.String); isStr {
				opts = append(opts, with(s.Str()))
			}
		}
	}
	str("indent", json.WithIndent)
	str("space", json.WithSpace)
	str("space_before", json.WithSpaceBefore)
	str("object_nl", json.WithObjectNL)
	str("array_nl", json.WithArrayNL)
	opts = append(opts, jsonSharedOpts(h)...)
	return opts
}

// jsonSharedOpts maps the max_nesting / allow_nan keywords common to parse and
// generate. max_nesting: false (or 0) disables the limit, matching MRI.
func jsonSharedOpts(h *object.Hash) []json.Option {
	var opts []json.Option
	if v, ok := h.Get(object.Symbol("max_nesting")); ok {
		switch n := v.(type) {
		case object.Integer:
			opts = append(opts, json.WithMaxNesting(int(n)))
		case object.Bool:
			if !bool(n) { // max_nesting: false disables the limit
				opts = append(opts, json.WithMaxNesting(0))
			}
		}
	}
	if v, ok := h.Get(object.Symbol("allow_nan")); ok {
		opts = append(opts, json.WithAllowNaN(v.Truthy()))
	}
	return opts
}

// jsonOptsHash returns the trailing options Hash of a JSON entry point, or nil
// when the last argument is not a Hash (no options given).
func jsonOptsHash(rest []object.Value) *object.Hash {
	if len(rest) == 0 {
		return nil
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	if !ok {
		return nil
	}
	return h
}
