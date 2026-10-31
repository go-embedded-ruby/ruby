// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	nd "github.com/go-ndarray/ndarray"
	"github.com/go-numerics/integrate"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The Integrate module is a thin, cgo-free binding of
// github.com/go-numerics/integrate, giving Ruby scipy.integrate's quadrature
// with no native SciPy dependency.
//
//	Integrate.quad(0.0, 1.0) { |x| x * x }       # => [0.3333333333333333, 3.7e-15]
//	Integrate.romberg(0.0, Math::PI) { |x| Math.sin(x) }
//	Integrate.trapezoid([0.0, 1.0, 4.0], dx: 1.0)
//
// The integrand is a BLOCK, which is how Ruby passes a callable, and which
// takes `&f` for a Proc or lambda without any further surface. scipy passes it
// as the first argument instead; the limits keep their order after it.
//
// Names are scipy.integrate's own, and so are the return shapes: a routine
// returning one number answers a Float, one returning a tuple answers an Array
// -- #quad is scipy's (value, abserr) and destructures the same way -- and
// full_output: true answers the whole record as a Hash with symbol keys, as
// scipy's full_output does.
//
// An error from the module is re-raised as the Ruby error SciPy maps to:
// anything about an argument is an ArgumentError, and a routine that did not
// converge is a RuntimeError, as scipy.integrate.romberg's AccuracyWarning
// becomes under -W error.

// blockArg takes the integrand or objective: a block, which is also how a Proc
// or lambda arrives (`&f`). It raises MRI's "no block given (yield)" when there
// is none, so a forgotten block is a Ruby error and not a nil call.
func blockArg(blk *Proc) *Proc {
	if blk == nil {
		raise("LocalJumpError", "no block given (yield)")
	}
	return blk
}

// callFloat1 turns a Ruby block into the Go callback the numeric modules take.
// A block answering something that is not a real number is a TypeError naming
// the block, which is more use than a NaN appearing later in the quadrature.
func (vm *VM) callFloat1(blk *Proc, what string) func(float64) float64 {
	return func(x float64) float64 {
		v := vm.callBlock(blk, []object.Value{object.Float(x)})
		f, ok := toFloat(v)
		if !ok {
			raise("TypeError", "%s must answer a Float, got %s", what, vm.ndTypeName(v))
		}
		return f
	}
}

// callFloat2 and callFloat3 are the two- and three-variable integrands of
// #dblquad and #tplquad, whose arguments arrive in scipy's order.
func (vm *VM) callFloat2(blk *Proc, what string) func(y, x float64) float64 {
	return func(y, x float64) float64 {
		v := vm.callBlock(blk, []object.Value{object.Float(y), object.Float(x)})
		f, ok := toFloat(v)
		if !ok {
			raise("TypeError", "%s must answer a Float, got %s", what, vm.ndTypeName(v))
		}
		return f
	}
}

// callFloatVec is the objective of a function of several variables: the block
// receives one Array.
func (vm *VM) callFloatVec(blk *Proc, what string) func([]float64) float64 {
	return func(x []float64) float64 {
		v := vm.callBlock(blk, []object.Value{floatArray(x)})
		f, ok := toFloat(v)
		if !ok {
			raise("TypeError", "%s must answer a Float, got %s", what, vm.ndTypeName(v))
		}
		return f
	}
}

// raiseIf raises when a numeric routine fails, and is the SINGLE place either
// module checks an error. Written at each of the twenty-one call sites instead,
// the `if err != nil` would be twenty-one branches that only a few inputs can
// reach; here it is one, which the refusals that ARE reachable cover.
func raiseIf(err error) {
	if err == nil {
		return
	}
	raiseNumerics(err)
}

// raiseNumerics re-raises an error from integrate or optimize. "did not
// converge" is a RuntimeError, as SciPy's AccuracyWarning and
// RuntimeError are; everything else describes an argument, so ArgumentError.
func raiseNumerics(err error) {
	msg := err.Error()
	if containsAny(msg, "converge", "DivMax", "maximum number", "maxiter", "MaxIter") {
		raise("RuntimeError", "%s", msg)
	}
	raise("ArgumentError", "%s", msg)
}

// containsAny reports whether s holds any of subs.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// symHash builds a Ruby Hash with Symbol keys, the shape scipy's OptimizeResult
// and full_output records take here. Pairs are given in order.
func symHash(pairs ...any) object.Value {
	h := object.NewHash()
	for i := 0; i+1 < len(pairs); i += 2 {
		h.Set(object.Symbol(pairs[i].(string)), pairs[i+1].(object.Value))
	}
	return h
}

