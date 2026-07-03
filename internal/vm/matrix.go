// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"math"
	"math/big"
	"strings"

	libmatrix "github.com/go-ruby-matrix/matrix"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Matrix and Vector bind github.com/go-ruby-matrix/matrix — the pure-Go,
// MRI-4.0.5-faithful port of Ruby's `matrix` stdlib — into rbgo. The library
// owns all of the linear algebra, the exact Integer/Rational/Float numeric
// tower, the MRI promotion rules (Matrix#determinant of an Integer matrix stays
// an Integer, Matrix#inverse yields exact Rationals like `(-2/1)`) and the
// MRI-faithful `to_s`/`inspect` rendering. This file is only the thin shell that
// maps Ruby numeric values onto the library's Num tower and back, and exposes
// the class/method surface MRI's `require "matrix"` provides.
//
// Entries flow Ruby Integer/Bignum -> Num(Integer), Ruby Rational -> Num(Rational),
// Ruby Float -> Num(Float); results come back by reading Num.String() (the
// library renders each kind exactly as Ruby would) and re-parsing it into the
// matching Ruby object, so an exact Rational result is an `object.Rational`,
// never silently degraded to Float.

// Matrix is the Ruby wrapper around a go-ruby-matrix Matrix.
type Matrix struct{ m *libmatrix.Matrix }

func (m *Matrix) ToS() string     { return m.m.ToS() }
func (m *Matrix) Inspect() string { return m.m.Inspect() }
func (m *Matrix) Truthy() bool    { return true }

// Vector is the Ruby wrapper around a go-ruby-matrix Vector.
type Vector struct{ v *libmatrix.Vector }

func (v *Vector) ToS() string     { return v.v.ToS() }
func (v *Vector) Inspect() string { return v.v.Inspect() }
func (v *Vector) Truthy() bool    { return true }

// numFromValue maps a Ruby numeric onto a library Num, keeping its kind so the
// library's exact arithmetic is preserved. Integer/Bignum -> Integer Num,
// Rational -> Rational Num, Float -> Float Num. Anything else raises TypeError,
// matching MRI, which only stores Numerics in a Matrix/Vector.
func numFromValue(v object.Value) libmatrix.Num {
	{
		__sw94 := v
		switch {
		case object.IsInt(__sw94):
			x := object.AsInteger(__sw94)
			_ = x
			return libmatrix.NewInt(int64(x))
		case object.IsKind[*object.Bignum](__sw94):
			x := object.Kind[*object.Bignum](__sw94)
			_ = x
			return libmatrix.NewBigInt(x.I)
		case object.IsKind[*object.Rational](__sw94):
			x := object.Kind[*object.Rational](__sw94)
			_ = x
			return libmatrix.NewBigRat(x.R)
		case object.IsFloat(__sw94):
			x := object.AsFloatV(__sw94)
			_ = x
			return libmatrix.NewFloat(float64(x))
		}
	}
	raise("TypeError", "not a numeric value")
	panic("unreachable")
}

// numToValue maps a library Num back to a Ruby numeric by reading its rendered
// form. The library prints an Integer as bare digits, a Rational as "(n/d)" and
// a Float through Ruby's Float#inspect ("0.6", "5.0", "1.0e+20", "Infinity",
// "NaN"), so the leading "(" and the presence of a "." / "e" / Inf/NaN word
// tells the three kinds apart unambiguously.
func numToValue(n libmatrix.Num) object.Value {
	s := n.String()
	if strings.HasPrefix(s, "(") {
		// Rational "(n/d)".
		body := strings.TrimSuffix(strings.TrimPrefix(s, "("), ")")
		r, _ := new(big.Rat).SetString(body)
		return &object.Rational{R: r}
	}
	if isFloatStr(s) {
		switch {
		case s == "Infinity":
			return object.Float(math.Inf(1))
		case s == "-Infinity":
			return object.Float(math.Inf(-1))
		case s == "NaN":
			return object.Float(math.NaN())
		}
		f, _, _ := big.ParseFloat(s, 10, 53, big.ToNearestEven)
		v, _ := f.Float64()
		return object.Float(v)
	}
	z, _ := new(big.Int).SetString(s, 10)
	return object.NormInt(z)
}

