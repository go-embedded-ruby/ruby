package vm

import (
	"math"
	"math/big"
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

	// isProduct marks an Enumerator::Product (built by Enumerator.product or
	// Enumerator::Product.new): #each walks the Cartesian product of productSources,
	// consuming each through #each_entry. productSources is nil for an allocated but
	// never-#initialize-d product (rendered "uninitialized"); an initialized product
	// holds a non-nil (possibly empty) slice — Enumerator.product with no arguments
	// yields the single empty tuple [[]].
	isProduct      bool
	productSources []object.Value

	// isArithSeq marks an Enumerator::ArithmeticSequence (built by Numeric#step
	// without a block): a step-driven Enumerator that additionally answers
	// #begin/#end/#step/#exclude_end?/#last from the defining triple below. asEnd is
	// nil for an endless sequence (m.step with no limit).
	isArithSeq bool
	asBegin    object.Value
	asEnd      object.Value
	asStep     object.Value
	asExcl     bool
	// asMethod is the constructing method name shown by ArithmeticSequence#inspect
	// for a Range receiver ("step" or "%"); "" defaults to "step".
	asMethod string
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
	// feedVal (when feedSet) is the value #feed queued for the source's next
	// `yield` to return during external iteration; it is consumed the next time the
	// driving fiber resumes past a yield. #feed refuses a second value before the
	// first is consumed (TypeError), and #rewind discards a pending one.
	feedVal object.Value
	feedSet bool

	// methodValueState makes an Enumerator a boxed value: it carries the frozen
	// flag (so #freeze/#frozen? and Enumerator#initialize's FrozenError work) and
	// any instance variables set on it, exactly as BoundMethod does. The zero value
	// is an unfrozen enumerator with no ivars.
	methodValueState
}

