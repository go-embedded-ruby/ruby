// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"math/big"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerRangeEdges installs the Range core methods that round out MRI 3.4/4.0
// coverage beyond the definitions in the builtins bootstrap: #reverse_each (and
// its sized Enumerator form) and #entries (an alias of #to_a). Range#cover? (the
// range-in-range variant) and Range#size (the endless/beginless/Float edges) are
// wired in the bootstrap itself and delegate to helpers defined here.
func (vm *VM) registerRangeEdges() {
	// bsearch finds an element by binary search over the range's numeric domain
	// (see rangeBsearch); without a block it returns an Enumerator.
	vm.cRange.define("bsearch", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		r := self.(*object.Range)
		// A non-numeric bound cannot be searched — MRI reports this even without a
		// block, before returning the Enumerator.
		for _, b := range []object.Value{r.Lo, r.Hi} {
			if !object.IsNil(b) && !bsearchNumeric(b) {
				raise("TypeError", "can't do binary search for %s", vm.classOf(b).name)
			}
		}
		if blk == nil {
			// bsearch has no meaningful element count, so its Enumerator #size is nil.
			return enumForSized(self, "bsearch", func(*VM) object.Value { return object.NilV })
		}
		return vm.rangeBsearch(r, blk)
	})
	// reverse_each yields the elements from end down to begin. rbgo materialises
	// the range forward (via rangeElems) and walks the slice backwards, so it
	// requires a range that can be enumerated: an endless range (nil end) or a
	// begin that is neither an Integer nor a String cannot be iterated and raises
	// a TypeError naming the offending class, exactly like MRI. Without a block it
	// returns an Enumerator whose #size is the Range#size.
	vm.cRange.define("reverse_each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		r := self.(*object.Range)
		if blk == nil {
			return enumForSized(self, "reverse_each", func(vm *VM) object.Value { return vm.rangeSizeVal(r) })
		}
		if object.IsNil(r.Hi) {
			return raise("TypeError", "can't iterate from %s", vm.classOf(r.Hi).name)
		}
		switch r.Lo.(type) {
		case object.Integer, *object.String:
		default:
			return raise("TypeError", "can't iterate from %s", vm.classOf(r.Lo).name)
		}
		elems := rangeElems(r)
		for i := len(elems) - 1; i >= 0; i-- {
			vm.callBlock(blk, []object.Value{elems[i]})
		}
		return r
	})

	// entries is a straight alias of to_a.
	vm.cRange.define("entries", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(rangeElems(self.(*object.Range)))
	})
}

// isIntegerValue reports whether v is an Integer or Bignum (an exact integer).
func isIntegerValue(v object.Value) bool {
	switch v.(type) {
	case object.Integer, *object.Bignum:
		return true
	}
	return false
}

// rangeCoverValue is the comparison-based membership test shared by Range#cover?,
// #include?, #member? and #=== for a plain (non-Range) argument: v is covered
// when it is not below begin and not past end. A nil bound is open on that side,
// and an incomparable value is simply not covered (MRI returns false, not error).
func rangeCoverValue(r *object.Range, v object.Value) bool {
	if !object.IsNil(r.Lo) {
		lc, lok := rangeCmp(v, r.Lo)
		if !lok || lc < 0 {
			return false
		}
	}
	if object.IsNil(r.Hi) {
		return true
	}
	hc, hok := rangeCmp(v, r.Hi)
	if !hok {
		return false
	}
	if r.Exclusive {
		return hc < 0
	}
	return hc <= 0
}

// rangeEndCmp compares two range ends the way MRI's r_less does for cover?: a nil
// end is +infinity, so nil vs nil compares equal and nil (self endless) vs a
// finite end compares greater. The bool is false when the two ends are of
// incomparable types.
func rangeEndCmp(a, b object.Value) (int, bool) {
	aNil, bNil := object.IsNil(a), object.IsNil(b)
	if aNil && bNil {
		return 0, true
	}
	if aNil {
		return 1, true
	}
	return rangeCmp(a, b)
}

// rangeExclMax returns the greatest value an exclusive range attains (for the
// mixed-exclusivity branch of cover?): for Integer ends that is end-1. Any other
// end type (notably Float, which has no discrete predecessor) reports false so
// cover? conservatively answers false, as MRI does when Range#max raises.
func rangeExclMax(o *object.Range) (object.Value, bool) {
	if hi, ok := o.Hi.(object.Integer); ok {
		return object.IntValue(int64(hi) - 1), true
	}
	return nil, false
}