// isFloatStr reports whether a non-Rational Num rendering is a Float (it carries
// a decimal point, a scientific exponent, or is an Inf/NaN word) rather than an
// Integer's bare digits.
func isFloatStr(s string) bool {
	if s == "NaN" || s == "Infinity" || s == "-Infinity" {
		return true
	}
	return strings.ContainsAny(s, ".eE")
}

// numsToArray materialises a row of Nums into a Ruby Array of Ruby numerics.
func numsToArray(ns []libmatrix.Num) object.Value {
	out := make([]object.Value, len(ns))
	for i, n := range ns {
		out[i] = numToValue(n)
	}
	return object.NewArrayFromSlice(out)
}

// rowsFromValue maps the Ruby [[..],[..]] (Array of Arrays) argument of
// Matrix.new / rows / [] into the library's [][]any of Nums, raising TypeError
// for a non-Array element exactly as the library would reject the entry.
func rowsFromValue(v object.Value) [][]any {
	arr, ok := object.KindOK[*object.Array](v)
	if !ok {
		raise("TypeError", "expected an Array of rows")
	}
	rows := make([][]any, len(arr.Elems))
	for i, rv := range arr.Elems {
		row, ok := object.KindOK[*object.Array](rv)
		if !ok {
			raise("TypeError", "expected an Array for each row")
		}
		cells := make([]any, len(row.Elems))
		for j, cv := range row.Elems {
			cells[j] = numFromValue(cv)
		}
		rows[i] = cells
	}
	return rows
}

// cellsFromValue maps a flat Ruby Array argument (row_vector / column_vector /
// Vector elements) into a []any of Nums.
func cellsFromValue(v object.Value) []any {
	arr, ok := object.KindOK[*object.Array](v)
	if !ok {
		raise("TypeError", "expected an Array")
	}
	cells := make([]any, len(arr.Elems))
	for i, cv := range arr.Elems {
		cells[i] = numFromValue(cv)
	}
	return cells
}

// argsToCells maps a positional argument list (Matrix.diagonal / Vector[…]) into
// a []any of Nums.
func argsToCells(args []object.Value) []any {
	cells := make([]any, len(args))
	for i, a := range args {
		cells[i] = numFromValue(a)
	}
	return cells
}

// matrixArg / vectorArg assert an argument's wrapper type, raising TypeError
// otherwise (MRI raises ErrOperationNotDefined / TypeError for a non-Matrix
// operand; a TypeError is the closest faithful surface for the binding seam).
func matrixArg(v object.Value) *Matrix {
	m, ok := object.KindOK[*Matrix](v)
	if !ok {
		raise("TypeError", "value must be a Matrix")
	}
	return m
}

func vectorArg(v object.Value) *Vector {
	vec, ok := object.KindOK[*Vector](v)
	if !ok {
		raise("TypeError", "value must be a Vector")
	}
	return vec
}

// raiseMatrixErr re-raises a library error as the matching MRI exception under
// ExceptionForMatrix, reproducing MRI's message verbatim ("Dimension mismatch",
// "Not Regular Matrix"). It never returns when err is non-nil.
func raiseMatrixErr(err error) {
	if err == nil {
		return
	}
	switch {
	case errors.Is(err, libmatrix.ErrDimensionMismatch):
		raise("ExceptionForMatrix::ErrDimensionMismatch", "%s", err.Error())
	case errors.Is(err, libmatrix.ErrNotRegular):
		raise("ExceptionForMatrix::ErrNotRegular", "%s", err.Error())
	case errors.Is(err, libmatrix.ErrOperationNotDefined):
		raise("ExceptionForMatrix::ErrOperationNotDefined", "%s", err.Error())
	case errors.Is(err, libmatrix.ErrArgument):
		// MRI raises a plain ArgumentError (e.g. Vector#cross_product on a
		// non-3-D vector); the library carries MRI's verbatim message.
		raise("ArgumentError", "%s", err.Error())
	}
	raise("RuntimeError", "%s", err.Error())
}