// uninitialized reports whether e came from Class#allocate and was never given a
// source: MRI renders such an enumerator as "#<Enumerator: uninitialized>" and
// raises when it is iterated. Every real constructor sets at least one of these,
// so an all-zero Enumerator is exactly the allocated-but-uninitialized one.
func (e *Enumerator) uninitialized() bool {
	if e.isProduct {
		return e.productSources == nil
	}
	if e.isChain {
		return e.chainParts == nil
	}
	return e.recv == nil && e.block == nil && e.produceBlk == nil
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
// and `y.yield(v)` feed values into the enumeration. emit returns the value the
// downstream consumer produced for this yield — the #feed value during external
// iteration — which `y.yield` surfaces as its own result (MRI's Yielder#yield).
type yielder struct {
	emit func(args []object.Value) object.Value
}

func (y *yielder) ToS() string     { return "#<Enumerator::Yielder>" }
func (y *yielder) Inspect() string { return y.ToS() }
func (y *yielder) Truthy() bool    { return true }

// Inspect renders the MRI form #<Enumerator: recv:meth(args)> (or, for a chain,
// #<Enumerator::Chain: [parts]>). (MRI's #to_s shows the object address, which we
// can't reproduce deterministically, so ToS reuses Inspect.)
func (e *Enumerator) Inspect() string {
	// Enumerator::Product renders its enumerables, guarding against a product that
	// contains itself (MRI's rb_exec_recursive → "...") via the shared repr guard.
	if e.isProduct {
		if e.productSources == nil {
			return "#<Enumerator::Product: uninitialized>"
		}
		if !object.ReprEnter(e) {
			return "#<Enumerator::Product: ...>"
		}
		defer object.ReprLeave(e)
		parts := make([]string, len(e.productSources))
		for i, p := range e.productSources {
			parts[i] = p.Inspect()
		}
		return "#<Enumerator::Product: [" + strings.Join(parts, ", ") + "]>"
	}
	if e.isChain {
		if e.chainParts == nil {
			return "#<Enumerator::Chain: uninitialized>"
		}
		parts := make([]string, len(e.chainParts))
		for i, p := range e.chainParts {
			parts[i] = p.Inspect()
		}
		return "#<Enumerator::Chain: [" + strings.Join(parts, ", ") + "]>"
	}
	if e.uninitialized() {
		// A plain Enumerator.allocate carries none of the subclass flags, so the
		// class name here is always the bare "Enumerator".
		return "#<Enumerator: uninitialized>"
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
	// .new and #initialize both store the parts; .allocate yields an uninitialized
	// chain (chainParts nil), which #inspect renders "uninitialized" and #initialize
	// later fills in. #initialize is private (MRI keeps it so).
	vm.cEnumeratorChain = newClass("Enumerator::Chain", vm.cEnumerator)
	vm.cEnumeratorChain.consts = vm.cEnumerator.consts
	vm.cEnumerator.consts["Chain"] = vm.cEnumeratorChain
	vm.cEnumeratorChain.smethods["new"] = &Method{name: "new", owner: vm.cEnumeratorChain,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return newChain(append([]object.Value{}, args...))
		}}
	vm.cEnumeratorChain.smethods["allocate"] = &Method{name: "allocate", owner: vm.cEnumeratorChain,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &Enumerator{isChain: true}
		}}
	vm.cEnumeratorChain.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		if isFrozen(e) {
			vm.raiseFrozen(e)
		}
		parts := append([]object.Value{}, args...)
		*e = Enumerator{isChain: true, chainParts: parts, entered: make([]bool, len(parts)), methodValueState: e.methodValueState}
		return e
	})
	vm.setInstanceVisibility(vm.cEnumeratorChain, "initialize", visPrivate)

	// Enumerator::Product — the Cartesian product Enumerator.product and
	// Enumerator::Product.new return. .new/#initialize store the enumerables;
	// .allocate yields an uninitialized product; #initialize/#initialize_copy are
	// private; #size/#rewind/#each read productSources so a post-hoc
	// #initialize_copy (or #rewind) is reflected.
	vm.cEnumeratorProduct = newClass("Enumerator::Product", vm.cEnumerator)
	vm.cEnumeratorProduct.consts = vm.cEnumerator.consts
	vm.cEnumerator.consts["Product"] = vm.cEnumeratorProduct
	vm.cEnumerator.smethods["product"] = &Method{name: "product", owner: vm.cEnumerator,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			return vm.enumProduct(args, blk)
		}}
	vm.cEnumeratorProduct.smethods["new"] = &Method{name: "new", owner: vm.cEnumeratorProduct,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			e := &Enumerator{isProduct: true}
			productInit(e, args)
			return e
		}}
	vm.cEnumeratorProduct.smethods["allocate"] = &Method{name: "allocate", owner: vm.cEnumeratorProduct,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &Enumerator{isProduct: true}
		}}
	vm.cEnumeratorProduct.define("initialize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		if isFrozen(e) {
			vm.raiseFrozen(e)
		}
		productInit(e, args)
		return e
	})
	vm.setInstanceVisibility(vm.cEnumeratorProduct, "initialize", visPrivate)
	vm.cEnumeratorProduct.define("initialize_copy", productInitCopy)
	vm.setInstanceVisibility(vm.cEnumeratorProduct, "initialize_copy", visPrivate)
	vm.cEnumeratorProduct.define("size", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.enumProductSize(self.(*Enumerator))
	})
	vm.cEnumeratorProduct.define("rewind", productRewind)

	// Enumerator::ArithmeticSequence — the subclass Numeric#step (and #step on an
	// Integer/Float) returns when called without a block. It is a step-driven
	// Enumerator (its #each/#to_a/#first/#size all reuse the inherited step
	// machinery) that additionally exposes the defining triple.
	vm.cArithSeq = newClass("Enumerator::ArithmeticSequence", vm.cEnumerator)
	vm.cArithSeq.consts = vm.cEnumerator.consts
	vm.cEnumerator.consts["ArithmeticSequence"] = vm.cArithSeq
	// MRI defines neither .new (an instance only ever comes from Numeric#step /
	// Range#step) nor an allocator for ArithmeticSequence: .new raises NoMethodError
	// and .allocate raises TypeError. Override the inherited Enumerator ones to match.
	vm.cArithSeq.smethods["new"] = &Method{name: "new", owner: vm.cArithSeq,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			raise("NoMethodError", "undefined method 'new' for class Enumerator::ArithmeticSequence")
			return object.NilV
		}}
	vm.cArithSeq.smethods["allocate"] = &Method{name: "allocate", owner: vm.cArithSeq,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			raise("TypeError", "allocator undefined for Enumerator::ArithmeticSequence")
			return object.NilV
		}}
	// #== / #hash key on the defining triple (begin, end, step) plus exclude_end?,
	// so two sequences built by different constructors (1.step(10,100) and
	// (1..10).step(100)) compare and hash equal — MRI's arith_seq_eq / arith_seq_hash.
	vm.cArithSeq.define("==", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		o, ok := args[0].(*Enumerator)
		if !ok || !o.isArithSeq {
			return object.False
		}
		eq := vm.send(e.asBegin, "==", []object.Value{o.asBegin}, nil).Truthy() &&
			vm.send(e.asEnd, "==", []object.Value{o.asEnd}, nil).Truthy() &&
			vm.send(e.asStep, "==", []object.Value{o.asStep}, nil).Truthy() &&
			e.asExcl == o.asExcl
		return object.Bool(eq)
	})
	vm.cArithSeq.define("hash", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		h := int64(1)
		for _, v := range []object.Value{e.asBegin, e.asEnd, e.asStep} {
			hv := vm.send(v, "hash", nil, nil)
			if i, ok := hv.(object.Integer); ok {
				h = h*31 + int64(i)
			}
		}
		if e.asExcl {
			h = h*31 + 1
		}
		return object.IntValue(h)
	})
	vm.cArithSeq.define("begin", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*Enumerator).asBegin
	})
	vm.cArithSeq.define("end", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*Enumerator).asEnd
	})
	vm.cArithSeq.define("step", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*Enumerator).asStep
	})
	vm.cArithSeq.define("exclude_end?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*Enumerator).asExcl)
	})
	// #inspect / #to_s mirror MRI's arith_seq_inspect: a Range receiver reads
	// ((begin..end).step(step)) — or .%(step) when built by Range#% — while a
	// Numeric receiver reads (begin.step(end, step)). A nil begin/end (an
	// unbounded sequence) renders empty on that side, as Range#inspect does.
	arithSeqInspect := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		if _, isRange := e.recv.(*object.Range); isRange {
			meth := e.asMethod
			if meth == "" {
				meth = "step"
			}
			rng := object.NewRange(e.asBegin, e.asEnd, e.asExcl)
			return object.NewString("((" + rng.Inspect() + ")." + meth + "(" + e.asStep.Inspect() + "))")
		}
		return object.NewString("(" + e.asBegin.Inspect() + ".step(" + e.asEnd.Inspect() + ", " + e.asStep.Inspect() + "))")
	}
	vm.cArithSeq.define("inspect", arithSeqInspect)
	vm.cArithSeq.define("to_s", arithSeqInspect)
	// #last materialises the (necessarily bounded) sequence; MRI refuses it for an
	// endless one rather than looping for ever.
	vm.cArithSeq.define("last", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		if object.IsNil(e.asEnd) {
			raise("RangeError", "cannot get the last element of endless arithmetic sequence")
		}
		elems := vm.enumMaterialize(e)
		if len(args) == 0 {
			if len(elems) == 0 {
				return object.NilV
			}
			return elems[len(elems)-1]
		}
		n := int(intArg(args[0]))
		if n < 0 {
			raise("ArgumentError", "negative array size")
		}
		if n > len(elems) {
			n = len(elems)
		}
		return object.NewArrayFromSlice(append([]object.Value{}, elems[len(elems)-n:]...))
	})

	// Enumerator::Yielder — `y << v` / `y.yield(v)` feed the generator's values in.
	vm.cYielder = newClass("Enumerator::Yielder", vm.cObject)
	vm.cYielder.consts = vm.cEnumerator.consts // (scope is cosmetic; share the map)
	vm.cEnumerator.consts["Yielder"] = vm.cYielder
	vm.cYielder.define("<<", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		self.(*yielder).emit(args)
		return self // << chains
	})
	vm.cYielder.define("yield", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return self.(*yielder).emit(args)
	})
	// &yielder passes the yielder as a block: its #to_proc calls #<< (so
	// `str.each_line(&y)` feeds each line into the enumeration).
	vm.cYielder.define("to_proc", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		y := self.(*yielder)
		return &Proc{native: func(vm *VM, args []object.Value) object.Value {
			return y.emit(args)
		}}
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
	// Enumerator.allocate yields an uninitialized Enumerator (a distinct Go value
	// so the instance methods work once #initialize gives it a source), rather than
	// the generic *RObject Class#allocate would produce.
	vm.cEnumerator.smethods["allocate"] = &Method{name: "allocate", owner: vm.cEnumerator,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &Enumerator{}
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
	// #inspect / #to_s render through the receiver's Go Inspect. An instance from
	// Class#allocate (never #initialize-d) is still a typed *Enumerator/*LazyEnum
	// whose Inspect reports the "#<ClassName: uninitialized>" form MRI shows, so no
	// separate uninitialized branch is needed. The receiver is always one of the
	// two concrete types (subclasses inherit the typed allocate above).
	inspectFn := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if l, ok := self.(*LazyEnum); ok {
			return object.NewString(l.Inspect())
		}
		return object.NewString(self.(*Enumerator).Inspect())
	}
	d("inspect", inspectFn)
	d("to_s", inspectFn)
	// Enumerator#initialize(size = nil) { |y| … } configures an allocated (or
	// re-initialised) Enumerator as a generator, mirroring Enumerator.new. It is a
	// private method, requires a block (ArgumentError otherwise, with MRI's Proc
	// message), refuses a frozen receiver (FrozenError), and returns self.
	d("initialize", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		e := self.(*Enumerator)
		if isFrozen(e) {
			vm.raiseFrozen(e)
		}
		if blk == nil {
			raise("ArgumentError", "tried to create Proc object without a block")
		}
		if len(args) > 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0..1)", len(args))
		}
		*e = Enumerator{block: blk, methodValueState: e.methodValueState}
		if len(args) == 1 {
			e.sizeSpec, e.sizeSpecSet = args[0], true
		}
		return e
	})
	vm.cEnumerator.methods["initialize"].vis = visPrivate
	d("each", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		e := self.(*Enumerator)
		if len(args) > 0 {
			// #each(*extra) appends the extra arguments to the receiver's #each call:
			// a fresh enumerator with the wider argument list, run (or returned) here.
			ne := *e
			ne.args = append(append([]object.Value{}, e.args...), args...)
			ne.extFiber, ne.peeked, ne.ended = nil, false, false
			if blk == nil {
				return &ne
			}
			return vm.enumRunEach(&ne, blk)
		}
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
	d("feed", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		if e.feedSet {
			raise("TypeError", "feed value already set")
		}
		var v object.Value = object.NilV
		if len(args) > 0 {
			v = args[0]
		}
		e.feedVal, e.feedSet = v, true
		return object.NilV
	})
	d("rewind", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		e.extFiber, e.peeked, e.ended, e.feedSet = nil, false, false, false
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
	d("each_with_index", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) > 0 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
		}
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
	// #take_while collects elements while the block is truthy and stops at the
	// first falsy one. MRI's rb_iter_break makes it terminate even for an unbounded
	// source (Enumerator.produce, Array#cycle); the generic Enumerable#take_while
	// keeps iterating to the end, which would hang here — so Enumerator overrides it
	// to break early (via the enumStop unwind #take already uses).
	d("take_while", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		e := self.(*Enumerator)
		if blk == nil {
			return enumFor(self, "take_while")
		}
		out := []object.Value{}
		collect := &Proc{native: func(_ *VM, cargs []object.Value) object.Value {
			v := enumPack(cargs)
			if !vm.callBlock(blk, []object.Value{v}).Truthy() {
				panic(enumStop{})
			}
			out = append(out, v)
			return object.NilV
		}}
		func() {
			defer func() {
				if r := recover(); r != nil {
					if _, ok := r.(enumStop); ok {
						return
					}
					panic(r)
				}
			}()
			vm.enumRunEach(e, collect)
		}()
		return object.NewArrayFromSlice(out)
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

// enumProduct implements Enumerator.product. It returns an Enumerator::Product
// over the Cartesian product of its enumerable arguments (stored on the
// returned enumerator so #size/#rewind/#inspect can read them). Keyword
// arguments are rejected. With a block it iterates the product and returns nil.
func (vm *VM) enumProduct(args []object.Value, blk *Proc) object.Value {
	if h := trailingKwHash(args); h != nil {
		bad := make([]string, len(h.Keys))
		for i, k := range h.Keys {
			bad[i] = k.Inspect()
		}
		raise("ArgumentError", "unknown keywords: %s", strings.Join(bad, ", "))
	}
	e := &Enumerator{isProduct: true}
	productInit(e, args)
	if blk == nil {
		return e
	}
	vm.enumProductEach(e, blk)
	return object.NilV
}

// productInit configures e as an initialized Enumerator::Product over args,
// resetting any prior state. append onto a fresh slice yields a non-nil (so
// "initialized", even for no arguments) copy that the product owns.
func productInit(e *Enumerator, args []object.Value) {
	*e = Enumerator{isProduct: true, productSources: append([]object.Value{}, args...), methodValueState: e.methodValueState}
}

// productInitCopy is Enumerator::Product#initialize_copy: it replaces the
// receiver's enumerables with the source's. It is a no-op for a self-copy (even
// when frozen), then rejects a frozen receiver, a source of a different class,
// and an uninitialized source — mirroring MRI's OBJ_INIT_COPY plus the
// "uninitialized product" guard.
func productInitCopy(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
	e := self.(*Enumerator)
	if len(args) != 1 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
	}
	o, ok := args[0].(*Enumerator)
	if ok && e == o {
		return e // self-copy: nothing to do, even on a frozen receiver
	}
	if isFrozen(e) {
		vm.raiseFrozen(e)
	}
	if !ok || vm.classOf(e) != vm.classOf(o) {
		raise("TypeError", "initialize_copy should take same class object")
	}
	if o.productSources == nil {
		raise("ArgumentError", "uninitialized product")
	}
	e.productSources = o.productSources
	e.extFiber, e.peeked, e.ended, e.feedSet = nil, false, false, false
	return e
}

