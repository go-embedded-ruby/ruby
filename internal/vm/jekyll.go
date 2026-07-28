// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"

	jekyll "github.com/go-ruby-jekyll/jekyll"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the `require "jekyll"` binding: it wires the
// interpreter-independent github.com/go-ruby-jekyll/jekyll static-site generator
// (config resolution, the document/render/permalink pipeline over go-ruby-liquid,
// Markdown and Sass conversion) into rbgo's object graph. The library's programmatic
// surface is LoadConfig(source, files) -> Config, NewSite(cfg) and site.Build(); this
// binding maps it to the gem-faithful trio Jekyll.configuration(overrides) ->
// config Hash, Jekyll::Site.new(config).process, plus the convenience
// Jekyll.build(source, dest, overrides). A build or config failure surfaces as
// Jekyll::Error. Config values marshal between rbgo object.Values and the library's
// Ruby-shaped Go tree (Hash=map[string]any, Array=[]any, scalars).

// registerJekyll installs the Jekyll module (require "jekyll"), its Jekyll::Error
// class, the Jekyll::VERSION constant, the module methods Jekyll.configuration and
// Jekyll.build, and the Jekyll::Site class (Site.new(config) / #process / #source /
// #dest). A build or configuration failure raises Jekyll::Error.
func (vm *VM) registerJekyll() {
	mod := newClass("Jekyll", nil)
	mod.isModule = true
	vm.consts["Jekyll"] = mod

	std := vm.consts["StandardError"].(*RClass)
	errCls := newClass("Jekyll::Error", std)
	mod.consts["Error"] = errCls
	vm.consts["Jekyll::Error"] = errCls

	mod.consts["VERSION"] = object.NewString(jekyll.Version)

	// Jekyll.configuration(overrides = {}) resolves the site configuration — the
	// built-in defaults, deep-merged with _config.yml in the (override-selected)
	// source, then the override keys — and returns it as a Ruby Hash.
	mod.smethods["configuration"] = &Method{name: "configuration", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return jekyllConfigToHash(jekyllConfiguration(jekyllOverridesAt(args, 0)))
		}}

	// Jekyll.build(source, dest, overrides = {}) resolves the configuration for
	// source (overlaying source/destination and any overrides), builds the whole
	// site, and returns the destination path String.
	mod.smethods["build"] = &Method{name: "build", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 2 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
			}
			source := sassSourceArg(args[0])
			dest := sassSourceArg(args[1])
			over := jekyllOverridesAt(args, 2)
			over["source"] = source
			over["destination"] = dest
			jekyllBuild(jekyllConfiguration(over))
			return object.NewString(dest)
		}}

	vm.registerJekyllSite(mod)
}

// registerJekyllSite installs the Jekyll::Site class: Site.new(config) wraps a
// native site built from a configuration Hash, #process builds it, and #source /
// #dest expose the resolved directories.
func (vm *VM) registerJekyllSite(mod *RClass) {
	siteCls := newClass("Jekyll::Site", vm.cObject)
	mod.consts["Site"] = siteCls
	vm.consts["Jekyll::Site"] = siteCls

	// Jekyll::Site.new(config) builds a native site from a configuration Hash (as
	// produced by Jekyll.configuration), storing it as an opaque native handle.
	siteCls.smethods["new"] = &Method{name: "new", owner: siteCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			inst := &RObject{class: siteCls, ivars: map[string]object.Value{}}
			inst.ivars["@__site"] = &jekyllSite{s: jekyll.NewSite(jekyllConfigArg(args[0]))}
			return inst
		}}

	// Jekyll::Site#process reads, renders and writes the whole site, matching the
	// gem's Site#process. It returns nil; a failure raises Jekyll::Error.
	siteCls.define("process", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := jekyllSiteHandle(self).s.Build(); err != nil {
			raise("Jekyll::Error", "%s", err.Error())
		}
		return object.NilV
	})

	// Jekyll::Site#source / #dest expose the resolved source and destination paths.
	siteCls.define("source", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(jekyllSiteHandle(self).s.Source)
	})
	siteCls.define("dest", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(jekyllSiteHandle(self).s.Dest)
	})
}

// jekyllSite is the opaque native handle stored on a Jekyll::Site instance (as
// @__site) by new. It satisfies object.Value only so it can live in an ivar; it is
// never exposed to Ruby as a first-class value.
type jekyllSite struct{ s *jekyll.Site }

func (h *jekyllSite) ToS() string     { return "#<Jekyll::Site>" }
func (h *jekyllSite) Inspect() string { return h.ToS() }
func (h *jekyllSite) Truthy() bool    { return true }