// matOK / vecOK wrap a (result, error) library call: it re-raises any error as
// the matching MRI exception, then returns the wrapped Ruby value.
func matOK(m *libmatrix.Matrix, err error) object.Value {
	raiseMatrixErr(err)
	return &Matrix{m: m}
}

func vecOK(v *libmatrix.Vector, err error) object.Value {
	raiseMatrixErr(err)
	return &Vector{v: v}
}

// matrixOp implements the Matrix/Vector operator fast path reached from binary()
// (the VM compiles + - * / == != to the arithmetic opcodes that route here; **
// and -@ stay on the method/negate paths). It mirrors MRI's overloads: Matrix *
// Matrix / Matrix * Vector / Matrix * scalar, Matrix / Matrix / Matrix / scalar,
// Vector * scalar, and structural == across the same wrapper type.
func matrixOp(op bytecode.Op, a object.Value, b object.Value) object.Value {
	{
		__sw95 := a
		switch {
		case object.IsKind[*Matrix](__sw95):
			lhs := object.Kind[*Matrix](__sw95)
			_ = lhs
			switch op {
			case bytecode.OpAdd:
				return matOK(lhs.m.Add(matrixArg(b).m))
			case bytecode.OpSub:
				return matOK(lhs.m.Sub(matrixArg(b).m))
			case bytecode.OpMul:
				{
					__sw96 := b
					switch {
					case object.IsKind[*Matrix](__sw96):
						o := object.Kind[*Matrix](__sw96)
						_ = o
						return matOK(lhs.m.Mul(o.m))
					case object.IsKind[*Vector](__sw96):
						o := object.Kind[*Vector](__sw96)
						_ = o
						return vecOK(lhs.m.MulVector(o.v))
					default:
						o := __sw96
						_ = o
						return matOK(lhs.m.MulScalar(numFromValue(b)))
					}
				}
			case bytecode.OpDiv:
				if o, ok := object.KindOK[*Matrix](b); ok {
					return matOK(lhs.m.Div(o.m))
				}
				return matOK(lhs.m.DivScalar(numFromValue(b)))
			}
		case object.IsKind[*Vector](__sw95):
			lhs := object.Kind[*Vector](__sw95)
			_ = lhs
			switch op {
			case bytecode.OpAdd:
				return vecOK(lhs.v.Add(vectorArg(b).v))
			case bytecode.OpSub:
				return vecOK(lhs.v.Sub(vectorArg(b).v))
			case bytecode.OpMul:
				return vecOK(lhs.v.Mul(numFromValue(b)))
			}
		}
	}
	return raise("NoMethodError", "undefined method '%s'", op)
}

// eqMatrix / eqVector implement structural == against any value: only a
// same-kind wrapper can be equal (a Matrix is never == a Vector or a scalar).
func eqMatrix(a *Matrix, b object.Value) bool {
	o, ok := object.KindOK[*Matrix](b)
	return ok && a.m.Eql(o.m)
}

func eqVector(a *Vector, b object.Value) bool {
	o, ok := object.KindOK[*Vector](b)
	return ok && a.v.Eql(o.v)
}

// registerMatrix installs Matrix, Vector and the ExceptionForMatrix module with
// its nested error classes (require "matrix"). It runs eagerly at boot; the
// error classes need StandardError in place.
func (vm *VM) registerMatrix() {
	vm.registerMatrixErrors()
	vm.registerMatrixClass()
	vm.registerVectorClass()
}

