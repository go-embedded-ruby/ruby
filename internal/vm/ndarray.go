package vm

import (
	"errors"
	"fmt"
	"math"
	"slices"

	nd "github.com/go-ndarray/ndarray"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// NDArray binds github.com/go-ndarray/ndarray — a pure-Go (cgo-free) NumPy-style
// n-dimensional array — into Ruby, so Ruby gets numpy-style arrays with no native
// dependency. It wraps a *nd.Array as a Ruby value.
//
// The Ruby surface is the one the go-ndarray playground's code generator emits
// (Go | Ruby | NumPy): creation (zeros/ones/full/arange/linspace/eye/from),
// views (reshape/transpose/flatten/squeeze/expand_dims and NumPy basic indexing
// through #[]), elementwise ufuncs and **, reductions with an axis and
// keepdims:, products and joins. Semantics are go-ndarray's, which are NumPy's,
// in float64.
//
// Every method checks its own arguments. A wrong count raises ArgumentError, a
// wrong type TypeError, and a go-ndarray error is re-raised with go-ndarray's own
// message (IndexError for an index or axis out of range, ArgumentError for the
// rest). No Go runtime fault reaches the caller.

// NDArray is the Ruby wrapper around a go-ndarray Array.
type NDArray struct{ a *nd.Array }

// ToS and Inspect print go-ndarray's NumPy-style Repr, e.g. "array([1., 2.])".
func (n *NDArray) ToS() string     { return n.a.Repr() }
func (n *NDArray) Inspect() string { return n.a.Repr() }
func (n *NDArray) Truthy() bool    { return true }

// ndArg asserts an argument is an NDArray.
func ndArg(v object.Value) *NDArray {
	a, ok := v.(*NDArray)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into NDArray", v.Inspect())
	}
	return a
}

// ndRaise re-raises a go-ndarray error as the Ruby exception NumPy's would map
// to: an index or axis out of range is an IndexError (NumPy's AxisError is an
// IndexError too), anything else (shape mismatch, broadcasting, an empty
// reduction) an ArgumentError. The message is go-ndarray's own.
func ndRaise(err error) {
	if errors.Is(err, nd.ErrIndex) || errors.Is(err, nd.ErrAxis) {
		raise("IndexError", "%s", err.Error())
	}
	raise("ArgumentError", "%s", err.Error())
}

// mustArray raises a Ruby error when a go-ndarray operation fails (shape
// mismatch, empty reduction, …) instead of returning a Go error.
func mustArray(a *nd.Array, err error) *NDArray {
	if err != nil {
		ndRaise(err)
	}
	return &NDArray{a: a}
}

// ndArgc raises MRI's ArgumentError unless min <= len(args) <= max; max < 0
// means no upper bound.
func ndArgc(args []object.Value, min, max int) {
	n := len(args)
	if n >= min && (max < 0 || n <= max) {
		return
	}
	want := fmt.Sprint(min)
	switch {
	case max < 0:
		want += "+"
	case max > min:
		want += fmt.Sprintf("..%d", max)
	}
	raise("ArgumentError", "wrong number of arguments (given %d, expected %s)", n, want)
}

// ndTypeName names v the way MRI's conversion errors do: nil/true/false by
// value, everything else by class.
func (vm *VM) ndTypeName(v object.Value) string {
	switch v.(type) {
	case object.Nil, object.Bool:
		return v.Inspect()
	}
	return vm.classOf(v).name
}

// ndInt reads an Integer argument (an index, an axis, a dimension).
func (vm *VM) ndInt(v object.Value) int {
	if i, ok := v.(object.Integer); ok {
		return int(i)
	}
	raise("TypeError", "no implicit conversion of %s into Integer", vm.ndTypeName(v))
	return 0
}

// ndFloat reads a real-number argument.
func (vm *VM) ndFloat(v object.Value) float64 {
	if f, ok := toFloat(v); ok {
		return f
	}
	raise("TypeError", "can't convert %s into Float", vm.ndTypeName(v))
	return 0
}

// ndArray reads an NDArray operand, or a real number as a 0-d array (which
// broadcasts against anything, as a NumPy scalar does).
func (vm *VM) ndArray(v object.Value) *nd.Array {
	if n, ok := v.(*NDArray); ok {
		return n.a
	}
	if f, ok := toFloat(v); ok {
		a, _ := nd.Full(f) // a 0-d shape is always valid
		return a
	}
	raise("TypeError", "no implicit conversion of %s into NDArray", vm.ndTypeName(v))
	return nil
}

