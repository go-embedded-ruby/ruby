// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"

	friendlyid "github.com/go-ruby-friendly-id/friendly-id"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file holds the Go<->Ruby bridges for the FriendlyId surface installed in
// friendly_id.go: the per-model state, the injectable seams (base-attribute read,
// slug-column uniqueness, slug history) and the pure slug-generation / finder
// logic that drives the library.

// fidState is one model's friendly_id configuration plus the reference slug store
// and the in-process record cache the finder resolves against. The MemStore is
// the library's own reference implementation of the uniqueness + history seams;
// records maps a resolved record id to the Ruby object that owns it so
// Model.friendly.find returns the record.
type fidState struct {
	model     *RClass
	base      string
	slugField string
	history   bool
	cfg       friendlyid.Config
	store     *friendlyid.MemStore
	records   map[string]object.Value
	counter   int
}

// fidStr renders a Ruby value as the plain Go string the slug engine consumes: a
// String is its bytes, a Symbol its name, nil the empty string, anything else its
// #to_s. It is the reader behind every seam value.
func fidStr(v object.Value) string {
	if object.IsNil(v) {
		return ""
	}
	switch s := v.(type) {
	case *object.String:
		return s.Str()
	case object.Symbol:
		return string(s)
	}
	return v.ToS()
}

// fidUseFlags reads the friendly_id use: option — an Array of module names or a
// single Symbol/String — into the (slugged, history) flags. :slugged is the
// default module; :history enables the old-slug store the finder consults.
func fidUseFlags(v object.Value) (slugged, history bool) {
	slugged = true
	var names []string
	if arr, ok := v.(*object.Array); ok {
		for _, e := range arr.Elems {
			names = append(names, fidStr(e))
		}
	} else {
		names = append(names, fidStr(v))
	}
	for _, n := range names {
		switch n {
		case "slugged":
			slugged = true
		case "history":
			history = true
		}
	}
	return slugged, history
}

// fidShouldGenerate mirrors friendly_id's should_generate_new_friendly_id?: a
// slug is (re)generated when it is still blank or when the base attribute changed
// since it was last recorded.
func fidShouldGenerate(oldSlug string, baseChanged bool) bool {
	return oldSlug == "" || baseChanged
}

// fidInstallMethods installs the sluggable instance methods on a model class: the
// #slug accessors (only when the class does not already supply them, e.g. as an
// ActiveRecord column), #to_param, the explicit #set_friendly_id_slug hook, and
// save / save! wrappers that generate the slug before delegating to any prior
// definition.
func (vm *VM) fidInstallMethods(cls *RClass, st *fidState) {
	field := st.slugField
	if lookupMethod(cls, field) == nil {
		cls.define(field, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return getIvar(self, "@"+field)
		})
	}
	if lookupMethod(cls, field+"=") == nil {
		cls.define(field+"=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			v := object.Value(object.NilV)
			if len(args) > 0 {
				v = args[0]
			}
			setIvar(self, "@"+field, v)
			return v
		})
	}

	// #to_param returns the slug, falling back to the record id (as ActiveRecord
	// does) when no slug has been assigned.
	cls.define("to_param", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if s := fidStr(vm.send(self, st.slugField, nil, nil)); s != "" {
			return object.NewString(s)
		}
		return object.NewString(vm.fidRecordID(self, st))
	})

	// #set_friendly_id_slug is the explicit hook (friendly_id's set_slug); the
	// save wrappers call the same generator.
	cls.define("set_friendly_id_slug", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.fidGenerate(self, st)
		return object.NilV
	})

	priorSave := lookupMethod(cls, "save")
	cls.define("save", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		vm.fidGenerate(self, st)
		if priorSave != nil {
			return vm.invoke(priorSave, self, args, blk)
		}
		return object.Bool(true)
	})

	priorSaveBang := lookupMethod(cls, "save!")
	cls.define("save!", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		vm.fidGenerate(self, st)
		if priorSaveBang != nil {
			return vm.invoke(priorSaveBang, self, args, blk)
		}
		return object.Bool(true)
	})
}

// fidGenerate runs the before-save slug generation: read the base attribute
// (send seam), decide whether to (re)generate, resolve a unique slug against the
// uniqueness seam, write it to the slug column (send seam) and record it in the
// reference store so the finder and the :history module resolve it.
func (vm *VM) fidGenerate(self object.Value, st *fidState) {
	base := fidStr(vm.send(self, st.base, nil, nil))
	oldSlug := fidStr(vm.send(self, st.slugField, nil, nil))
	baseChanged := fidStr(getIvar(self, "@_friendly_id_base")) != base
	if !fidShouldGenerate(oldSlug, baseChanged) {
		return
	}

	id := vm.fidRecordID(self, st)
	slug, err := st.cfg.Resolve(base, func(candidate string) bool {
		return vm.fidExists(st, candidate)
	})
	if err != nil {
		raise("ArgumentError", "friendly_id: %s", err.Error())
	}

	vm.send(self, st.slugField+"=", []object.Value{object.NewString(slug)}, nil)
	// The MemStore keeps every slug a record ever held, so a later regeneration
	// leaves the old slug in history for the :history finder. A store collision
	// cannot happen here (Resolve already skipped taken slugs), so its error is
	// discarded.
	_ = st.store.Assign(id, slug)
	st.records[id] = self
	setIvar(self, "@_friendly_id_base", object.NewString(base))
}

// fidExists is the uniqueness seam: a query against the model's slug column. When
// the model responds to `where` (an ActiveRecord model) it runs
// `where(slug: candidate).exists?`; otherwise it consults the reference store's
// current slugs.
func (vm *VM) fidExists(st *fidState, candidate string) bool {
	if lookupSMethod(st.model, "where") != nil {
		h := object.NewHash()
		h.Set(object.Symbol(st.slugField), object.NewString(candidate))
		rel := vm.send(st.model, "where", []object.Value{h}, nil)
		return vm.send(rel, "exists?", nil, nil).Truthy()
	}
	return st.store.Exists(candidate)
}

// fidRecordID resolves the record's primary key for the store: its #id when the
// model exposes one, otherwise a stable synthesized id memoized on the record so
// history and the finder stay consistent for a PORO with no id column.
func (vm *VM) fidRecordID(self object.Value, st *fidState) string {
	if vm.respondsTo(self, "id") {
		if v := vm.send(self, "id", nil, nil); !object.IsNil(v) {
			if s := fidStr(v); s != "" {
				return s
			}
		}
	}
	if rid := getIvar(self, "@_friendly_id_rid"); !object.IsNil(rid) {
		return fidStr(rid)
	}
	st.counter++
	id := fmt.Sprintf("fid-%d", st.counter)
	setIvar(self, "@_friendly_id_rid", object.NewString(id))
	return id
}

// fidFind runs the library's Finder for Model.friendly.find(input): resolve a
// current slug, then (when :history is on) an old slug, then a raw id, and return
// the cached record — raising FriendlyId::RecordNotFound on a miss.
func (vm *VM) fidFind(model *RClass, input string) object.Value {
	st := vm.fidConfigs[model]
	finder := friendlyid.Finder{
		BySlug: func(slug string) (string, bool) { return st.store.CurrentID(slug) },
		ByID:   func(id string) bool { _, ok := st.records[id]; return ok },
	}
	if st.history {
		finder.ByHistory = func(slug string) (string, bool) { return st.store.HistoryID(slug) }
	}
	id, err := finder.Find(input)
	if err != nil {
		raise("FriendlyId::RecordNotFound", "can't find record with friendly id: %q", input)
	}
	return st.records[id]
}
