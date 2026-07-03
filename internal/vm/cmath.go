// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-ruby-cmath/cmath"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// CMath binds github.com/go-ruby-cmath/cmath — the pure-Go, MRI-4.0.5-faithful
// port of Ruby's CMath stdlib — into rbgo. The library is the complex-aware
// counterpart of Math: every function takes a real-or-complex cmath.Number and
// returns one, choosing the real result shape exactly where MRI's CMath does
// (so CMath.sqrt(4) is the Float 2.0 while CMath.sqrt(-4) is the Complex 0+2i).
//
// This file is the thin shell that maps Ruby values onto cmath.Number and back:
// a Ruby Float/Integer/Bignum/Rational argument becomes cmath.Real, a Ruby
// Complex (rbgo's *object.Complex) becomes cmath.Complex, and the result is
// returned as object.Float when the library answer is real and as
// *object.Complex when it is complex. CMath has module-functions only, so there
// is no wrapper instance type and no classOf case.

// cmathArg maps a Ruby numeric Value onto a cmath.Number. A Complex stays
// complex (cmath.Complex) — its components are always numeric (the Complex
// constructor rejects non-numerics) — so the MRI branch where Complex(1,0) is
// treated as complex is preserved; everything else coerces to a real through
// toFloat, raising TypeError on a non-numeric argument.
func cmathArg(v object.Value) cmath.Number {
	if c, ok := object.KindOK[*object.Complex](v); ok {
		return cmath.Complex(complexFloat(c.Re), complexFloat(c.Im))
	}
	f, ok := toFloat(v)
	if !ok {
		raise("TypeError", "can't convert %s into Float", v.Inspect())
	}
	return cmath.Real(f)
}

// cmathResult turns a cmath.Number back into a Ruby value: object.Float on the
// real branch, *object.Complex (Float components) on the complex branch.
func cmathResult(n cmath.Number) object.Value {
	if n.IsComplex {
		return object.Wrap(&object.Complex{Re: object.FloatValue(float64(object.Float(n.Real))), Im: object.FloatValue(float64(object.Float(n.Imag)))})
	}
	return object.FloatValue(float64(object.Float(n.Real)))
}

// registerCMath installs the CMath module and its complex-aware functions as
// module-functions (callable as CMath.fn and, like Ruby's module_function, as
// private instance methods on includers).
func (vm *VM) registerCMath() {
	mod := newClass("CMath", nil)
	mod.isModule = true
	vm.cCMath = mod
	vm.consts["CMath"] = object.Wrap(mod)

	// define installs fn as both a module-method (CMath.fn) and a private
	// instance method, mirroring Ruby's module_function.
	define := func(name string, fn func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value) {
		m := &Method{name: name, owner: mod, native: fn}
		mod.smethods[name] = m
		mod.methods[name] = m
	}

	unary := func(name string, f func(cmath.Number) cmath.Number) {
		define(name, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return cmathResult(f(cmathArg(args[0])))
		})
	}
	for name, f := range map[string]func(cmath.Number) cmath.Number{
		"sqrt": cmath.Sqrt, "cbrt": cmath.Cbrt, "exp": cmath.Exp,
		"log2": cmath.Log2, "log10": cmath.Log10,
		"sin": cmath.Sin, "cos": cmath.Cos, "tan": cmath.Tan,
		"sinh": cmath.Sinh, "cosh": cmath.Cosh, "tanh": cmath.Tanh,
		"asin": cmath.Asin, "acos": cmath.Acos, "atan": cmath.Atan,
		"asinh": cmath.Asinh, "acosh": cmath.Acosh, "atanh": cmath.Atanh,
	} {
		unary(name, f)
	}

	// atan2(y, x) is the two-argument arctangent.
	define("atan2", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return cmathResult(cmath.Atan2(cmathArg(args[0]), cmathArg(args[1])))
	})

	// log(x) is the natural log; log(x, base) is log(x) in the given base.
	define("log", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		z := cmathArg(args[0])
		if len(args) > 1 {
			return cmathResult(cmath.Log(z, cmathArg(args[1])))
		}
		return cmathResult(cmath.Log(z))
	})
}