// ndShape reads a shape given either as Integer arguments (zeros(2, 3)) or as a
// single Array of them (zeros([2, 3])), as NumPy accepts both.
func (vm *VM) ndShape(args []object.Value) []int {
	if len(args) == 1 {
		if arr, ok := args[0].(*object.Array); ok {
			args = arr.Elems
		}
	}
	shape := make([]int, len(args))
	for i, v := range args {
		shape[i] = vm.ndInt(v)
	}
	return shape
}

// ndKw splits a trailing keyword Hash off args, accepting only the given
// keyword names (MRI's "unknown keyword" otherwise).
func ndKw(args []object.Value, names ...string) ([]object.Value, map[string]object.Value) {
	kw := map[string]object.Value{}
	n := len(args)
	if n == 0 {
		return args, kw
	}
	h, ok := args[n-1].(*object.Hash)
	if !ok {
		return args, kw
	}
	for _, k := range h.Keys {
		s, ok := k.(object.Symbol)
		if !ok || !slices.Contains(names, string(s)) {
			raise("ArgumentError", "unknown keyword: %s", k.Inspect())
		}
		kw[string(s)], _ = h.Get(k)
	}
	return args[:n-1], kw
}

// ndKeep reads the keepdims: keyword (default false).
func ndKeep(kw map[string]object.Value) bool {
	v, ok := kw["keepdims"]
	return ok && v.Truthy()
}

// ndOnes is the all-ones shape of the given rank: what a whole-array reduction
// with keepdims: true leaves.
func ndOnes(ndim int) []int {
	s := make([]int, ndim)
	for i := range s {
		s[i] = 1
	}
	return s
}

// ndApply dispatches a binary operator over two go-ndarray arrays.
func ndApply(op bytecode.Op, a, b *nd.Array) *NDArray {
	switch op {
	case bytecode.OpAdd:
		return mustArray(a.Add(b))
	case bytecode.OpSub:
		return mustArray(a.Sub(b))
	case bytecode.OpMul:
		return mustArray(a.Mul(b))
	case bytecode.OpDiv:
		return mustArray(a.Div(b))
	}
	return raiseND(op)
}

func raiseND(op bytecode.Op) *NDArray {
	raise("NoMethodError", "undefined method '%s' for an NDArray", op)
	return nil
}

// ndScalar dispatches a binary operator between an array and a scalar (array on
// the left).
func ndScalar(op bytecode.Op, a *nd.Array, s float64) *NDArray {
	switch op {
	case bytecode.OpAdd:
		return &NDArray{a: a.AddScalar(s)}
	case bytecode.OpSub:
		return &NDArray{a: a.SubScalar(s)}
	case bytecode.OpMul:
		return &NDArray{a: a.MulScalar(s)}
	case bytecode.OpDiv:
		return &NDArray{a: a.DivScalar(s)}
	}
	return raiseND(op)
}

// ndarrayOp implements the arithmetic operators when an NDArray is involved:
// array⊕array (element-wise), array⊕scalar, and scalar⊕array.
func ndarrayOp(op bytecode.Op, a, b object.Value) object.Value {
	if an, ok := a.(*NDArray); ok {
		if bn, ok := b.(*NDArray); ok {
			return ndApply(op, an.a, bn.a)
		}
		if f, ok := toFloat(b); ok {
			return ndScalar(op, an.a, f)
		}
		return raise("TypeError", "no implicit conversion of %s into NDArray", b.Inspect())
	}
	// a is a scalar, b is an NDArray: broadcast the scalar to an array first so
	// non-commutative ops (scalar - array, scalar / array) are correct.
	bn := b.(*NDArray)
	f, ok := toFloat(a)
	if !ok {
		return raise("TypeError", "no implicit conversion of %s into NDArray", a.Inspect())
	}
	full := mustArray(nd.Full(f, bn.a.Shape()...))
	return ndApply(op, full.a, bn.a)
}

// ndPow is array ** array, broadcasting as NumPy's power does. go-ndarray has
// only a scalar Power, so both operands are first broadcast to their common
// shape by multiplying with ones (exact for every float64, -0 and ±Inf
// included), then math.Pow — the function go-ndarray's Power uses — runs
// elementwise.
func ndPow(a, b *nd.Array) *NDArray {
	onesA, _ := nd.Ones(a.Shape()...) // the shape of an existing array is valid
	onesB, _ := nd.Ones(b.Shape()...)
	x, err := a.Mul(onesB)
	if err != nil {
		ndRaise(err)
	}
	y, _ := b.Mul(onesA) // same pair of shapes: cannot fail once x did not
	xs, ys := x.Flatten(), y.Flatten()
	out := make([]float64, xs.Size())
	for i := range out {
		out[i] = math.Pow(xs.At(i), ys.At(i))
	}
	return mustArray(nd.FromData(out, x.Shape()...))
}

