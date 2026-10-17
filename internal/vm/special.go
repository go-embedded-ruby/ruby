// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	nd "github.com/go-ndarray/ndarray"
	"github.com/go-numerics/special"
	snd "github.com/go-numerics/special/nd"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The Special module is a thin, cgo-free binding of github.com/go-numerics/special,
// giving Ruby scipy.special's function set with no native SciPy dependency.
//
// Every name is scipy.special's own, taken from the correspondence the Go module
// measures itself against (bench/scipy_server.py and testdata/gen/fam_*.py pair
// each Go function with the scipy function it is compared to in ULPs) rather than
// transliterated from the Go name -- which is why Log1pExp is #softplus and
// LogGamma is #gammaln. Two functions have no scipy counterpart and are named
// after the Go one: #log_factorial and #loggamma_sign.
//
// Each binding is a ufunc, as scipy's is: an argument may be a Float, an Integer
// or an NDArray, and the answer follows. The scalar path calls the float64
// function directly; as soon as one argument is an NDArray every argument goes
// through go-ndarray, a Float becoming a 0-d array, which broadcasts against any
// shape. So Special.xlogy(2, arr) answers element-wise and Special.gamma(5.0)
// answers a Float, from one Ruby name.
//
// Arguments are checked before use: a wrong count raises ArgumentError and a
// wrong type TypeError, through the same helpers NDArray uses (ndArgc, ndFloat,
// ndInt), and a go-ndarray error is re-raised as the Ruby error NumPy's maps to.
// No Go runtime fault reaches the caller.

// ndOperand reads one ufunc argument as a go-ndarray array: an NDArray as
// itself, a real number as go-ndarray's 0-d scalar, which broadcasts.
func (vm *VM) ndOperand(v object.Value) *nd.Array {
	if a, ok := v.(*NDArray); ok {
		return a.a
	}
	return nd.Scalar(vm.ndFloat(v))
}

// anyND reports whether some argument is an NDArray, which is what selects the
// element-wise path for the whole call.
func anyND(args []object.Value) bool {
	for _, a := range args {
		if _, ok := a.(*NDArray); ok {
			return true
		}
	}
	return false
}

// realSamples reads an argument that is a whole sample rather than one point --
// #logsumexp, #softmax, #log_softmax -- accepting an NDArray or a Ruby Array of
// numbers, so either side of the numeric stack can be fed in, and answers the
// observations as a []float64.
func (vm *VM) realSamples(v object.Value) []float64 {
	if a, ok := v.(*NDArray); ok {
		flat := a.a.Flatten()
		out := make([]float64, flat.Size())
		for i := range out {
			out[i] = flat.At(i)
		}
		return out
	}
	return floatSlice(v)
}