// registerMatrixErrors installs the ExceptionForMatrix module and its three
// error classes. MRI names them ExceptionForMatrix::ErrDimensionMismatch etc.
// and mixes ExceptionForMatrix into Matrix, so `Matrix::ErrDimensionMismatch`
// resolves to the very same class. The binding reproduces that by nesting each
// class under BOTH modules' const tables (and the qualified top-level names so a
// re-raised library error's exceptionObject lookup finds the same class), the
// CSV::Row / URI:: pattern.
func (vm *VM) registerMatrixErrors() {
	std := object.Kind[*RClass](vm.consts["StandardError"])

	efm := newClass("ExceptionForMatrix", vm.cObject)
	efm.isModule = true
	vm.consts["ExceptionForMatrix"] = efm
	vm.cExceptionForMatrix = efm

	mk := func(short string) *RClass {
		full := "ExceptionForMatrix::" + short
		cls := newClass(full, std)
		efm.consts[short] = cls
		vm.consts[full] = cls
		return cls
	}
	vm.cErrDimensionMismatch = mk("ErrDimensionMismatch")
	vm.cErrNotRegular = mk("ErrNotRegular")
	vm.cErrOperationNotDefined = mk("ErrOperationNotDefined")
}

// registerMatrixClass installs the Matrix class, its constructors and instance
// methods. ExceptionForMatrix is mixed in so Matrix::ErrDimensionMismatch (etc.)
// resolves through the include chain, exactly as MRI does.
func (vm *VM) registerMatrixClass() {
	cls := newClass("Matrix", vm.cObject)
	vm.cMatrix = cls
	vm.consts["Matrix"] = cls
	cls.includes = append(cls.includes, vm.cExceptionForMatrix)

	sm := func(name string, fn NativeFn) { cls.smethods[name] = &Method{name: name, owner: cls, native: fn} }

	// Matrix.new(rows) / Matrix.rows(rows) / Matrix[*rows] / Matrix.columns(cols).
	rowsFn := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(libmatrix.Rows(rowsFromValue(args[0])))
	}
	sm("new", rowsFn)
	sm("rows", rowsFn)
	sm("[]", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		rows := make([][]any, len(args))
		for i, a := range args {
			row, ok := object.KindOK[*object.Array](a)
			if !ok {
				raise("TypeError", "expected an Array for each row")
			}
			cells := make([]any, len(row.Elems))
			for j, cv := range row.Elems {
				cells[j] = numFromValue(cv)
			}
			rows[i] = cells
		}
		return matOK(libmatrix.Rows(rows))
	})
	sm("columns", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(libmatrix.Columns(rowsFromValue(args[0])))
	})

	sm("build", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (build)")
		}
		r := int(object.AsInteger(args[0]))
		c := r
		if len(args) > 1 {
			c = int(object.AsInteger(args[1]))
		}
		return matOK(libmatrix.Build(r, c, func(i, j int) any {
			res := vm.callBlock(blk, []object.Value{object.IntValue(int64(i)), object.IntValue(int64(j))})
			return numFromValue(res)
		}))
	})

	identFn := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return &Matrix{m: libmatrix.Identity(int(object.AsInteger(args[0])))}
	}
	sm("identity", identFn)
	sm("I", identFn)
	sm("unit", identFn)

	sm("zero", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		r := int(object.AsInteger(args[0]))
		c := r
		if len(args) > 1 {
			c = int(object.AsInteger(args[1]))
		}
		return &Matrix{m: libmatrix.Zero(r, c)}
	})
	sm("diagonal", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(libmatrix.Diagonal(argsToCells(args)...))
	})
	sm("scalar", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(libmatrix.Scalar(int(object.AsInteger(args[0])), numFromValue(args[1])))
	})
	sm("row_vector", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(libmatrix.RowVector(cellsFromValue(args[0])))
	})
	sm("column_vector", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(libmatrix.ColumnVector(cellsFromValue(args[0])))
	})
	sm("hstack", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(libmatrix.HStack(matrixSlice(args)...))
	})
	sm("vstack", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(libmatrix.VStack(matrixSlice(args)...))
	})

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *Matrix { return object.Kind[*Matrix](v) }

	d("row_count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).m.RowCount()))
	})
	d("column_count", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).m.ColumnCount()))
	})
	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		n, ok := self(v).m.At(int(object.AsInteger(args[0])), int(object.AsInteger(args[1])))
		if !ok {
			return object.NilV
		}
		return numToValue(n)
	})
	d("row", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		vec, ok := self(v).m.Row(int(object.AsInteger(args[0])))
		if !ok {
			return object.NilV
		}
		return &Vector{v: vec}
	})
	d("column", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		vec, ok := self(v).m.Column(int(object.AsInteger(args[0])))
		if !ok {
			return object.NilV
		}
		return &Vector{v: vec}
	})
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		self(v).m.Each(func(n libmatrix.Num) { vm.callBlock(blk, []object.Value{numToValue(n)}) })
		return v
	})
	d("each_with_index", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each_with_index)")
		}
		self(v).m.EachWithIndex(func(n libmatrix.Num, i, j int) {
			vm.callBlock(blk, []object.Value{numToValue(n), object.IntValue(int64(i)), object.IntValue(int64(j))})
		})
		return v
	})
	d("minor", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(self(v).m.Minor(
			int(object.AsInteger(args[0])), int(object.AsInteger(args[1])),
			int(object.AsInteger(args[2])), int(object.AsInteger(args[3]))))
	})
	d("first_minor", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(self(v).m.FirstMinor(int(object.AsInteger(args[0])), int(object.AsInteger(args[1]))))
	})
	d("to_a", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		rows := self(v).m.ToA()
		out := make([]object.Value, len(rows))
		for i, r := range rows {
			out[i] = numsToArray(r)
		}
		return object.NewArrayFromSlice(out)
	})

	d("+", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(self(v).m.Add(matrixArg(args[0]).m))
	})
	d("-", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(self(v).m.Sub(matrixArg(args[0]).m))
	})
	d("*", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		{
			__sw97 := args[0]
			switch {
			case object.IsKind[*Matrix](__sw97):
				o := object.Kind[*Matrix](__sw97)
				_ = o
				return matOK(self(v).m.Mul(o.m))
			case object.IsKind[*Vector](__sw97):
				o := object.Kind[*Vector](__sw97)
				_ = o
				return vecOK(self(v).m.MulVector(o.v))
			default:
				o := __sw97
				_ = o
				return matOK(self(v).m.MulScalar(numFromValue(args[0])))
			}
		}
	})
	d("/", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if o, ok := object.KindOK[*Matrix](args[0]); ok {
			return matOK(self(v).m.Div(o.m))
		}
		return matOK(self(v).m.DivScalar(numFromValue(args[0])))
	})
	d("**", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return matOK(self(v).m.Pow(int(object.AsInteger(args[0]))))
	})
	d("-@", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &Matrix{m: self(v).m.Neg()}
	})

	transposeFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &Matrix{m: self(v).m.Transpose()}
	}
	d("transpose", transposeFn)
	d("t", transposeFn)

	traceFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		n, err := self(v).m.Trace()
		raiseMatrixErr(err)
		return numToValue(n)
	}
	d("trace", traceFn)
	d("tr", traceFn)

	detFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		n, err := self(v).m.Determinant()
		raiseMatrixErr(err)
		return numToValue(n)
	}
	d("determinant", detFn)
	d("det", detFn)

	invFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return matOK(self(v).m.Inverse())
	}
	d("inverse", invFn)
	d("inv", invFn)

	d("rank", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).m.Rank()))
	})
	d("round", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		n := 0
		if len(args) > 0 {
			n = int(object.AsInteger(args[0]))
		}
		return &Matrix{m: self(v).m.RoundEntries(n)}
	})

	d("square?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).m.Square())
	})
	d("zero?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).m.IsZero())
	})
	d("diagonal?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).m.IsDiagonal())
	})
	d("symmetric?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).m.Symmetric())
	})
	d("lower_triangular?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).m.LowerTriangular())
	})
	d("upper_triangular?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).m.UpperTriangular())
	})
	d("singular?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		b, err := self(v).m.Singular()
		raiseMatrixErr(err)
		return object.Bool(b)
	})
	d("regular?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		b, err := self(v).m.Regular()
		raiseMatrixErr(err)
		return object.Bool(b)
	})
	d("orthogonal?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		b, err := self(v).m.Orthogonal()
		raiseMatrixErr(err)
		return object.Bool(b)
	})

	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := object.KindOK[*Matrix](args[0])
		if !ok {
			return object.False
		}
		return object.Bool(self(v).m.Eql(o.m))
	})
	d("eql?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := object.KindOK[*Matrix](args[0])
		if !ok {
			return object.False
		}
		return object.Bool(self(v).m.Eql(o.m))
	})
	d("hash", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NormInt(new(big.Int).SetUint64(self(v).m.Hash()))
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).m.ToS())
	})
	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).m.Inspect())
	})
}

