package vm

import (
	"strings"

	goset "github.com/go-composites/set/src"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Set binds github.com/go-composites/set — the first real consumer of the
// go-composites family — into Ruby, mirroring how the FFT module binds go-fft.
// A go-composites Set stores arbitrary comparable Go items; Ruby values are not
// all comparable by value (a *String is a pointer), so each member is marshalled
// to a canonical comparable key (setKey) which is what the go-composites Set
// actually holds. A parallel order-preserving table recovers the original Ruby
// value from a key for each / to_a / inspect, and gives Ruby's insertion-ordered
// iteration on top of the (unordered, map-backed) go-composites Set.

// Set is the Ruby wrapper around a go-composites Set.
type Set struct {
	s     goset.Interface      // membership / algebra, keyed by canonical keys
	vals  map[any]object.Value // canonical key -> original Ruby value
	order []any                // canonical keys in insertion order (Ruby ordering)
}

func (s *Set) ToS() string     { return s.repr() }
func (s *Set) Inspect() string { return s.repr() }
func (s *Set) Truthy() bool    { return true }

// repr renders MRI's "#<Set: {1, 2, 3}>", members in insertion order.
func (s *Set) repr() string {
	var b strings.Builder
	b.WriteString("#<Set: {")
	for i, k := range s.order {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(s.vals[k].Inspect())
	}
	b.WriteString("}>")
	return b.String()
}

// newSet builds an empty Ruby Set wrapper.
func newSet() *Set {
	return &Set{s: goset.New(), vals: map[any]object.Value{}}
}

// setKey marshals a Ruby value to a canonical comparable Go key, raising
// TypeError for a value that cannot be a Set member (Array/Hash/Set/Proc/…).
// A String keys by its byte content (distinct from a Symbol of the same name,
// as in Hash keys); the other immutable value types key by themselves.
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
		return "str:" + string(x.B)
	}
	raise("TypeError", "%s is not a valid Set member", v.Inspect())
	return nil
}

// add inserts a Ruby value, preserving first-insertion order (idempotent).
func (s *Set) add(v object.Value) {
	k := setKey(v)
	if !s.s.Has(k) {
		s.order = append(s.order, k)
		s.vals[k] = v
	}
	s.s.Add(k)
}

