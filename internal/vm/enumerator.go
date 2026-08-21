package vm

import (
	"math"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Enumerator is an external iterator over recv.meth(*args). #each forwards a
// block to the underlying method; #to_a/#size materialise the yielded elements
// eagerly, while #next/#peek drive the iteration one element at a time in a
// suspending Fiber so unbounded sources (Enumerator.produce, Array#cycle) work.
// An element is the single yielded value, or an Array of the values when several
// are yielded at once (rb_enum_values_pack).
type Enumerator struct {
	recv object.Value
	meth string
	args []object.Value
	// block, when set, is an Enumerator.new { |y| … } generator: it is run with a
	// yielder rather than driving recv.meth.
	block *Proc
	// sizeBlock, when set (via enum_for/to_enum given a block), computes #size on
	// demand — its result (which may be nil) is returned verbatim. When nil, #size
	// falls back to materialising (unless sizeSpecSet is set, below).
	sizeBlock *Proc
	// sizeSpec, when sizeSpecSet, is the size given to Enumerator.new(size) { … } or
	// Enumerator.produce(size:): an Integer/nil returned verbatim, or a callable
	// (Proc or any #call responder) invoked on demand.
	sizeSpec    object.Value
	sizeSpecSet bool

	// produceBlk drives Enumerator.produce: produceInit (when produceHas) is the
	// first element, then each subsequent element is produceBlk.call(prev); a
	// StopIteration raised in the block ends the enumeration.
	produceBlk  *Proc
	produceInit object.Value
	produceHas  bool

	// isChain marks an Enumerator::Chain: #each iterates chainParts in turn, and
	// entered[i] records whether part i has been iterated (so #rewind can rewind,
	// in reverse, exactly the parts that were entered).
	isChain    bool
	chainParts []object.Value
	entered    []bool

	// External-iteration (#next/#peek) state. extFiber drives the source and
	// suspends at each element; peekArgs buffers the element #peek looked ahead to;
	// ended records that the source has been exhausted; finish is the source's
	// return value, surfaced as StopIteration#result at the end of the stream.
	extFiber *Fiber
	peekArgs object.Value
	peeked   bool
	ended    bool
	finish   object.Value
}

// forPull returns a copy of e carrying its definition and none of its
// external-iteration state, so it can be pulled from the start without moving
// the cursor #next and #peek share. See lazySource, which drives a lazy pipeline
// with it.
func (e *Enumerator) forPull() *Enumerator {
	c := *e
	c.extFiber, c.peekArgs, c.peeked, c.ended, c.finish = nil, nil, false, false, nil
	if e.isChain {
		c.entered = make([]bool, len(e.chainParts))
	}
	return &c
}

// yielder is the object passed to an Enumerator.new generator block; `y << v`
// and `y.yield(v)` feed values into the enumeration.
type yielder struct{ emit func(args []object.Value) }

func (y *yielder) ToS() string     { return "#<Enumerator::Yielder>" }
func (y *yielder) Inspect() string { return y.ToS() }
func (y *yielder) Truthy() bool    { return true }

// Inspect renders the MRI form #<Enumerator: recv:meth(args)> (or, for a chain,
// #<Enumerator::Chain: [parts]>). (MRI's #to_s shows the object address, which we
// can't reproduce deterministically, so ToS reuses Inspect.)
func (e *Enumerator) Inspect() string {
	if e.isChain {
		parts := make([]string, len(e.chainParts))
		for i, p := range e.chainParts {
			parts[i] = p.Inspect()
		}
		return "#<Enumerator::Chain: [" + strings.Join(parts, ", ") + "]>"
	}
	// A generator (Enumerator.new) or produce enumerator has no driven receiver;
	// MRI shows an internal Generator/Producer object there (its address, which we
	// can't reproduce), so render a stable placeholder and default the method name.
	recvStr, meth := "#<Enumerator::Generator>", e.meth
	if e.recv != nil {
		recvStr = e.recv.Inspect()
	}
	if meth == "" {
		meth = "each"
	}
	s := "#<Enumerator: " + recvStr + ":" + meth
	if len(e.args) > 0 {
		parts := make([]string, len(e.args))
		for i, a := range e.args {
			parts[i] = a.Inspect()
		}
		s += "(" + strings.Join(parts, ", ") + ")"
	}
	return s + ">"
}
func (e *Enumerator) ToS() string  { return e.Inspect() }
func (e *Enumerator) Truthy() bool { return true }

// enumFor builds an Enumerator for recv.meth(*args).
func enumFor(recv object.Value, meth string, args ...object.Value) *Enumerator {
	return &Enumerator{recv: recv, meth: meth, args: args}
}

// newChain builds an Enumerator::Chain over parts.
func newChain(parts []object.Value) *Enumerator {
	return &Enumerator{isChain: true, chainParts: parts, entered: make([]bool, len(parts))}
}

func (vm *VM) registerEnumerator() {
	vm.cEnumerator = newClass("Enumerator", vm.cObject)
	vm.consts["Enumerator"] = vm.cEnumerator
	// Mix in Enumerable so map/select/reduce/… work via #each.
	en, _ := vm.consts["Enumerable"].(*RClass)
	if en != nil {
		vm.cEnumerator.includes = append(vm.cEnumerator.includes, en)
	}

	// Enumerator::Chain — a subclass whose instances chain several enumerables.
	vm.cEnumeratorChain = newClass("Enumerator::Chain", vm.cEnumerator)
	vm.cEnumeratorChain.consts = vm.cEnumerator.consts
	vm.cEnumerator.consts["Chain"] = vm.cEnumeratorChain
	vm.cEnumeratorChain.smethods["new"] = &Method{name: "new", owner: vm.cEnumeratorChain,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return newChain(append([]object.Value{}, args...))
		}}

	// Enumerator::Yielder — `y << v` / `y.yield(v)` feed the generator's values in.
	vm.cYielder = newClass("Enumerator::Yielder", vm.cObject)
	vm.cYielder.consts = vm.cEnumerator.consts // (scope is cosmetic; share the map)
	vm.cEnumerator.consts["Yielder"] = vm.cYielder
	vm.cYielder.define("<<", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*yielder).emit(args)
		return self // << chains
	})
	vm.cYielder.define("yield", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*yielder).emit(args)
		return object.NilV
	})
	// Enumerator.new(size = nil) { |y| … } builds a generator-block enumerator. An
	// optional leading argument sets #size (an Integer/nil returned verbatim, or a
	// callable invoked on demand).
	vm.cEnumerator.smethods["new"] = &Method{name: "new", owner: vm.cEnumerator,
		native: func(_ *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			if blk == nil {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
			}
			e := &Enumerator{block: blk}
			if len(args) > 0 {
				e.sizeSpec, e.sizeSpecSet = args[0], true
			}
			return e
		}}
	// Enumerator.produce(initial = nil, size: Float::INFINITY) { |prev| … }.
	vm.cEnumerator.smethods["produce"] = &Method{name: "produce", owner: vm.cEnumerator,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			return vm.enumProduce(args, blk)
		}}

	// Kernel#enum_for / #to_enum: build an Enumerator for self.meth(*rest). A block
	// supplies the enumerator's #size (called lazily, its result — possibly nil —
	// returned as-is), as in `enum_for(:each_slice, n) { … }`.
	enumForFn := func(_ *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		meth, rest := "each", []object.Value(nil)
		if len(args) > 0 {
			meth, rest = args[0].ToS(), args[1:]
		}
		return &Enumerator{recv: self, meth: meth, args: rest, sizeBlock: blk}
	}
	vm.cObject.define("enum_for", enumForFn)
	vm.cObject.define("to_enum", enumForFn)

	// Enumerable#chain — a chain of self followed by the given enumerables. Mixed
	// into every Enumerable (Array/Range/…) and inherited by Enumerator.
	if en != nil {
		en.define("chain", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return newChain(append([]object.Value{self}, args...))
		})
	}

	d := func(name string, fn NativeFn) { vm.cEnumerator.define(name, fn) }
	d("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		e := self.(*Enumerator)
		if blk == nil {
			return e
		}
		return vm.enumRunEach(e, blk)
	})
	// Enumerator#+ — a chain of self and other.
	d("+", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return newChain([]object.Value{self, args[0]})
	})
	d("to_a", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(vm.enumMaterialize(self.(*Enumerator)))
	})
	d("size", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		if e.isChain {
			total := int64(0)
			for _, part := range e.chainParts {
				switch v := vm.send(part, "size", nil, nil).(type) {
				case object.Integer:
					total += int64(v)
				default:
					// nil or Float::INFINITY (or any non-Integer): MRI returns it as-is,
					// short-circuiting without asking later parts for their size.
					return v
				}
			}
			return object.IntValue(total)
		}
		if e.sizeSpecSet {
			return vm.enumResolveSize(e.sizeSpec)
		}
		if e.sizeBlock != nil {
			return vm.callBlock(e.sizeBlock, nil)
		}
		if e.block != nil {
			return object.NilV // a bare generator's size is unknown (MRI returns nil)
		}
		return object.IntValue(int64(len(vm.enumMaterialize(e))))
	})
	d("next", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return enumPack(vm.enumNextRaw(self.(*Enumerator)).Elems)
	})
	d("next_values", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.enumNextRaw(self.(*Enumerator))
	})
	d("peek", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return enumPack(vm.enumPeekRaw(self.(*Enumerator)).Elems)
	})
	d("peek_values", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.enumPeekRaw(self.(*Enumerator))
	})
	d("rewind", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		e.extFiber, e.peeked, e.ended = nil, false, false
		if e.isChain {
			// Rewind the parts that were iterated, in reverse, and forget them.
			for i := len(e.chainParts) - 1; i >= 0; i-- {
				if e.entered[i] && vm.respondsTo(e.chainParts[i], "rewind") {
					vm.send(e.chainParts[i], "rewind", nil, nil)
				}
				e.entered[i] = false
			}
			return e
		}
		// Rewind hook: a driven source that responds to #rewind is rewound too.
		if e.recv != nil && vm.respondsTo(e.recv, "rewind") {
			vm.send(e.recv, "rewind", nil, nil)
		}
		return e
	})
	withIndex := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		e := self.(*Enumerator)
		off := int64(0)
		if len(args) > 0 && !object.IsNil(args[0]) { // nil argument means "no offset"
			off = coerceInt(vm, args[0])
		}
		if blk == nil {
			// No block: an Enumerator that yields [element, index] pairs.
			elems := vm.enumMaterialize(e)
			pairs := make([]object.Value, len(elems))
			for i, v := range elems {
				pairs[i] = object.NewArray(v, object.IntValue(off+int64(i)))
			}
			return enumFor(object.NewArrayFromSlice(pairs), "each")
		}
		// With a block, re-run the source, appending the running index to each yield
		// and forwarding the block's result — so map collects, each returns the
		// receiver, etc., exactly as the wrapped method would.
		i := off
		wrapper := &Proc{native: func(_ *VM, cargs []object.Value) object.Value {
			withIdx := append(append([]object.Value{}, cargs...), object.IntValue(i))
			i++
			return vm.callBlock(blk, withIdx)
		}}
		return vm.enumRunEach(e, wrapper)
	}
	d("with_index", withIndex)
	d("each_with_index", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		return withIndex(vm, self, nil, blk) // each_with_index ignores any offset
	})
	// with_object is MRI's alias of each_with_object; share the one Method so
	// Enumerator.instance_method(:with_object) == …(:each_with_object).
	withObject := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		e := self.(*Enumerator)
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		memo := args[0]
		if blk == nil {
			return enumFor(self, "each_with_object", memo)
		}
		wrapper := &Proc{native: func(_ *VM, cargs []object.Value) object.Value {
			return vm.callBlock(blk, []object.Value{enumPack(cargs), memo})
		}}
		vm.enumRunEach(e, wrapper)
		return memo
	}
	d("each_with_object", withObject)
	vm.cEnumerator.methods["with_object"] = vm.cEnumerator.methods["each_with_object"]
	// first/take pull only as many elements as requested, so they terminate even
	// for unbounded enumerators such as Enumerator.produce or Array#cycle.
	d("first", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		if len(args) == 0 {
			got := vm.enumTake(e, 1)
			if len(got) == 0 {
				return object.NilV
			}
			return got[0]
		}
		return object.NewArrayFromSlice(vm.enumTake(e, int(intArg(args[0]))))
	})
	d("take", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(vm.enumTake(self.(*Enumerator), int(intArg(args[0]))))
	})
}