// registerSpecial installs the Special module and its scipy.special functions.
func (vm *VM) registerSpecial() {
	mod := newClass("Special", nil)
	mod.isModule = true
	vm.consts["Special"] = mod

	def := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// u1/u2/u3 bind a scipy ufunc of one, two or three real arguments. Each takes
	// the float64 function and the element-wise one, and chooses between them by
	// looking at the arguments.
	u1 := func(name string, sf func(float64) float64, af func(*nd.Array) (*nd.Array, error)) {
		def(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			ndArgc(args, 1, 1)
			if anyND(args) {
				return mustArray(af(vm.ndOperand(args[0])))
			}
			return object.Float(sf(vm.ndFloat(args[0])))
		})
	}
	u2 := func(name string, sf func(a, b float64) float64, af func(a, b *nd.Array) (*nd.Array, error)) {
		def(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			ndArgc(args, 2, 2)
			if anyND(args) {
				return mustArray(af(vm.ndOperand(args[0]), vm.ndOperand(args[1])))
			}
			return object.Float(sf(vm.ndFloat(args[0]), vm.ndFloat(args[1])))
		})
	}
	u3 := func(name string, sf func(a, b, c float64) float64, af func(a, b, c *nd.Array) (*nd.Array, error)) {
		def(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			ndArgc(args, 3, 3)
			if anyND(args) {
				return mustArray(af(vm.ndOperand(args[0]), vm.ndOperand(args[1]), vm.ndOperand(args[2])))
			}
			return object.Float(sf(vm.ndFloat(args[0]), vm.ndFloat(args[1]), vm.ndFloat(args[2])))
		})
	}

	// uInt1 binds a ufunc whose first argument is an order or a count: an Integer
	// on the scalar path (scipy takes an int there too), an array like any other
	// on the element-wise one.
	uInt1 := func(name string, sf func(n int, x float64) float64, af func(a, b *nd.Array) (*nd.Array, error)) {
		def(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			ndArgc(args, 2, 2)
			if anyND(args) {
				return mustArray(af(vm.ndOperand(args[0]), vm.ndOperand(args[1])))
			}
			return object.Float(sf(vm.ndInt(args[0]), vm.ndFloat(args[1])))
		})
	}

	// uInt2 binds the combinatorial pair, whose scalar form counts in int64.
	uInt2 := func(name string, sf func(n, k int64) float64, af func(a, b *nd.Array) (*nd.Array, error)) {
		def(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			ndArgc(args, 2, 2)
			if anyND(args) {
				return mustArray(af(vm.ndOperand(args[0]), vm.ndOperand(args[1])))
			}
			return object.Float(sf(int64(vm.ndInt(args[0])), int64(vm.ndInt(args[1]))))
		})
	}

	// ---------------------------------------------------------------- gamma
	u1("gamma", special.Gamma, snd.Gamma)
	u1("gammaln", special.LogGamma, snd.LogGamma)
	u1("gammasgn", special.GammaSign, snd.GammaSign)
	u1("digamma", special.Digamma, snd.Digamma)
	uInt1("polygamma", special.Polygamma, snd.Polygamma)
	u1("factorial", special.Factorial, snd.Factorial)
	u1("log_factorial", special.LogFactorial, snd.LogFactorial)
	def("factorial2", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 1)
		if anyND(args) {
			return mustArray(snd.Factorial2(vm.ndOperand(args[0])))
		}
		return object.Float(special.Factorial2(vm.ndInt(args[0])))
	})

	// loggamma_sign answers [log|Gamma(x)|, sign] -- the pair scipy splits across
	// gammaln and gammasgn. It has no scipy name, and no element-wise form.
	def("loggamma_sign", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 1)
		l, s := special.LogGammaSign(vm.ndFloat(args[0]))
		return object.NewArrayFromSlice([]object.Value{object.Float(l), object.Integer(s)})
	})

	// ----------------------------------------------------------------- beta
	u2("beta", special.Beta, snd.Beta)
	u2("betaln", special.LogBeta, snd.LogBeta)
	u2("binom", special.Binom, snd.Binom)
	uInt2("comb", special.Comb, snd.Comb)
	uInt2("perm", special.Perm, snd.Perm)

	// ------------------------------------------------------------------ erf
	u1("erf", special.Erf, snd.Erf)
	u1("erfc", special.Erfc, snd.Erfc)
	u1("erfcx", special.Erfcx, snd.Erfcx)
	u1("erfinv", special.ErfInv, snd.ErfInv)
	u1("erfcinv", special.ErfcInv, snd.ErfcInv)
	u1("ndtr", special.Ndtr, snd.Ndtr)
	u1("ndtri", special.Ndtri, snd.Ndtri)
	u1("log_ndtr", special.LogNdtr, snd.LogNdtr)

	// ------------------------------------------------------------- logistic
	u1("expit", special.Expit, snd.Expit)
	u1("log_expit", special.LogExpit, snd.LogExpit)
	u1("logit", special.Logit, snd.Logit)
	u1("softplus", special.Log1pExp, snd.Log1pExp)
	u2("xlogy", special.Xlogy, snd.Xlogy)
	u2("xlog1py", special.Xlog1py, snd.Xlog1py)

	// ----------------------------------------------------------- reductions
	// logsumexp answers a Float from a whole sample; softmax and log_softmax
	// answer the same kind they were given.
	def("logsumexp", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 1)
		// The root function, not the nd one: we already hold the samples as a
		// []float64, and nd.LogSumExp only converts them back. Its error arm
		// reports a COMPLEX array, which rbgo cannot make -- NDArray is float64
		// only -- so going through it would add a branch nothing can reach.
		return object.Float(special.LogSumExp(vm.realSamples(args[0])))
	})
	vector := func(name string, sf func(dst, x []float64) []float64, af func(*nd.Array) (*nd.Array, error)) {
		def(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			ndArgc(args, 1, 1)
			if anyND(args) {
				return mustArray(af(vm.ndOperand(args[0])))
			}
			return floatArray(sf(nil, floatSlice(args[0])))
		})
	}
	vector("softmax", special.Softmax, snd.Softmax)
	vector("log_softmax", special.LogSoftmax, snd.LogSoftmax)

	// ----------------------------------------------------------------- zeta
	// zeta(x) is the Riemann zeta and zeta(x, q) the Hurwitz one, the one scipy
	// signature with an optional second argument.
	def("zeta", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 2)
		if len(args) == 1 {
			if anyND(args) {
				return mustArray(snd.Zeta(vm.ndOperand(args[0])))
			}
			return object.Float(special.Zeta(vm.ndFloat(args[0])))
		}
		if anyND(args) {
			return mustArray(snd.HurwitzZeta(vm.ndOperand(args[0]), vm.ndOperand(args[1])))
		}
		return object.Float(special.HurwitzZeta(vm.ndFloat(args[0]), vm.ndFloat(args[1])))
	})

	// ----------------------------------------------------------- incomplete
	u2("gammainc", special.GammaP, snd.GammaP)
	u2("gammaincc", special.GammaQ, snd.GammaQ)
	u2("gammaincinv", special.GammaPInv, snd.GammaPInv)
	u2("gammainccinv", special.GammaQInv, snd.GammaQInv)
	u3("betainc", special.BetaInc, snd.BetaInc)
	u3("betaincc", special.BetaIncC, snd.BetaIncC)
	u3("betaincinv", special.BetaIncInv, snd.BetaIncInv)
	u3("betainccinv", special.BetaIncCInv, snd.BetaIncCInv)

	// --------------------------------------------------------------- bessel
	u1("j0", special.J0, snd.J0)
	u1("j1", special.J1, snd.J1)
	u1("y0", special.Y0, snd.Y0)
	u1("y1", special.Y1, snd.Y1)
	u1("i0", special.I0, snd.I0)
	u1("i1", special.I1, snd.I1)
	u1("i0e", special.I0e, snd.I0e)
	u1("i1e", special.I1e, snd.I1e)
	u1("k0", special.K0, snd.K0)
	u1("k1", special.K1, snd.K1)
	u1("k0e", special.K0e, snd.K0e)
	u1("k1e", special.K1e, snd.K1e)
	u2("jv", special.Jv, snd.Jv)
	u2("yv", special.Yv, snd.Yv)
	u2("iv", special.Iv, snd.Iv)
	u2("kv", special.Kv, snd.Kv)
	u2("ive", special.Ive, snd.Ive)
	u2("kve", special.Kve, snd.Kve)
	uInt1("jn", special.Jn, snd.Jn)
	uInt1("yn", special.Yn, snd.Yn)
	uInt1("in", special.In, snd.In)
	uInt1("kn", special.Kn, snd.Kn)

	// jn_zeros answers the first nt positive zeros of Jn as an Array of Float.
	// It has no element-wise form, taking two counts and answering a sample.
	def("jn_zeros", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 2, 2)
		n, nt := vm.ndInt(args[0]), vm.ndInt(args[1])
		if n < 0 {
			raise("ArgumentError", "order must be non-negative")
		}
		if nt < 0 {
			raise("ArgumentError", "count must be non-negative")
		}
		return floatArray(special.JnZeros(n, nt))
	})

	// -------------------------------------------------------------- elliptic
	u1("ellipk", special.EllipK, snd.EllipK)
	u1("ellipkm1", special.EllipKm1, snd.EllipKm1)
	u1("ellipe", special.EllipE, snd.EllipE)
}