// matrixSlice maps a positional Matrix argument list (hstack / vstack) into a
// []*libmatrix.Matrix, raising TypeError for a non-Matrix operand.
func matrixSlice(args []object.Value) []*libmatrix.Matrix {
	out := make([]*libmatrix.Matrix, len(args))
	for i, a := range args {
		out[i] = matrixArg(a).m
	}
	return out
}

// registerVectorClass installs the Vector class, its constructors and instance
// methods.
func (vm *VM) registerVectorClass() {
	cls := newClass("Vector", vm.cObject)
	vm.cVector = cls
	vm.consts["Vector"] = cls

	sm := func(name string, fn NativeFn) { cls.smethods[name] = &Method{name: name, owner: cls, native: fn} }

	// Vector[*elems] and Vector.elements(array).
	sm("[]", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vecOK(libmatrix.NewVector(argsToCells(args)))
	})
	sm("elements", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vecOK(libmatrix.NewVector(cellsFromValue(args[0])))
	})
	sm("independent?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vs := make([]*libmatrix.Vector, len(args))
		for i, a := range args {
			vs[i] = vectorArg(a).v
		}
		ok, err := libmatrix.Independent(vs...)
		raiseMatrixErr(err)
		return object.Bool(ok)
	})

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *Vector { return object.Kind[*Vector](v) }

	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		n, ok := self(v).v.At(int(object.AsInteger(args[0])))
		if !ok {
			return object.NilV
		}
		return numToValue(n)
	})
	d("size", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).v.Size()))
	})
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (each)")
		}
		self(v).v.Each(func(n libmatrix.Num) { vm.callBlock(blk, []object.Value{numToValue(n)}) })
		return v
	})
	d("map", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (map)")
		}
		return vecOK(self(v).v.Map(func(n libmatrix.Num) any {
			return numFromValue(vm.callBlock(blk, []object.Value{numToValue(n)}))
		}))
	})
	d("to_a", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return numsToArray(self(v).v.Elements())
	})

	d("+", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vecOK(self(v).v.Add(vectorArg(args[0]).v))
	})
	d("-", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vecOK(self(v).v.Sub(vectorArg(args[0]).v))
	})
	d("*", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vecOK(self(v).v.Mul(numFromValue(args[0])))
	})

	innerFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		n, err := self(v).v.InnerProduct(vectorArg(args[0]).v)
		raiseMatrixErr(err)
		return numToValue(n)
	}
	d("inner_product", innerFn)
	d("dot", innerFn)

	crossFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vecOK(self(v).v.CrossProduct(vectorArg(args[0]).v))
	}
	d("cross_product", crossFn)
	d("cross", crossFn)

	magFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return numToValue(self(v).v.Magnitude())
	}
	d("magnitude", magFn)
	d("r", magFn)
	d("norm", magFn)

	d("normalize", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vecOK(self(v).v.Normalize())
	})

	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := object.KindOK[*Vector](args[0])
		if !ok {
			return object.False
		}
		return object.Bool(self(v).v.Eql(o.v))
	})
	d("eql?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o, ok := object.KindOK[*Vector](args[0])
		if !ok {
			return object.False
		}
		return object.Bool(self(v).v.Eql(o.v))
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).v.ToS())
	})
	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).v.Inspect())
	})
}