// fullOutput reads the full_output: keyword (scipy's), default false.
func fullOutput(kw map[string]object.Value) bool {
	v, ok := kw["full_output"]
	return ok && v.Truthy()
}

// optFloat reads an optional real keyword, falling back to the module's own
// default when absent (zero, which each option struct documents).
func (vm *VM) optFloat(kw map[string]object.Value, name string) float64 {
	if v, ok := kw[name]; ok && !object.IsNil(v) {
		return vm.ndFloat(v)
	}
	return 0
}

// optInt reads an optional integer keyword; 0 means "the module's default".
func (vm *VM) optInt(kw map[string]object.Value, name string) int {
	if v, ok := kw[name]; ok && !object.IsNil(v) {
		return vm.ndInt(v)
	}
	return 0
}

// quadOptions reads #quad's keywords into the module's option struct.
func (vm *VM) quadOptions(kw map[string]object.Value) *integrate.QuadOptions {
	o := &integrate.QuadOptions{
		EpsAbs: vm.optFloat(kw, "epsabs"),
		EpsRel: vm.optFloat(kw, "epsrel"),
		Limit:  vm.optInt(kw, "limit"),
	}
	if v, ok := kw["points"]; ok && !object.IsNil(v) {
		o.Points = floatSlice(v)
	}
	return o
}

// quadAnswer shapes a QuadResult: scipy's (value, abserr) by default, the whole
// record with full_output: true. QUADPACK's per-cycle ier is left out: it is
// only non-nil for the QAWF weight, and weight: is not bound, so the key would
// always be empty and the code filling it unreachable.
func quadAnswer(r integrate.QuadResult, full bool) object.Value {
	if !full {
		return floatArray([]float64{r.Value, r.AbsErr})
	}
	return symHash(
		"value", object.Float(r.Value),
		"abserr", object.Float(r.AbsErr),
		"neval", object.Integer(r.NEval),
		"nintervals", object.Integer(r.NIntervals),
	)
}

// sampled reads the arguments of the sampled rules (#trapezoid, #simpson,
// #romb, #cumulative_trapezoid): the samples, and either x: or dx:, exactly as
// scipy takes them. dx defaults to 1.0, as scipy's does.
func (vm *VM) sampled(args []object.Value, kw map[string]object.Value) (y, x []float64, dx float64) {
	y = floatSlice(args[0])
	dx = 1.0
	if v, ok := kw["dx"]; ok && !object.IsNil(v) {
		dx = vm.ndFloat(v)
	}
	if v, ok := kw["x"]; ok && !object.IsNil(v) {
		x = floatSlice(v)
	}
	return y, x, dx
}