// rangeCoverRange implements Range#cover? when the argument is itself a Range: it
// answers whether the whole of o lies within self, mirroring MRI's
// r_cover_range_p (begin containment, end comparison with exclusive-end fix-ups,
// and rejection of empty/backward/unbounded-past-self arguments).
func rangeCoverRange(self, o *object.Range) bool {
	begNil, endNil := object.IsNil(self.Lo), object.IsNil(self.Hi)
	vBegNil, vEndNil := object.IsNil(o.Lo), object.IsNil(o.Hi)

	// A bounded side of self cannot contain an unbounded side of o.
	if !endNil && vEndNil {
		return false
	}
	if !begNil && vBegNil {
		return false
	}
	// A backward or empty o (its begin already past its end) is never covered.
	if !vBegNil && !vEndNil {
		thresh := 0
		if o.Exclusive {
			thresh = -1
		}
		if c, _ := rangeCmp(o.Lo, o.Hi); c > thresh {
			return false
		}
	}
	// o's begin must be a covered value of self.
	if !vBegNil && !rangeCoverValue(self, o.Lo) {
		return false
	}

	cmp, ok := rangeEndCmp(self.Hi, o.Hi)
	if !ok {
		return false
	}
	if self.Exclusive == o.Exclusive {
		return cmp >= 0
	}
	if self.Exclusive { // self exclusive, o inclusive: self must end strictly past o
		return cmp > 0
	}
	// self inclusive, o exclusive: self may still cover o if o's greatest value
	// (max, for a discrete end) does not exceed self's end.
	if cmp >= 0 {
		return true
	}
	max, ok := rangeExclMax(o)
	if !ok {
		return false
	}
	mc, ok := rangeCmp(self.Hi, max)
	return ok && mc >= 0
}

// rangeSizeVal is Range#size with MRI 3.4/4.0 semantics. An Integer begin yields
// the element count: a nil or +Infinity end is Infinity, an Integer/Bignum end is
// the exact (possibly big) count, and a finite Float end is counted by whole
// steps. A String/Symbol begin returns nil; any other non-integer begin (Float,
// nil, or a non-iterable object) raises a TypeError naming its class.
func (vm *VM) rangeSizeVal(r *object.Range) object.Value {
	if !isIntegerValue(r.Lo) {
		switch r.Lo.(type) {
		case *object.String, object.Symbol:
			return object.NilV
		}
		return raise("TypeError", "can't iterate from %s", vm.classOf(r.Lo).name)
	}
	if object.IsNil(r.Hi) {
		return object.Float(math.Inf(1))
	}
	if hf, ok := r.Hi.(object.Float); ok {
		lo, _ := toFloat(r.Lo)
		return rangeSizeFloatEnd(lo, float64(hf), r.Exclusive)
	}
	if !isIntegerValue(r.Hi) {
		return raise("TypeError", "can't iterate from %s", vm.classOf(r.Hi).name)
	}
	n := new(big.Int).Sub(bigVal(r.Hi), bigVal(r.Lo))
	if !r.Exclusive {
		n.Add(n, big.NewInt(1))
	}
	if n.Sign() < 0 {
		return object.IntValue(0)
	}
	return object.NormInt(n)
}

// rangeSizeFloatEnd counts the integer steps of an Integer-begin range with a
// Float end: +Infinity end is Infinity, an end at or below begin is 0, and a
// finite end counts floor(end-begin)+1 elements — dropping the last when an
// exclusive end lands exactly on an integer step.
func rangeSizeFloatEnd(lo, hf float64, excl bool) object.Value {
	if math.IsInf(hf, 1) {
		return object.Float(math.Inf(1))
	}
	diff := hf - lo
	if diff < 0 {
		return object.IntValue(0)
	}
	if excl {
		if d := math.Floor(diff); d == diff {
			return object.IntValue(int64(d))
		}
	}
	return object.IntValue(int64(math.Floor(diff)) + 1)
}

// rangeOverlap implements Range#overlap? — whether self and o share at least one
// element. It mirrors MRI's range_overlap: the ranges are disjoint when one's
// end is below the other's begin (touching ends count only when both endpoints
// are inclusive), and an empty (backward, or same-endpoint exclusive) range
// overlaps nothing. A nil bound is treated as an infinity. Incomparable bounds
// report no overlap.
func rangeOverlap(self, o *object.Range) bool {
	// self.end below o.begin, or o.end below self.begin → disjoint.
	if !object.IsNil(self.Hi) && !object.IsNil(o.Lo) {
		c, ok := rangeCmp(self.Hi, o.Lo)
		if !ok || c < 0 || (c == 0 && (self.Exclusive || o.Exclusive)) {
			return false
		}
	}
	if !object.IsNil(o.Hi) && !object.IsNil(self.Lo) {
		c, ok := rangeCmp(o.Hi, self.Lo)
		if !ok || c < 0 || (c == 0 && (self.Exclusive || o.Exclusive)) {
			return false
		}
	}
	// Either range being empty (begin past end, or equal with an exclusive end)
	// means it holds no elements, so nothing overlaps it.
	if rangeIsEmpty(self) || rangeIsEmpty(o) {
		return false
	}
	return true
}

// rangeIsEmpty reports whether a bounded range holds no elements: its begin lies
// past its end, or the two coincide with an exclusive end. An unbounded range is
// never empty.
func rangeIsEmpty(r *object.Range) bool {
	if object.IsNil(r.Lo) || object.IsNil(r.Hi) {
		return false
	}
	// A valid range always has comparable bounds, so rangeCmp reports a result.
	c, _ := rangeCmp(r.Lo, r.Hi)
	return c > 0 || (c == 0 && r.Exclusive)
}
