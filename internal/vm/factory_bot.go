// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	factorybot "github.com/go-ruby-factory-bot/factory-bot"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the FactoryBot module and its define/build DSL
// (require "factory_bot"). The deterministic factory engine — the registry,
// attribute resolution, sequences, traits, associations, transient attributes,
// parent/child inheritance and the build/create callback pipeline — lives in
// github.com/go-ruby-factory-bot/factory-bot. rbgo owns only the two object-model
// seams that factory_bot delegates to the interpreter: instantiating a class
// and assigning attributes (BuildFunc), and persisting a built object
// (PersistFunc), plus the Ruby-block seams (a dynamic `attr { … }` body, a
// sequence generator, and the after/before callbacks). Those seams are wired in
// factory_bot_bind.go; this file is the class/module surface and the DSL proxies
// the blocks run against.

// FactoryBotProxy is the self a `factory`/`trait`/`transient`/nested-`factory`
// block runs against (factory_bot's FactoryBot::DefinitionProxy). Its
// method_missing turns a bare `name "value"` into a static attribute and a
// `name { … }` into a dynamic one, while the explicit DSL methods (sequence,
// association, trait, transient, factory, after, before) configure the wrapped
// library *Definition directly.
type FactoryBotProxy struct{ d *factorybot.Definition }

func (p *FactoryBotProxy) ToS() string     { return "#<FactoryBot::DefinitionProxy>" }
func (p *FactoryBotProxy) Inspect() string { return p.ToS() }
func (p *FactoryBotProxy) Truthy() bool    { return true }

// FactoryBotTopProxy is the self a `FactoryBot.define do … end` block runs
// against: it exposes the top-level `factory` and (global) `sequence`
// declarations, registering each into the VM's factory registry.
type FactoryBotTopProxy struct{ vm *VM }

func (p *FactoryBotTopProxy) ToS() string     { return "#<FactoryBot::Syntax::Default::DSL>" }
func (p *FactoryBotTopProxy) Inspect() string { return p.ToS() }
func (p *FactoryBotTopProxy) Truthy() bool    { return true }

// FactoryBotEvaluator is the self a dynamic attribute block or a callback reads
// sibling / transient attributes through (factory_bot's evaluator). Its
// method_missing resolves a bare sibling name to the memoized attribute value,
// and it is also handed to the block as its parameter (`name { |e| e.first }`).
type FactoryBotEvaluator struct{ e *factorybot.Evaluator }

func (e *FactoryBotEvaluator) ToS() string     { return "#<FactoryBot::Evaluator>" }
func (e *FactoryBotEvaluator) Inspect() string { return e.ToS() }
func (e *FactoryBotEvaluator) Truthy() bool    { return true }

// registerFactoryBot installs the FactoryBot module (require "factory_bot"):
// FactoryBot.define registers factories/sequences into the VM's registry, and
// build/create/attributes_for/build_list/create_list/generate/reload run the
// engine through the object-model seams wired in registerFactoryBot's SetBuild /
// SetPersist calls. The registry is per-VM so factories never leak across
// interpreters.
func (vm *VM) registerFactoryBot() {
	reg := factorybot.New()
	vm.factoryBotReg = reg
	vm.factoryBotWireSeams(reg)

	mod := newClass("FactoryBot", nil)
	mod.isModule = true
	vm.consts["FactoryBot"] = mod

	vm.registerFactoryBotProxies(mod)

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// FactoryBot.define { … } runs the block against the top-level DSL proxy so
	// bare `factory`/`sequence` declarations register into the VM registry.
	sm("define", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		proxy := &FactoryBotTopProxy{vm: vm}
		vm.callBlockSelf(blk, proxy, []object.Value{proxy})
		return object.NilV
	})

	// build/create/attributes_for run one object; build_list/create_list run n.
	sm("build", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.factoryBotOne(args, false)
	})
	sm("create", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.factoryBotOne(args, true)
	})
	sm("attributes_for", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.factoryBotAttributesFor(args)
	})
	sm("build_list", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.factoryBotList(args, false)
	})
	sm("create_list", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.factoryBotList(args, true)
	})

	// generate advances a global sequence (FactoryBot.generate(:email)).
	sm("generate", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		v, err := vm.factoryBotReg.Generate(nameArg(args[0]))
		if err != nil {
			vm.factoryBotRaise(err)
		}
		return fbToRuby(v)
	})

	// reload/reset clear the registry (FactoryBot.reload in a test suite).
	reload := func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		fresh := factorybot.New()
		vm.factoryBotReg = fresh
		vm.factoryBotWireSeams(fresh)
		return object.NilV
	}
	sm("reload", reload)
	sm("reset", reload)
}

