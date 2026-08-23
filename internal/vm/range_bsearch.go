// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// bsearchProbe evaluates a bsearch block for one candidate value and reports how
// the search should proceed: smaller narrows to the lower half, exact is a
// find-any hit (the block returned 0), and satisfied marks a find-minimum match
// (the block returned true). A block result that is neither numeric, true, false
// nor nil is a TypeError. Shared by Array#bsearch and Range#bsearch.
func (vm *VM) bsearchProbe(blk *Proc, val object.Value) (smaller, exact, satisfied bool) {
	switch v := vm.callBlock(blk, []object.Value{val}).(type) {
	case object.Bool:
		if bool(v) {
			return true, false, true
		}
		return false, false, false
	case object.Nil:
		return false, false, false
	default:
		rv := object.Value(v)
		if !isNumericValue(rv) {
			raise("TypeError", "wrong argument type %s (must be numeric, true, false or nil)", vm.classOf(rv).name)
		}
		switch c := vm.spaceship(rv, object.IntValue(0)); {
		case c == 0:
			return false, true, false
		case c < 0:
			return true, false, false
		default:
			return false, false, false
		}
	}
}

// bsearchMid returns ⌊(low+high)/2⌋ without overflowing when the two ends span
// more than half the int64 range — as the ordered-bits domain of a float bsearch
// crossing zero does. It computes the distance in unsigned arithmetic.
func bsearchMid(low, high int64) int64 {
	return int64(uint64(low) + (uint64(high)-uint64(low))>>1)
}

// bsearchNumeric reports whether v is a valid Range#bsearch bound: an Integer,
// Bignum or Float. nil (a beginless/endless bound) is handled separately.
func bsearchNumeric(v object.Value) bool {
	switch v.(type) {
	case object.Integer, *object.Bignum, object.Float:
		return true
	}
	return false
}

// rangeBsearch implements Range#bsearch. An all-integer range searches the
// integer domain directly; a range with any Float bound searches the IEEE-754
// bit domain (which orders ±Infinity and Float::MAX correctly). Beginless and
// endless ranges probe outward for a finite bracket first.
func (vm *VM) rangeBsearch(r *object.Range, blk *Proc) object.Value {
	// The bounds were validated as numeric by the Range#bsearch entry point; a
	// Float bound selects the bit-domain search, anything else the integer one.
	_, loFloat := r.Lo.(object.Float)
	_, hiFloat := r.Hi.(object.Float)
	if loFloat || hiFloat {
		return vm.rangeBsearchFloat(r, blk)
	}
	return vm.rangeBsearchInt(r, blk)
}

// bsearchInt64 narrows an integer (Integer or Bignum) bound to an int64 for the
// integer bsearch domain.
func bsearchInt64(v object.Value) int64 {
	b, _ := object.BigOf(v)
	return b.Int64()
}

// rangeBsearchInt searches an integer range (both bounds Integer or nil).
func (vm *VM) rangeBsearchInt(r *object.Range, blk *Proc) object.Value {
	// Resolve the [low, high) half-open integer interval, growing an unbounded
	// end (or beginning) outward until the predicate brackets a finite range.
	var low, high int64
	if object.IsNil(r.Lo) {
		// Beginless: walk the lower bound down (geometrically) until the target is
		// no longer at/below it, i.e. the block stops narrowing to the left.
		high = intBoundInclusiveEnd(r) + 1
		low = high - 1
		for diff := int64(1); ; diff *= 2 {
			if s, x, _ := vm.bsearchProbe(blk, object.IntValue(low)); x {
				return object.IntValue(low)
			} else if !s {
				break // the target, if any, is above low
			}
			low -= diff
		}
	} else {
		low = bsearchInt64(r.Lo)
		if object.IsNil(r.Hi) {
			// Endless: grow the upper bound (geometrically) until the target is
			// bracketed (the block narrows to the left) or found exactly.
			high = low
			for diff := int64(1); ; diff *= 2 {
				if s, x, _ := vm.bsearchProbe(blk, object.IntValue(high)); x {
					return object.IntValue(high)
				} else if s {
					break
				}
				high += diff
			}
			high++ // make the bracket half-open
		} else {
			high = intBoundInclusiveEnd(r) + 1
		}
	}
	satisfied := false
	for low < high {
		mid := bsearchMid(low, high)
		smaller, exact, sat := vm.bsearchProbe(blk, object.IntValue(mid))
		if exact {
			return object.IntValue(mid)
		}
		if sat {
			satisfied = true
		}
		if smaller {
			high = mid
		} else {
			low = mid + 1
		}
	}
	if satisfied {
		return object.IntValue(low)
	}
	return object.NilV
}

// intBoundInclusiveEnd returns the last integer contained in an integer range
// (r.Hi adjusted for an exclusive end).
func intBoundInclusiveEnd(r *object.Range) int64 {
	end := bsearchInt64(r.Hi)
	if r.Exclusive {
		end--
	}
	return end
}

// rangeBsearchFloat searches a range with a Float bound over the IEEE-754 bit
// domain, mapping doubles to a monotonic int64 so binary search works across the
// sign boundary and out to ±Infinity.
func (vm *VM) rangeBsearchFloat(r *object.Range, blk *Proc) object.Value {
	lo := math.Inf(-1)
	if !object.IsNil(r.Lo) {
		lo, _ = toFloat(r.Lo)
	}
	hi := math.Inf(1)
	if !object.IsNil(r.Hi) {
		hi, _ = toFloat(r.Hi)
	}
	low := floatToOrderedInt(lo)
	high := floatToOrderedInt(hi)
	if !r.Exclusive {
		high++ // include the endpoint: search the half-open [low, high)
	}
	satisfied := false
	for low < high {
		mid := bsearchMid(low, high)
		smaller, exact, sat := vm.bsearchProbe(blk, object.Float(orderedIntToFloat(mid)))
		if exact {
			return object.Float(orderedIntToFloat(mid))
		}
		if sat {
			satisfied = true
		}
		if smaller {
			high = mid
		} else {
			low = mid + 1
		}
	}
	if satisfied {
		return object.Float(orderedIntToFloat(low))
	}
	return object.NilV
}

// floatToOrderedInt maps a float64 to an int64 whose natural ordering matches the
// float ordering (so a binary search over the int64s walks the doubles in
// order), as MRI's double_as_int64 does.
func floatToOrderedInt(d float64) int64 {
	i := int64(math.Float64bits(d))
	if i < 0 {
		return math.MinInt64 - i
	}
	return i
}

// orderedIntToFloat is the inverse of floatToOrderedInt (the transform is its own
// inverse).
func orderedIntToFloat(i int64) float64 {
	if i < 0 {
		i = math.MinInt64 - i
	}
	return math.Float64frombits(uint64(i))
}
