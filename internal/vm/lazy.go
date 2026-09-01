package vm

import (
	"math"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// LazyEnum is Ruby's Enumerator::Lazy: a source plus a chain of transformations
// that are applied on demand, one element at a time, when a terminal operation
// (first/to_a/force/each) pulls. This makes infinite sources usable —
// (1..Float::INFINITY).lazy.map { … }.first(5) — without materialising them.
type LazyEnum struct {
	recv object.Value
	ops  []lazyOp
	// sizeSpec, when sizeSpecSet, is the size given to Enumerator::Lazy.new(obj,
	// size) { … }: it seeds #size (an Integer/nil verbatim, or a callable invoked on
	// demand) in place of the source's own #size. Carried across #with so ops thread
	// through it. When unset, #size starts from the source's #size (if any).
	sizeSpec    object.Value
	sizeSpecSet bool
	// ext caches the Enumerator that drives this pipeline for external iteration
	// (#next/#peek/#rewind), pulling one element at a time through a fiber so the
	// pipeline is never over-evaluated (and an infinite source stays usable).
	ext *Enumerator
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
	return &LazyEnum{recv: l.recv, ops: ops, sizeSpec: l.sizeSpec, sizeSpecSet: l.sizeSpecSet}
}

func (vm *VM) registerLazy() {
	vm.cLazy = newClass("Enumerator::Lazy", vm.cEnumerator)
	vm.cEnumerator.consts["Lazy"] = vm.cLazy

	// Enumerator::Lazy.new(obj, size = nil) { |yielder, *values| … } builds a lazy
	// enumerator whose block is run once per element of obj.each, receiving a yielder
	// (its `<<`/`yield` feed the pipeline) followed by that element's yielded values.
	// The optional size seeds #size (Integer/nil verbatim, or a callable on demand).
	vm.cLazy.smethods["new"] = &Method{name: "new", owner: vm.cLazy,
		native: func(_ *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			if blk == nil {
				raise("ArgumentError", "tried to call lazy new without a block")
			}
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
			}
			src := args[0]
			gen := &Proc{native: func(vm *VM, gargs []object.Value) object.Value {
				y := gargs[0]
				step := &Proc{native: func(vm *VM, vals []object.Value) object.Value {
					vm.callBlock(blk, append([]object.Value{y}, vals...))
					return object.NilV
				}}
				return vm.send(src, "each", nil, step)
			}}
			le := &LazyEnum{recv: &Enumerator{block: gen}}
			if len(args) > 1 {
				le.sizeSpec, le.sizeSpecSet = args[1], true
			}
			return le
		}}

	makeLazy := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &LazyEnum{recv: self}
	}
	// Sources that lazy iteration can drive directly. (#lazy on a Lazy is itself,
	// defined separately below.)
	vm.cArray.define("lazy", makeLazy)
	vm.cRange.define("lazy", makeLazy)
	vm.cHash.define("lazy", makeLazy)
	vm.cEnumerator.define("lazy", makeLazy)
	// Every Enumerable (a class that mixes in Enumerable and defines #each) gets
	// #lazy too; the source is driven through #each (see lazySource's default).
	if en, ok := vm.consts["Enumerable"].(*RClass); ok {
		en.define("lazy", makeLazy)
	}

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
	d("select", chain("select"))
	d("reject", chain("reject"))
	d("filter_map", chain("filter_map"))
	d("flat_map", chain("flat_map"))
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
	d("zip", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// Each argument must be list-like (respond to #each); MRI validates this when
		// #zip is called, before any element is pulled.
		for _, a := range args {
			if _, isArr := a.(*object.Array); !isArr && !vm.respondsToDynamic(a, "each") {
				raise("TypeError", "wrong argument type %s (must respond to :each)", vm.classOf(a).name)
			}
		}
		return self.(*LazyEnum).with(lazyOp{kind: "zip", others: append([]object.Value{}, args...)})
	})
	// with_index(offset = 0): optional block maps (element, index); without a
	// block each element becomes the pair [element, index].
	d("with_index", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		off := 0
		if len(args) > 0 && !object.IsNil(args[0]) { // a nil offset means "start at 0"
			off = int(coerceInt(vm, args[0]))
		}
		return self.(*LazyEnum).with(lazyOp{kind: "with_index", n: off, blk: blk})
	})
	// chunk_while / slice_when split the stream into runs at each adjacent pair
	// (a, b) for which the block does not hold (chunk_while) / does hold
	// (slice_when). Both require a block. Truly lazy: a completed run is emitted
	// downstream as soon as its boundary is seen, so an infinite source whose runs
	// are finite (e.g. .chunk_while { |a, b| b % 3 != 0 }) can be driven by first.
	reqBlock := func(kind string) NativeFn {
		return func(_ *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
			if blk == nil {
				raise("ArgumentError", "tried to create Proc object without a block")
			}
			return self.(*LazyEnum).with(lazyOp{kind: kind, blk: blk})
		}
	}
	d("chunk_while", reqBlock("chunk_while"))
	d("slice_when", reqBlock("slice_when"))
	// chunk groups consecutive elements sharing the block's value into
	// [value, [elements...]] runs. A block value of nil or :_separator drops the
	// element and closes the current run; :_alone puts its element in its own run;
	// any other Symbol beginning with an underscore is reserved (RuntimeError).
	// Without a block MRI returns a Lazy awaiting the block, so we return self.
	d("chunk", func(_ *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return self
		}
		return self.(*LazyEnum).with(lazyOp{kind: "chunk", blk: blk})
	})
	// slice_before / slice_after split the stream into runs, starting a new run
	// just before (slice_before) / just after (slice_after) each element that
	// matches. The boundary test is a block over the element or `pat === x` for a
	// single pattern argument — exactly one must be given.
	sliceFn := func(kind string) NativeFn {
		return func(_ *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			if blk != nil {
				if len(args) > 0 {
					raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
				}
				return self.(*LazyEnum).with(lazyOp{kind: kind, blk: blk})
			}
			if len(args) != 1 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
			}
			return self.(*LazyEnum).with(lazyOp{kind: kind, pat: args[0]})
		}
	}
	d("slice_before", sliceFn("slice_before"))
	d("slice_after", sliceFn("slice_after"))
	// eager returns a non-lazy Enumerator over this pipeline: its terminal
	// operations (first/take) drive #each element-by-element, so it stays usable on
	// an infinite source while returning an ordinary (non-lazy) Enumerator.
	d("eager", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &Enumerator{recv: self, meth: "each"}
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
		// Incremental: drive the pipeline one element at a time so a break (or an
		// early-abort panic from an eager Enumerator's #first/#take) unwinds without
		// materialising an unbounded source.
		vm.lazyRun(l, func(v object.Value) bool {
			vm.callBlock(blk, []object.Value{v})
			return true
		})
		return l
	})
	// #size threads the source's size through the pipeline: filtering/reshaping ops
	// (select, flat_map, grep, uniq, chunk, …) make it unknown (nil); map/with_index/
	// zip keep it; take/drop bound it — see lazyOpSize.
	d("size", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.lazySize(self.(*LazyEnum))
	})
	// External iteration over the lazy pipeline (#next/#peek/#rewind/…): driven by a
	// cached Enumerator so each pull advances the pipeline one element at a time,
	// never over-evaluating and never materialising an infinite source.
	lazyExt := func(self object.Value) *Enumerator {
		l := self.(*LazyEnum)
		if l.ext == nil {
			l.ext = &Enumerator{recv: l, meth: "each"}
		}
		return l.ext
	}
	d("next", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return enumPack(vm.enumNextRaw(lazyExt(self)).Elems)
	})
	d("next_values", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.enumNextRaw(lazyExt(self))
	})
	d("peek", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return enumPack(vm.enumPeekRaw(lazyExt(self)).Elems)
	})
	d("peek_values", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.enumPeekRaw(lazyExt(self))
	})
	d("rewind", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		e := lazyExt(self)
		e.extFiber, e.peeked, e.ended = nil, false, false
		return self
	})
	// enum_for / to_enum on a Lazy return a fresh Lazy that drives self.meth(*rest)
	// lazily (through a generator Enumerator pulled one element at a time). A block
	// supplies #size (returned verbatim, possibly nil) as elsewhere.
	lazyToEnum := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		l := self.(*LazyEnum)
		meth, rest := "each", []object.Value(nil)
		if len(args) > 0 {
			meth, rest = args[0].ToS(), args[1:]
		}
		gen := &Proc{native: func(vm *VM, gargs []object.Value) object.Value {
			y := gargs[0]
			step := &Proc{native: func(vm *VM, a []object.Value) object.Value {
				vm.send(y, "yield", a, nil)
				return object.NilV
			}}
			return vm.send(l, meth, rest, step)
		}}
		return &LazyEnum{recv: &Enumerator{block: gen, sizeBlock: blk}}
	}
	d("to_enum", lazyToEnum)
	// Alias the shared Method objects so instance_method identities match MRI
	// (Enumerator::Lazy#collect == #map, #filter == #select, …) and the Lazy method
	// whitelist (instance_methods(false)) carries every expected name.
	alias := func(name, of string) { vm.cLazy.methods[name] = vm.cLazy.methods[of] }
	alias("collect", "map")
	alias("filter", "select")
	alias("find_all", "select")
	alias("collect_concat", "flat_map")
	alias("enum_for", "to_enum")
}