// registerFactoryBotProxies installs the three DSL proxy classes and their
// methods: the top-level define proxy, the factory/trait/transient proxy, and
// the evaluator. They are exposed under the FactoryBot namespace so classOf maps
// each wrapper value onto its class for dispatch.
func (vm *VM) registerFactoryBotProxies(mod *RClass) {
	// Top-level define proxy: factory + global sequence.
	top := newClass("FactoryBot::Syntax::Default::DSL", vm.cObject)
	mod.consts["Syntax::Default::DSL"] = top
	vm.consts["FactoryBot::Syntax::Default::DSL"] = top

	top.define("factory", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		vm.factoryBotDefineFactory(args, blk)
		return object.NilV
	})
	top.define("sequence", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		name, start := factoryBotSeqArgs(args)
		vm.factoryBotReg.SequenceFrom(name, start, vm.factoryBotSeqGen(blk))
		return object.NilV
	})

	// Definition proxy: the body of a factory / trait / transient / nested factory.
	proxy := newClass("FactoryBot::DefinitionProxy", vm.cObject)
	mod.consts["DefinitionProxy"] = proxy
	vm.consts["FactoryBot::DefinitionProxy"] = proxy
	vm.registerFactoryBotProxyMethods(proxy)

	// Evaluator: sibling/transient reads inside a dynamic block or callback.
	ev := newClass("FactoryBot::Evaluator", vm.cObject)
	mod.consts["Evaluator"] = ev
	vm.consts["FactoryBot::Evaluator"] = ev
	ev.define("method_missing", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		e := self.(*FactoryBotEvaluator)
		return fbToRuby(e.e.Get(nameArg(args[0])))
	})
	ev.define("respond_to_missing?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(true)
	})
}

// registerFactoryBotProxyMethods installs the DefinitionProxy DSL: method_missing
// (bare attribute declarations) plus the explicit declarations that need their
// own shape (sequence, association, trait, transient, nested factory, and the
// after/before callbacks).
func (vm *VM) registerFactoryBotProxyMethods(proxy *RClass) {
	self := func(v object.Value) *FactoryBotProxy { return v.(*FactoryBotProxy) }

	// method_missing declares an attribute: `name { … }` is dynamic, `name value`
	// is static, and a bare `name` with neither is a static nil.
	proxy.define("method_missing", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		d := self(v).d
		key := nameArg(args[0])
		rest := args[1:]
		switch {
		case blk != nil:
			d.Dynamic(key, vm.factoryBotBlockFn(blk))
		case len(rest) >= 1:
			d.Attr(key, fbStore(rest[0]))
		default:
			d.Attr(key, fbStore(object.NilV))
		}
		return object.NilV
	})
	proxy.define("respond_to_missing?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(true)
	})

	// sequence(:name[, start]) { |n| … } declares a per-factory sequence.
	proxy.define("sequence", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		name, start := factoryBotSeqArgs(args)
		self(v).d.SequenceFrom(name, start, vm.factoryBotSeqGen(blk))
		return object.NilV
	})

	// association(:name, factory: :other, trait, …, key: value) declares an association.
	proxy.define("association", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		name := nameArg(args[0])
		factoryName, traits, over := factoryBotAssocOpts(args[1:])
		self(v).d.Association(name, factoryName, over, traits...)
		return object.NilV
	})

	// trait(:name) { … } overlays attributes/callbacks; the body runs against a
	// fresh proxy over the trait's definition.
	proxy.define("trait", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		name := nameArg(args[0])
		self(v).d.Trait(name, func(sub *factorybot.Definition) {
			vm.factoryBotRunBody(sub, blk)
		})
		return object.NilV
	})

	// transient { … } declares transient attributes (available to blocks/callbacks
	// but never passed to the BuildFunc seam).
	proxy.define("transient", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		self(v).d.Transient(func(sub *factorybot.Definition) {
			vm.factoryBotRunBody(sub, blk)
		})
		return object.NilV
	})

	// factory(:child[, opts]) { … } declares a nested (inheriting) factory.
	proxy.define("factory", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		name := nameArg(args[0])
		self(v).d.Factory(name, func(sub *factorybot.Definition) {
			vm.factoryBotApplyOpts(sub, lastHashOrNil(args))
			vm.factoryBotRunBody(sub, blk)
		})
		return object.NilV
	})

	// after(:build|:create) / before(:create) register lifecycle callbacks.
	proxy.define("after", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		vm.factoryBotCallback(self(v).d, "after", args, blk)
		return object.NilV
	})
	proxy.define("before", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		vm.factoryBotCallback(self(v).d, "before", args, blk)
		return object.NilV
	})
}
