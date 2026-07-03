package vm

import (
	"math"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// LazyEnum is Ruby's Enumerator::Lazy: a source plus a chain of transformations
// that are applied on demand, one element at a time, when a terminal operation
// (first/to_a/force/each) pulls. This makes infinite sources usable —
// (1..Float::INFINITY).lazy.map { … }.first(5) — without materialising them.
type LazyEnum struct {
	recv object.Value
	ops  []lazyOp
}

type lazyOp struct {
	kind string // map / select / reject / filter_map / take_while / drop_while / take / drop
	blk  *Proc
	n    int // element count for take / drop
}

func (l *LazyEnum) ToS() string {
	s := "#<Enumerator::Lazy: " + l.recv.Inspect()
	for _, op := range l.ops {
		s += ":" + op.kind
	}
	return s + ">"
}
func (l *LazyEnum) Inspect() string { return l.ToS() }
func (l *LazyEnum) Truthy() bool    { return true }

// with returns a copy of l with one more transformation appended.
func (l *LazyEnum) with(op lazyOp) *LazyEnum {
	ops := append(append([]lazyOp{}, l.ops...), op)
	return &LazyEnum{recv: l.recv, ops: ops}
}

func (vm *VM) registerLazy() {
	vm.cLazy = newClass("Enumerator::Lazy", vm.cEnumerator)
	vm.cEnumerator.consts["Lazy"] = vm.cLazy

	makeLazy := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &LazyEnum{recv: self}
	}
	// Sources that lazy iteration can drive directly. (#lazy on a Lazy is itself,
	// defined separately below.)
	vm.cArray.define("lazy", makeLazy)
	vm.cRange.define("lazy", makeLazy)
	vm.cHash.define("lazy", makeLazy)
	vm.cEnumerator.define("lazy", makeLazy)

	d := func(name string, fn NativeFn) { vm.cLazy.define(name, fn) }
	chain := func(kind string) NativeFn {
		return func(_ *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
			if blk == nil {
				raise("ArgumentError", "tried to call lazy %s without a block", kind)
			}
			return self.(*LazyEnum).with(lazyOp{kind: kind, blk: blk})
		}
	}
	d("map", chain("map"))
	d("collect", chain("map"))
	d("select", chain("select"))
	d("filter", chain("select"))
	d("reject", chain("reject"))
	d("filter_map", chain("filter_map"))
	d("take_while", chain("take_while"))
	d("drop_while", chain("drop_while"))
	d("take", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return self.(*LazyEnum).with(lazyOp{kind: "take", n: int(intArg(args[0]))})
	})
	d("drop", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return self.(*LazyEnum).with(lazyOp{kind: "drop", n: int(intArg(args[0]))})
	})
	d("lazy", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return self })

	toA := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(vm.lazyForce(self.(*LazyEnum), -1))
	}
	d("to_a", toA)
	d("force", toA)
	d("first", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			got := vm.lazyForce(self.(*LazyEnum), 1)
			if len(got) == 0 {
				return object.NilV
			}
			return got[0]
		}
		return object.NewArrayFromSlice(vm.lazyForce(self.(*LazyEnum), int(intArg(args[0]))))
	})
	d("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		l := self.(*LazyEnum)
		if blk == nil {
			return l
		}
		for _, v := range vm.lazyForce(l, -1) {
			vm.callBlock(blk, []object.Value{v})
		}
		return l
	})
}

// lazySource returns a restartable pull function over le.recv: successive calls
// yield the next source element until it returns ok=false. Integer ranges
// (including endless and Float::INFINITY-bounded) are walked by counter; arrays
// by index; any other Enumerable is materialised once (so it must be finite).
func (vm *VM) lazySource(recv object.Value) func() (object.Value, bool) {
	switch r := recv.(type) {
	case *object.Array:
		i := 0
		return func() (object.Value, bool) {
			if i < len(r.Elems) {
				v := r.Elems[i]
				i++
				return v, true
			}
			return object.NilVal(), false
		}
	case *object.Range:
		lo, ok := r.Lo.(object.Integer)
		if !ok {
			raise("TypeError", "can't iterate from %s", r.Lo.Inspect())
		}
		i, hi, unbounded := int64(lo), int64(0), false
		switch h := r.Hi.(type) {
		case object.Nil:
			unbounded = true
		case object.Float:
			if math.IsInf(float64(h), 1) {
				unbounded = true
			} else {
				hi = int64(h)
			}
		case object.Integer:
			hi = int64(h)
		default:
			raise("TypeError", "can't iterate to %s", r.Hi.Inspect())
		}
		if r.Exclusive && !unbounded {
			hi--
		}
		return func() (object.Value, bool) {
			if !unbounded && i > hi {
				return object.NilVal(), false
			}
			v := object.IntValue(i)
			i++
			return v, true
		}
	default:
		buf := vm.collectEach(recv)
		i := 0
		return func() (object.Value, bool) {
			if i < len(buf) {
				v := buf[i]
				i++
				return v, true
			}
			return object.NilVal(), false
		}
	}
}

// collectEach materialises recv by running its #each — reusing the Enumerator
// machinery so a multi-value yield (Hash pairs, etc.) is handled identically.
func (vm *VM) collectEach(recv object.Value) []object.Value {
	return vm.enumMaterialize(&Enumerator{recv: recv, meth: "each"})
}

// lazyForce pulls from the source, applying the op chain to each element, until
// want elements are produced (want < 0 means all — only safe for finite/limited
// chains).
func (vm *VM) lazyForce(le *LazyEnum, want int) []object.Value {
	src := vm.lazySource(le.recv)
	// Per-op mutable counters for take/drop and the drop_while latch.
	rem := make([]int, len(le.ops))
	dropping := make([]bool, len(le.ops))
	for i, op := range le.ops {
		switch op.kind {
		case "take", "drop":
			rem[i] = op.n
		case "drop_while":
			dropping[i] = true
		}
	}
	var out []object.Value
	for {
		v, ok := src()
		if !ok {
			break
		}
		cur, keep, stop := v, true, false
		for i, op := range le.ops {
			switch op.kind {
			case "map":
				cur = vm.callBlock(op.blk, []object.Value{cur})
			case "select":
				keep = vm.callBlock(op.blk, []object.Value{cur}).Truthy()
			case "reject":
				keep = !vm.callBlock(op.blk, []object.Value{cur}).Truthy()
			case "filter_map":
				w := vm.callBlock(op.blk, []object.Value{cur})
				if keep = w.Truthy(); keep {
					cur = w
				}
			case "take_while":
				if !vm.callBlock(op.blk, []object.Value{cur}).Truthy() {
					stop = true
				}
			case "drop_while":
				if dropping[i] {
					if vm.callBlock(op.blk, []object.Value{cur}).Truthy() {
						keep = false
					} else {
						dropping[i] = false
					}
				}
			case "take":
				if rem[i] <= 0 {
					stop = true
				} else {
					rem[i]--
				}
			case "drop":
				if rem[i] > 0 {
					rem[i]--
					keep = false
				}
			}
			if stop || !keep {
				break
			}
		}
		if stop {
			break
		}
		if keep {
			out = append(out, cur)
			if want >= 0 && len(out) >= want {
				break
			}
		}
	}
	return out
}
