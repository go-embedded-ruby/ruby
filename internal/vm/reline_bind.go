// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	"math"

	"github.com/go-embedded-ruby/ruby/internal/object"
	reline "github.com/go-ruby-reline/reline"
)

// RelineHistory backs Reline::HISTORY: the Array-like, size-capped line history
// (MRI's Reline::HISTORY object). It wraps the same *reline.History the read
// loop appends submitted lines to, so entries pushed from Ruby and lines added
// by Reline.readline(..., true) share one store.
type RelineHistory struct {
	cls *RClass
	h   *reline.History
}

func (*RelineHistory) ToS() string     { return "#<Reline::History>" }
func (*RelineHistory) Inspect() string { return "#<Reline::History>" }
func (*RelineHistory) Truthy() bool    { return true }

// newRelineHistory creates the Reline::History class (nested under Reline) with
// the Array-subset protocol MRI's HISTORY object answers to, and returns the
// singleton instance wrapping h.
func (vm *VM) newRelineHistory(mod *RClass, h *reline.History) *RelineHistory {
	cls := newClass("History", vm.cObject)
	mod.consts["History"] = cls
	def := func(name string, fn NativeFn) { cls.define(name, fn) }

	def("<<", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*RelineHistory).h.Append(strArg(args[0]))
		return self
	})
	def("push", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		hh := self.(*RelineHistory).h
		for _, a := range args {
			hh.Append(strArg(a))
		}
		return self
	})
	def("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self.(*RelineHistory).h.Get(relineIndex(self.(*RelineHistory).h, args[0]))
		if err != nil {
			raiseHistoryError(err)
		}
		return object.NewString(s)
	})
	def("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self.(*RelineHistory).h.Set(relineIndex(self.(*RelineHistory).h, args[0]), strArg(args[1])); err != nil {
			raiseHistoryError(err)
		}
		return args[1]
	})
	def("size", relineHistSize)
	def("length", relineHistSize)
	def("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*RelineHistory).h.Empty())
	})
	def("clear", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		self.(*RelineHistory).h.Clear()
		return self
	})
	def("delete_at", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := self.(*RelineHistory).h.DeleteAt(relineIndex(self.(*RelineHistory).h, args[0]))
		if err != nil {
			raiseHistoryError(err)
		}
		return object.NewString(s)
	})
	def("pop", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		hh := self.(*RelineHistory).h
		if hh.Empty() {
			return object.NilV
		}
		s, _ := hh.DeleteAt(hh.Size() - 1)
		return object.NewString(s)
	})
	def("to_a", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(relineHistEntries(self.(*RelineHistory).h))
	})
	def("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return self
		}
		for _, e := range relineHistEntries(self.(*RelineHistory).h) {
			vm.callBlock(blk, []object.Value{e})
		}
		return self
	})
	def("last", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		entries := relineHistEntries(self.(*RelineHistory).h)
		if len(args) > 0 {
			n := clampInt(intArg(args[0]))
			if n > len(entries) {
				n = len(entries)
			}
			return object.NewArrayFromSlice(entries[len(entries)-n:])
		}
		if len(entries) == 0 {
			return object.NilV
		}
		return entries[len(entries)-1]
	})

	return &RelineHistory{cls: cls, h: h}
}

// relineHistSize answers #size / #length: the number of stored entries.
func relineHistSize(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
	return object.IntValue(int64(self.(*RelineHistory).h.Size()))
}

// relineIndex converts a history index for Get/Set/DeleteAt. The library's own
// check_index range test (index outside a C int => RangeError) compares a Go
// int, which on a 32-bit target can never be outside a C int once converted;
// so the test is made here, on the 64-bit value, after the same negative-index
// adjustment, with the library's message.
func relineIndex(h *reline.History, v object.Value) int {
	idx := intArg(v)
	adj := idx
	if adj < 0 {
		adj += int64(h.Size())
	}
	if adj < math.MinInt32 || adj > math.MaxInt32 {
		raise("RangeError", "integer %d too big to convert to 'int'", adj)
	}
	return clampInt(idx)
}

// relineHistEntries snapshots every history entry as Ruby Strings, in order.
func relineHistEntries(h *reline.History) []object.Value {
	out := make([]object.Value, 0, h.Size())
	for i := 0; i < h.Size(); i++ {
		s, _ := h.Get(i)
		out = append(out, object.NewString(s))
	}
	return out
}

// raiseHistoryError surfaces a library HistoryError as the matching MRI
// exception: an out-of-bounds index is IndexError (mirroring
// Reline::History#check_index). check_index's other answer, RangeError for an
// index outside a C int, never comes from the library here: relineIndex raises
// it first, on the 64-bit value.
func raiseHistoryError(err error) {
	raise("IndexError", "%s", err.Error())
}
