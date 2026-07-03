// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rbset "github.com/go-ruby-set/set"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Set binds github.com/go-ruby-set/set — the pure-Go, MRI-4.0.5-faithful port of
// Ruby's Set stdlib — into rbgo. The library does all of the membership,
// set algebra, iteration ordering, predicates and the MRI 4.0 "Set[…]" inspect
// rendering; this file is only the thin shell that maps Ruby values onto the
// library's any-typed member model and supplies the Ruby hash/eql? semantics
// through the library's Hasher.
//
// A go-ruby-set Set built with NewWith(hasher) keys its members through the
// host function (here setKey), so two distinct String objects with the same
// bytes are the same member while a Symbol of the same name is a different one,
// exactly as MRI's hash/eql? protocol — the very semantics rbgo already uses for
// Hash keys. The original Ruby value for each canonical key is recovered for
// each/to_a/inspect through the order-preserving vals/order table, which is also
// what the Bag type and the YAML emitter read, so they keep working unchanged.

// Set is the Ruby wrapper around a go-ruby-set Set. The library Set holds the
// original Ruby value as its member, keyed through setKey, so it is the single
// source of truth for membership, the canonical key→value mapping and the
// insertion order — there is no parallel store to keep in sync, and an algebra
// result already carries the Ruby values in MRI order in one pass.
type Set struct {
	s *rbset.Set // members are the Ruby values, keyed by setKey
}

func (s *Set) ToS() string     { return s.repr() }
func (s *Set) Inspect() string { return s.repr() }
func (s *Set) Truthy() bool    { return true }

// repr renders MRI 4.0's "Set[1, 2, 3]" (empty: "Set[]"), members in insertion
// order, each rendered with Ruby #inspect. The library owns the format; we feed
// it the canonical-key → Ruby-value lookup so members inspect as Ruby values.
func (s *Set) repr() string {
	return s.s.Inspect(func(m any) string { return m.(object.Value).Inspect() })
}

// newSet builds an empty Ruby Set wrapper. The library Set keys its members
// through setKey, so any Ruby value (including reference types) is an accepted
// member and the stored member is the Ruby value itself.
func newSet() *Set {
	return &Set{s: rbset.NewWith(func(elem any) any { return setKey(elem.(object.Value)) })}
}

// wrap adorns a library Set (whose members are Ruby values keyed by setKey) as a
// Ruby Set wrapper without copying — used to surface algebra results, which the
// library already returns with the Ruby member values in MRI insertion order.
func wrap(s *rbset.Set) *Set { return &Set{s: s} }

// each calls fn for every member (a Ruby value) in insertion order, in a single
// pass over the library's tables (no per-element membership re-lookup).
func (s *Set) each(fn func(object.Value)) {
	s.s.EachPair(func(_, member any) { fn(member.(object.Value)) })
}

// size returns the member count.
func (s *Set) size() int { return s.s.Size() }

// setKey marshals a Ruby value to a canonical comparable Go key, raising
// TypeError for a value that cannot be a Set member is NOT done here — like a
// Hash key, every Ruby value is a valid member. A String keys by its byte
// content (distinct from a Symbol of the same name, as in Hash keys); the other
// immutable value types key by themselves.
func setKey(v object.Value) any {
	{
		__sw157 := v
		switch {
		case object.IsInt(__sw157):
			x := object.AsInteger(__sw157)
			_ = x
			return x
		case object.IsFloat(__sw157):
			x := object.AsFloatV(__sw157)
			_ = x
			return x
		case object.IsKind[object.Symbol](__sw157):
			x := object.Kind[object.Symbol](__sw157)
			_ = x
			return x
		case object.IsBool(__sw157):
			x := object.AsBoolV(__sw157)
			_ = x
			return x
		case object.IsNilObj(__sw157):
			x := object.NilObj()
			_ = x
			return x
		case object.IsKind[*object.Bignum](__sw157):
			x := object.Kind[*object.Bignum](__sw157)
			_ = x
			return "bignum:" + x.I.String()
		case object.IsKind[*object.String](__sw157):
			x := object.Kind[*object.String](__sw157)
			_ = x
			return "str:" + string(x.Bytes())
		}
	}
	// Any other object is a valid Set member in Ruby — it keys by identity
	// (object equality / hash), exactly like a Hash key. A reference-typed Ruby
	// value is a Go pointer, which is comparable, so the pointer itself is a
	// stable per-object key. This lets a Set hold arbitrary objects (e.g. Puppet
	// resources, model nodes) the way MRI does.
	return v
}

