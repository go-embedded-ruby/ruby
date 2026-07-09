// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	hiera "github.com/go-ruby-hiera/hiera"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// hieraNew implements Hiera.new(config: "path"[, scope: {…}]): it loads the
// hiera.yaml at :config, resolving %{…} interpolation against a MapScope built
// from :scope, and returns the wrapped adapter. A missing :config or a config
// load error raises.
func (vm *VM) hieraNew(args []object.Value) object.Value {
	kw := hieraKwHash(args)
	config := ""
	var scope hiera.Scope = hiera.MapScope{}
	if kw != nil {
		if v, ok := hieraKwGet(kw, "config"); ok {
			config = v.ToS()
		}
		if v, ok := hieraKwGet(kw, "scope"); ok {
			if sh, ok := v.(*object.Hash); ok {
				if m, ok := rubyToGoValue(sh).(map[string]any); ok {
					scope = hiera.MapScope(m)
				}
			}
		}
	}
	if config == "" {
		raise("ArgumentError", "Hiera.new requires a :config path")
	}
	h, err := hiera.New(config, scope)
	if err != nil {
		raise("RuntimeError", "hiera: %v", err)
	}
	return &HieraObj{vm: vm, h: h}
}

// lookup implements Hiera#lookup(key[, default][, resolution_type: :priority]):
// it resolves key (dotted keys dig structured data), applying an optional default
// and merge behaviour, and returns the value or nil when not found.
func (o *HieraObj) lookup(args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
	}
	key := args[0].ToS()
	rest := args[1:]

	var opts hiera.LookupOptions
	if h := hieraKwHash(rest); h != nil {
		if rt, ok := hieraResType(h); ok {
			opts.Merge = &rt
			rest = rest[:len(rest)-1]
		}
	}
	if len(rest) > 0 {
		opts.Default = rubyToGoValue(rest[0])
		opts.HasDefault = true
	}

	v, found, err := o.h.Lookup(key, opts)
	if err != nil {
		raise("RuntimeError", "hiera: %v", err)
	}
	if !found {
		return object.NilV
	}
	return facterValueToRuby(v)
}

// hieraKwHash returns the trailing Hash argument (keyword options), or nil.
func hieraKwHash(args []object.Value) *object.Hash {
	if n := len(args); n > 0 {
		if h, ok := args[n-1].(*object.Hash); ok {
			return h
		}
	}
	return nil
}

// hieraKwGet reads a keyword by symbol or string key from a kwargs Hash.
func hieraKwGet(h *object.Hash, key string) (object.Value, bool) {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}

// hieraResType extracts a resolution_type / merge behaviour from a kwargs Hash,
// mapping the Ruby symbol onto a go-ruby-hiera ResolutionType. It reports false
// when neither keyword is present (so a plain trailing Hash is left as a
// positional default). An unrecognised value degrades to Priority.
func hieraResType(h *object.Hash) (hiera.ResolutionType, bool) {
	v, ok := hieraKwGet(h, "resolution_type")
	if !ok {
		if v, ok = hieraKwGet(h, "merge"); !ok {
			return 0, false
		}
	}
	switch strings.TrimPrefix(v.ToS(), ":") {
	case "array", "unique":
		return hiera.Unique, true
	case "hash":
		return hiera.Hash, true
	case "deep":
		return hiera.Deep, true
	default:
		return hiera.Priority, true
	}
}