// productRewind is Enumerator::Product#rewind: it rewinds each enumerable that
// responds to #rewind, in forward order, and drops any external-iteration state.
func productRewind(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
	e := self.(*Enumerator)
	e.extFiber, e.peeked, e.ended, e.feedSet = nil, false, false, false
	for _, src := range e.productSources {
		if vm.respondsTo(src, "rewind") {
			vm.send(src, "rewind", nil, nil)
		}
	}
	return e
}

// enumProductEach walks the Cartesian product of e.productSources, consuming
// each source through #each_entry afresh at every outer step (so an infinite
// source yields an infinite product), and forwards each combination (an Array)
// to blk. It returns e.
func (vm *VM) enumProductEach(e *Enumerator, blk *Proc) object.Value {
	var combine func(rest, prefix []object.Value)
	combine = func(rest, prefix []object.Value) {
		if len(rest) == 0 {
			vm.callBlock(blk, []object.Value{object.NewArrayFromSlice(append([]object.Value{}, prefix...))})
			return
		}
		first, tail := rest[0], rest[1:]
		step := &Proc{native: func(vm *VM, a []object.Value) object.Value {
			// each_entry yields one entry per element; a multi-value yield keeps only
			// its first value, a bare yield contributes nil — as MRI's product does.
			var x object.Value = object.NilV
			if len(a) > 0 {
				x = a[0]
			}
			combine(tail, append(append([]object.Value{}, prefix...), x))
			return object.NilV
		}}
		vm.send(first, "each_entry", nil, step)
	}
	combine(e.productSources, nil)
	return e
}