// add inserts a Ruby value, preserving first-insertion order (idempotent). The
// library stores the Ruby value as the member and keeps the canonical key→value
// mapping and order itself, so there is nothing else to update.
func (s *Set) add(v object.Value) { s.s.Add(v) }

// delete removes a Ruby value (a no-op when absent).
func (s *Set) delete(v object.Value) { s.s.Delete(v) }

// setArg asserts an argument is a Set, raising TypeError otherwise.
func setArg(v object.Value) *Set {
	s, ok := object.KindOK[*Set](v)
	if !ok {
		raise("TypeError", "value must be a Set")
	}
	return s
}

// seed adds every element of an enumerable (Array or Set) to s.
func (s *Set) seed(v object.Value) {
	{
		__sw158 := v
		switch {
		case object.IsKind[*object.Array](__sw158):
			e := object.Kind[*object.Array](__sw158)
			_ = e
			for _, el := range e.Elems {
				s.add(el)
			}
		case object.IsKind[*Set](__sw158):
			e := object.Kind[*Set](__sw158)
			_ = e
			e.each(s.add)
		default:
			e := __sw158
			_ = e
			raise("TypeError", "value must be enumerable (Array or Set)")
		}
	}
}

// copy returns a shallow clone: a new Set holding the same members in the same
// insertion order (the Ruby values are shared, as MRI's Set#dup does).
func (s *Set) copy() *Set { return wrap(s.s.Dup()) }

// toArray materialises the Set into a Ruby Array in insertion order.
func (s *Set) toArray() object.Value {
	src := s.s.ToSlice()
	out := make([]object.Value, len(src))
	for i, m := range src {
		out[i] = m.(object.Value)
	}
	return object.Wrap(object.NewArrayFromSlice(out))
}

// setOp implements the Set operator fast path reached from binary(): + is union
// and - is difference (& | << dispatch as ordinary methods). The right operand
// must be a Set.
func setOp(op bytecode.Op, a *Set, b object.Value) object.Value {
	switch op {
	case bytecode.OpAdd:
		return object.Wrap(wrap(a.s.Union(setArg(b).s)))
	case bytecode.OpSub:
		return object.Wrap(wrap(a.s.Difference(setArg(b).s)))
	}
	return raise("NoMethodError", "undefined method '%s' for a Set", op)
}

