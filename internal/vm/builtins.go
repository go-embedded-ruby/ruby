package vm

import (
	"fmt"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// bootstrap builds the base class hierarchy and installs the Phase 1 kernel.
// Kernel methods live on Object so every value answers them.
func (vm *VM) bootstrap() {
	vm.cBasicObject = newClass("BasicObject", nil)
	vm.cObject = newClass("Object", vm.cBasicObject)
	vm.cModule = newClass("Module", vm.cObject)
	vm.cClass = newClass("Class", vm.cModule)
	vm.cInteger = newClass("Integer", vm.cObject)
	vm.cFloat = newClass("Float", vm.cObject)
	vm.cString = newClass("String", vm.cObject)
	vm.cSymbol = newClass("Symbol", vm.cObject)
	vm.cArray = newClass("Array", vm.cObject)
	vm.cHash = newClass("Hash", vm.cObject)
	vm.cRange = newClass("Range", vm.cObject)
	vm.cTrueClass = newClass("TrueClass", vm.cObject)
	vm.cFalseClass = newClass("FalseClass", vm.cObject)
	vm.cNilClass = newClass("NilClass", vm.cObject)

	for _, c := range []*RClass{
		vm.cBasicObject, vm.cObject, vm.cModule, vm.cClass, vm.cInteger,
		vm.cFloat, vm.cString, vm.cSymbol, vm.cArray, vm.cHash, vm.cRange,
		vm.cTrueClass, vm.cFalseClass, vm.cNilClass,
	} {
		vm.consts[c.name] = c
	}

	// Kernel (on Object).
	vm.cObject.define("puts", nativePuts)
	vm.cObject.define("print", nativePrint)
	vm.cObject.define("p", nativeP)
	vm.cObject.define("class", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.classOf(self)
	})
	vm.cObject.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(self.ToS())
	})
	vm.cObject.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(self.Inspect())
	})
	vm.cObject.define("nil?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		_, isNil := self.(object.Nil)
		return object.Bool(isNil)
	})
	vm.cObject.define("initialize", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NilV
	})
	vm.cObject.define("method_missing", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := args[0].ToS()
		return raise("NoMethodError", "undefined method '%s' for %s", name, vm.classOf(self).name)
	})
	// Default equality: object identity for instances, structural for value
	// types (Comparable#== and user-defined == override this via dispatch).
	vm.cObject.define("==", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(rubyEqual(self, args[0]))
	})
	// Default <=>: 0 when equal (by ==), nil otherwise — the MRI Object#<=>.
	vm.cObject.define("<=>", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if vm.send(self, "==", []object.Value{args[0]}, nil).Truthy() {
			return object.Integer(0)
		}
		return object.NilV
	})

	// Module (Class inherits these).
	vm.cModule.define("include", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		target := self.(*RClass)
		for _, a := range args {
			target.includes = append(target.includes, a.(*RClass))
		}
		return target
	})

	// Symbol.
	vm.cSymbol.define("to_sym", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	// Spaceship (<=>) for the built-in ordered types; numerics compare across
	// Integer/Float, strings lexically, and a mismatched type yields nil.
	vm.cInteger.define("<=>", spaceshipNumeric)
	vm.cFloat.define("<=>", spaceshipNumeric)
	vm.cString.define("<=>", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(object.String)
		b, ok := args[0].(object.String)
		if !ok {
			return object.NilV
		}
		return object.Integer(strings.Compare(string(a), string(b)))
	})

	// Array.
	vm.cArray.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(len(self.(*object.Array).Elems))
	})
	vm.cArray.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(len(self.(*object.Array).Elems))
	})
	vm.cArray.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(len(self.(*object.Array).Elems) == 0)
	})
	vm.cArray.define("first", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		if len(a.Elems) == 0 {
			return object.NilV
		}
		return a.Elems[0]
	})
	vm.cArray.define("last", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		if len(a.Elems) == 0 {
			return object.NilV
		}
		return a.Elems[len(a.Elems)-1]
	})
	vm.cArray.define("push", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		a.Elems = append(a.Elems, args...)
		return a
	})
	vm.cArray.define("<<", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		a.Elems = append(a.Elems, args[0])
		return a
	})
	vm.cArray.define("include?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		for _, e := range self.(*object.Array).Elems {
			if valueEqual(e, args[0]) {
				return object.True
			}
		}
		return object.False
	})
	vm.cArray.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		if i, ok := arrayIndex(a, intArg(args[0])); ok {
			return a.Elems[i]
		}
		return object.NilV
	})
	vm.cArray.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := self.(*object.Array)
		i, n := intArg(args[0]), int64(len(a.Elems))
		if i < 0 {
			i += n
		}
		if i < 0 || i > n {
			raise("IndexError", "index %d out of array", intArg(args[0]))
		}
		if i == n {
			a.Elems = append(a.Elems, args[1])
		} else {
			a.Elems[i] = args[1]
		}
		return args[1]
	})
	vm.cArray.define("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		a := self.(*object.Array)
		for _, e := range a.Elems {
			vm.callBlock(blk, []object.Value{e})
		}
		return a
	})
	vm.cArray.define("map", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (map)")
		}
		a := self.(*object.Array)
		out := make([]object.Value, len(a.Elems))
		for i, e := range a.Elems {
			out[i] = vm.callBlock(blk, []object.Value{e})
		}
		return &object.Array{Elems: out}
	})

	// Hash.
	vm.cHash.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if v, ok := self.(*object.Hash).Get(args[0]); ok {
			return v
		}
		return object.NilV
	})
	vm.cHash.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*object.Hash).Set(args[0], args[1])
		return args[1]
	})
	vm.cHash.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self.(*object.Hash).Len())
	})
	vm.cHash.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self.(*object.Hash).Len())
	})
	vm.cHash.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*object.Hash).Len() == 0)
	})
	hashKeyP := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := self.(*object.Hash).Get(args[0])
		return object.Bool(ok)
	}
	vm.cHash.define("key?", hashKeyP)
	vm.cHash.define("has_key?", hashKeyP)
	vm.cHash.define("include?", hashKeyP)
	vm.cHash.define("keys", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		ks := make([]object.Value, len(h.Keys))
		copy(ks, h.Keys)
		return &object.Array{Elems: ks}
	})
	vm.cHash.define("values", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := self.(*object.Hash)
		vs := make([]object.Value, 0, len(h.Keys))
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			vs = append(vs, v)
		}
		return &object.Array{Elems: vs}
	})
	vm.cHash.define("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		h := self.(*object.Hash)
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			vm.callBlock(blk, []object.Value{k, v})
		}
		return h
	})

	// Range.
	vm.cRange.define("begin", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*object.Range).Lo
	})
	vm.cRange.define("first", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*object.Range).Lo
	})
	vm.cRange.define("end", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*object.Range).Hi
	})
	vm.cRange.define("last", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*object.Range).Hi
	})
	vm.cRange.define("exclude_end?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*object.Range).Exclusive)
	})
	rangeCover := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		v := args[0]
		// cover? is comparison-based: an incomparable member is simply not
		// covered (Ruby returns false rather than raising).
		lc, lok := rangeCmp(v, r.Lo)
		hc, hok := rangeCmp(v, r.Hi)
		if !lok || !hok || lc < 0 {
			return object.False
		}
		if r.Exclusive {
			return object.Bool(hc < 0)
		}
		return object.Bool(hc <= 0)
	}
	vm.cRange.define("include?", rangeCover)
	vm.cRange.define("cover?", rangeCover)
	vm.cRange.define("member?", rangeCover)
	vm.cRange.define("min", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		lo, _, _ := rangeInts(r)
		if rangeSize(r) == 0 {
			return object.NilV
		}
		return object.Integer(lo)
	})
	vm.cRange.define("max", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		r := self.(*object.Range)
		_, hi, _ := rangeInts(r)
		if rangeSize(r) == 0 {
			return object.NilV
		}
		if r.Exclusive {
			return object.Integer(hi - 1)
		}
		return object.Integer(hi)
	})
	rangeSizeFn := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(rangeSize(self.(*object.Range)))
	}
	vm.cRange.define("size", rangeSizeFn)
	vm.cRange.define("count", rangeSizeFn)
	vm.cRange.define("to_a", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Array{Elems: rangeElems(self.(*object.Range))}
	})
	vm.cRange.define("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		r := self.(*object.Range)
		for _, e := range rangeElems(r) {
			vm.callBlock(blk, []object.Value{e})
		}
		return r
	})
	vm.cRange.define("map", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (map)")
		}
		elems := rangeElems(self.(*object.Range))
		out := make([]object.Value, len(elems))
		for i, e := range elems {
			out[i] = vm.callBlock(blk, []object.Value{e})
		}
		return &object.Array{Elems: out}
	})

	// Class.
	vm.cClass.define("new", nativeNew)

	// Integer#times — the first block-driven iterator.
	vm.cInteger.define("times", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (times)")
		}
		n := int64(self.(object.Integer))
		for i := int64(0); i < n; i++ {
			vm.callBlock(blk, []object.Value{object.Integer(i)})
		}
		return self
	})
}