// enumProduce builds the enumerator for Enumerator.produce(initial, size:) { … }.
func (vm *VM) enumProduce(args []object.Value, blk *Proc) object.Value {
	if blk == nil {
		raise("ArgumentError", "no block given")
	}
	pos := args
	var sizeSpec object.Value = object.Float(math.Inf(1))
	if h, ok := trailingHash(args); ok {
		pos = args[:len(args)-1]
		var bad []string
		for _, k := range h.Keys {
			if sym, isSym := k.(object.Symbol); isSym && string(sym) == "size" {
				sizeSpec, _ = h.Get(k)
			} else {
				bad = append(bad, k.Inspect())
			}
		}
		if len(bad) > 0 {
			raise("ArgumentError", "unknown keywords: %s", strings.Join(bad, ", "))
		}
	}
	if len(pos) > 1 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 0..1)", len(pos))
	}
	e := &Enumerator{produceBlk: blk, sizeSpec: sizeSpec, sizeSpecSet: true}
	if len(pos) == 1 {
		e.produceInit, e.produceHas = pos[0], true
	}
	return e
}

// enumResolveSize resolves a stored #size specification: a callable (Proc or any
// object responding to #call) is invoked, anything else (an Integer or nil) is
// returned verbatim.
func (vm *VM) enumResolveSize(v object.Value) object.Value {
	if !object.IsNil(v) && vm.respondsTo(v, "call") {
		return vm.send(v, "call", nil, nil)
	}
	return v
}

