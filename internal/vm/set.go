// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Set is rbgo's Ruby Set. Like MRI's set.rb it is a thin shell over a Hash whose
// keys are the members (the value is always true); the backing object.Hash owns
// membership, the canonical key→member mapping, first-insertion order and the
// compare_by_identity flag, so there is no parallel store to keep in sync. Using
// object.Hash means members are keyed by the very #hash/#eql? semantics rbgo's
// Hash keys use — including content equality for Array/Hash/Set members and the
// CustomKeyHook path for objects that override #hash/#eql? — exactly as MRI's
// Hash-backed Set does.
//
// The bulk of the Ruby-observable Set API (algebra, predicates, higher-order
// methods, aliases, Enumerable mix-in, initialize/each_entry protocol) lives in
// the prelude, reopening `class Set` on top of the primitives registered here.
type Set struct {
	h         *object.Hash // member -> true, keyed by the member's #hash/#eql?
	iterating int          // >0 while an #each is in progress (structural-mod guard)
}

// checkIter raises the RuntimeError MRI raises when a Set is structurally
// modified while an #each over it is in progress.
func (s *Set) checkIter(what string) {
	if s.iterating > 0 {
		raise("RuntimeError", "can't %s during iteration", what)
	}
}

func (s *Set) ToS() string     { return s.repr() }
func (s *Set) Inspect() string { return s.repr() }
func (s *Set) Truthy() bool    { return true }

// repr renders MRI 4.0's "Set[1, 2, 3]" (empty: "Set[]"), members in insertion
// order each rendered with Ruby #inspect, with a cycle guard so a Set that
// (transitively) contains itself renders "Set[...]" instead of looping. (Ruby 4.0
// replaced the older "#<Set: {…}>" form with the "Set[…]" literal form.)
func (s *Set) repr() string {
	if !object.ReprEnter(s) {
		return "Set[...]"
	}
	defer object.ReprLeave(s)
	var b strings.Builder
	b.WriteString("Set[")
	for i, k := range s.h.Keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(k.Inspect())
	}
	b.WriteString("]")
	return b.String()
}

// newSet builds an empty Ruby Set wrapper over a fresh content-keyed Hash. It is
// also the constructor other bindings (e.g. redis) use to surface a Ruby Set.
func newSet() *Set { return &Set{h: object.NewHash()} }

// each calls fn for every member (a Ruby value) in insertion order, in a single
// pass over the backing Hash's key slice.
func (s *Set) each(fn func(object.Value)) {
	for _, k := range s.h.Keys {
		fn(k)
	}
}

// size returns the member count.
func (s *Set) size() int { return s.h.Len() }

// add inserts a Ruby value (idempotent), preserving first-insertion order.
func (s *Set) add(v object.Value) { s.h.Set(v, object.True) }

// delete removes a Ruby value (a no-op when absent).
func (s *Set) delete(v object.Value) { s.h.Delete(v) }

// has reports membership.
func (s *Set) has(v object.Value) bool { _, ok := s.h.Get(v); return ok }

// eq reports Set equality by members (ignoring the compare_by_identity flag,
// which the Ruby #== layer checks): same size and every member of s present in o.
// It backs the operator-level == fast path for two native Sets.
func (s *Set) eq(o *Set) bool {
	if s.size() != o.size() {
		return false
	}
	for _, k := range s.h.Keys {
		if !o.has(k) {
			return false
		}
	}
	return true
}

// toArray materialises the Set into a Ruby Array in insertion order.
func (s *Set) toArray() object.Value {
	out := make([]object.Value, len(s.h.Keys))
	copy(out, s.h.Keys)
	return object.NewArrayFromSlice(out)
}

// setKey marshals a Ruby value to a canonical comparable Go key for the Bag
// multiset and the Concurrent::Map / TSort bindings (Set itself now keys members
// through its backing Hash). A String keys by its byte content (distinct from a
// Symbol of the same name); the other immutable value types key by themselves;
// any other object keys by identity, exactly like a Hash key with default
// (non-content) semantics.
func setKey(v object.Value) any {
	switch x := v.(type) {
	case object.Integer:
		return x
	case object.Float:
		return x
	case object.Symbol:
		return x
	case object.Bool:
		return x
	case object.Nil:
		return x
	case *object.Bignum:
		return "bignum:" + x.I.String()
	case *object.String:
		return "str:" + string(x.Bytes())
	}
	return v
}

// registerSet installs the Set class, its constructor and the native primitives
// the prelude's `class Set` reopening builds the full API on top of.
func (vm *VM) registerSet() {
	vm.cSet = newClass("Set", vm.cObject)
	vm.consts["Set"] = vm.cSet

	// Set.new(enumerable=nil, &block): allocate an empty Set, then run the
	// (prelude, private) #initialize which seeds it via the each_entry/each
	// protocol and applies the block, exactly like MRI's Class#new.
	vm.cSet.smethods["new"] = &Method{name: "new", owner: vm.cSet,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			s := newSet()
			vm.send(s, "initialize", args, blk)
			return s
		}}
	// Set[a, b, …] builds a Set from its arguments (MRI's Set.[]).
	vm.cSet.smethods["[]"] = &Method{name: "[]", owner: vm.cSet,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			s := newSet()
			for _, a := range args {
				s.add(a)
			}
			return s
		}}

	d := func(name string, fn NativeFn) { vm.cSet.define(name, fn) }
	self := func(v object.Value) *Set { return v.(*Set) }

	d("add", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.checkIter("add to set")
		s.add(args[0])
		return s
	})
	d("delete", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.checkIter("delete from set")
		s.delete(args[0])
		return s
	})
	d("include?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).has(args[0]))
	})
	d("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).size()))
	})
	d("clear", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.checkIter("clear set")
		s.h.Clear()
		return s
	})
	d("to_a", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).toArray()
	})

	// __each yields every member in insertion order and returns self; the prelude
	// #each wraps it to return an Enumerator when called without a block.
	d("__each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		s := self(v)
		s.iterating++
		defer func() { s.iterating-- }()
		s.each(func(m object.Value) { vm.callBlock(blk, []object.Value{m}) })
		return s
	})

	// compare_by_identity switches membership to object identity and rehashes the
	// existing members; returns self. compare_by_identity? reports the flag.
	d("compare_by_identity", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.h.CompareByIdentity()
		return s
	})
	d("compare_by_identity?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).h.Identity)
	})

	// __replace_cbi(flag) empties the Set and sets its compare_by_identity flag to
	// flag, returning self. The prelude's #replace / #map! / #flatten! use it to
	// reset contents while controlling whether the identity flag is retained.
	d("__replace_cbi", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.checkIter("modify set")
		s.h.Clear()
		s.h.Identity = args[0].Truthy()
		return s
	})

	// dup / clone: a shallow copy with the same members, insertion order and
	// compare_by_identity flag (ReplaceWith carries the flag across).
	dupFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		n := newSet()
		n.h.ReplaceWith(self(v).h)
		return n
	}
	d("dup", dupFn)
	d("clone", dupFn)

	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).repr())
	})

	// Genuine built-in aliases share one Method record, so Set.instance_method of
	// each name compares equal to its original (as MRI models these aliases).
	aliasBuiltin(vm.cSet, "<<", "add")
	aliasBuiltin(vm.cSet, "member?", "include?")
	aliasBuiltin(vm.cSet, "===", "include?")
	aliasBuiltin(vm.cSet, "length", "size")
	aliasBuiltin(vm.cSet, "inspect", "to_s")
}
