// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	friendlyid "github.com/go-ruby-friendly-id/friendly-id"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the FriendlyId slug macro (require "friendly_id"): the
// `extend FriendlyId` / `friendly_id :title, use: [:slugged, :history]` surface
// over github.com/go-ruby-friendly-id/friendly-id. The library owns everything
// portable — the ActiveSupport parameterize/normalize semantics, candidate
// generation with collision suffixing, the optional slug history and the
// friendly finder — and leaves the three host-specific parts as injectable
// seams. This binding wires them onto rbgo's object model (see friendly_id_bind.go):
//
//   - the base attribute is read with `send(record, base_field)`;
//   - uniqueness is a query against the model's slug column — the ActiveRecord
//     `where(slug:).exists?` path when the model responds to `where`, otherwise
//     the reference in-memory store;
//   - :history records old slugs in that same reference store (the MemStore the
//     library ships), so `Model.friendly.find(old_slug)` resolves.
//
// On save the binding generates a unique slug from the base attribute and writes
// it to `#slug`, regenerating only when the base attribute changed (or the slug
// is still blank). `Model.friendly.find(slug_or_id)` resolves a current slug,
// then a historical one, then a raw id; `#to_param` returns the slug.

// friendlyIDScope is the object `Model.friendly` returns: a thin handle carrying
// the model class, whose #find runs the library's Finder. classOf reports it as
// FriendlyId::FinderMethods so #find dispatches.
type friendlyIDScope struct {
	model *RClass
	cls   *RClass
}

func (s *friendlyIDScope) ToS() string     { return "#<FriendlyId::FinderMethods>" }
func (s *friendlyIDScope) Inspect() string { return s.ToS() }
func (s *friendlyIDScope) Truthy() bool    { return true }

// registerFriendlyId installs the FriendlyId module, its RecordNotFound error and
// FinderMethods scope class, and the two macros a model picks up through
// `extend FriendlyId`: the class-level `friendly_id` declaration and the
// `friendly` scope reader. Both are defined as instance methods of the module so
// `extend FriendlyId` turns them into class methods of the model (matching the
// gem), with `self` the model class inside each.
func (vm *VM) registerFriendlyId() {
	mod := newClass("FriendlyId", nil)
	mod.isModule = true
	vm.consts["FriendlyId"] = mod

	// FriendlyId::RecordNotFound < StandardError — the finder's miss (the gem
	// raises ActiveRecord::RecordNotFound; we keep a self-contained analogue so
	// the finder works without active_record loaded).
	std := vm.consts["StandardError"].(*RClass)
	nf := newClass("FriendlyId::RecordNotFound", std)
	mod.consts["RecordNotFound"] = nf
	vm.consts["FriendlyId::RecordNotFound"] = nf

	// FriendlyId::FinderMethods carries #find for the scope object.
	fm := newClass("FriendlyId::FinderMethods", vm.cObject)
	mod.consts["FinderMethods"] = fm
	vm.consts["FriendlyId::FinderMethods"] = fm
	fm.define("find", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		scope := self.(*friendlyIDScope)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return vm.fidFind(scope.model, fidStr(args[0]))
	})

	// friendly_id(base, use:, slug_column:): declare the sluggable base attribute
	// and options, storing the per-model config and installing the instance
	// methods (#slug accessors, #to_param, #set_friendly_id_slug, save hooks).
	mod.define("friendly_id", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cls, ok := self.(*RClass)
		if !ok {
			raise("TypeError", "friendly_id must be declared on a class")
		}
		vm.fidDeclare(cls, args)
		return object.NilV
	})

	// friendly: the scope object whose #find resolves a slug / old slug / raw id.
	mod.define("friendly", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		cls, ok := self.(*RClass)
		if !ok {
			raise("TypeError", "friendly must be called on a class")
		}
		return &friendlyIDScope{model: cls, cls: vm.consts["FriendlyId::FinderMethods"].(*RClass)}
	})
}

// fidDeclare parses a friendly_id declaration and installs the model's slug
// config + instance methods. base is the first non-Hash argument; the trailing
// options Hash supplies use: (an Array/Symbol enabling :slugged/:history) and an
// optional slug_column: override (default "slug").
func (vm *VM) fidDeclare(cls *RClass, args []object.Value) {
	var base string
	var opts *object.Hash
	for _, a := range args {
		if h, ok := a.(*object.Hash); ok {
			opts = h
			continue
		}
		if base == "" {
			base = arStr(a)
		}
	}
	if base == "" {
		raise("ArgumentError", "friendly_id requires a base attribute")
	}

	slugField := "slug"
	history := false
	if opts != nil {
		if v, ok := opts.Get(object.Symbol("use")); ok {
			_, history = fidUseFlags(v)
		}
		if v, ok := opts.Get(object.Symbol("slug_column")); ok {
			slugField = arStr(v)
		}
	}

	st := &fidState{
		model:     cls,
		base:      base,
		slugField: slugField,
		history:   history,
		cfg:       friendlyid.Config{Reserved: []string{"new", "edit"}},
		store:     friendlyid.NewMemStore(cls.name),
		records:   map[string]object.Value{},
	}
	if vm.fidConfigs == nil {
		vm.fidConfigs = map[*RClass]*fidState{}
	}
	vm.fidConfigs[cls] = st
	vm.fidInstallMethods(cls, st)
}
