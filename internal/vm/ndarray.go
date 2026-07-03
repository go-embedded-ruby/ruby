package vm

import (
	nd "github.com/go-ndarray/ndarray"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// NDArray binds github.com/go-ndarray/ndarray — a pure-Go (cgo-free) NumPy-style
// n-dimensional array — into Ruby, so Ruby gets numpy-style arrays with no native
// dependency. It wraps a *nd.Array as a Ruby value.

// NDArray is the Ruby wrapper around a go-ndarray Array.
type NDArray struct{ a *nd.Array }

func (n *NDArray) ToS() string     { return n.a.String() }
func (n *NDArray) Inspect() string { return n.a.String() }
func (n *NDArray) Truthy() bool    { return true }

// ndArg asserts an argument is an NDArray.
func ndArg(v object.Value) *NDArray {
	a, ok := v.(*NDArray)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into NDArray", v.Inspect())
	}
	return a
}

// mustArray raises a Ruby error when a go-ndarray operation fails (shape
// mismatch, empty reduction, …) instead of returning a Go error.
func mustArray(a *nd.Array, err error) *NDArray {
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	return &NDArray{a: a}
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

// shapeArgs reads a Ruby argument list of integers into a shape slice.
func shapeArgs(args []object.Value) []int {
	shape := make([]int, len(args))
	for i, v := range args {
		shape[i] = int(intArg(v))
	}
	return shape
}

// registerNDArray installs the NDArray class, its constructors and methods.
func (vm *VM) registerNDArray() {
	vm.cNDArray = newClass("NDArray", vm.cObject)
	vm.consts["NDArray"] = vm.cNDArray

	s := func(name string, fn NativeFn) {
		vm.cNDArray.smethods[name] = &Method{name: name, owner: vm.cNDArray, native: fn}
	}
	s("zeros", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return mustArray(nd.Zeros(shapeArgs(args)...))
	})
	s("ones", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return mustArray(nd.Ones(shapeArgs(args)...))
	})
	s("full", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		v, _ := toFloat(args[0])
		return mustArray(nd.Full(v, shapeArgs(args[1:])...))
	})
	s("arange", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		start, _ := toFloat(args[0])
		stop, _ := toFloat(args[1])
		step := 1.0
		if len(args) > 2 {
			step, _ = toFloat(args[2])
		}
		return mustArray(nd.Arange(start, stop, step))
	})
	s("from", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return mustArray(nd.FromData(floatSlice(args[0]), intSlice(args[1])...))
	})

	d := func(name string, fn NativeFn) { vm.cNDArray.define(name, fn) }
	self := func(v object.Value) *nd.Array { return v.(*NDArray).a }

	d("shape", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		sh := self(v).Shape()
		out := make([]object.Value, len(sh))
		for i, x := range sh {
			out[i] = object.IntValue(int64(x))
		}
		return &object.Array{Elems: out}
	})
	d("ndim", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Ndim()))
	})
	d("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Size()))
	})
	d("reshape", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustArray(self(v).Reshape(shapeArgs(args)...))
	})
	d("to_a", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		flat := self(v).Flatten()
		out := make([]float64, flat.Size())
		for i := range out {
			out[i] = flat.At(i)
		}
		return floatArray(out)
	})
	d("sum", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).Sum())
	})
	reduce := func(name string, f func(*nd.Array) (float64, error)) {
		d(name, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			r, err := f(self(v))
			if err != nil {
				raise("ArgumentError", "%s", err.Error())
			}
			return object.Float(r)
		})
	}
	reduce("mean", (*nd.Array).Mean)
	reduce("max", (*nd.Array).Max)
	reduce("min", (*nd.Array).Min)
	d("matmul", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustArray(self(v).MatMul(ndArg(args[0]).a))
	})
	d("dot", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return mustArray(self(v).Dot(ndArg(args[0]).a))
	})

	// Element-wise unary ufuncs (each returns a new array).
	unary := func(name string, f func(*nd.Array) *nd.Array) {
		d(name, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return &NDArray{a: f(self(v))}
		})
	}
	unary("sqrt", (*nd.Array).Sqrt)
	unary("exp", (*nd.Array).Exp)
	unary("log", (*nd.Array).Log)
	unary("sin", (*nd.Array).Sin)
	unary("cos", (*nd.Array).Cos)
	unary("abs", (*nd.Array).Abs)
	unary("neg", (*nd.Array).Neg)
	unary("transpose", (*nd.Array).Transpose)
	unary("flatten", (*nd.Array).Flatten)

	d("prod", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).Prod())
	})
	argReduce := func(name string, f func(*nd.Array) (int, error)) {
		d(name, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			i, err := f(self(v))
			if err != nil {
				raise("ArgumentError", "%s", err.Error())
			}
			return object.IntValue(int64(i))
		})
	}
	argReduce("argmax", (*nd.Array).ArgMax)
	argReduce("argmin", (*nd.Array).ArgMin)

	// Element access: a.at(i, j) / a[i, j] returns the scalar at the index.
	at := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).At(shapeArgs(args)...))
	}
	d("at", at)
	d("[]", at)
	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).String())
	})
	vm.cNDArray.define("to_s", vm.cNDArray.methods["inspect"].native)
}