// ndSpan converts a Ruby range (lo..hi, lo...hi, or an ArithmeticSequence with
// a step) into a go-ndarray slice index for an axis of the given length.
//
// NumPy's slice is start:stop:step with stop exclusive. A Ruby range may be
// inclusive, so an inclusive end e becomes stop e+1 for a positive step and e-1
// for a negative one — except at the boundary where that would wrap: 1..-1 is
// "to the end" (1:), and (5..0) % -1 is "down to the first element" (5::-1),
// whose stop is not -1 (which NumPy reads as the last element) but absent. A
// nil end is absent too, and an absent bound takes NumPy's default for the sign
// of the step, made concrete here because go-ndarray's constructors cannot
// express "start and step but no stop": a stop of -(length+1) normalises to
// "past the first element".
func (vm *VM) ndSpan(lo, hi object.Value, step int, excl bool, length int) nd.Index {
	start, stop := -1, -(length + 1)
	if step > 0 {
		start, stop = 0, length
	}
	if !object.IsNil(lo) {
		start = vm.ndInt(lo)
	}
	if !object.IsNil(hi) {
		e := vm.ndInt(hi)
		switch {
		case excl:
			stop = e
		case step > 0 && e != -1:
			stop = e + 1
		case step < 0 && e != 0:
			stop = e - 1
		}
	}
	return nd.Rng(start, stop, step)
}

// ndIndex converts one #[] argument for an axis of the given length: an Integer
// (selects, dropping the axis), true (the whole axis), a Range, or an
// Enumerator::ArithmeticSequence ((0..) % 2, (5..1).step(-1)).
func (vm *VM) ndIndex(x object.Value, length int) nd.Index {
	switch r := x.(type) {
	case object.Integer:
		return nd.A(int(r))
	case object.Bool:
		if r {
			return nd.All()
		}
	case *object.Range:
		return vm.ndSpan(r.Lo, r.Hi, 1, r.Exclusive, length)
	case *Enumerator:
		if r.isArithSeq {
			return vm.ndSpan(r.asBegin, r.asEnd, vm.ndInt(r.asStep), r.asExcl, length)
		}
	}
	raise("TypeError", "no implicit conversion of %s into an NDArray index", vm.ndTypeName(x))
	return nd.Index{}
}

// ndSlice implements a[i0, i1, ...]: NumPy basic indexing, one argument per
// leading axis, a view sharing the data. When every argument is an Integer and
// there is one per axis the result is the element, as a Float.
func (vm *VM) ndSlice(a *nd.Array, args []object.Value) object.Value {
	shape := a.Shape()
	idx := make([]nd.Index, len(args))
	element := len(args) == len(shape)
	for i, x := range args {
		length := 0 // an index past the rank: Slice reports it below
		if i < len(shape) {
			length = shape[i]
		}
		if _, ok := x.(object.Integer); !ok {
			element = false
		}
		idx[i] = vm.ndIndex(x, length)
	}
	r, err := a.Slice(idx...)
	if err != nil {
		ndRaise(err)
	}
	if element {
		return object.Float(r.At())
	}
	return &NDArray{a: r}
}

// ndArrays reads an Array of NDArrays (the operands of concatenate / stack).
func (vm *VM) ndArrays(v object.Value) []*nd.Array {
	arr, ok := v.(*object.Array)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Array", vm.ndTypeName(v))
	}
	out := make([]*nd.Array, len(arr.Elems))
	for i, e := range arr.Elems {
		n, ok := e.(*NDArray)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into NDArray", vm.ndTypeName(e))
		}
		out[i] = n.a
	}
	return out
}

// ndAxisKw reads the axis: keyword of concatenate / stack (default 0).
func (vm *VM) ndAxisKw(kw map[string]object.Value) int {
	if v, ok := kw["axis"]; ok {
		return vm.ndInt(v)
	}
	return 0
}

