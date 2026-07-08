// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"sort"

	inflector "github.com/go-ruby-activesupport/activesupport/inflector"
	factorybot "github.com/go-ruby-factory-bot/factory-bot"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between the deterministic factory engine in
// github.com/go-ruby-factory-bot/factory-bot and rbgo's object model. It wires
// the two object-model seams factory_bot delegates to the interpreter —
// instantiating a class + assigning attributes (BuildFunc) and persisting a
// built object (PersistFunc) — plus the Ruby-block seams (a dynamic attribute
// body, a sequence generator, and the after/before callbacks), and it converts
// values across the `any`/object.Value boundary and parses the build/create call
// options. Every value the engine holds is an object.Value (or a Go nil for an
// unresolved sibling read); no other Go-native ever crosses the seam.

// factoryBotWireSeams installs the BuildFunc / PersistFunc seams on reg so the
// engine constructs and persists real Ruby objects through this VM.
func (vm *VM) factoryBotWireSeams(reg *factorybot.Registry) {
	reg.SetBuild(vm.factoryBotBuild)
	reg.SetPersist(vm.factoryBotPersist)
}

// factoryBotBuild is the BuildFunc seam: it resolves the class name to a Ruby
// class, instantiates it with a no-argument `new`, and assigns each resolved
// non-transient attribute — through its writer (`attr=`) when the object answers
// to one, else by setting the matching instance variable. Attributes are applied
// in a stable (sorted) order so a build is deterministic.
func (vm *VM) factoryBotBuild(class string, attrs map[string]any) (any, error) {
	cls := vm.factoryBotResolveClass(class)
	obj := vm.send(cls, "new", nil, nil)
	for _, name := range fbSortedKeys(attrs) {
		val := fbToRuby(attrs[name])
		if setter := name + "="; vm.findMethod(obj, setter) != nil {
			vm.send(obj, setter, []object.Value{val}, nil)
		} else {
			setIvar(obj, "@"+name, val)
		}
	}
	return object.Value(obj), nil
}

// factoryBotPersist is the PersistFunc seam (factory_bot's to_create, default
// save!): it sends save! (or save) to the built object, and is a no-op when the
// object answers to neither — so create works against plain Ruby objects.
func (vm *VM) factoryBotPersist(_ string, obj any) error {
	o := obj.(object.Value)
	switch {
	case vm.findMethod(o, "save!") != nil:
		vm.send(o, "save!", nil, nil)
	case vm.findMethod(o, "save") != nil:
		vm.send(o, "save", nil, nil)
	}
	return nil
}

// factoryBotResolveClass maps a class name to its Ruby class: the literal name
// first (an explicit `class: "User"`), then its camelized form (the default,
// where the factory name `:user` maps to the constant User). A name that
// resolves to no class raises NameError, exactly as referencing the missing
// constant would.
func (vm *VM) factoryBotResolveClass(class string) *RClass {
	if c, ok := vm.consts[class].(*RClass); ok {
		return c
	}
	camel := inflector.Camelize(class)
	if c, ok := vm.consts[camel].(*RClass); ok {
		return c
	}
	raise("NameError", "uninitialized constant %s", camel)
	return nil
}

// factoryBotBlockFn wraps a Ruby dynamic-attribute block as the library's Block
// seam: it runs the block against a fresh evaluator (so a bare sibling name
// resolves through method_missing, and `|e|` receives it) and returns the
// resulting Ruby value.
func (vm *VM) factoryBotBlockFn(blk *Proc) factorybot.Block {
	return func(e *factorybot.Evaluator) any {
		ev := &FactoryBotEvaluator{e: e}
		return object.Value(vm.callBlockSelf(blk, ev, []object.Value{ev}))
	}
}

// factoryBotSeqGen wraps a Ruby sequence block as the library's generator: with
// a block, `sequence(:email) { |n| … }` yields the counter; with none,
// `sequence(:email)` yields the raw counter as an Integer.
func (vm *VM) factoryBotSeqGen(blk *Proc) func(n int) any {
	if blk == nil {
		return func(n int) any { return object.Value(object.Integer(n)) }
	}
	return func(n int) any {
		return object.Value(vm.callBlock(blk, []object.Value{object.Integer(n)}))
	}
}