// enumRunEach drives the enumerator's source once, forwarding every yield to blk.
// It unifies the four source kinds: a chain iterates its parts, a produce loops
// until StopIteration, a generator runs its block with a yielder, and the default
// forwards to recv.meth. The return value is the source's finish value.
func (vm *VM) enumRunEach(e *Enumerator, blk *Proc) object.Value {
	switch {
	case e.isChain:
		for i, part := range e.chainParts {
			e.entered[i] = true
			vm.send(part, "each", nil, blk)
		}
		return e
	case e.produceBlk != nil:
		return vm.enumRunProduce(e, blk)
	case e.block != nil:
		return vm.callBlock(e.block, []object.Value{&yielder{emit: func(args []object.Value) {
			vm.callBlock(blk, args)
		}}})
	default:
		return vm.send(e.recv, e.meth, e.args, blk)
	}
}

// enumRunProduce drives an Enumerator.produce source: it emits the seed (or the
// first block result when no seed was given) and then repeatedly feeds the last
// value back through the block, stopping cleanly when the block raises
// StopIteration. Any other exception (or a break/enumStop from blk) propagates.
func (vm *VM) enumRunProduce(e *Enumerator, blk *Proc) (result object.Value) {
	result = object.NilV
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok && vm.errIsStopIteration(re) {
				return // StopIteration ends the enumeration
			}
			panic(r)
		}
	}()
	var prev object.Value = object.NilV
	first := true
	for {
		var v object.Value
		if first && e.produceHas {
			v = e.produceInit
		} else {
			v = vm.callBlock(e.produceBlk, []object.Value{prev})
		}
		first, prev = false, v
		vm.callBlock(blk, []object.Value{v})
	}
}