// registerIntegrate installs the Integrate module.
func (vm *VM) registerIntegrate() {
	mod := newClass("Integrate", nil)
	mod.isModule = true
	vm.consts["Integrate"] = mod

	def := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// quad(a, b, epsabs:, epsrel:, limit:, points:, full_output:) { |x| … }
	def("quad", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		args, kw := ndKw(args, "epsabs", "epsrel", "limit", "points", "full_output")
		ndArgc(args, 2, 2)
		f := vm.callFloat1(blockArg(blk), "the integrand")
		r, err := integrate.Quad(f, vm.ndFloat(args[0]), vm.ndFloat(args[1]), vm.quadOptions(kw))
		raiseIf(err)
		return quadAnswer(r, fullOutput(kw))
	})

	// fixed_quad(a, b, n) { |x| … }: the n-point Gauss-Legendre rule.
	def("fixed_quad", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		ndArgc(args, 3, 3)
		f := vm.callFloat1(blockArg(blk), "the integrand")
		v, err := integrate.FixedQuad(f, vm.ndFloat(args[0]), vm.ndFloat(args[1]), vm.ndInt(args[2]))
		raiseIf(err)
		return object.Float(v)
	})

	// romberg(a, b, tol:, rtol:, divmax:) { |x| … }
	def("romberg", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		args, kw := ndKw(args, "tol", "rtol", "divmax")
		ndArgc(args, 2, 2)
		f := vm.callFloat1(blockArg(blk), "the integrand")
		v, err := integrate.Romberg(f, vm.ndFloat(args[0]), vm.ndFloat(args[1]), &integrate.RombergOptions{
			Tol:    vm.optFloat(kw, "tol"),
			RTol:   vm.optFloat(kw, "rtol"),
			DivMax: vm.optInt(kw, "divmax"),
		})
		raiseIf(err)
		return object.Float(v)
	})

	// gauss_legendre(n) -> [nodes, weights]
	def("gauss_legendre", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 1)
		n := vm.ndInt(args[0])
		if n <= 0 {
			raise("ArgumentError", "n must be positive")
		}
		x, w := integrate.GaussLegendre(n)
		return object.NewArrayFromSlice([]object.Value{floatArray(x), floatArray(w)})
	})

	// The sampled rules, over an Array of samples with x: or dx:.
	sampledRule := func(name string, f func(y, x []float64, dx float64) (float64, error)) {
		def(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			args, kw := ndKw(args, "x", "dx")
			ndArgc(args, 1, 1)
			y, x, dx := vm.sampled(args, kw)
			v, err := f(y, x, dx)
			raiseIf(err)
			return object.Float(v)
		})
	}
	sampledRule("trapezoid", integrate.Trapezoid)
	sampledRule("simpson", integrate.Simpson)
	def("romb", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		args, kw := ndKw(args, "dx")
		ndArgc(args, 1, 1)
		y, _, dx := vm.sampled(args, kw)
		v, err := integrate.Romb(y, dx)
		raiseIf(err)
		return object.Float(v)
	})

	// cumulative_trapezoid(y, x:, dx:, initial:) -> Array
	def("cumulative_trapezoid", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		args, kw := ndKw(args, "x", "dx", "initial")
		ndArgc(args, 1, 1)
		y, x, dx := vm.sampled(args, kw)
		init := false
		if v, ok := kw["initial"]; ok {
			init = v.Truthy()
		}
		out, err := integrate.CumulativeTrapezoid(y, x, dx, init)
		raiseIf(err)
		return floatArray(out)
	})

	// dblquad(a, b, gfun, hfun, …) { |y, x| … }: the inner limits may each be a
	// number or a block-like callable; a number becomes a constant limit, as
	// scipy allows.
	limitFn := func(vm *VM, v object.Value, what string) func(float64) float64 {
		if p, ok := v.(*Proc); ok {
			return vm.callFloat1(p, what)
		}
		return integrate.Const(vm.ndFloat(v))
	}
	def("dblquad", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		args, kw := ndKw(args, "epsabs", "epsrel", "limit", "points", "full_output")
		ndArgc(args, 4, 4)
		f := vm.callFloat2(blockArg(blk), "the integrand")
		r, err := integrate.DblQuad(f, vm.ndFloat(args[0]), vm.ndFloat(args[1]),
			limitFn(vm, args[2], "the lower inner limit"),
			limitFn(vm, args[3], "the upper inner limit"),
			vm.quadOptions(kw))
		raiseIf(err)
		return quadAnswer(r, fullOutput(kw))
	})

	// nquad(ranges, full_output:) { |x| … }: ranges is an Array of [lo, hi]
	// pairs, innermost first, as scipy orders them.
	def("nquad", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		args, kw := ndKw(args, "full_output")
		ndArgc(args, 1, 1)
		f := vm.callFloatVec(blockArg(blk), "the integrand")
		pairs := arrayArg(args[0])
		ranges := make([]integrate.Range, len(pairs.Elems))
		for i, p := range pairs.Elems {
			lohi := floatSlice(p)
			if len(lohi) != 2 {
				raise("ArgumentError", "range %d must be a pair [lo, hi], got %d element(s)", i, len(lohi))
			}
			ranges[i] = integrate.ConstRange(lohi[0], lohi[1])
		}
		r, err := integrate.NQuad(f, ranges, nil)
		raiseIf(err)
		return quadAnswer(r, fullOutput(kw))
	})
}

// procArg takes a callable keyword argument (a derivative, a limit): a Proc or
// a lambda, which is what `method(:f)` and `->(x){…}` both produce.
func procArg(v object.Value, name string) *Proc {
	p, ok := v.(*Proc)
	if !ok {
		raise("TypeError", "%s: must be a Proc or lambda, got %s", name, v.Inspect())
	}
	return p
}

// stringArg takes a String keyword (a method name).
func stringArg(v object.Value, name string) string {
	s, ok := v.(*object.String)
	if !ok {
		if sym, ok := v.(object.Symbol); ok {
			return string(sym)
		}
		raise("TypeError", "%s: must be a String or Symbol, got %s", name, v.Inspect())
	}
	return s.Str()
}

// ndFromFlat builds a 1-D go-ndarray array from a slice.
func ndFromFlat(xs []float64) (*nd.Array, error) {
	return nd.FromData(xs, len(xs))
}
