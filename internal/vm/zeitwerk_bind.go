// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	zeitwerk "github.com/go-ruby-zeitwerk/zeitwerk"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// ZeitwerkLoader is the Ruby handle a Zeitwerk::Loader.new / .for_gem returns: a
// thin wrapper over a *zeitwerk.Loader (the pure-Go engine of the zeitwerk gem's
// autoloader). The engine owns the directory scan and the const<->path map; this
// wrapper carries the rbgo VM so the loader's seams (DefineAutoload / Load /
// OnUnload) can reach rbgo's real Module#autoload, require and constant table
// (they are wired once in registerZeitwerk when the loader is built). inf is the
// wrapper over the engine's own inflector, cached so Loader#inflector returns a
// stable object whose #inflect mutations are seen by the scan.
type ZeitwerkLoader struct {
	vm  *VM
	l   *zeitwerk.Loader
	inf *ZeitwerkInflector
}

func (z *ZeitwerkLoader) ToS() string     { return "#<Zeitwerk::Loader>" }
func (z *ZeitwerkLoader) Inspect() string { return z.ToS() }
func (z *ZeitwerkLoader) Truthy() bool    { return true }

// ZeitwerkInflector is the Ruby handle for a Zeitwerk::Inflector: a wrapper over
// the engine's *zeitwerk.Inflector, the snake_case -> CamelCase mapper (with
// hard-coded acronym overrides) the scan uses to name constants.
type ZeitwerkInflector struct {
	in *zeitwerk.Inflector
}

func (z *ZeitwerkInflector) ToS() string     { return "#<Zeitwerk::Inflector>" }
func (z *ZeitwerkInflector) Inspect() string { return z.ToS() }
func (z *ZeitwerkInflector) Truthy() bool    { return true }

// zeitwerkGlobs coerces the variadic ignore / collapse arguments to their String
// glob patterns, raising TypeError on a non-String exactly like strArg.
func zeitwerkGlobs(args []object.Value) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, strArg(a))
	}
	return out
}

// zeitwerkCpath coerces an on_load / on_unload target argument (a String or
// Symbol, e.g. "Admin::User" or :ANY) to the constant-path string the engine
// expects. :ANY (the default) selects every constant.
func zeitwerkCpath(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	default:
		raise("TypeError", "no implicit conversion of %s into String", v.Inspect())
		return ""
	}
}

// zeitwerkNamespace coerces the push_dir `namespace:` value to the constant-path
// string the engine roots that directory under. A Module/Class contributes its
// name, a Symbol or String its text; nil / "Object" mean the top level (the empty
// string). Anything else is a TypeError, as the gem rejects a non-module
// namespace.
func zeitwerkNamespace(v object.Value) string {
	switch n := v.(type) {
	case *RClass:
		return n.name
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	case object.Nil:
		return ""
	default:
		raise("TypeError", "%s is not a class/module", v.Inspect())
		return ""
	}
}
