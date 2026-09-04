// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerArrayEdges installs the Array core methods that round out MRI 3.4/4.0
// coverage: the binary-search pair (#bsearch/#bsearch_index), the multi-argument
// set operations (#difference/#union/#intersection and the #intersect? predicate,
// all comparing with eql? like the &,|,- operators), the iteration helpers
// (#each_index and #cycle, including their sized Enumerator forms).
func (vm *VM) registerArrayEdges() {
	// bsearch / bsearch_index share the same search; only the returned projection
	// (element vs index) differs. Without a block both return an Enumerator, as MRI
	// does.
	vm.cArray.define("bsearch", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "bsearch")
		}
		elems := self.(*object.Array).Elems
		if i, found := vm.arrayBsearchIndex(elems, blk); found {
			return elems[i]
		}
		return object.NilV
	})
	vm.cArray.define("bsearch_index", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "bsearch_index")
		}
		if i, found := vm.arrayBsearchIndex(self.(*object.Array).Elems, blk); found {
			return object.IntValue(int64(i))
		}
		return object.NilV
	})

	// difference keeps every element of the receiver that is eql? to no element of
	// any argument array — duplicates in the receiver are preserved, exactly like a
	// chained `-`.
	vm.cArray.define("difference", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		others := vm.toAryArgs(args)
		var out []object.Value
		for _, e := range self.(*object.Array).Elems {
			if !vm.inAnyArray(others, e) {
				out = append(out, e)
			}
		}
		return object.NewArrayFromSlice(out)
	})
	// union concatenates the receiver and every argument, then removes eql?
	// duplicates keeping first-seen order — a multi-argument, de-duplicating `|`.
	vm.cArray.define("union", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		others := vm.toAryArgs(args)
		var out []object.Value
		add := func(elems []object.Value) {
			for _, e := range elems {
				if !vm.arrayIncludesEql(out, e) {
					out = append(out, e)
				}
			}
		}
		add(self.(*object.Array).Elems)
		for _, o := range others {
			add(o.Elems)
		}
		return object.NewArrayFromSlice(out)
	})
	// intersection keeps each element of the receiver that is eql? to some element
	// of every argument array, de-duplicated in receiver order. With no arguments
	// it is simply the receiver de-duplicated (like #uniq under eql?).
	vm.cArray.define("intersection", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		others := vm.toAryArgs(args)
		var out []object.Value
		for _, e := range self.(*object.Array).Elems {
			if vm.arrayIncludesEql(out, e) {
				continue // already emitted → keep the result de-duplicated
			}
			if vm.inAllArrays(others, e) {
				out = append(out, e)
			}
		}
		return object.NewArrayFromSlice(out)
	})
	// intersect? reports whether the receiver and the one argument array share any
	// element (compared with eql?), short-circuiting on the first match.
	vm.cArray.define("intersect?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		other := vm.toAryArg(args[0])
		for _, e := range self.(*object.Array).Elems {
			if vm.arrayIncludesEql(other.Elems, e) {
				return object.True
			}
		}
		return object.False
	})

	// each_index yields every valid index in order and returns the array; without a
	// block it returns an Enumerator whose #size is the array length.
	vm.cArray.define("each_index", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		a := self.(*object.Array)
		if blk == nil {
			n := len(a.Elems)
			return enumForSized(self, "each_index", func(*VM) object.Value { return object.IntValue(int64(n)) })
		}
		// Live-index loop so indices for elements the block appends are yielded too
		// (Ruby's Array#each_index tolerates size increase during iteration).
		for i := 0; i < len(a.Elems); i++ {
			vm.callBlock(blk, []object.Value{object.IntValue(int64(i))})
		}
		return self
	})

	// cycle yields the elements n times (or forever when n is omitted); a negative
	// or zero count yields nothing. With a block it returns nil once a finite cycle
	// completes (an unbounded cycle only ends via break); without a block it returns
	// an Enumerator whose #size is n*length, 0 for a non-positive count, or infinity
	// for an unbounded cycle.
	vm.cArray.define("cycle", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		a := self.(*object.Array)
		hasN := len(args) > 0 && !object.IsNil(args[0])
		if blk == nil {
			return enumForSized(self, "cycle", func(vm *VM) object.Value {
				return arrayCycleSize(vm, len(a.Elems), args, hasN)
			}, args...)
		}
		if !hasN {
			for len(a.Elems) > 0 { // unbounded: only break/return unwinds this
				for _, e := range a.Elems {
					vm.callBlock(blk, []object.Value{e})
				}
			}
			return object.NilV
		}
		n := vm.toIntCoerce(args[0])
		for i := int64(0); i < n; i++ {
			for _, e := range a.Elems {
				vm.callBlock(blk, []object.Value{e})
			}
		}
		return object.NilV
	})
}