// nativeNew allocates an instance of the receiver class and runs initialize,
// forwarding any block.
func nativeNew(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
	class := self.(*RClass)
	obj := &RObject{class: class, ivars: map[string]object.Value{}}
	vm.send(obj, "initialize", args, blk)
	return obj
}

// intArg coerces an argument used as an array index to int64, or raises.
func intArg(v object.Value) int64 {
	if i, ok := v.(object.Integer); ok {
		return int64(i)
	}
	raise("TypeError", "no implicit conversion of %s into Integer", v.Inspect())
	return 0
}

// arrayIndex normalizes a (possibly negative) index and reports whether it is in
// range.
func arrayIndex(a *object.Array, i int64) (int, bool) {
	n := int64(len(a.Elems))
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return 0, false
	}
	return int(i), true
}

func nativePuts(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	if len(args) == 0 {
		fmt.Fprintln(vm.out)
		return object.NilV
	}
	for _, a := range args {
		putsValue(vm, a)
	}
	return object.NilV
}

// putsValue prints one value the way Kernel#puts does: arrays are flattened (an
// empty array prints nothing), everything else prints its to_s plus a newline.
func putsValue(vm *VM, v object.Value) {
	if arr, ok := v.(*object.Array); ok {
		for _, e := range arr.Elems {
			putsValue(vm, e)
		}
		return
	}
	fmt.Fprintln(vm.out, v.ToS())
}