// registerSet installs the Set class, its constructor and instance methods.
func (vm *VM) registerSet() {
	vm.cSet = newClass("Set", vm.cObject)
	vm.consts["Set"] = object.Wrap(vm.cSet)

	// Set.new(enumerable=nil): empty, or seeded from an Array/Set.
	vm.cSet.smethods["new"] = &Method{name: "new", owner: vm.cSet,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			s := newSet()
			if len(args) > 0 {
				if _, isNil := object.AsNilOK(args[0]); !isNil {
					s.seed(args[0])
				}
			}
			return object.Wrap(s)
		}}
	// Set[a, b, …] builds a Set from its arguments (MRI's Set.[]).
	vm.cSet.smethods["[]"] = &Method{name: "[]", owner: vm.cSet,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			s := newSet()
			for _, a := range args {
				s.add(a)
			}
			return object.Wrap(s)
		}}

	d := func(name string, fn NativeFn) { vm.cSet.define(name, fn) }
	self := func(v object.Value) *Set { return object.Kind[*Set](v) }

	addFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.add(args[0])
		return object.Wrap(s)
	}
	d("add", addFn)
	d("<<", addFn)

	d("add?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		if s.s.Include(args[0]) {
			return object.NilVal()
		}
		s.add(args[0])
		return object.Wrap(s)
	})
	d("delete", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.delete(args[0])
		return object.Wrap(s)
	})

	includeFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).s.Include(args[0]))))
	}
	d("include?", includeFn)
	d("member?", includeFn)
	d("===", includeFn)

	sizeFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).s.Size()))
	}
	d("size", sizeFn)
	d("length", sizeFn)
	d("count", sizeFn)

	d("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).s.Empty())))
	})
	d("clear", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.s.Clear()
		return object.Wrap(s)
	})

	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		s := self(v)
		s.each(func(m object.Value) { vm.callBlock(blk, []object.Value{m}) })
		return object.Wrap(s)
	})
	d("to_a", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).toArray()
	})
	d("to_set", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return v
	})

	unionFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return object.Wrap(wrap(a.s.Union(b.s)))
	}
	d("|", unionFn)
	d("union", unionFn)

	interFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return object.Wrap(wrap(a.s.Intersection(b.s)))
	}
	d("&", interFn)
	d("intersection", interFn)

	diffFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return object.Wrap(wrap(a.s.Difference(b.s)))
	}
	d("difference", diffFn)

	subsetFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).s.SubsetQ(setArg(args[0]).s))))
	}
	d("subset?", subsetFn)
	d("<=", subsetFn)
	d("superset?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).s.SupersetQ(setArg(args[0]).s))))
	})
	d(">=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(self(v).s.SupersetQ(setArg(args[0]).s))))
	})

	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		b, ok := object.KindOK[*Set](args[0])
		if !ok {
			return object.BoolValue(bool(object.False))
		}
		return object.BoolValue(bool(object.Bool(self(v).s.EqualQ(b.s))))
	})

	d("merge", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		for _, a := range args {
			s.seed(a)
		}
		return object.Wrap(s)
	})

	// map / collect: yield each member, collect the block results into a new
	// Array (MRI's Set#map/#collect return an Array, not a Set).
	mapFn := func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (map)")
		}
		s := self(v)
		out := make([]object.Value, 0, s.size())
		s.each(func(m object.Value) { out = append(out, vm.callBlock(blk, []object.Value{m})) })
		return object.Wrap(object.NewArrayFromSlice(out))
	}
	d("map", mapFn)
	d("collect", mapFn)

	// select / filter: a new Set of the members for which the block is truthy.
	selectFn := func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (select)")
		}
		s := self(v)
		out := newSet()
		s.each(func(m object.Value) {
			if vm.callBlock(blk, []object.Value{m}).Truthy() {
				out.add(m)
			}
		})
		return object.Wrap(out)
	}
	d("select", selectFn)
	d("filter", selectFn)

	// reject: a new Set of the members for which the block is falsy.
	d("reject", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (reject)")
		}
		s := self(v)
		out := newSet()
		s.each(func(m object.Value) {
			if !vm.callBlock(blk, []object.Value{m}).Truthy() {
				out.add(m)
			}
		})
		return object.Wrap(out)
	})

	// find / detect: the first member (insertion order) for which the block is
	// truthy, or nil when none match.
	findFn := func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (find)")
		}
		s := self(v)
		for _, m := range s.s.ToSlice() {
			mv := m.(object.Value)
			if vm.callBlock(blk, []object.Value{mv}).Truthy() {
				return mv
			}
		}
		return object.NilVal()
	}
	d("find", findFn)
	d("detect", findFn)

	// all?: true when the block is truthy for every member (true on the empty set).
	d("all?", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (all?)")
		}
		s := self(v)
		for _, m := range s.s.ToSlice() {
			if !vm.callBlock(blk, []object.Value{m.(object.Value)}).Truthy() {
				return object.BoolValue(bool(object.False))
			}
		}
		return object.BoolValue(bool(object.True))
	})

	// any?: true when the block is truthy for some member (false on the empty set).
	d("any?", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (any?)")
		}
		s := self(v)
		for _, m := range s.s.ToSlice() {
			if vm.callBlock(blk, []object.Value{m.(object.Value)}).Truthy() {
				return object.BoolValue(bool(object.True))
			}
		}
		return object.BoolValue(bool(object.False))
	})

	// none?: true when the block is truthy for no member (true on the empty set).
	d("none?", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (none?)")
		}
		s := self(v)
		for _, m := range s.s.ToSlice() {
			if vm.callBlock(blk, []object.Value{m.(object.Value)}).Truthy() {
				return object.BoolValue(bool(object.False))
			}
		}
		return object.BoolValue(bool(object.True))
	})

	// ^: symmetric difference — a new Set of the members in exactly one operand.
	d("^", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return object.Wrap(wrap(a.s.XorSym(b.s)))
	})

	// disjoint?: true when the two sets share no member.
	d("disjoint?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return object.BoolValue(bool(object.Bool(a.s.DisjointQ(b.s))))
	})
	// intersect?: the negation of disjoint? (the sets share at least one member).
	d("intersect?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return object.BoolValue(bool(object.Bool(a.s.IntersectQ(b.s))))
	})

	// <: proper subset — a subset of the argument and not equal to it.
	d("<", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return object.BoolValue(bool(object.Bool(a.s.ProperSubsetQ(b.s))))
	})
	// >: proper superset — the argument is a proper subset of the receiver.
	d(">", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return object.BoolValue(bool(object.Bool(a.s.ProperSupersetQ(b.s))))
	})

	// dup / clone: a shallow copy with the same members in insertion order.
	dupFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(self(v).copy())
	}
	d("dup", dupFn)
	d("clone", dupFn)

	// sort: a new Array of the members ordered by <=>. Materialise into an Array
	// and reuse the VM's Array#sort (which folds through spaceship), so mixed
	// incomparable members raise the very ArgumentError Array#sort raises.
	d("sort", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.send(self(v).toArray(), "sort", nil, nil)
	})

	// min / max: the smallest / largest member by <=> (nil on the empty set, as
	// MRI's Enumerable#min/#max). The fold uses the VM's spaceship helper.
	d("min", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return setExtreme(vm, self(v), -1)
	})
	d("max", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return setExtreme(vm, self(v), 1)
	})

	// sum(init=0): fold the members with + starting from init, reusing the VM's
	// add (binaryOp OpAdd) — so it agrees with Array#sum / numeric coercions.
	d("sum", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		acc := object.IntValue(0)
		if len(args) > 0 {
			acc = args[0]
		}
		s := self(v)
		s.each(func(m object.Value) { acc = vm.binaryOp(bytecode.OpAdd, acc, m) })
		return acc
	})

	// reduce / inject: Enumerable reduce with a block {|acc, x| ...}. An optional
	// initial value seeds the accumulator; without one the first member seeds it
	// (and reduce of the empty set with no initial value is nil, as in MRI). This
	// is the documented block-form subset — the symbol-operator form is omitted.
	reduceFn := func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (reduce)")
		}
		s := self(v)
		members := s.s.ToSlice()
		var acc object.Value
		i := 0
		if len(args) > 0 {
			acc = args[0]
		} else {
			if len(members) == 0 {
				return object.NilVal()
			}
			acc = members[0].(object.Value)
			i = 1
		}
		for ; i < len(members); i++ {
			acc = vm.callBlock(blk, []object.Value{acc, members[i].(object.Value)})
		}
		return acc
	}
	d("reduce", reduceFn)
	d("inject", reduceFn)

	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).repr()))
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(self(v).repr()))
	})
}

// setExtreme folds the members of s to the minimum (want=-1) or maximum
// (want=1) by the VM's spaceship (<=>), returning nil for the empty set. Mixed
// incomparable members surface the ArgumentError spaceship raises.
func setExtreme(vm *VM, s *Set, want int) object.Value {
	members := s.s.ToSlice()
	if len(members) == 0 {
		return object.NilVal()
	}
	best := members[0].(object.Value)
	for _, m := range members[1:] {
		cur := m.(object.Value)
		if cmp := vm.spaceship(cur, best); (want < 0 && cmp < 0) || (want > 0 && cmp > 0) {
			best = cur
		}
	}
	return best
}