// factoryBotRunBody runs a factory/trait/transient/nested-factory block against a
// proxy over its definition, so bare attribute declarations configure it. A nil
// block (e.g. `trait :x` with no body) is a no-op.
func (vm *VM) factoryBotRunBody(d *factorybot.Definition, blk *Proc) {
	if blk == nil {
		return
	}
	proxy := &FactoryBotProxy{d: d}
	vm.callBlockSelf(blk, proxy, []object.Value{proxy})
}

// factoryBotApplyOpts applies the `class:` and `parent:` options of a
// factory/nested-factory declaration to its definition.
func (vm *VM) factoryBotApplyOpts(d *factorybot.Definition, h *object.Hash) {
	if h == nil {
		return
	}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		switch nameArg(k) {
		case "class":
			d.Class(vm.factoryBotClassName(val))
		case "parent":
			d.Parent(nameArg(val))
		}
	}
}

// factoryBotClassName resolves a `class:` option value to a class name: a Class
// yields its name, a String / Symbol its text.
func (vm *VM) factoryBotClassName(v object.Value) string {
	if c, ok := v.(*RClass); ok {
		return c.ToS()
	}
	return nameArg(v)
}

// factoryBotDefineFactory registers a top-level `factory :name[, opts] do … end`
// into the VM registry, raising on a duplicate (or otherwise invalid) definition.
func (vm *VM) factoryBotDefineFactory(args []object.Value, blk *Proc) {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
	}
	name := nameArg(args[0])
	h := lastHashOrNil(args)
	err := vm.factoryBotReg.Define(name, func(d *factorybot.Definition) {
		vm.factoryBotApplyOpts(d, h)
		vm.factoryBotRunBody(d, blk)
	})
	if err != nil {
		vm.factoryBotRaise(err)
	}
}

// factoryBotCallback registers an after(:build|:create) / before(:create)
// callback: the Ruby block receives the built object and the evaluator. kind is
// "after" or "before"; the event symbol (defaulting to :build) selects the hook.
func (vm *VM) factoryBotCallback(d *factorybot.Definition, kind string, args []object.Value, blk *Proc) {
	if blk == nil {
		raise("LocalJumpError", "no block given (yield)")
	}
	event := "build"
	if len(args) > 0 {
		event = nameArg(args[0])
	}
	cb := func(obj any, e *factorybot.Evaluator) error {
		ev := &FactoryBotEvaluator{e: e}
		vm.callBlock(blk, []object.Value{fbToRuby(obj), ev})
		return nil
	}
	switch {
	case kind == "before":
		d.BeforeCreate(cb)
	case event == "create":
		d.AfterCreate(cb)
	default:
		d.AfterBuild(cb)
	}
}

// factoryBotOne runs build/create for a single object:
// `FactoryBot.build(:user, :admin, name: "x")`.
func (vm *VM) factoryBotOne(args []object.Value, create bool) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
	}
	name := nameArg(args[0])
	opts := factoryBotCallOpts(args[1:])
	var (
		obj any
		err error
	)
	if create {
		obj, err = vm.factoryBotReg.Create(name, opts...)
	} else {
		obj, err = vm.factoryBotReg.Build(name, opts...)
	}
	if err != nil {
		vm.factoryBotRaise(err)
	}
	return fbToRuby(obj)
}

// factoryBotList runs build_list/create_list:
// `FactoryBot.build_list(:user, 3, :admin, name: "x")`.
func (vm *VM) factoryBotList(args []object.Value, create bool) object.Value {
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2..)", len(args))
	}
	name := nameArg(args[0])
	n := int(fbInt(args[1]))
	opts := factoryBotCallOpts(args[2:])
	var (
		list []any
		err  error
	)
	if create {
		list, err = vm.factoryBotReg.CreateList(name, n, opts...)
	} else {
		list, err = vm.factoryBotReg.BuildList(name, n, opts...)
	}
	if err != nil {
		vm.factoryBotRaise(err)
	}
	elems := make([]object.Value, len(list))
	for i, o := range list {
		elems[i] = fbToRuby(o)
	}
	return object.NewArrayFromSlice(elems)
}

