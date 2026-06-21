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

	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).repr())
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).repr())
	})
}
