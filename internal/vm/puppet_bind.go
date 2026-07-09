// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	puppet "github.com/go-ruby-puppet/puppet"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// puppetStrArg reads the single manifest-string argument common to Puppet.parse /
// Puppet.compile, raising ArgumentError when it is missing.
func puppetStrArg(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
	}
	return args[0].ToS()
}

// puppetCompile implements Puppet.compile(manifest[, facts:, node_name:,
// hiera_config:]): it builds the compile options from the keyword Hash and
// returns a wrapped catalog, raising Puppet::Error on failure.
func (vm *VM) puppetCompile(args []object.Value) object.Value {
	src := puppetStrArg(args)
	opts := puppet.CompileOptions{NodeName: "default"}
	if h := puppetKwHash(args[1:]); h != nil {
		if v, ok := puppetKwGet(h, "facts"); ok {
			if fh, ok := v.(*object.Hash); ok {
				if m, ok := rubyToGoValue(fh).(map[string]any); ok {
					opts.Facts = m
				}
			}
		}
		if v, ok := puppetKwGet(h, "node_name"); ok {
			opts.NodeName = v.ToS()
		}
		if v, ok := puppetKwGet(h, "hiera_config"); ok {
			opts.HieraConfig = v.ToS()
		}
	}
	cat, logs, err := puppet.Compile(src, opts)
	if err != nil {
		raise("Puppet::Error", "%s", err.Error())
	}
	return &PuppetCatalog{vm: vm, c: cat, logs: logs}
}

// registerPuppetCatalog installs the Puppet::Resource::Catalog instance methods.
func (vm *VM) registerPuppetCatalog(cls *RClass) {
	self := func(v object.Value) *PuppetCatalog { return v.(*PuppetCatalog) }

	cls.define("resources", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		rs := self(v).c.Resources()
		out := make([]object.Value, len(rs))
		for i, r := range rs {
			out[i] = &PuppetResource{r: r}
		}
		return object.NewArrayFromSlice(out)
	})
	cls.define("resource", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		r, ok := self(v).c.Resource(args[0].ToS())
		if !ok {
			return object.NilV
		}
		return &PuppetResource{r: r}
	})
	cls.define("edges", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		es := self(v).c.Edges()
		out := make([]object.Value, len(es))
		for i, e := range es {
			out[i] = object.NewArrayFromSlice([]object.Value{object.NewString(e[0]), object.NewString(e[1])})
		}
		return object.NewArrayFromSlice(out)
	})
	cls.define("to_json", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).c.JSON())
	})
	sizeFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).c.Size()))
	}
	cls.define("size", sizeFn)
	cls.define("length", sizeFn)
	cls.define("logs", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		logs := self(v).logs
		out := make([]object.Value, len(logs))
		for i, l := range logs {
			h := object.NewHash()
			h.Set(object.Symbol("level"), object.NewString(l.Level))
			h.Set(object.Symbol("message"), object.NewString(l.Message))
			out[i] = h
		}
		return object.NewArrayFromSlice(out)
	})
}

// registerPuppetResource installs the Puppet::Resource instance methods.
func (vm *VM) registerPuppetResource(cls *RClass) {
	self := func(v object.Value) *puppet.Resource { return v.(*PuppetResource).r }

	cls.define("type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Type)
	})
	cls.define("title", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Title)
	})
	cls.define("ref", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Ref())
	})
	cls.define("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Ref())
	})
	cls.define("parameters", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return facterValueToRuby(self(v).Parameters)
	})
	cls.define("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		p := self(v).Parameters
		val, ok := p[args[0].ToS()]
		if !ok {
			return object.NilV
		}
		return facterValueToRuby(val)
	})
	cls.define("tags", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return facterValueToRuby(self(v).Tags)
	})
}

// puppetKwHash returns the trailing Hash argument (keyword options), or nil.
func puppetKwHash(args []object.Value) *object.Hash {
	if n := len(args); n > 0 {
		if h, ok := args[n-1].(*object.Hash); ok {
			return h
		}
	}
	return nil
}

// puppetKwGet reads a keyword by symbol or string key from a kwargs Hash.
func puppetKwGet(h *object.Hash, key string) (object.Value, bool) {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}
