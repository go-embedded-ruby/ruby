// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-numerics/optimize"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The Optimize module is a thin, cgo-free binding of
// github.com/go-numerics/optimize, giving Ruby scipy.optimize's root finders
// and minimisers with no native SciPy dependency.
//
//	Optimize.brentq(0.0, 2.0) { |x| x * x - 2 }   # => 1.4142135623730951
//	Optimize.minimize_scalar { |x| (x - 3) ** 2 } # => {x: 3.0, fun: 0.0, …}
//	Optimize.minimize([0.0, 0.0]) { |v| (v[0]-1)**2 + (v[1]-2)**2 }
//
// As in Integrate, the function is a BLOCK, which is how Ruby passes a callable
// and which takes `&f` for a Proc or lambda. A function of several variables
// receives one Array.
//
// Names and return shapes are scipy.optimize's: a routine answering one number
// answers a Float (scipy's default for the bracketing root finders and
// fminbound), one answering a tuple answers an Array (#curve_fit is scipy's
// (popt, pcov)), and an OptimizeResult or RootResults answers a Hash with
// symbol keys. full_output: true turns a root finder's Float into that Hash,
// as scipy's full_output does.

// rootScalarHash shapes a RootScalarResult as scipy's RootResults.
func rootScalarHash(r *optimize.RootScalarResult) object.Value {
	return symHash(
		"root", object.Float(r.Root),
		"iterations", object.Integer(r.Iterations),
		"function_calls", object.Integer(r.FunctionCalls),
		"converged", object.Bool(r.Converged),
		"flag", object.NewString(r.Flag),
		"method", object.NewString(r.Method),
	)
}

// scalarResultHash shapes a ScalarResult as scipy's OptimizeResult.
func scalarResultHash(r *optimize.ScalarResult) object.Value {
	return symHash(
		"x", object.Float(r.X),
		"fun", object.Float(r.Fun),
		"nit", object.Integer(r.Nit),
		"nfev", object.Integer(r.Nfev),
		"status", object.Integer(r.Status),
		"success", object.Bool(r.Success),
		"message", object.NewString(r.Message),
	)
}

// resultHash shapes a Result as scipy's OptimizeResult.
func resultHash(r *optimize.Result) object.Value {
	return symHash(
		"x", floatArray(r.X),
		"fun", object.Float(r.Fun),
		"jac", floatArray(r.Jac),
		"nit", object.Integer(r.Nit),
		"nfev", object.Integer(r.Nfev),
		"njev", object.Integer(r.Njev),
		"nhev", object.Integer(r.Nhev),
		"status", object.Integer(r.Status),
		"success", object.Bool(r.Success),
		"message", object.NewString(r.Message),
	)
}

// bracketOptions reads the keywords the bracketing root finders share.
func (vm *VM) bracketOptions(kw map[string]object.Value) *optimize.BracketOptions {
	return &optimize.BracketOptions{
		Xtol:    vm.optFloat(kw, "xtol"),
		Rtol:    vm.optFloat(kw, "rtol"),
		MaxIter: vm.optInt(kw, "maxiter"),
	}
}