// errIsStopIteration reports whether e is a StopIteration (or a subclass such as
// ClosedQueueError).
func (vm *VM) errIsStopIteration(e RubyError) bool {
	target := vm.consts["StopIteration"].(*RClass)
	for _, a := range vm.ancestors(vm.classOf(vm.exceptionObject(e))) {
		if a == target {
			return true
		}
	}
	return false
}

// enumFiber returns (creating on first use) the Fiber that drives e's source for
// external iteration, suspending at each element via Fiber.yield.
func (vm *VM) enumFiber(e *Enumerator) *Fiber {
	if e.extFiber == nil {
		driver := &Proc{native: func(vm *VM, _ []object.Value) object.Value {
			collect := &Proc{native: func(vm *VM, args []object.Value) object.Value {
				wrapper := object.NewArrayFromSlice(append([]object.Value{}, args...))
				vm.fiberYield([]object.Value{wrapper})
				return object.NilV
			}}
			return vm.enumRunEach(e, collect)
		}}
		e.extFiber = newFiber(vm.currentThread, driver)
	}
	return e.extFiber
}

// enumPull advances e's driving fiber one step, returning the packed yield
// arguments (as an Array) and true, or nil and false when the source is
// exhausted (recording its finish value). An exception thrown by the source
// resets the fiber (so a later #next restarts) and propagates.
func (vm *VM) enumPull(e *Enumerator) (*object.Array, bool) {
	// enumFiber returns a live fiber here: a fiber only dies mid-resume (handled
	// below), after which the caller sets e.ended and the e.ended guards in
	// enumNextRaw/enumPeekRaw prevent re-entry until #rewind (which nils extFiber).
	f := vm.enumFiber(e)
	val := vm.enumResume(e, f)
	if f.state == fibDead {
		e.finish = val
		return nil, false
	}
	return val.(*object.Array), true
}