// delete removes a Ruby value (a no-op when absent).
func (s *Set) delete(v object.Value) {
	k := setKey(v)
	if !s.s.Has(k) {
		return
	}
	s.s.Delete(k)
	delete(s.vals, k)
	for i, ok := range s.order {
		if ok == k {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

// setArg asserts an argument is a Set, raising TypeError otherwise.
func setArg(v object.Value) *Set {
	s, ok := v.(*Set)
	if !ok {
		raise("TypeError", "value must be a Set")
	}
	return s
}

// seed adds every element of an enumerable (Array or Set) to s.
func (s *Set) seed(v object.Value) {
	switch e := v.(type) {
	case *object.Array:
		for _, el := range e.Elems {
			s.add(el)
		}
	case *Set:
		for _, k := range e.order {
			s.add(e.vals[k])
		}
	default:
		raise("TypeError", "value must be enumerable (Array or Set)")
	}
}

// fromKeys rebuilds a Ruby Set from a go-composites key set, recovering each
// key's Ruby value from a (b before a, so a's order/value wins) — used by the
// algebraic combinators.
func fromKeys(keys goset.Interface, a, b *Set) *Set {
	out := newSet()
	// Preserve a's insertion order first, then b's, keeping only kept keys.
	for _, src := range []*Set{a, b} {
		if src == nil {
			continue
		}
		for _, k := range src.order {
			if keys.Has(k) && !out.s.Has(k) {
				out.order = append(out.order, k)
				out.vals[k] = src.vals[k]
				out.s.Add(k)
			}
		}
	}
	return out
}

// copy returns a shallow clone: a new Set holding the same members in the same
// insertion order (the Ruby values are shared, as MRI's Set#dup does).
func (s *Set) copy() *Set {
	out := newSet()
	for _, k := range s.order {
		out.add(s.vals[k])
	}
	return out
}

// toArray materialises the Set into a Ruby Array in insertion order.
func (s *Set) toArray() object.Value {
	out := make([]object.Value, len(s.order))
	for i, k := range s.order {
		out[i] = s.vals[k]
	}
	return &object.Array{Elems: out}
}

// setOp implements the Set operator fast path reached from binary(): + is union
// and - is difference (& | << dispatch as ordinary methods). The right operand
// must be a Set.
func setOp(op bytecode.Op, a *Set, b object.Value) object.Value {
	switch op {
	case bytecode.OpAdd:
		return fromKeys(a.s.Union(setArg(b).s), a, setArg(b))
	case bytecode.OpSub:
		return fromKeys(a.s.Difference(setArg(b).s), a, nil)
	}
	return raise("NoMethodError", "undefined method '%s' for a Set", op)
}

// registerSet installs the Set class, its constructor and instance methods.
func (vm *VM) registerSet() {
	vm.cSet = newClass("Set", vm.cObject)
	vm.consts["Set"] = vm.cSet

	// Set.new(enumerable=nil): empty, or seeded from an Array/Set.
	vm.cSet.smethods["new"] = &Method{name: "new", owner: vm.cSet,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			s := newSet()
			if len(args) > 0 {
				if _, isNil := args[0].(object.Nil); !isNil {
					s.seed(args[0])
				}
			}
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

	addFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.add(args[0])
		return s
	}
	d("add", addFn)
	d("<<", addFn)

	d("add?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		if s.s.Has(setKey(args[0])) {
			return object.NilV
		}
		s.add(args[0])
		return s
	})
	d("delete", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.delete(args[0])
		return s
	})

	includeFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).s.Has(setKey(args[0])))
	}
	d("include?", includeFn)
	d("member?", includeFn)
	d("===", includeFn)

	sizeFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self(v).s.Len())
	}
	d("size", sizeFn)
	d("length", sizeFn)
	d("count", sizeFn)

	d("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).s.IsEmpty())
	})
	d("clear", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.s = goset.New()
		s.vals = map[any]object.Value{}
		s.order = nil
		return s
	})

	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		s := self(v)
		for _, k := range s.order {
			vm.callBlock(blk, []object.Value{s.vals[k]})
		}
		return s
	})
	d("to_a", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).toArray()
	})
	d("to_set", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return v
	})

	unionFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return fromKeys(a.s.Union(b.s), a, b)
	}
	d("|", unionFn)
	d("union", unionFn)

	interFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return fromKeys(a.s.Intersection(b.s), a, b)
	}
	d("&", interFn)
	d("intersection", interFn)

	diffFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return fromKeys(a.s.Difference(b.s), a, nil)
	}
	d("difference", diffFn)

	subsetFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).s.IsSubset(setArg(args[0]).s))
	}
	d("subset?", subsetFn)
	d("<=", subsetFn)
	d("superset?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(setArg(args[0]).s.IsSubset(self(v).s))
	})
	d(">=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(setArg(args[0]).s.IsSubset(self(v).s))
	})

	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		b, ok := args[0].(*Set)
		if !ok {
			return object.False
		}
		return object.Bool(self(v).s.Equal(b.s))
	})

	d("merge", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		for _, a := range args {
			s.seed(a)
		}
		return s
	})

	// map / collect: yield each member, collect the block results into a new
	// Array (MRI's Set#map/#collect return an Array, not a Set).
	mapFn := func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (map)")
		}
		s := self(v)
		out := make([]object.Value, 0, len(s.order))
		for _, k := range s.order {
			out = append(out, vm.callBlock(blk, []object.Value{s.vals[k]}))
		}
		return &object.Array{Elems: out}
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
		for _, k := range s.order {
			if vm.callBlock(blk, []object.Value{s.vals[k]}).Truthy() {
				out.add(s.vals[k])
			}
		}
		return out
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
		for _, k := range s.order {
			if !vm.callBlock(blk, []object.Value{s.vals[k]}).Truthy() {
				out.add(s.vals[k])
			}
		}
		return out
	})

	// find / detect: the first member (insertion order) for which the block is
	// truthy, or nil when none match.
	findFn := func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (find)")
		}
		s := self(v)
		for _, k := range s.order {
			if vm.callBlock(blk, []object.Value{s.vals[k]}).Truthy() {
				return s.vals[k]
			}
		}
		return object.NilV
	}
	d("find", findFn)
	d("detect", findFn)

	// all?: true when the block is truthy for every member (true on the empty set).
	d("all?", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (all?)")
		}
		s := self(v)
		for _, k := range s.order {
			if !vm.callBlock(blk, []object.Value{s.vals[k]}).Truthy() {
				return object.False
			}
		}
		return object.True
	})

	// any?: true when the block is truthy for some member (false on the empty set).
	d("any?", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (any?)")
		}
		s := self(v)
		for _, k := range s.order {
			if vm.callBlock(blk, []object.Value{s.vals[k]}).Truthy() {
				return object.True
			}
		}
		return object.False
	})

	// none?: true when the block is truthy for no member (true on the empty set).
	d("none?", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (none?)")
		}
		s := self(v)
		for _, k := range s.order {
			if vm.callBlock(blk, []object.Value{s.vals[k]}).Truthy() {
				return object.False
			}
		}
		return object.True
	})

	// ^: symmetric difference — a new Set of the members in exactly one operand,
	// computed as (a | b) - (a & b).
	d("^", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		union := a.s.Union(b.s)
		inter := a.s.Intersection(b.s)
		return fromKeys(union.Difference(inter), a, b)
	})

	// disjoint?: true when the two sets share no member.
	d("disjoint?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return object.Bool(a.s.Intersection(b.s).IsEmpty())
	})
	// intersect?: the negation of disjoint? (the sets share at least one member).
	d("intersect?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return object.Bool(!a.s.Intersection(b.s).IsEmpty())
	})

	// <: proper subset — a subset of the argument and not equal to it.
	d("<", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return object.Bool(a.s.IsSubset(b.s) && !a.s.Equal(b.s))
	})
	// >: proper superset — the argument is a proper subset of the receiver.
	d(">", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), setArg(args[0])
		return object.Bool(b.s.IsSubset(a.s) && !a.s.Equal(b.s))
	})

	// dup / clone: a shallow copy with the same members in insertion order.
	dupFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).copy()
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
		acc := object.Value(object.Integer(0))
		if len(args) > 0 {
			acc = args[0]
		}
		s := self(v)
		for _, k := range s.order {
			acc = vm.binaryOp(bytecode.OpAdd, acc, s.vals[k])
		}
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
		var acc object.Value
		i := 0
		if len(args) > 0 {
			acc = args[0]
		} else {
			if len(s.order) == 0 {
				return object.NilV
			}
			acc = s.vals[s.order[0]]
			i = 1
		}
		for ; i < len(s.order); i++ {
			acc = vm.callBlock(blk, []object.Value{acc, s.vals[s.order[i]]})
		}
		return acc
	}
	d("reduce", reduceFn)
	d("inject", reduceFn)

	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).repr())
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).repr())
	})
}

// setExtreme folds the members of s to the minimum (want=-1) or maximum
// (want=1) by the VM's spaceship (<=>), returning nil for the empty set. Mixed
// incomparable members surface the ArgumentError spaceship raises.
func setExtreme(vm *VM, s *Set, want int) object.Value {
	if len(s.order) == 0 {
		return object.NilV
	}
	best := s.vals[s.order[0]]
	for _, k := range s.order[1:] {
		cur := s.vals[k]
		if cmp := vm.spaceship(cur, best); (want < 0 && cmp < 0) || (want > 0 && cmp > 0) {
			best = cur
		}
	}
	return best
}