// enumProductSize is Enumerator::Product#size: the product of the sources'
// sizes. It returns 0 as soon as any source is empty; nil when any source lacks
// #size, reports nil, or reports a non-Integer finite size (including NaN);
// Float::INFINITY when any reports an infinite size; otherwise the Integer
// product. Mirrors MRI's enum_product_total_size.
func (vm *VM) enumProductSize(e *Enumerator) object.Value {
	total := big.NewInt(1)
	for _, src := range e.productSources {
		if !vm.respondsToDynamic(src, "size") {
			return object.NilV
		}
		switch s := vm.send(src, "size", nil, nil).(type) {
		case object.Integer:
			if s == 0 {
				return object.IntValue(0)
			}
			total.Mul(total, big.NewInt(int64(s)))
		case *object.Bignum:
			if s.I.Sign() == 0 {
				return object.IntValue(0)
			}
			total.Mul(total, s.I)
		case object.Float:
			if math.IsInf(float64(s), 1) {
				return object.Float(math.Inf(1))
			}
			return object.NilV // a finite (or NaN) Float size is not multiplied
		default:
			return object.NilV // nil, or a value with no integer meaning
		}
	}
	return object.NormInt(total)
}

// isInfFloat reports whether v is a positive-infinite Float.
func isInfFloat(v object.Value) bool {
	f, ok := v.(object.Float)
	return ok && math.IsInf(float64(f), 1)
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
	case e.isProduct:
		return vm.enumProductEach(e, blk)
	case e.isChain:
		for i, part := range e.chainParts {
			e.entered[i] = true
			vm.send(part, "each", nil, blk)
		}
		return e
	case e.produceBlk != nil:
		return vm.enumRunProduce(e, blk)
	case e.block != nil:
		return vm.callBlock(e.block, []object.Value{&yielder{emit: func(args []object.Value) object.Value {
			return vm.callBlock(blk, args)
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
				// On resume, hand the source's `yield` any value #feed queued.
				if e.feedSet {
					fv := e.feedVal
					e.feedVal, e.feedSet = nil, false
					return fv
				}
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
