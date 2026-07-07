// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

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
		s, err := self.(*RelineHistory).h.Get(int(intArg(args[0])))
		if err != nil {
			raiseHistoryError(err)
		}
		return object.NewString(s)
	})
	def("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if err := self.(*RelineHistory).h.Set(int(intArg(args[0])), strArg(args[1])); err != nil {
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
		s, err := self.(*RelineHistory).h.DeleteAt(int(intArg(args[0])))
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
			n := int(intArg(args[0]))
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
// exception: the overflow message maps to RangeError, an out-of-bounds index to
// IndexError (mirroring Reline::History#check_index).
func raiseHistoryError(err error) {
	msg := err.Error()
	if strings.HasPrefix(msg, "integer ") {
		raise("RangeError", "%s", msg)
	}
	raise("IndexError", "%s", msg)
}
