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

// rangeCmpV compares a <=> b by dispatching Ruby's #<=> through the VM, so that
// custom Comparable objects, Numeric subclasses and Time all order correctly (the
// pure rangeCmp only understands built-in numerics and strings). It returns the
// comparison sign and whether the two values were comparable at all — a #<=> that
// returns nil (or any non-Integer) yields ok=false, exactly as MRI's r_less
// treats an incomparable pair.
func (vm *VM) rangeCmpV(a, b object.Value) (int, bool) {
	r := vm.send(a, "<=>", []object.Value{b}, nil)
	n, ok := r.(object.Integer)
	if !ok {
		return 0, false
	}
	switch {
	case n < 0:
		return -1, true
	case n > 0:
		return 1, true
	default:
		return 0, true
	}
}

// linearObjectP mirrors MRI's linear_object_p: a value whose position on the
// number/time line is well defined — any Numeric (built-in or a Numeric subclass)
// or a Time. Range#include? uses cover? (pure #<=>) logic when either endpoint is
// such a value, rather than walking discrete successors.
func (vm *VM) linearObjectP(v object.Value) bool {
	if isNumericValue(v) {
		return true
	}
	cls := vm.classOf(v)
	if num := vm.cInteger.super; num != nil && classIsA(cls, num) {
		return true
	}
	return classIsA(cls, vm.cTime)
}

// rangeCoverValueV is MRI's r_cover_p — the comparison-based membership test
// shared by Range#cover?, #=== and (for linear/String endpoints) #include? on a
// plain argument. The low bound is checked as begin <=> v (so the endpoint, not
// the argument, drives coercion — matching which object receives #coerce/#<=>),
// the high bound as v <=> end. A nil bound is open on that side; an incomparable
// value is simply not covered (MRI answers false, never raising).
func (vm *VM) rangeCoverValueV(r *object.Range, v object.Value) bool {
	if !object.IsNil(r.Lo) {
		lc, lok := vm.rangeCmpV(r.Lo, v)
		if !lok || lc > 0 {
			return false
		}
	}
	if object.IsNil(r.Hi) {
		return true
	}
	hc, hok := vm.rangeCmpV(v, r.Hi)
	if !hok {
		return false
	}
	if r.Exclusive {
		return hc < 0
	}
	return hc <= 0
}

// rangeEndCmpV compares two range ends the way MRI's r_less does for cover?: a nil
// end is +infinity, so nil vs nil compares equal and nil (self endless) vs a
// finite end compares greater. The bool is false when the two ends are of
// incomparable types. It dispatches #<=> so generic comparable bounds work.
func (vm *VM) rangeEndCmpV(a, b object.Value) (int, bool) {
	aNil, bNil := object.IsNil(a), object.IsNil(b)
	if aNil && bNil {
		return 0, true
	}
	if aNil {
		return 1, true
	}
	return vm.rangeCmpV(a, b)
}

// rangeCoverRange implements Range#cover? when the argument is itself a Range: it
// answers whether the whole of o lies within self, mirroring MRI's
// r_cover_range_p (begin containment, end comparison with exclusive-end fix-ups,
// and rejection of empty/backward/unbounded-past-self arguments). It dispatches
// #<=> so generic comparable bounds (a Numeric subclass, a Comparable object) are
// ordered by their own comparison, not just the built-in numeric/string cases.
func (vm *VM) rangeCoverRange(self, o *object.Range) bool {
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
		if c, ok := vm.rangeCmpV(o.Lo, o.Hi); ok && c > thresh {
			return false
		}
	}
	// o's begin must be a covered value of self.
	if !vBegNil && !vm.rangeCoverValueV(self, o.Lo) {
		return false
	}

	cmp, ok := vm.rangeEndCmpV(self.Hi, o.Hi)
	if !ok {
		return false
	}
	if self.Exclusive == o.Exclusive {
		return cmp >= 0
	}
	if self.Exclusive { // self exclusive, o inclusive: self must end strictly past o
		return cmp > 0
	}
	// self inclusive, o exclusive, self.end < o.end: self still covers o when o's
	// greatest value does not exceed self.end. For a discrete end that greatest
	// value is o.end's predecessor, so — mirroring MRI's r_cover_range_p — self
	// covers o iff self.end's successor reaches or passes o.end (self.end.succ >=
	// o.end). An end without #succ (e.g. a Float) has no such neighbour, so cover
	// conservatively answers false.
	if cmp >= 0 {
		return true
	}
	if !vm.respondsToDynamic(self.Hi, "succ") {
		return false
	}
	sc, ok := vm.rangeCmpV(vm.send(self.Hi, "succ", nil, nil), o.Hi)
	return ok && sc >= 0
}

// rangeInclude implements Range#include? / #member? — MRI's range_include_internal
// with string_use_cover = 0. When either endpoint is a linear object (Numeric or
// Time), or when both endpoints are nil and the argument itself is linear, it
// falls back to the pure #<=> cover test. When both endpoints are Strings it does
// a discrete #succ-walk membership test comparing with #== (so ('a'..'ab') covers
// 'aa' but not 'ac'). Every other shape — a beginless/endless String range, a
// custom Comparable range, (nil..nil) with a non-linear argument — is iterated by
// Enumerable#include?, which walks #succ and raises TypeError when the range
// cannot be enumerated. This matches MRI's routing to rb_call_super.
func (vm *VM) rangeInclude(r *object.Range, val object.Value) bool {
	beg, end := r.Lo, r.Hi
	begNil, endNil := object.IsNil(beg), object.IsNil(end)
	if vm.linearObjectP(beg) || vm.linearObjectP(end) ||
		(begNil && endNil && vm.linearObjectP(val)) {
		return vm.rangeCoverValueV(r, val)
	}
	_, begStr := beg.(*object.String)
	_, endStr := end.(*object.String)
	if begStr && endStr {
		return vm.strIncludeRangeP(r, val)
	}
	// Fall back to Enumerable#include?: walk the range's elements (via #succ) and
	// compare with #==. A beginless or endless range (including (nil..nil) with a
	// non-linear argument) cannot be enumerated, so — like MRI's super call, whose
	// #each raises here — it is a TypeError; rangeElemsV likewise raises for a
	// bounded begin that lacks #succ.
	if begNil || endNil {
		raise("TypeError", "can't iterate from %s", vm.classOf(beg).name)
	}
	for _, e := range vm.rangeElemsV(r) {
		if vm.send(e, "==", []object.Value{val}, nil).Truthy() {
			return true
		}
	}
	return false
}

// strIncludeRangeP is the discrete membership test for a String..String range
// (MRI's rb_str_include_range_p with string_use_cover = 0): the argument is
// coerced to a String via #to_str (a non-String that does not respond to #to_str
// is simply not a member; a #to_str that returns a non-String raises TypeError),
// then tested against the range's succ-walked elements with String equality.
func (vm *VM) strIncludeRangeP(r *object.Range, val object.Value) bool {
	s, ok := val.(*object.String)
	if !ok {
		if !vm.respondsToDynamic(val, "to_str") {
			return false
		}
		conv := vm.send(val, "to_str", nil, nil)
		cs, isStr := conv.(*object.String)
		if !isStr {
			raise("TypeError", "can't convert %s to String (%s#to_str gives %s)",
				vm.classOf(val).name, vm.classOf(val).name, vm.classOf(conv).name)
		}
		s = cs
	}
	target := s.Str()
	beg := r.Lo.(*object.String).Str()
	end := r.Hi.(*object.String).Str()
	for _, e := range strRangeElems(beg, end, r.Exclusive) {
		if e.(*object.String).Str() == target {
			return true
		}
	}
	return false
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