// registerOptimize installs the Optimize module.
func (vm *VM) registerOptimize() {
	mod := newClass("Optimize", nil)
	mod.isModule = true
	vm.consts["Optimize"] = mod

	def := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// The bracketing root finders: f(a) and f(b) must straddle a root.
	// brentq(a, b, xtol:, rtol:, maxiter:, full_output:) { |x| … }
	bracketing := func(name string, solve func(f func(float64) float64, a, b float64, o *optimize.BracketOptions) (*optimize.RootScalarResult, error)) {
		def(name, func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			args, kw := ndKw(args, "xtol", "rtol", "maxiter", "full_output")
			ndArgc(args, 2, 2)
			f := vm.callFloat1(blockArg(blk), "the function")
			r, err := solve(f, vm.ndFloat(args[0]), vm.ndFloat(args[1]), vm.bracketOptions(kw))
			raiseIf(err)
			if fullOutput(kw) {
				return rootScalarHash(r)
			}
			return object.Float(r.Root)
		})
	}
	bracketing("brentq", optimize.Brentq)
	bracketing("brenth", optimize.Brenth)
	bracketing("bisect", optimize.Bisect)
	bracketing("ridder", optimize.Ridder)

	// newton(x0, fprime:, fprime2:, tol:, rtol:, maxiter:, full_output:) { |x| … }
	// With no fprime: it is the secant method, as scipy's newton is.
	def("newton", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		args, kw := ndKw(args, "fprime", "fprime2", "tol", "rtol", "maxiter", "x1", "full_output")
		ndArgc(args, 1, 1)
		o := &optimize.NewtonOptions{
			Tol:     vm.optFloat(kw, "tol"),
			Rtol:    vm.optFloat(kw, "rtol"),
			MaxIter: vm.optInt(kw, "maxiter"),
		}
		if v, ok := kw["fprime"]; ok && !object.IsNil(v) {
			o.Fprime = vm.callFloat1(procArg(v, "fprime"), "fprime")
		}
		if v, ok := kw["fprime2"]; ok && !object.IsNil(v) {
			o.Fprime2 = vm.callFloat1(procArg(v, "fprime2"), "fprime2")
		}
		if v, ok := kw["x1"]; ok && !object.IsNil(v) {
			o.X1, o.X1Set = vm.ndFloat(v), true
		}
		f := vm.callFloat1(blockArg(blk), "the function")
		r, err := optimize.Newton(f, vm.ndFloat(args[0]), o)
		raiseIf(err)
		if fullOutput(kw) {
			return rootScalarHash(r)
		}
		return object.Float(r.Root)
	})

	// minimize_scalar(method:, bracket:, bounds:, tol:, xtol:, xatol:,
	// maxiter:) { |x| … } -> an OptimizeResult Hash, as scipy's does.
	def("minimize_scalar", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		args, kw := ndKw(args, "method", "bracket", "bounds", "tol", "xtol", "xatol", "maxiter")
		ndArgc(args, 0, 0)
		o := &optimize.ScalarOptions{
			Tol:     vm.optFloat(kw, "tol"),
			Xtol:    vm.optFloat(kw, "xtol"),
			Xatol:   vm.optFloat(kw, "xatol"),
			MaxIter: vm.optInt(kw, "maxiter"),
		}
		if v, ok := kw["bracket"]; ok && !object.IsNil(v) {
			o.Bracket = floatSlice(v)
		}
		if v, ok := kw["bounds"]; ok && !object.IsNil(v) {
			b := floatSlice(v)
			if len(b) != 2 {
				raise("ArgumentError", "bounds must be a pair [lo, hi], got %d element(s)", len(b))
			}
			o.Bounds = &optimize.Bound{Lo: b[0], Hi: b[1]}
		}
		method := optimize.ScalarMethod("")
		if v, ok := kw["method"]; ok && !object.IsNil(v) {
			method = optimize.ScalarMethod(stringArg(v, "method"))
		}
		f := vm.callFloat1(blockArg(blk), "the objective")
		r, err := optimize.MinimizeScalar(f, method, o)
		raiseIf(err)
		return scalarResultHash(r)
	})

	// fminbound(x1, x2, xtol:, maxfun:) { |x| … } -> the minimiser, a Float,
	// which is what scipy returns without full_output.
	def("fminbound", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		args, kw := ndKw(args, "xtol", "maxfun", "full_output")
		ndArgc(args, 2, 2)
		xtol := vm.optFloat(kw, "xtol")
		if xtol == 0 {
			xtol = 1e-5 // scipy's default
		}
		maxfun := vm.optInt(kw, "maxfun")
		if maxfun == 0 {
			maxfun = 500 // scipy's default
		}
		f := vm.callFloat1(blockArg(blk), "the objective")
		r, err := optimize.Fminbound(f, vm.ndFloat(args[0]), vm.ndFloat(args[1]), xtol, maxfun)
		raiseIf(err)
		if fullOutput(kw) {
			return scalarResultHash(r)
		}
		return object.Float(r.X)
	})

	// minimize(x0, method:, tol:, maxiter:, maxfev:, gtol:, bounds:) { |x| … }
	// x0 is an Array and the block receives one. The method names are scipy's.
	def("minimize", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		args, kw := ndKw(args, "method", "tol", "maxiter", "maxfev", "gtol", "bounds")
		ndArgc(args, 1, 1)
		o := &optimize.Options{
			Tol:     vm.optFloat(kw, "tol"),
			MaxIter: vm.optInt(kw, "maxiter"),
			MaxFev:  vm.optInt(kw, "maxfev"),
			Gtol:    vm.optFloat(kw, "gtol"),
		}
		if v, ok := kw["bounds"]; ok && !object.IsNil(v) {
			pairs := arrayArg(v)
			o.Bounds = make([]optimize.Bound, len(pairs.Elems))
			for i, p := range pairs.Elems {
				b := floatSlice(p)
				if len(b) != 2 {
					raise("ArgumentError", "bound %d must be a pair [lo, hi], got %d element(s)", i, len(b))
				}
				o.Bounds[i] = optimize.Bound{Lo: b[0], Hi: b[1]}
			}
		}
		method := optimize.Method(optimize.BFGS)
		if v, ok := kw["method"]; ok && !object.IsNil(v) {
			method = optimize.Method(stringArg(v, "method"))
		}
		p := optimize.Problem{Func: vm.callFloatVec(blockArg(blk), "the objective")}
		r, err := optimize.Minimize(p, floatSlice(args[0]), method, o)
		raiseIf(err)
		return resultHash(r)
	})

	// approx_fprime(x, epsilon:) { |v| … } -> the forward-difference gradient.
	def("approx_fprime", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		args, kw := ndKw(args, "epsilon")
		ndArgc(args, 1, 1)
		eps := vm.optFloat(kw, "epsilon")
		if eps == 0 {
			eps = 1.4901161193847656e-08 // scipy's sqrt(eps)
		}
		f := vm.callFloatVec(blockArg(blk), "the objective")
		return floatArray(optimize.ApproxFprime(f, floatSlice(args[0]), eps))
	})

	// bracket(xa, xb) { |x| … } -> [[xa, xb, xc], [fa, fb, fc], nfev]: the
	// downhill bracket search scipy's bracket performs.
	def("bracket", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		ndArgc(args, 2, 2)
		f := vm.callFloat1(blockArg(blk), "the function")
		x, fx, nfev, err := optimize.Bracket(f, vm.ndFloat(args[0]), vm.ndFloat(args[1]))
		raiseIf(err)
		return object.NewArrayFromSlice([]object.Value{
			floatArray(x[:]), floatArray(fx[:]), object.Integer(nfev),
		})
	})

	// curve_fit(xdata, ydata, p0) { |x, p| … } -> [popt, pcov], scipy's pair.
	// The block receives one abscissa and the parameter Array.
	def("curve_fit", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		args, kw := ndKw(args, "full_output")
		ndArgc(args, 3, 3)
		b := blockArg(blk)
		model := func(x float64, p []float64) float64 {
			v := vm.callBlock(b, []object.Value{object.Float(x), floatArray(p)})
			f, ok := toFloat(v)
			if !ok {
				raise("TypeError", "the model must answer a Float, got %s", vm.ndTypeName(v))
			}
			return f
		}
		xs, ys := floatSlice(args[0]), floatSlice(args[1])
		xd := mustArray(ndFromFlat(xs))
		yd := mustArray(ndFromFlat(ys))
		r, err := optimize.CurveFit(model, xd.a, yd.a, floatSlice(args[2]), nil)
		raiseIf(err)
		if fullOutput(kw) {
			return symHash(
				"popt", floatArray(r.Popt),
				"pcov", floatArray(r.Pcov),
				"cov_ok", object.Bool(r.CovOK),
				"fvec", floatArray(r.Fvec),
				"nfev", object.Integer(r.Nfev),
				"status", object.Integer(r.Status),
				"message", object.NewString(r.Message),
			)
		}
		return object.NewArrayFromSlice([]object.Value{floatArray(r.Popt), floatArray(r.Pcov)})
	})
}