// arrayBsearchIndex runs MRI's rb_ary_bsearch_index over elems. The block result
// selects the mode per iteration: true/false/nil drive find-minimum (true means
// "search left" and marks the range as satisfiable), while a Numeric drives
// find-any (0 is an immediate hit, a negative value searches left, a positive one
// searches right). Any other result raises TypeError. It returns the resolved
// index and whether a matching element exists.
func (vm *VM) arrayBsearchIndex(elems []object.Value, blk *Proc) (int, bool) {
	low, high := 0, len(elems)
	satisfied := false
	for low < high {
		mid := low + (high-low)/2
		val := vm.callBlock(blk, []object.Value{elems[mid]})
		var smaller bool
		switch v := val.(type) {
		case object.Bool:
			if bool(v) { // true → find-minimum: this element satisfies, look left
				satisfied = true
				smaller = true
			}
		case object.Nil: // nil behaves like false → look right
		default:
			if !isNumericValue(val) {
				raise("TypeError", "wrong argument type %s (must be numeric, true, false or nil)", vm.classOf(val).name)
			}
			switch c := vm.spaceship(val, object.IntValue(0)); {
			case c == 0:
				return mid, true // find-any: exact hit
			case c < 0:
				// MRI rb_bsearch_index: a negative block result means the
				// wanted element is at or before mid, so narrow to the left half
				// (smaller). A positive result narrows to the right.
				smaller = true
			}
		}
		if smaller {
			high = mid
		} else {
			low = mid + 1
		}
	}
	return low, satisfied
}

// arrayCycleSize computes Array#cycle's Enumerator #size: 0 for an empty array,
// infinity for an unbounded cycle, otherwise n*length clamped to 0 for a
// non-positive count.
func arrayCycleSize(vm *VM, length int, args []object.Value, hasN bool) object.Value {
	if length == 0 {
		return object.IntValue(0)
	}
	if !hasN {
		return object.Float(math.Inf(1))
	}
	count := vm.toIntCoerce(args[0]) // #to_int-coerced, matching the block form
	if count <= 0 {
		return object.IntValue(0)
	}
	return object.IntValue(int64(length) * count)
}

// inAnyArray reports whether v is eql? to an element of any of the arrays.
func (vm *VM) inAnyArray(arrs []*object.Array, v object.Value) bool {
	for _, a := range arrs {
		if vm.arrayIncludesEql(a.Elems, v) {
			return true
		}
	}
	return false
}

// inAllArrays reports whether v is eql? to an element of every array (vacuously
// true when there are none).
func (vm *VM) inAllArrays(arrs []*object.Array, v object.Value) bool {
	for _, a := range arrs {
		if !vm.arrayIncludesEql(a.Elems, v) {
			return false
		}
	}
	return true
}

// enumForSized builds an Enumerator for recv.meth(*args) whose #size is computed
// on demand by size (matching MRI's size-block enumerators).
func enumForSized(recv object.Value, meth string, size func(*VM) object.Value, args ...object.Value) *Enumerator {
	return &Enumerator{recv: recv, meth: meth, args: args,
		sizeBlock: &Proc{native: func(vm *VM, _ []object.Value) object.Value { return size(vm) }}}
}