// lazySize threads the source's #size through the op chain. A filtering or
// reshaping op makes the size unknown (nil); once unknown it stays unknown.
func (vm *VM) lazySize(l *LazyEnum) object.Value {
	var sz object.Value
	switch {
	case l.sizeSpecSet:
		sz = vm.enumResolveSize(l.sizeSpec)
	case vm.respondsToDynamic(l.recv, "size"):
		sz = vm.send(l.recv, "size", nil, nil)
	default:
		return object.NilV
	}
	for _, op := range l.ops {
		if object.IsNil(sz) {
			return object.NilV
		}
		sz = lazyOpSize(op, sz)
	}
	return sz
}

// lazyOpSize maps a known incoming size through one op: map/with_index/zip keep
// it, take bounds it to its count (min, or the count itself for an infinite
// source), drop subtracts its count (flooring at 0, infinity staying infinite),
// and every filtering/reshaping op makes it unknown.
func lazyOpSize(op lazyOp, sz object.Value) object.Value {
	switch op.kind {
	case "map", "with_index", "zip":
		return sz
	case "take":
		n := int64(op.n)
		if isInfFloat(sz) {
			return object.IntValue(n)
		}
		if i, ok := sz.(object.Integer); ok {
			if int64(i) < n {
				return sz
			}
			return object.IntValue(n)
		}
		return object.NilV
	case "drop":
		n := int64(op.n)
		if isInfFloat(sz) {
			return sz
		}
		if i, ok := sz.(object.Integer); ok {
			d := int64(i) - n
			if d < 0 {
				d = 0
			}
			return object.IntValue(d)
		}
		return object.NilV
	default:
		return object.NilV
	}
}