func nativePrint(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	for _, a := range args {
		fmt.Fprint(vm.out, a.ToS())
	}
	return object.NilV
}

func nativeP(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
	for _, a := range args {
		fmt.Fprintln(vm.out, a.Inspect())
	}
	switch len(args) {
	case 0:
		return object.NilV
	case 1:
		return args[0]
	default:
		return object.NilV // Ruby returns the args array; arrays arrive in Phase 2
	}
}


// rubyEqual is the default Object#== : pointer identity for instances, and
// structural equality for the immutable value types.
func rubyEqual(a, b object.Value) bool {
	if ao, ok := a.(*RObject); ok {
		bo, ok := b.(*RObject)
		return ok && ao == bo
	}
	return valueEqual(a, b)
}

// spaceshipNumeric implements Integer#<=> and Float#<=>: -1/0/1 across the
// numeric tower, nil for a non-numeric argument.
func spaceshipNumeric(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
	a, _ := toFloat(self)
	b, ok := toFloat(args[0])
	if !ok {
		return object.NilV
	}
	switch {
	case a < b:
		return object.Integer(-1)
	case a > b:
		return object.Integer(1)
	default:
		return object.Integer(0)
	}
}

// rangeCmp orders two values for Range membership tests: numerics compare
// numerically, strings lexically; any other pairing is incomparable (ok=false,
// mirroring Ruby's <=> returning nil).
func rangeCmp(a, b object.Value) (ord int, ok bool) {
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			switch {
			case af < bf:
				return -1, true
			case af > bf:
				return 1, true
			default:
				return 0, true
			}
		}
		return 0, false
	}
	as, aok := a.(object.String)
	bs, bok := b.(object.String)
	if aok && bok {
		return strings.Compare(string(as), string(bs)), true
	}
	return 0, false
}

// rangeInts extracts integer endpoints. ok is false when either endpoint is not
// an Integer (string/float ranges are not iterable in this phase).
func rangeInts(r *object.Range) (lo, hi int64, ok bool) {
	li, lok := r.Lo.(object.Integer)
	hi2, hok := r.Hi.(object.Integer)
	if !lok || !hok {
		return 0, 0, false
	}
	return int64(li), int64(hi2), true
}

// rangeSize is the element count of an integer range (0 if empty or
// non-integer), matching Ruby's Range#size.
func rangeSize(r *object.Range) int64 {
	lo, hi, ok := rangeInts(r)
	if !ok {
		raise("TypeError", "can't iterate from %s", r.Lo.Inspect())
	}
	n := hi - lo
	if !r.Exclusive {
		n++
	}
	if n < 0 {
		return 0
	}
	return n
}

// rangeElems materializes an integer range to a slice, raising TypeError on
// non-integer endpoints (Ruby: "can't iterate from String").
func rangeElems(r *object.Range) []object.Value {
	lo, hi, ok := rangeInts(r)
	if !ok {
		raise("TypeError", "can't iterate from %s", r.Lo.Inspect())
	}
	if r.Exclusive {
		hi--
	}
	if hi < lo {
		return nil
	}
	out := make([]object.Value, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		out = append(out, object.Integer(i))
	}
	return out
}