// jekyllSiteHandle returns the native site handle stored on a Jekyll::Site instance
// by new. A receiver without one (never initialized) raises.
func jekyllSiteHandle(self object.Value) *jekyllSite {
	if h, ok := getIvar(self, "@__site").(*jekyllSite); ok {
		return h
	}
	raise("Jekyll::Error", "site was not initialized")
	return nil
}

// jekyllConfiguration resolves the site configuration for the override-selected
// source: it loads the defaults + _config.yml, then overlays the override keys. A
// config-load failure raises Jekyll::Error.
func jekyllConfiguration(over map[string]any) jekyll.Config {
	source := "."
	if s, ok := over["source"].(string); ok {
		source = s
	}
	cfg, err := jekyll.LoadConfig(source, nil)
	if err != nil {
		raise("Jekyll::Error", "%s", err.Error())
	}
	for k, v := range over {
		cfg[k] = v
	}
	return cfg
}

// jekyllBuild builds the whole site described by cfg, raising Jekyll::Error on any
// read/render/write failure.
func jekyllBuild(cfg jekyll.Config) {
	if err := jekyll.NewSite(cfg).Build(); err != nil {
		raise("Jekyll::Error", "%s", err.Error())
	}
}

// jekyllConfigArg coerces the Site.new argument to a jekyll.Config: it must be a
// configuration Hash, else TypeError.
func jekyllConfigArg(v object.Value) jekyll.Config {
	h, ok := v.(*object.Hash)
	if !ok {
		raise("TypeError", "Jekyll::Site.new expects a configuration Hash")
	}
	return jekyll.Config(jekyllHashToConfig(h))
}

// jekyllOverridesAt reads the override argument at idx as a config map. A missing or
// nil argument is the empty override ({}); a Hash is mapped key-by-key; any other
// value raises TypeError.
func jekyllOverridesAt(args []object.Value, idx int) map[string]any {
	if idx >= len(args) {
		return map[string]any{}
	}
	switch a := args[idx].(type) {
	case object.Nil:
		return map[string]any{}
	case *object.Hash:
		return jekyllHashToConfig(a)
	}
	raise("TypeError", "config overrides must be a Hash")
	return nil
}

// jekyllHashToConfig maps a Ruby Hash to a config map[string]any, rendering each key
// as its bare name so a config key resolves whether it was written as a Symbol or a
// String.
func jekyllHashToConfig(h *object.Hash) map[string]any {
	m := make(map[string]any, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		m[jekyllKey(k)] = jekyllValueToConfig(val)
	}
	return m
}

// jekyllKey renders a Ruby Hash key as its bare name (Symbol / String / to_s).
func jekyllKey(k object.Value) string {
	switch n := k.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return k.ToS()
}

// jekyllValueToConfig maps a Ruby value into the config value tree (nil / bool /
// int64 / float64 / string / []any / map[string]any). A Symbol coerces to its name.
// A value with no config shape raises TypeError.
func jekyllValueToConfig(v object.Value) any {
	switch x := v.(type) {
	case object.Nil:
		return nil
	case object.Bool:
		return bool(x)
	case object.Integer:
		return int64(x)
	case object.Float:
		return float64(x)
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	case *object.Array:
		out := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = jekyllValueToConfig(e)
		}
		return out
	case *object.Hash:
		return jekyllHashToConfig(x)
	default:
		return raise("TypeError", "cannot use a %s in a Jekyll config", v.Inspect())
	}
}

// jekyllConfigToHash converts a resolved jekyll.Config into a Ruby Hash for
// Jekyll.configuration.
func jekyllConfigToHash(cfg jekyll.Config) object.Value {
	h := object.NewHash()
	for k, v := range cfg {
		h.Set(object.NewString(k), jekyllAnyToValue(v))
	}
	return h
}

// jekyllAnyToValue converts a config value (a Ruby-shaped Go value from the library:
// nil / bool / int / int64 / float64 / string / []any / map[string]any) into an rbgo
// object.Value. Any other Go value (e.g. a YAML timestamp) renders via its %v form.
func jekyllAnyToValue(v any) object.Value {
	switch x := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(x)
	case int:
		return object.IntValue(int64(x))
	case int64:
		return object.IntValue(x)
	case float64:
		return object.Float(x)
	case string:
		return object.NewString(x)
	case []any:
		out := make([]object.Value, len(x))
		for i, e := range x {
			out[i] = jekyllAnyToValue(e)
		}
		return object.NewArrayFromSlice(out)
	case map[string]any:
		h := object.NewHash()
		for k, val := range x {
			h.Set(object.NewString(k), jekyllAnyToValue(val))
		}
		return h
	default:
		return object.NewString(fmt.Sprintf("%v", v))
	}
}