// lazySource returns a restartable pull function over le.recv: successive calls
// yield the next source element until it returns ok=false. Integer ranges
// (including endless and Float::INFINITY-bounded) are walked by counter; arrays
// by index; an Enumerator is pulled one element at a time; any other Enumerable
// is materialised once (so it must be finite).
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
	case *Enumerator:
		// Pulled, not materialised. An Enumerator.new { |y| loop { … } } has no
		// end, and reading it whole allocated 8 GB before a CI runner died under
		// it — which is the thing lazy exists to avoid, defeated at its source.
		//
		// The machinery is the one behind Enumerator#next: a fiber that runs the
		// source until it yields and then suspends. Pulling runs on a copy of the
		// enumerator, because each terminal operation starts from the beginning —
		// e.lazy.first(3) twice gives the same three values, and would not if
		// this advanced the caller's own cursor.
		//
		// A pipeline that stops early leaves that fiber suspended, exactly as an
		// abandoned #next does. That is the existing behaviour of external
		// iteration here, not something this adds.
		pull := r.forPull()
		return func() (object.Value, bool) {
			w, ok := vm.enumPull(pull)
			if !ok {
				return object.NilVal(), false
			}
			return enumPack(w.Elems), true
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
// chains). It is a thin collector over lazyRun.
func (vm *VM) lazyForce(le *LazyEnum, want int) []object.Value {
	if want == 0 {
		return nil
	}
	var out []object.Value
	vm.lazyRun(le, func(v object.Value) bool {
		out = append(out, v)
		return !(want >= 0 && len(out) >= want)
	})
	return out
}

// chunkState is the per-op accumulator for the grouping ops (chunk_while,
// slice_when, chunk, slice_before, slice_after): a run is buffered and only
// emitted downstream once its boundary is seen (or the source is exhausted),
// which is what makes those ops truly lazy over an infinite source.
type chunkState struct {
	buf     []object.Value // the run being accumulated
	prev    object.Value   // previous element (chunk_while / slice_when)
	hasPrev bool           // a run exists (chunk_while / slice_when / slice_before)
	key     object.Value   // current run's chunk key (chunk)
	open    bool           // there is an open run the key may extend (chunk)
}

// lazyRun pulls from the source and threads each element through the op chain,
// invoking sink for every value reaching the chain's end. sink returns false to
// stop early (e.g. once a `first`/`take` quota is met). When the source is
// exhausted without an early stop, grouping ops flush their buffered runs. Each
// source element is threaded by feed, which recurses op-by-op so that expanding
// ops (flat_map), grouping ops, and index/zip-carrying ops compose the same way
// MRI's fibered lazy pipeline does.
func (vm *VM) lazyRun(le *LazyEnum, sink func(object.Value) bool) {
	src := vm.lazySource(le.recv)
	n := len(le.ops)
	// Per-op mutable run state.
	rem := make([]int, n)                             // take / drop remaining
	dropping := make([]bool, n)                       // drop_while latch
	idx := make([]int, n)                             // with_index counter
	seen := make([][]object.Value, n)                 // uniq keys
	zpull := make([][]func() (object.Value, bool), n) // zip other sources
	cst := make([]*chunkState, n)                     // grouping-op accumulators
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
		case "chunk_while", "slice_when", "chunk", "slice_before", "slice_after":
			cst[i] = &chunkState{}
		}
	}
	stop := false
	// feed threads v through ops[i:]; it returns false to abort the whole pull
	// (want satisfied, or a take/take_while boundary reached).
	var feed func(i int, v object.Value) bool
	// emitBuf flushes a grouping op's buffered run downstream as one Array and
	// clears it; emitRun does the same for chunk's [key, [elems]] pair.
	emitBuf := func(i int, st *chunkState) bool {
		if len(st.buf) == 0 {
			return true
		}
		grp := object.NewArrayFromSlice(st.buf)
		st.buf = nil
		return feed(i+1, grp)
	}
	emitRun := func(i int, st *chunkState) bool {
		if !st.open {
			return true
		}
		grp := object.NewArrayFromSlice([]object.Value{st.key, object.NewArrayFromSlice(st.buf)})
		st.open = false
		st.buf = nil
		return feed(i+1, grp)
	}
	feed = func(i int, v object.Value) bool {
		if i == n {
			if !sink(v) {
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
			// MRI flattens an Array, or a value that responds to both #each and
			// #force (an Enumerator::Lazy) — the latter forced to an Array first. A
			// plain Enumerator (responds to #each but not #force) is passed through
			// unflattened.
			if arr, ok := lazyFlattenable(vm, w); ok {
				for _, e := range arr {
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
		case "chunk_while":
			st := cst[i]
			if !st.hasPrev {
				st.buf = []object.Value{v}
				st.prev = v
				st.hasPrev = true
				return true
			}
			keep := vm.callBlock(op.blk, []object.Value{st.prev, v}).Truthy()
			st.prev = v
			if keep {
				st.buf = append(st.buf, v)
				return true
			}
			if !emitBuf(i, st) {
				return false
			}
			st.buf = []object.Value{v}
			return true
		case "slice_when":
			st := cst[i]
			if !st.hasPrev {
				st.buf = []object.Value{v}
				st.prev = v
				st.hasPrev = true
				return true
			}
			brk := vm.callBlock(op.blk, []object.Value{st.prev, v}).Truthy()
			st.prev = v
			if brk {
				if !emitBuf(i, st) {
					return false
				}
				st.buf = []object.Value{v}
				return true
			}
			st.buf = append(st.buf, v)
			return true
		case "chunk":
			st := cst[i]
			k := vm.callBlock(op.blk, []object.Value{v})
			drop, alone, reserved := chunkKeyKind(k)
			if reserved {
				raise("RuntimeError", "symbols beginning with an underscore are reserved")
			}
			if drop {
				return emitRun(i, st)
			}
			if alone {
				if !emitRun(i, st) {
					return false
				}
				pair := object.NewArrayFromSlice([]object.Value{k, object.NewArrayFromSlice([]object.Value{v})})
				return feed(i+1, pair)
			}
			if st.open && valueEql(st.key, k) {
				st.buf = append(st.buf, v)
				return true
			}
			if !emitRun(i, st) {
				return false
			}
			st.key = k
			st.buf = []object.Value{v}
			st.open = true
			return true
		case "slice_before":
			st := cst[i]
			var match bool
			if op.blk != nil {
				match = vm.callBlock(op.blk, []object.Value{v}).Truthy()
			} else {
				match = vm.send(op.pat, "===", []object.Value{v}, nil).Truthy()
			}
			if match {
				if st.hasPrev && !emitBuf(i, st) {
					return false
				}
				st.buf = []object.Value{v}
				st.hasPrev = true
				return true
			}
			st.hasPrev = true
			st.buf = append(st.buf, v)
			return true
		case "slice_after":
			st := cst[i]
			st.buf = append(st.buf, v)
			var match bool
			if op.blk != nil {
				match = vm.callBlock(op.blk, []object.Value{v}).Truthy()
			} else {
				match = vm.send(op.pat, "===", []object.Value{v}, nil).Truthy()
			}
			if match {
				return emitBuf(i, st)
			}
			return true
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
			if !feed(i+1, v) {
				return false
			}
			// Stop pulling once the quota is met, rather than waiting to reject the
			// next element — so take(n).force drives the source exactly n times (and
			// never triggers a source that raises on its n+1-th step).
			if rem[i] == 0 {
				stop = true
				return false
			}
			return true
		case "drop":
			if rem[i] > 0 {
				rem[i]--
				return true
			}
			// Past the drop count: fall through to pass v downstream.
		}
		return feed(i+1, v)
	}
	// finish flushes each grouping op's pending run once the source is exhausted,
	// threading it through the ops downstream (which may themselves group).
	var finish func(i int) bool
	finish = func(i int) bool {
		if i == n {
			return true
		}
		switch le.ops[i].kind {
		case "chunk_while", "slice_when", "slice_before", "slice_after":
			if !emitBuf(i, cst[i]) {
				return false
			}
		case "chunk":
			if !emitRun(i, cst[i]) {
				return false
			}
		}
		return finish(i + 1)
	}
	// takeSaturated reports whether a take op has already met its quota, so the
	// source need not be driven at all — take(0) then drives it zero times.
	takeSaturated := func() bool {
		for i, op := range le.ops {
			if op.kind == "take" && rem[i] <= 0 {
				return true
			}
		}
		return false
	}
	for !stop {
		if takeSaturated() {
			break
		}
		v, ok := src()
		if !ok {
			break
		}
		if !feed(0, v) {
			break
		}
	}
	if !stop {
		finish(0)
	}
}

// chunkKeyKind classifies a value returned by chunk's block: nil or :_separator
// drop the element (drop), :_alone forces its own run (alone), and any other
// Symbol beginning with an underscore is reserved (reserved).
func chunkKeyKind(k object.Value) (drop, alone, reserved bool) {
	if _, isNil := k.(object.Nil); isNil {
		return true, false, false
	}
	s, ok := k.(object.Symbol)
	if !ok {
		return false, false, false
	}
	switch string(s) {
	case "_separator":
		return true, false, false
	case "_alone":
		return false, true, false
	}
	if strings.HasPrefix(string(s), "_") {
		return false, false, true
	}
	return false, false, false
}

// lazyFlattenable reports whether flat_map should flatten w, returning its
// elements when so: an Array flattens to its own elements; a value responding to
// both #each and #force (an Enumerator::Lazy) is forced to an Array and
// flattened. Anything else (including a plain Enumerator, which has #each but not
// #force) is not flattened.
func lazyFlattenable(vm *VM, w object.Value) ([]object.Value, bool) {
	if arr, ok := w.(*object.Array); ok {
		return arr.Elems, true
	}
	if vm.respondsToDynamic(w, "force") && vm.respondsToDynamic(w, "each") {
		if arr, ok := vm.send(w, "force", nil, nil).(*object.Array); ok {
			return arr.Elems, true
		}
	}
	return nil, false
}

// lazyGrepValue returns the value grep/grep_v should emit for a match: the
// element itself, or the block's mapping of it when a block was given.
func (vm *VM) lazyGrepValue(blk *Proc, v object.Value) object.Value {
	if blk != nil {
		return vm.callBlock(blk, []object.Value{v})
	}
	return v
}