// factoryBotAttributesFor returns the resolved non-transient, non-association
// attributes as a Ruby Hash keyed by Symbol (factory_bot's attributes_for).
func (vm *VM) factoryBotAttributesFor(args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
	}
	name := nameArg(args[0])
	opts := factoryBotCallOpts(args[1:])
	m, err := vm.factoryBotReg.AttributesFor(name, opts...)
	if err != nil {
		vm.factoryBotRaise(err)
	}
	h := object.NewHash()
	for _, k := range fbSortedKeys(m) {
		h.Set(object.SymVal(k), fbToRuby(m[k]))
	}
	return h
}

// factoryBotCallOpts parses the trailing arguments of a build/create call into
// engine options: positional Symbols/Strings become traits and a trailing Hash
// becomes attribute overrides.
func factoryBotCallOpts(args []object.Value) []factorybot.Opt {
	var opts []factorybot.Opt
	positional := args
	h := lastHashOrNil(args)
	if h != nil {
		positional = args[:len(args)-1]
	}
	for _, a := range positional {
		opts = append(opts, factorybot.WithTrait(nameArg(a)))
	}
	if h != nil {
		m := map[string]any{}
		for _, k := range h.Keys {
			val, _ := h.Get(k)
			m[nameArg(k)] = fbStore(val)
		}
		opts = append(opts, factorybot.WithAttrs(m))
	}
	return opts
}

// factoryBotAssocOpts parses an association's arguments (after its name) into a
// factory name, traits, and attribute overrides: positional Symbols are traits,
// the trailing Hash's `factory:` names the target factory and its remaining keys
// are overrides.
func factoryBotAssocOpts(args []object.Value) (factoryName string, traits []string, over map[string]any) {
	over = map[string]any{}
	positional := args
	h := lastHashOrNil(args)
	if h != nil {
		positional = args[:len(args)-1]
	}
	for _, a := range positional {
		traits = append(traits, nameArg(a))
	}
	if h != nil {
		for _, k := range h.Keys {
			val, _ := h.Get(k)
			if key := nameArg(k); key == "factory" {
				factoryName = nameArg(val)
			} else {
				over[key] = fbStore(val)
			}
		}
	}
	return factoryName, traits, over
}

// factoryBotSeqArgs parses a sequence declaration's arguments into its name and
// starting counter (default 1): `sequence(:email)` / `sequence(:email, 1000)`.
func factoryBotSeqArgs(args []object.Value) (string, int64) {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
	}
	start := int64(1)
	if len(args) >= 2 {
		start = fbInt(args[1])
	}
	return nameArg(args[0]), start
}

// factoryBotRaise maps a library error to a Ruby exception: a missing factory,
// trait or sequence to KeyError, everything else (duplicate, parent/association
// cycle, sequence overflow) to ArgumentError, carrying the library's message.
func (vm *VM) factoryBotRaise(err error) {
	switch {
	case fbIsAny(err, factorybot.ErrUnknownFactory, factorybot.ErrUnknownTrait, factorybot.ErrUnknownSequence):
		raise("KeyError", "%s", err.Error())
	default:
		raise("ArgumentError", "%s", err.Error())
	}
}

// fbIsAny reports whether err matches any of the sentinel targets.
func fbIsAny(err error, targets ...error) bool {
	for _, t := range targets {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

// fbStore records a Ruby value as the engine's `any`. Every value the engine
// holds is an object.Value, so storage is a plain widening.
func fbStore(v object.Value) any { return v }

// fbToRuby narrows an engine value back to a Ruby value: a Go nil (an
// unresolved sibling read) becomes nil, otherwise it is the object.Value the
// seam stored.
func fbToRuby(v any) object.Value {
	if v == nil {
		return object.NilV
	}
	return v.(object.Value)
}

// fbInt coerces an Integer argument (a sequence start / list count), raising
// TypeError otherwise.
func fbInt(v object.Value) int64 {
	if n, ok := v.(object.Integer); ok {
		return int64(n)
	}
	raise("TypeError", "no implicit conversion of %s into Integer", v.Inspect())
	return 0
}

// fbSortedKeys returns m's keys in a stable order for deterministic iteration.
func fbSortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
