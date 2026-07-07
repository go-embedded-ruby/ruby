// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the method surface of the kaminari paginators: the shared
// page-metadata methods on both PaginatableArray and PaginatableRelation, the
// chainable page/per/padding + records methods on each, Array#page, and the
// #page entry points on the ActiveRecord::Base / Model / Relation surface.

// installKaminariMeta binds the read-only page-metadata methods common to both
// paginators onto cls, reading through scope. current_page / total_pages /
// total_count / limit_value / offset_value / current_per_page report Integers;
// first_page? / last_page? / out_of_range? report booleans; prev_page / next_page
// report an Integer or nil; page_entries_info renders the entries sentence.
func (vm *VM) installKaminariMeta(cls *RClass, scope func(object.Value) kaminariScope) {
	cls.define("current_page", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(scope(v).CurrentPage()))
	})
	cls.define("total_pages", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(scope(v).TotalPages()))
	})
	cls.define("total_count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(scope(v).TotalCount()))
	})
	cls.define("limit_value", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(scope(v).LimitValue()))
	})
	cls.define("offset_value", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(scope(v).OffsetValue()))
	})
	cls.define("current_per_page", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(scope(v).CurrentPerPage()))
	})
	cls.define("first_page?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(scope(v).FirstPage())
	})
	cls.define("last_page?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(scope(v).LastPage())
	})
	cls.define("out_of_range?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(scope(v).OutOfRange())
	})
	cls.define("prev_page", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return kaminariIntpValue(scope(v).PrevPage())
	})
	cls.define("next_page", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return kaminariIntpValue(scope(v).NextPage())
	})
	cls.define("page_entries_info", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(kaminariEntriesInfo(scope(v).EntriesInfo()))
	})
}

// registerKaminariArrayClass installs Kaminari::PaginatableArray: the chainable
// page / per / padding snapshots, #records / #to_a materialising the current
// window, and the shared metadata methods.
func (vm *VM) registerKaminariArrayClass(mod *RClass) {
	cls := newClass("Kaminari::PaginatableArray", vm.cObject)
	mod.consts["PaginatableArray"] = cls
	vm.consts["Kaminari::PaginatableArray"] = cls

	self := func(v object.Value) *KaminariArray { return v.(*KaminariArray) }

	cls.define("page", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &KaminariArray{a: self(v).a.Page(kaminariPageNum(args)), vm: vm}
	})
	cls.define("per", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &KaminariArray{a: self(v).a.Per(kaminariPerPtr(args)), vm: vm}
	})
	cls.define("padding", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &KaminariArray{a: self(v).a.Padding(kaminariPaddingNum(args)), vm: vm}
	})
	records := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return kaminariRecordsToRuby(self(v).a.Records())
	}
	cls.define("records", records)
	cls.define("to_a", records)

	vm.installKaminariMeta(cls, func(v object.Value) kaminariScope { return self(v).a })
}

// registerKaminariRelationClass installs Kaminari::PaginatableRelation: the
// chainable page / per / padding snapshots over the same Relation seam, #records
// / #to_a calling through the seam, and the shared metadata methods.
func (vm *VM) registerKaminariRelationClass(mod *RClass) {
	cls := newClass("Kaminari::PaginatableRelation", vm.cObject)
	mod.consts["PaginatableRelation"] = cls
	vm.consts["Kaminari::PaginatableRelation"] = cls

	self := func(v object.Value) *KaminariRelation { return v.(*KaminariRelation) }

	cls.define("page", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &KaminariRelation{p: self(v).p.Page(kaminariPageNum(args)), vm: vm}
	})
	cls.define("per", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &KaminariRelation{p: self(v).p.Per(kaminariPerPtr(args)), vm: vm}
	})
	cls.define("padding", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &KaminariRelation{p: self(v).p.Padding(kaminariPaddingNum(args)), vm: vm}
	})
	records := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		out := self(v).p.Records()
		if rv, ok := out.(object.Value); ok {
			return rv
		}
		return object.NilV
	}
	cls.define("records", records)
	cls.define("to_a", records)

	vm.installKaminariMeta(cls, func(v object.Value) kaminariScope { return self(v).p })
}

// registerKaminariArrayCoreMethod adds Array#page: a plain Ruby Array paginates
// itself, matching kaminari mixing PageScopeMethods into Array.
func (vm *VM) registerKaminariArrayCoreMethod() {
	vm.cArray.define("page", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		arr := self.(*object.Array)
		a := kaminariNewArray(arr).Page(kaminariPageNum(args))
		return &KaminariArray{a: a, vm: vm}
	})
}

// registerKaminariActiveRecord adds #page to the ActiveRecord surface, wiring the
// Relation seam to a live ActiveRecord relation (Count -> #count, Slice ->
// #offset(o).limit(l)): as a class method on ActiveRecord::Base (so a
// `class User < ActiveRecord::Base` answers User.page), an instance method on the
// ActiveRecord::Model factory value, and an instance method on
// ActiveRecord::Relation (so an already-chained query answers .page). Each is a
// no-op when ActiveRecord was not itself registered.
func (vm *VM) registerKaminariActiveRecord() {
	if base, ok := vm.consts["ActiveRecord::Base"].(*RClass); ok {
		base.smethods["page"] = &Method{name: "page", owner: base, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			m := vm.arModelForClass(self.(*RClass))
			rel := &ActiveRecordRelation{r: m.m.All(), model: m}
			return vm.kaminariPaginateRel(rel, kaminariPageNum(args))
		}}
	}
	if model, ok := vm.consts["ActiveRecord::Model"].(*RClass); ok {
		model.define("page", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			m := v.(*ActiveRecordModel)
			rel := &ActiveRecordRelation{r: m.m.All(), model: m}
			return vm.kaminariPaginateRel(rel, kaminariPageNum(args))
		})
	}
	if rel, ok := vm.consts["ActiveRecord::Relation"].(*RClass); ok {
		rel.define("page", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.kaminariPaginateRel(v, kaminariPageNum(args))
		})
	}
}