// enumResume resumes f, resetting e's external-iteration state and re-raising if
// the source terminated with an exception (matching MRI, which restarts the
// enumerator after an exception ended a previous iteration).
func (vm *VM) enumResume(e *Enumerator, f *Fiber) object.Value {
	defer func() {
		if r := recover(); r != nil {
			e.extFiber, e.peeked, e.ended = nil, false, false
			panic(r)
		}
	}()
	return vm.fiberResume(f, nil)
}

// enumNextRaw returns the next yield's arguments (as an Array), advancing the
// position; it raises StopIteration past the end.
func (vm *VM) enumNextRaw(e *Enumerator) *object.Array {
	if e.peeked {
		e.peeked = false
		return e.peekArgs.(*object.Array)
	}
	if e.ended {
		vm.enumRaiseStop(e)
	}
	wrapper, ok := vm.enumPull(e)
	if !ok {
		e.ended = true
		vm.enumRaiseStop(e)
	}
	return wrapper
}

// enumPeekRaw returns the next yield's arguments (as an Array) without advancing;
// it raises StopIteration past the end.
func (vm *VM) enumPeekRaw(e *Enumerator) *object.Array {
	if e.peeked {
		return e.peekArgs.(*object.Array)
	}
	if e.ended {
		vm.enumRaiseStop(e)
	}
	wrapper, ok := vm.enumPull(e)
	if !ok {
		e.ended = true
		vm.enumRaiseStop(e)
	}
	e.peekArgs, e.peeked = wrapper, true
	return wrapper
}

// enumRaiseStop raises StopIteration whose #result is the source's finish value.
func (vm *VM) enumRaiseStop(e *Enumerator) {
	vm.raiseWithIvars("StopIteration", "iteration reached an end",
		map[string]object.Value{"@result": e.finish})
}

// enumPack packs the arguments of one #each yield into a single value, matching
// CRuby's rb_enum_values_pack: a zero-argument yield becomes nil, a lone value
// stays scalar, and several values gather into an Array.
func enumPack(args []object.Value) object.Value {
	switch len(args) {
	case 0:
		return object.NilV
	case 1:
		return args[0]
	default:
		return object.NewArrayFromSlice(append([]object.Value{}, args...))
	}
}

// enumStop is the sentinel panic used by enumTake to unwind out of a possibly
// unbounded #each once enough elements have been collected.
type enumStop struct{}

// enumTake drives the enumerator's source and collects at most n elements,
// aborting the (possibly infinite) iteration as soon as the quota is met.
func (vm *VM) enumTake(e *Enumerator, n int) (out []object.Value) {
	if n <= 0 {
		return []object.Value{}
	}
	out = make([]object.Value, 0, n)
	collect := &Proc{native: func(_ *VM, args []object.Value) object.Value {
		out = append(out, enumPack(args))
		if len(out) >= n {
			panic(enumStop{})
		}
		return object.NilV
	}}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(enumStop); ok {
				return
			}
			panic(r)
		}
	}()
	vm.enumRunEach(e, collect)
	return out
}

// enumMaterialize runs the source with a collecting block and returns the yielded
// elements, recording the source's return value as the finish value.
func (vm *VM) enumMaterialize(e *Enumerator) []object.Value {
	out := []object.Value{}
	collect := &Proc{native: func(_ *VM, args []object.Value) object.Value {
		out = append(out, enumPack(args))
		return object.NilV
	}}
	e.finish = vm.enumRunEach(e, collect)
	return out
}
