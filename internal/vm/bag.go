package vm

import (
	"sort"
	"strings"

	gobag "github.com/go-composites/bag/src"
	goresult "github.com/go-composites/result/src"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Bag binds github.com/go-composites/bag — the fifth consumer of the
// go-composites family (after Set, Time, BigDecimal and Date) — into Ruby. A
// Bag is a multiset / Counter: a collection in which every member carries a
// multiplicity (a count). It is NOT a standard Ruby class; Ruby's closest
// idiom is `Hash.new(0)` tallying or `Array#tally`. We expose it as a plain
// Ruby class named `Bag`, documented as a go-composites extension rather than
// Ruby core.
//
// Like Set, a go-composites Bag stores arbitrary comparable Go items, whereas
// Ruby values are not all comparable by value (a *String is a pointer). Each
// member is therefore marshalled to a canonical comparable key (the shared
// setKey marshaller, which raises TypeError on an Array/Hash/Proc/… member),
// and that key is what the go-composites Bag actually counts. A parallel
// key -> Ruby-value table recovers the original Ruby value from a key for
// each / to_a / distinct / inspect. The (unordered, map-backed) Bag has no
// insertion order, so renderings sort by the inspected member for determinism.

// Bag is the Ruby wrapper around a go-composites Bag.
type Bag struct {
	b    gobag.Interface      // counts, keyed by canonical keys
	vals map[any]object.Value // canonical key -> original Ruby value
}

func (b *Bag) ToS() string     { return b.repr() }
func (b *Bag) Inspect() string { return b.repr() }
func (b *Bag) Truthy() bool    { return true }

// repr renders "#<Bag: {item=>count, …}>" with members sorted by their
// inspected form, so the output is deterministic despite the map backing.
func (b *Bag) repr() string {
	type entry struct {
		ins   string
		count int
	}
	entries := make([]entry, 0, len(b.vals))
	for k, v := range b.vals {
		entries = append(entries, entry{ins: v.Inspect(), count: b.b.Count(k)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ins < entries[j].ins })
	var sb strings.Builder
	sb.WriteString("#<Bag: {")
	for i, e := range entries {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(e.ins)
		sb.WriteString("=>")
		sb.WriteString(itoa(e.count))
	}
	sb.WriteString("}>")
	return sb.String()
}

// itoa renders a non-negative count without pulling in strconv at the call site.
func itoa(n int) string { return object.IntValue(int64(n)).ToS() }

// newBag builds an empty Ruby Bag wrapper.
func newBag() *Bag {
	return &Bag{b: gobag.New(), vals: map[any]object.Value{}}
}

// add increments a Ruby value's multiplicity, remembering its Ruby value.
func (b *Bag) add(v object.Value) {
	k := setKey(v)
	b.vals[k] = v
	b.b = b.b.Add(k)
}

// remove decrements a Ruby value's multiplicity, forgetting it at zero.
func (b *Bag) remove(v object.Value) {
	k := setKey(v)
	b.b = b.b.Remove(k)
	if !b.b.Has(k) {
		delete(b.vals, k)
	}
}

// bagArg asserts an argument is a Bag, raising TypeError otherwise.
func bagArg(v object.Value) *Bag {
	bag, ok := object.KindOK[*Bag](v)
	if !ok {
		raise("TypeError", "value must be a Bag")
	}
	return bag
}

// seed adds every element of an enumerable to b. An Array (or Bag) seeds with
// multiplicity — each occurrence bumps the count — and a Set seeds each member
// once.
func (b *Bag) seed(v object.Value) {
	{
		__sw12 := v
		switch {
		case object.IsKind[*object.Array](__sw12):
			e := object.Kind[*object.Array](__sw12)
			_ = e
			for _, el := range e.Elems {
				b.add(el)
			}
		case object.IsKind[*Set](__sw12):
			e := object.Kind[*Set](__sw12)
			_ = e
			e.each(b.add)
		case object.IsKind[*Bag](__sw12):
			e := object.Kind[*Bag](__sw12)
			_ = e
			e.b.Each(func(item interface{}, count int) goresult.Interface {
				for i := 0; i < count; i++ {
					b.add(e.vals[item])
				}
				return goresult.New()
			})
		default:
			e := __sw12
			_ = e
			raise("TypeError", "value must be enumerable (Array, Set or Bag)")
		}
	}
}

// fromBag rebuilds a Ruby Bag from a combined go-composites Bag, recovering each
// key's Ruby value from a then b (a's value wins for a shared key) — used by the
// algebraic combinators (Sum/Union/Intersection/Difference).
func fromBag(combined gobag.Interface, a, b *Bag) *Bag {
	out := newBag()
	out.b = combined
	for _, src := range []*Bag{a, b} {
		for k, v := range src.vals {
			if combined.Has(k) {
				if _, seen := out.vals[k]; !seen {
					out.vals[k] = v
				}
			}
		}
	}
	return out
}

// toArray materialises the Bag into a Ruby Array, repeating each member by its
// count (members sorted by inspected form for determinism).
func (b *Bag) toArray() object.Value {
	type entry struct {
		ins   string
		val   object.Value
		count int
	}
	entries := make([]entry, 0, len(b.vals))
	for k, v := range b.vals {
		entries = append(entries, entry{ins: v.Inspect(), val: v, count: b.b.Count(k)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ins < entries[j].ins })
	var out []object.Value
	for _, e := range entries {
		for i := 0; i < e.count; i++ {
			out = append(out, e.val)
		}
	}
	return object.NewArrayFromSlice(out)
}

// distinctArray materialises the distinct members into a Ruby Array (each once),
// sorted by inspected form for determinism.
func (b *Bag) distinctArray() object.Value {
	type entry struct {
		ins string
		val object.Value
	}
	entries := make([]entry, 0, len(b.vals))
	for _, v := range b.vals {
		entries = append(entries, entry{ins: v.Inspect(), val: v})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ins < entries[j].ins })
	out := make([]object.Value, len(entries))
	for i, e := range entries {
		out[i] = e.val
	}
	return object.NewArrayFromSlice(out)
}

// sortedKeys returns the canonical keys ordered by their member's inspected
// form, giving each / inspect / to_a a deterministic traversal.
func (b *Bag) sortedKeys() []any {
	keys := make([]any, 0, len(b.vals))
	for k := range b.vals {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return b.vals[keys[i]].Inspect() < b.vals[keys[j]].Inspect()
	})
	return keys
}

// bagOp implements the Bag operator fast path reached from binary(): + is Sum
// (additive union, counts add) and - is Difference (counts subtract, floored at
// zero). The right operand must be a Bag.
func bagOp(op bytecode.Op, a *Bag, b object.Value) object.Value {
	switch op {
	case bytecode.OpAdd:
		bb := bagArg(b)
		return fromBag(a.b.Sum(bb.b), a, bb)
	case bytecode.OpSub:
		bb := bagArg(b)
		return fromBag(a.b.Difference(bb.b), a, bb)
	}
	return raise("NoMethodError", "undefined method '%s' for a Bag", op)
}

// registerBag installs the Bag class, its constructor and instance methods.
func (vm *VM) registerBag() {
	vm.cBag = newClass("Bag", vm.cObject)
	vm.consts["Bag"] = vm.cBag

	// Bag.new(enumerable=nil): empty, or seeded from an Array/Set/Bag (an Array
	// or Bag seeds with multiplicity).
	vm.cBag.smethods["new"] = &Method{name: "new", owner: vm.cBag,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			bag := newBag()
			if len(args) > 0 {
				if _, isNil := object.AsNilOK(args[0]); !isNil {
					bag.seed(args[0])
				}
			}
			return bag
		}}
	// Bag[a, b, …] builds a Bag from its arguments, counting duplicates.
	vm.cBag.smethods["[]"] = &Method{name: "[]", owner: vm.cBag,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			bag := newBag()
			for _, a := range args {
				bag.add(a)
			}
			return bag
		}}

	d := func(name string, fn NativeFn) { vm.cBag.define(name, fn) }
	self := func(v object.Value) *Bag { return object.Kind[*Bag](v) }

	addFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bag := self(v)
		bag.add(args[0])
		return bag
	}
	d("add", addFn)
	d("<<", addFn)

	removeFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bag := self(v)
		bag.remove(args[0])
		return bag
	}
	d("remove", removeFn)
	d("delete", removeFn)

	d("count", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).b.Count(setKey(args[0]))))
	})

	includeFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).b.Has(setKey(args[0])))
	}
	d("include?", includeFn)
	d("member?", includeFn)

	sizeFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).b.Len()))
	}
	d("size", sizeFn)
	d("length", sizeFn)

	d("distinct_size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).b.DistinctLen()))
	})
	d("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).b.IsEmpty())
	})
	d("clear", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		bag := self(v)
		bag.b = gobag.New()
		bag.vals = map[any]object.Value{}
		return bag
	})

	// each yields a two-element [item, count] Array per distinct member, in a
	// deterministic (inspected-member) order — mirroring Hash#each's pair shape.
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		bag := self(v)
		for _, k := range bag.sortedKeys() {
			pair := object.NewArray(bag.vals[k], object.IntValue(int64(bag.b.Count(k))))
			vm.callBlock(blk, []object.Value{pair})
		}
		return bag
	})
	d("to_a", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).toArray()
	})
	d("distinct", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).distinctArray()
	})

	sumFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), bagArg(args[0])
		return fromBag(a.b.Sum(b.b), a, b)
	}
	d("+", sumFn)

	unionFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), bagArg(args[0])
		return fromBag(a.b.Union(b.b), a, b)
	}
	d("|", unionFn)
	d("union", unionFn)

	interFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), bagArg(args[0])
		return fromBag(a.b.Intersection(b.b), a, b)
	}
	d("&", interFn)
	d("intersection", interFn)

	diffFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		a, b := self(v), bagArg(args[0])
		return fromBag(a.b.Difference(b.b), a, b)
	}
	d("-", diffFn)
	d("difference", diffFn)

	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		b, ok := object.KindOK[*Bag](args[0])
		if !ok {
			return object.False
		}
		return object.Bool(self(v).b.Equal(b.b))
	})

	// most_common(n=nil): an Array of [item, count] pairs ordered by count
	// descending (ties broken by the inspected member, ascending, for
	// determinism). With an Integer n, only the top n pairs; without an arg (or
	// nil), all of them. This is Python's Counter.most_common, exposed as a Bag
	// extension rather than a Ruby-core method.
	d("most_common", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		bag := self(v)
		type entry struct {
			ins   string
			val   object.Value
			count int
		}
		entries := make([]entry, 0, len(bag.vals))
		for k, val := range bag.vals {
			entries = append(entries, entry{ins: val.Inspect(), val: val, count: bag.b.Count(k)})
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].count != entries[j].count {
				return entries[i].count > entries[j].count
			}
			return entries[i].ins < entries[j].ins
		})
		// An Integer n caps the result; a missing arg or explicit nil keeps all.
		if len(args) > 0 {
			if _, isNil := object.AsNilOK(args[0]); !isNil {
				n := int(intArg(args[0]))
				if n < 0 {
					n = 0
				}
				if n < len(entries) {
					entries = entries[:n]
				}
			}
		}
		out := make([]object.Value, len(entries))
		for i, e := range entries {
			out[i] = object.NewArray(e.val, object.IntValue(int64(e.count)))
		}
		return object.NewArrayFromSlice(out)
	})

	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).repr())
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).repr())
	})
}
