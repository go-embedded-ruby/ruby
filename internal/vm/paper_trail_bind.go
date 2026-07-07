// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	papertrail "github.com/go-ruby-paper-trail/paper-trail"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the has_paper_trail class macro and the create/update/destroy
// callback seams over the paper-trail library. has_paper_trail(only:/ignore:/on:/
// skip:) records the model's Config and installs the versioned instance surface —
// #save and #destroy (the callback points), #versions and #paper_trail. The
// attribute snapshot the library versions is the instance's @ivar map, read at
// each callback point.

// installHasPaperTrail defines the has_paper_trail class macro on Module, so any
// model class body can declare versioning (require "paper_trail"). It records the
// class's Config and installs the #save / #destroy callback points plus the
// #versions and #paper_trail readers.
func (vm *VM) installHasPaperTrail() {
	vm.cModule.define("has_paper_trail", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cls := self.(*RClass)
		vm.ptSetConfig(cls, ptBuildConfig(asKwargs(args)))
		vm.ptInstallModelMethods(cls)
		return object.NilV
	})
}

// ptSetConfig stores a model class's has_paper_trail Config and drops any cached
// Tracker so the next access rebuilds against the new Config.
func (vm *VM) ptSetConfig(cls *RClass, c papertrail.Config) {
	if vm.paperTrail.configs == nil {
		vm.paperTrail.configs = map[*RClass]papertrail.Config{}
	}
	vm.paperTrail.configs[cls] = c
	delete(vm.paperTrail.trackers, cls)
}

// ptConfigFor returns a model class's recorded Config (the zero Config — all
// events, no filters — when the class used a bare has_paper_trail).
func (vm *VM) ptConfigFor(cls *RClass) papertrail.Config {
	return vm.paperTrail.configs[cls]
}

// ptBuildConfig reads the has_paper_trail keyword options (only:/ignore:/skip:/on:)
// into a library Config. Each option is a Symbol/String or an Array of them.
func ptBuildConfig(kw *object.Hash) papertrail.Config {
	return papertrail.Config{
		Only:   ptNameList(kw, "only"),
		Ignore: ptNameList(kw, "ignore"),
		Skip:   ptNameList(kw, "skip"),
		On:     ptNameList(kw, "on"),
	}
}

// ptNameList reads a keyword option as a list of attribute/event names: absent ->
// nil, a single Symbol/String -> one name, an Array -> each element's name.
func ptNameList(kw *object.Hash, key string) []string {
	v, ok := kw.Get(object.Symbol(key))
	if !ok || !v.Truthy() {
		return nil
	}
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = arStr(e)
		}
		return out
	}
	return []string{arStr(v)}
}

// ptInstallModelMethods installs the versioned instance surface on a model class:
// #save and #destroy (the callback points that record a version) and the
// #versions / #paper_trail readers.
func (vm *VM) ptInstallModelMethods(cls *RClass) {
	cls.define("save", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.ptRecordSave(self.(*RObject), cls)
		return object.True
	})
	cls.define("save!", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.ptRecordSave(self.(*RObject), cls)
		return object.True
	})
	cls.define("destroy", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.ptRecordDestroy(self.(*RObject), cls)
		return self
	})
	cls.define("versions", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.ptVersionsFor(self.(*RObject), cls)
	})
	cls.define("paper_trail", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &PTProxy{inst: self.(*RObject), cls: cls}
	})
}

// ptRecordSave records a create (first save) or update (subsequent save) version
// from the instance's current attribute snapshot, inferring the event from
// whether the instance already has a recorded before-image.
func (vm *VM) ptRecordSave(inst *RObject, cls *RClass) {
	after := vm.ptSnapshot(inst)
	st := vm.ptStateFor(inst, after)
	if _, err := vm.ptTrackerFor(cls).Record(st.itemID, st.last, after); err != nil {
		raise("PaperTrail::Error", "%s", err.Error())
	}
	st.last = after
}

// ptRecordDestroy records a destroy version from the instance's before-image (its
// last recorded snapshot, or its current attributes if it was never saved) and
// resets the instance so a later save is a fresh create.
func (vm *VM) ptRecordDestroy(inst *RObject, cls *RClass) {
	before := vm.ptSnapshot(inst)
	st := vm.ptStateFor(inst, before)
	if st.last != nil {
		before = st.last
	}
	if _, err := vm.ptTrackerFor(cls).RecordDestroy(st.itemID, before); err != nil {
		raise("PaperTrail::Error", "%s", err.Error())
	}
	st.last = nil
}

// ptStateFor returns the per-instance record-keeping, creating it (and assigning a
// stable item id) on first use. The id is the instance's `id` attribute when it
// has a truthy one, else a synthetic per-VM sequence value.
func (vm *VM) ptStateFor(inst *RObject, attrs map[string]any) *ptInstanceState {
	if st, ok := vm.paperTrail.states[inst]; ok {
		return st
	}
	id := ptItemID(attrs["id"])
	if id == "" {
		vm.paperTrail.nextID++
		id = "pt-" + ptItemID(vm.paperTrail.nextID)
	}
	st := &ptInstanceState{itemID: id}
	vm.paperTrail.states[inst] = st
	return st
}

// ptSnapshot builds the model's attribute map from its @ivars: each `@name`
// becomes the attribute `name`, its value mapped into the library's generic Go
// value model. This is the snapshot seam read at every callback point.
func (vm *VM) ptSnapshot(inst *RObject) map[string]any {
	attrs := map[string]any{}
	for name, val := range inst.ivars {
		attrs[strings.TrimPrefix(name, "@")] = goOfRuby(val)
	}
	return attrs
}