// registerNDArray installs the NDArray class, its constructors and methods.
func (vm *VM) registerNDArray() {
	vm.cNDArray = newClass("NDArray", vm.cObject)
	vm.consts["NDArray"] = vm.cNDArray

	s := func(name string, fn NativeFn) {
		vm.cNDArray.smethods[name] = &Method{name: name, owner: vm.cNDArray, native: fn}
	}

	// Creation.
	s("zeros", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return mustArray(nd.Zeros(vm.ndShape(args)...))
	})
	s("ones", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return mustArray(nd.Ones(vm.ndShape(args)...))
	})
	s("full", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, -1)
		return mustArray(nd.Full(vm.ndFloat(args[0]), vm.ndShape(args[1:])...))
	})
	// arange(stop), arange(start, stop), arange(start, stop, step), as in NumPy.
	s("arange", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 3)
		f := make([]float64, len(args))
		for i, v := range args {
			f[i] = vm.ndFloat(v)
		}
		start, step := 0.0, 1.0
		stop := f[0]
		if len(f) > 1 {
			start, stop = f[0], f[1]
		}
		if len(f) > 2 {
			step = f[2]
		}
		return mustArray(nd.Arange(start, stop, step))
	})
	// linspace(start, stop, num = 50), as in NumPy.
	s("linspace", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 2, 3)
		num := 50
		if len(args) > 2 {
			num = vm.ndInt(args[2])
		}
		return mustArray(nd.Linspace(vm.ndFloat(args[0]), vm.ndFloat(args[1]), num))
	})
	// eye(n, m = n, k = 0), as in NumPy.
	s("eye", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 3)
		n := vm.ndInt(args[0])
		m, k := n, 0
		if len(args) > 1 {
			m = vm.ndInt(args[1])
		}
		if len(args) > 2 {
			k = vm.ndInt(args[2])
		}
		return mustArray(nd.Eye(n, m, k))
	})
	s("from", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 2, 2)
		return mustArray(nd.FromData(floatSlice(args[0]), intSlice(args[1])...))
	})

	// Elementwise and products of two operands.
	binary := func(name string, f func(a, b *nd.Array) (*nd.Array, error)) {
		s(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			ndArgc(args, 2, 2)
			return mustArray(f(vm.ndArray(args[0]), vm.ndArray(args[1])))
		})
	}
	binary("maximum", (*nd.Array).Maximum)
	binary("minimum", (*nd.Array).Minimum)
	binary("outer", func(a, b *nd.Array) (*nd.Array, error) { return a.Outer(b), nil })

	// Joins: concatenate([a, b], axis: 0) / stack([a, b], axis: 0).
	join := func(name string, f func([]*nd.Array, int) (*nd.Array, error)) {
		s(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			args, kw := ndKw(args, "axis")
			ndArgc(args, 1, 1)
			return mustArray(f(vm.ndArrays(args[0]), vm.ndAxisKw(kw)))
		})
	}
	join("concatenate", nd.Concatenate)
	join("stack", nd.Stack)

	d := func(name string, fn NativeFn) { vm.cNDArray.define(name, fn) }
	self := func(v object.Value) *nd.Array { return v.(*NDArray).a }

	// Methods of no argument returning an array (views, copies, ufuncs).
	unary := func(name string, f func(*nd.Array) *nd.Array) {
		d(name, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			ndArgc(args, 0, 0)
			return &NDArray{a: f(self(v))}
		})
	}
	unary("sqrt", (*nd.Array).Sqrt)
	unary("exp", (*nd.Array).Exp)
	unary("log", (*nd.Array).Log)
	unary("sin", (*nd.Array).Sin)
	unary("cos", (*nd.Array).Cos)
	unary("abs", (*nd.Array).Abs)
	unary("square", (*nd.Array).Square)
	unary("neg", (*nd.Array).Neg)
	unary("transpose", (*nd.Array).Transpose)
	unary("T", (*nd.Array).Transpose)
	unary("flatten", (*nd.Array).Flatten)
	// dup / copy copy the DATA (ndarray.Copy), so writes to one never show in
	// the other — unlike a view from #[] or #reshape.
	unary("dup", (*nd.Array).Copy)
	unary("copy", (*nd.Array).Copy)

	// Methods of no argument returning a Ruby value.
	plain := func(name string, f func(*nd.Array) object.Value) {
		d(name, func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			ndArgc(args, 0, 0)
			return f(self(v))
		})
	}
	plain("shape", func(a *nd.Array) object.Value {
		sh := a.Shape()
		out := make([]object.Value, len(sh))
		for i, x := range sh {
			out[i] = object.IntValue(int64(x))
		}
		return object.NewArrayFromSlice(out)
	})
	plain("ndim", func(a *nd.Array) object.Value { return object.IntValue(int64(a.Ndim())) })
	plain("size", func(a *nd.Array) object.Value { return object.IntValue(int64(a.Size())) })
	// to_a is FLAT, row-major, Floats.
	plain("to_a", func(a *nd.Array) object.Value {
		flat := a.Flatten()
		out := make([]float64, flat.Size())
		for i := range out {
			out[i] = flat.At(i)
		}
		return floatArray(out)
	})
	plain("inspect", func(a *nd.Array) object.Value { return object.NewString(a.Repr()) })
	plain("to_s", func(a *nd.Array) object.Value { return object.NewString(a.Repr()) })

	d("reshape", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustArray(self(v).Reshape(vm.ndShape(args)...))
	})
	d("squeeze", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustArray(self(v).Squeeze(vm.ndShape(args)...))
	})
	d("expand_dims", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 1)
		return mustArray(self(v).ExpandDims(vm.ndInt(args[0])))
	})
	d("clip", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 2, 2)
		return mustArray(self(v).Clip(vm.ndFloat(args[0]), vm.ndFloat(args[1])))
	})
	// a ** x: a scalar exponent, or an array one (broadcasting).
	d("**", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 1)
		if f, ok := toFloat(args[0]); ok {
			return &NDArray{a: self(v).Power(f)}
		}
		return ndPow(self(v), vm.ndArray(args[0]))
	})
	// coerce lets a number on the LEFT of ** (2 ** a) reach NDArray#**, through
	// Integer#** / Float#**'s coercion protocol: the number becomes an array of
	// the receiver's shape.
	d("coerce", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 1)
		full := mustArray(nd.Full(vm.ndFloat(args[0]), self(v).Shape()...))
		return object.NewArrayFromSlice([]object.Value{full, v})
	})
	d("matmul", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 1)
		return mustArray(self(v).MatMul(ndArg(args[0]).a))
	})
	d("dot", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 1)
		return mustArray(self(v).Dot(ndArg(args[0]).a))
	})

	// Reductions: a.sum is a Float; a.sum(axis) an NDArray; keepdims: true keeps
	// the reduced axes as length 1 (all of them when there is no axis).
	reduce := func(name string, whole func(*nd.Array) (float64, error), along func(*nd.Array, int, bool) (*nd.Array, error)) {
		d(name, func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			args, kw := ndKw(args, "keepdims")
			ndArgc(args, 0, 1)
			a, keep := self(v), ndKeep(kw)
			if len(args) == 1 && !object.IsNil(args[0]) {
				return mustArray(along(a, vm.ndInt(args[0]), keep))
			}
			r, err := whole(a)
			if err != nil {
				ndRaise(err)
			}
			if keep {
				return mustArray(nd.Full(r, ndOnes(a.Ndim())...))
			}
			return object.Float(r)
		})
	}
	noErr := func(f func(*nd.Array) float64) func(*nd.Array) (float64, error) {
		return func(a *nd.Array) (float64, error) { return f(a), nil }
	}
	reduce("sum", noErr((*nd.Array).Sum), (*nd.Array).SumAxis)
	reduce("prod", noErr((*nd.Array).Prod), (*nd.Array).ProdAxis)
	reduce("mean", (*nd.Array).Mean, (*nd.Array).MeanAxis)
	reduce("max", (*nd.Array).Max, (*nd.Array).MaxAxis)
	reduce("min", (*nd.Array).Min, (*nd.Array).MinAxis)

	// argmax / argmin: a flat index (an Integer) with no axis, else an array of
	// indices along the axis.
	argReduce := func(name string, whole func(*nd.Array) (int, error), along func(*nd.Array, int, bool) (*nd.Array, error)) {
		d(name, func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			args, kw := ndKw(args, "keepdims")
			ndArgc(args, 0, 1)
			a, keep := self(v), ndKeep(kw)
			if len(args) == 1 && !object.IsNil(args[0]) {
				return mustArray(along(a, vm.ndInt(args[0]), keep))
			}
			i, err := whole(a)
			if err != nil {
				ndRaise(err)
			}
			if keep {
				return mustArray(nd.FromSlice([]int64{int64(i)}, ndOnes(a.Ndim())...))
			}
			return object.IntValue(int64(i))
		})
	}
	argReduce("argmax", (*nd.Array).ArgMax, (*nd.Array).ArgMaxAxis)
	argReduce("argmin", (*nd.Array).ArgMin, (*nd.Array).ArgMinAxis)

	// cumsum: flattened with no axis, else along the axis.
	d("cumsum", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 0, 1)
		if len(args) == 1 && !object.IsNil(args[0]) {
			return mustArray(self(v).CumSum(vm.ndInt(args[0])))
		}
		return &NDArray{a: self(v).CumSumFlat()}
	})

	// Indexing: a[i, 1...3, true, (0..) % 2] (see ndSlice); at is the same.
	index := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.ndSlice(self(v), args)
	}
	d("at", index)
	d("[]", index)
}
