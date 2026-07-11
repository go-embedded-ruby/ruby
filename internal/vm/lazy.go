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
	kind string // map / select / reject / filter_map / flat_map / grep / grep_v /
	// zip / uniq / compact / with_index / take_while / drop_while / take / drop
	blk    *Proc
	n      int            // element count for take / drop; offset for with_index
	pat    object.Value   // pattern for grep / grep_v
	others []object.Value // extra sources for zip
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
	d("flat_map", chain("flat_map"))
	d("collect_concat", chain("flat_map"))
	d("take_while", chain("take_while"))
	d("drop_while", chain("drop_while"))
	d("take", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return self.(*LazyEnum).with(lazyOp{kind: "take", n: int(intArg(args[0]))})
	})
	d("drop", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return self.(*LazyEnum).with(lazyOp{kind: "drop", n: int(intArg(args[0]))})
	})
	// grep / grep_v take a pattern (matched with #===) and an optional block that
	// maps the elements that pass the filter.
	grepFn := func(kind string) NativeFn {
		return func(_ *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			return self.(*LazyEnum).with(lazyOp{kind: kind, pat: args[0], blk: blk})
		}
	}
	d("grep", grepFn("grep"))
	d("grep_v", grepFn("grep_v"))
	d("compact", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*LazyEnum).with(lazyOp{kind: "compact"})
	})
	// uniq: optional block computes the uniqueness key.
	d("uniq", func(_ *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		return self.(*LazyEnum).with(lazyOp{kind: "uniq", blk: blk})
	})
	// zip pairs each element with the corresponding elements of the other
	// sources (padding with nil once a source is exhausted).
	d("zip", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return self.(*LazyEnum).with(lazyOp{kind: "zip", others: append([]object.Value{}, args...)})
	})
	// with_index(offset = 0): optional block maps (element, index); without a
	// block each element becomes the pair [element, index].
	d("with_index", func(_ *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return self.(*LazyEnum).with(lazyOp{kind: "with_index", n: int(intArgOr(args, 0)), blk: blk})
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
// chains). Each source element is threaded through the ops by feed, which
// recurses op-by-op so that expanding ops (flat_map) and index/zip-carrying ops
// compose the same way MRI's fibered lazy pipeline does.
func (vm *VM) lazyForce(le *LazyEnum, want int) []object.Value {
	if want == 0 {
		return nil
	}
	src := vm.lazySource(le.recv)
	n := len(le.ops)
	// Per-op mutable run state.
	rem := make([]int, n)                             // take / drop remaining
	dropping := make([]bool, n)                       // drop_while latch
	idx := make([]int, n)                             // with_index counter
	seen := make([][]object.Value, n)                 // uniq keys
	zpull := make([][]func() (object.Value, bool), n) // zip other sources
	for i, op := range le.ops {
		switch op.kind {
		case "take", "drop":
			rem[i] = op.n
		case "drop_while":
			dropping[i] = true
		case "with_index":
			idx[i] = op.n // starting offset
		case "zip":
			ps := make([]func() (object.Value, bool), len(op.others))
			for j, o := range op.others {
				ps[j] = vm.lazySource(o)
			}
			zpull[i] = ps
		}
	}
	var out []object.Value
	stop := false
	// feed threads v through ops[i:]; it returns false to abort the whole pull
	// (want satisfied, or a take/take_while boundary reached).
	var feed func(i int, v object.Value) bool
	feed = func(i int, v object.Value) bool {
		if i == n {
			out = append(out, v)
			if want >= 0 && len(out) >= want {
				stop = true
				return false
			}
			return true
		}
		op := le.ops[i]
		switch op.kind {
		case "map":
			return feed(i+1, vm.callBlock(op.blk, []object.Value{v}))
		case "select":
			if vm.callBlock(op.blk, []object.Value{v}).Truthy() {
				return feed(i+1, v)
			}
			return true
		case "reject":
			if !vm.callBlock(op.blk, []object.Value{v}).Truthy() {
				return feed(i+1, v)
			}
			return true
		case "filter_map":
			w := vm.callBlock(op.blk, []object.Value{v})
			if w.Truthy() {
				return feed(i+1, w)
			}
			return true
		case "flat_map":
			w := vm.callBlock(op.blk, []object.Value{v})
			if arr, ok := w.(*object.Array); ok {
				for _, e := range arr.Elems {
					if !feed(i+1, e) {
						return false
					}
				}
				return true
			}
			return feed(i+1, w)
		case "grep":
			if vm.send(op.pat, "===", []object.Value{v}, nil).Truthy() {
				return feed(i+1, vm.lazyGrepValue(op.blk, v))
			}
			return true
		case "grep_v":
			if !vm.send(op.pat, "===", []object.Value{v}, nil).Truthy() {
				return feed(i+1, vm.lazyGrepValue(op.blk, v))
			}
			return true
		case "compact":
			if _, isNil := v.(object.Nil); isNil {
				return true
			}
			return feed(i+1, v)
		case "uniq":
			key := v
			if op.blk != nil {
				key = vm.callBlock(op.blk, []object.Value{v})
			}
			for _, k := range seen[i] {
				if valueEql(key, k) {
					return true
				}
			}
			seen[i] = append(seen[i], key)
			return feed(i+1, v)
		case "with_index":
			j := idx[i]
			idx[i]++
			jv := object.IntValue(int64(j))
			if op.blk != nil {
				// With a block, MRI evaluates it for its side effects and passes
				// the original item downstream (the block's result is ignored).
				vm.callBlock(op.blk, []object.Value{v, jv})
				return feed(i+1, v)
			}
			return feed(i+1, object.NewArrayFromSlice([]object.Value{v, jv}))
		case "zip":
			row := make([]object.Value, len(zpull[i])+1)
			row[0] = v
			for j, pf := range zpull[i] {
				if e, ok := pf(); ok {
					row[j+1] = e
				} else {
					row[j+1] = object.NilV
				}
			}
			return feed(i+1, object.NewArrayFromSlice(row))
		case "take_while":
			if !vm.callBlock(op.blk, []object.Value{v}).Truthy() {
				stop = true
				return false
			}
			return feed(i+1, v)
		case "drop_while":
			if dropping[i] {
				if vm.callBlock(op.blk, []object.Value{v}).Truthy() {
					return true
				}
				dropping[i] = false
			}
			return feed(i+1, v)
		case "take":
			if rem[i] <= 0 {
				stop = true
				return false
			}
			rem[i]--
			return feed(i+1, v)
		case "drop":
			if rem[i] > 0 {
				rem[i]--
				return true
			}
			// Past the drop count: fall through to pass v downstream.
		}
		return feed(i+1, v)
	}
	for !stop {
		v, ok := src()
		if !ok {
			break
		}
		if !feed(0, v) {
			break
		}
	}
	return out
}

// lazyGrepValue returns the value grep/grep_v should emit for a match: the
// element itself, or the block's mapping of it when a block was given.
func (vm *VM) lazyGrepValue(blk *Proc, v object.Value) object.Value {
	if blk != nil {
		return vm.callBlock(blk, []object.Value{v})
	}
	return v
}
