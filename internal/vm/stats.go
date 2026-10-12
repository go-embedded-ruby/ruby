// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	nd "github.com/go-ndarray/ndarray"
	"github.com/go-ndarray/ndarray/random"
	"github.com/go-statistics/stats"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The Stats module is a thin, cgo-free binding of github.com/go-statistics/stats,
// giving Ruby scipy.stats's distributions with no native SciPy dependency: the
// 32 univariate ones, under scipy's own names.
//
// As with Special, the names are the ones the Go module MEASURES itself against
// -- each distribution's doc comment states its scipy.stats equivalent, and the
// fixtures in testdata/gen are generated from it -- rather than transliterated
// from the Go name. That is why StudentT is Stats.t, ChiSquared is Stats.chi2,
// DiscreteUniform is Stats.randint, Weibull is Stats.weibull_min, Gumbel is
// Stats.gumbel_r and NoncentralChiSquared is Stats.ncx2.
//
//	n = Stats.norm(0.0, 1.0)
//	n.pdf(0.0)    # => 0.3989422804014327
//	n.ppf(0.975)  # => 1.9599639845400538
//	Stats.poisson(3.0).pmf(2)
//	Stats.t(10.0).cdf(NDArray.from([-1.0, 0.0, 1.0], [3]))
//
// Arguments follow scipy's order: the shape parameters, then loc, then scale,
// each optional with scipy's default. A continuous distribution takes loc and
// scale; a discrete one takes loc alone, as scipy's does.
//
// The method set is not invented here either: the module's Continuous and
// Discrete interfaces name their scipy counterpart method by method, and the two
// differ only in the density -- #pdf/#logpdf against #pmf/#logpmf. Everything
// else is defined once, for both.
//
// Every per-point method is a ufunc, as scipy's is: #pdf, #cdf, #ppf and the
// rest take a Float or an NDArray, through the module's own stats.Map. The
// summary methods (#mean, #entropy, …) take no argument and answer a Float.

// distCommon is the part of stats.Continuous and stats.Discrete that is the
// same -- everything but the density, which is PDF for one and PMF for the
// other. The two Ruby classes share one definition of these methods.
type distCommon interface {
	CDF(x float64) float64
	LogCDF(x float64) float64
	SF(x float64) float64
	LogSF(x float64) float64
	PPF(p float64) float64
	ISF(q float64) float64
	Mean() float64
	Var() float64
	Std() float64
	Skew() float64
	Kurtosis() float64
	Median() float64
	Entropy() float64
	Moment(n int) float64
	Interval(confidence float64) (lo, hi float64)
	Support() (lo, hi float64)
	Rvs(g *random.Generator, size ...int) (*nd.Array, error)
}

// RContDist and RDiscDist wrap one frozen distribution -- scipy's "frozen"
// distribution, parameters fixed at construction. name is scipy's name, for
// #inspect.
type RContDist struct {
	d    stats.Continuous
	name string
}

// RDiscDist is the discrete counterpart.
type RDiscDist struct {
	d    stats.Discrete
	name string
}

func (d *RContDist) ToS() string     { return "#<Stats::Continuous " + d.name + ">" }
func (d *RContDist) Inspect() string { return d.ToS() }
func (d *RContDist) Truthy() bool    { return true }

func (d *RDiscDist) ToS() string     { return "#<Stats::Discrete " + d.name + ">" }
func (d *RDiscDist) Inspect() string { return d.ToS() }
func (d *RDiscDist) Truthy() bool    { return true }

// raiseStats re-raises a go-statistics error as the Ruby error SciPy's maps to:
// a bad parameter or bad data is a ValueError in SciPy, which is an
// ArgumentError here, and a fit that did not converge is a RuntimeError in
// both. The message is the module's own.
func raiseStats(err error) {
	switch {
	case errors.Is(err, stats.ErrFit):
		raise("RuntimeError", "%s", err.Error())
	case errors.Is(err, stats.ErrParam), errors.Is(err, stats.ErrData):
		raise("ArgumentError", "%s", err.Error())
	}
	raise("ArgumentError", "%s", err.Error())
}

// argFloatOr reads a trailing optional real argument, scipy's loc or scale,
// falling back to scipy's default when it is absent.
func (vm *VM) argFloatOr(args []object.Value, i int, def float64) float64 {
	if len(args) <= i {
		return def
	}
	return vm.ndFloat(args[i])
}

// argIntAt reads a required integer parameter (a count, a population size).
func (vm *VM) argIntAt(args []object.Value, i int) int64 {
	return int64(vm.ndInt(args[i]))
}

// statsRNG is the generator #rvs draws from when no seed: is given. It is
// created once per VM and advances across calls, so successive draws are
// independent -- which is what scipy's module-level random state does. A seed:
// asks for a fresh, reproducible stream instead.
func (vm *VM) rvsGenerator(kw map[string]object.Value) *random.Generator {
	if s, ok := kw["seed"]; ok && !object.IsNil(s) {
		return random.DefaultRNG(uint64(vm.ndInt(s)))
	}
	if vm.statsRNG == nil {
		vm.statsRNG = random.DefaultRNG()
	}
	return vm.statsRNG
}

// pointwise turns one scalar method of a distribution into a ufunc: a Float
// answers a Float, an NDArray answers an NDArray of the same shape, through the
// module's own stats.Map.
func pointwise(f func(float64) float64) NativeFn {
	return func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 1)
		if a, ok := args[0].(*NDArray); ok {
			return &NDArray{a: stats.Map(a.a, f)}
		}
		return object.Float(f(vm.ndFloat(args[0])))
	}
}

// defineDistMethods installs the methods Continuous and Discrete share. get
// unwraps the receiver, so one definition serves both Ruby classes.
func (vm *VM) defineDistMethods(c *RClass, get func(object.Value) distCommon) {
	// Per-point functions: ufuncs over a Float or an NDArray.
	for name, pick := range map[string]func(distCommon) func(float64) float64{
		"cdf":    func(d distCommon) func(float64) float64 { return d.CDF },
		"logcdf": func(d distCommon) func(float64) float64 { return d.LogCDF },
		"sf":     func(d distCommon) func(float64) float64 { return d.SF },
		"logsf":  func(d distCommon) func(float64) float64 { return d.LogSF },
		"ppf":    func(d distCommon) func(float64) float64 { return d.PPF },
		"isf":    func(d distCommon) func(float64) float64 { return d.ISF },
	} {
		pick := pick
		c.define(name, func(vm *VM, self object.Value, args []object.Value, p *Proc) object.Value {
			return pointwise(pick(get(self)))(vm, self, args, p)
		})
	}

	// Summary values: no argument, a Float.
	for name, pick := range map[string]func(distCommon) float64{
		"mean":     func(d distCommon) float64 { return d.Mean() },
		"var":      func(d distCommon) float64 { return d.Var() },
		"std":      func(d distCommon) float64 { return d.Std() },
		"skew":     func(d distCommon) float64 { return d.Skew() },
		"kurtosis": func(d distCommon) float64 { return d.Kurtosis() },
		"median":   func(d distCommon) float64 { return d.Median() },
		"entropy":  func(d distCommon) float64 { return d.Entropy() },
	} {
		pick := pick
		c.define(name, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			ndArgc(args, 0, 0)
			return object.Float(pick(get(self)))
		})
	}

	// moment(n): the n-th non-central moment, scipy's .moment(n).
	c.define("moment", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 1)
		return object.Float(get(self).Moment(vm.ndInt(args[0])))
	})

	// interval(confidence) and support answer the pair scipy returns as a
	// two-tuple, which destructures in Ruby the same way: lo, hi = d.support.
	c.define("interval", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 1, 1)
		lo, hi := get(self).Interval(vm.ndFloat(args[0]))
		return floatArray([]float64{lo, hi})
	})
	c.define("support", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ndArgc(args, 0, 0)
		lo, hi := get(self).Support()
		return floatArray([]float64{lo, hi})
	})

	// rvs(size = nil, seed: nil): a draw. With no size it answers a Float, as
	// scipy's does; with a size an NDArray of that shape. Successive calls
	// advance one stream unless a seed: asks for a fresh one.
	c.define("rvs", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		args, kw := ndKw(args, "seed")
		ndArgc(args, 0, -1)
		size := make([]int, len(args))
		for i, a := range args {
			size[i] = vm.ndInt(a)
			if size[i] < 0 {
				raise("ArgumentError", "negative dimension %d", size[i])
			}
		}
		a, err := get(self).Rvs(vm.rvsGenerator(kw), size...)
		if err != nil {
			ndRaise(err)
		}
		if len(size) == 0 {
			return object.Float(a.At())
		}
		return &NDArray{a: a}
	})
}

// registerStats installs the Stats module, its two distribution classes and the
// 32 univariate distributions of scipy.stats.
func (vm *VM) registerStats() {
	mod := newClass("Stats", nil)
	mod.isModule = true
	vm.consts["Stats"] = mod

	vm.cContDist = newClass("Stats::Continuous", vm.cObject)
	vm.cDiscDist = newClass("Stats::Discrete", vm.cObject)
	mod.consts["Continuous"] = vm.cContDist
	mod.consts["Discrete"] = vm.cDiscDist

	vm.defineDistMethods(vm.cContDist, func(v object.Value) distCommon { return v.(*RContDist).d })
	vm.defineDistMethods(vm.cDiscDist, func(v object.Value) distCommon { return v.(*RDiscDist).d })

	// The density, the one method whose name differs between the two.
	vm.cContDist.define("pdf", func(vm *VM, self object.Value, args []object.Value, p *Proc) object.Value {
		return pointwise(self.(*RContDist).d.PDF)(vm, self, args, p)
	})
	vm.cContDist.define("logpdf", func(vm *VM, self object.Value, args []object.Value, p *Proc) object.Value {
		return pointwise(self.(*RContDist).d.LogPDF)(vm, self, args, p)
	})
	vm.cDiscDist.define("pmf", func(vm *VM, self object.Value, args []object.Value, p *Proc) object.Value {
		return pointwise(self.(*RDiscDist).d.PMF)(vm, self, args, p)
	})
	vm.cDiscDist.define("logpmf", func(vm *VM, self object.Value, args []object.Value, p *Proc) object.Value {
		return pointwise(self.(*RDiscDist).d.LogPMF)(vm, self, args, p)
	})

	def := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// cont and disc register one distribution each: min..max arguments, and a
	// builder reading them in scipy's order.
	cont := func(name string, min, max int, build func(vm *VM, a []object.Value) (stats.Continuous, error)) {
		def(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			ndArgc(args, min, max)
			d, err := build(vm, args)
			if err != nil {
				raiseStats(err)
			}
			return &RContDist{d: d, name: name}
		})
	}
	disc := func(name string, min, max int, build func(vm *VM, a []object.Value) (stats.Discrete, error)) {
		def(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			ndArgc(args, min, max)
			d, err := build(vm, args)
			if err != nil {
				raiseStats(err)
			}
			return &RDiscDist{d: d, name: name}
		})
	}

	// ------------------------------------------------------- continuous, loc+scale
	cont("norm", 0, 2, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewNormal(vm.argFloatOr(a, 0, 0), vm.argFloatOr(a, 1, 1))
	})
	cont("cauchy", 0, 2, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewCauchy(vm.argFloatOr(a, 0, 0), vm.argFloatOr(a, 1, 1))
	})
	cont("expon", 0, 2, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewExponential(vm.argFloatOr(a, 0, 0), vm.argFloatOr(a, 1, 1))
	})
	cont("gumbel_r", 0, 2, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewGumbel(vm.argFloatOr(a, 0, 0), vm.argFloatOr(a, 1, 1))
	})
	cont("halfnorm", 0, 2, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewHalfNormal(vm.argFloatOr(a, 0, 0), vm.argFloatOr(a, 1, 1))
	})
	cont("laplace", 0, 2, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewLaplace(vm.argFloatOr(a, 0, 0), vm.argFloatOr(a, 1, 1))
	})
	cont("logistic", 0, 2, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewLogistic(vm.argFloatOr(a, 0, 0), vm.argFloatOr(a, 1, 1))
	})
	cont("rayleigh", 0, 2, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewRayleigh(vm.argFloatOr(a, 0, 0), vm.argFloatOr(a, 1, 1))
	})
	cont("uniform", 0, 2, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewUniform(vm.argFloatOr(a, 0, 0), vm.argFloatOr(a, 1, 1))
	})

	// ------------------------------------------- continuous, one shape parameter
	cont("chi", 1, 3, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewChi(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0), vm.argFloatOr(a, 2, 1))
	})
	cont("chi2", 1, 3, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewChiSquared(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0), vm.argFloatOr(a, 2, 1))
	})
	cont("gamma", 1, 3, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewGamma(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0), vm.argFloatOr(a, 2, 1))
	})
	cont("invgamma", 1, 3, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewInverseGamma(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0), vm.argFloatOr(a, 2, 1))
	})
	cont("lognorm", 1, 3, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewLogNormal(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0), vm.argFloatOr(a, 2, 1))
	})
	cont("lomax", 1, 3, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewLomax(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0), vm.argFloatOr(a, 2, 1))
	})
	cont("pareto", 1, 3, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewPareto(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0), vm.argFloatOr(a, 2, 1))
	})
	cont("t", 1, 3, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewStudentT(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0), vm.argFloatOr(a, 2, 1))
	})
	cont("triang", 1, 3, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewTriangular(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0), vm.argFloatOr(a, 2, 1))
	})
	cont("vonmises", 1, 3, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewVonMises(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0), vm.argFloatOr(a, 2, 1))
	})
	cont("weibull_min", 1, 3, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewWeibull(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0), vm.argFloatOr(a, 2, 1))
	})

	// ------------------------------------------ continuous, two shape parameters
	cont("beta", 2, 4, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewBeta(vm.ndFloat(a[0]), vm.ndFloat(a[1]), vm.argFloatOr(a, 2, 0), vm.argFloatOr(a, 3, 1))
	})
	cont("f", 2, 4, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewF(vm.ndFloat(a[0]), vm.ndFloat(a[1]), vm.argFloatOr(a, 2, 0), vm.argFloatOr(a, 3, 1))
	})
	cont("ncx2", 2, 4, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewNoncentralChiSquared(vm.ndFloat(a[0]), vm.ndFloat(a[1]), vm.argFloatOr(a, 2, 0), vm.argFloatOr(a, 3, 1))
	})
	cont("reciprocal", 2, 4, func(vm *VM, a []object.Value) (stats.Continuous, error) {
		return stats.NewReciprocal(vm.ndFloat(a[0]), vm.ndFloat(a[1]), vm.argFloatOr(a, 2, 0), vm.argFloatOr(a, 3, 1))
	})

	// ------------------------------------------------------------------ discrete
	// A discrete distribution takes loc but no scale, as scipy's does.
	disc("bernoulli", 1, 2, func(vm *VM, a []object.Value) (stats.Discrete, error) {
		return stats.NewBernoulli(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0))
	})
	disc("geom", 1, 2, func(vm *VM, a []object.Value) (stats.Discrete, error) {
		return stats.NewGeometric(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0))
	})
	disc("poisson", 1, 2, func(vm *VM, a []object.Value) (stats.Discrete, error) {
		return stats.NewPoisson(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0))
	})
	disc("zipf", 1, 2, func(vm *VM, a []object.Value) (stats.Discrete, error) {
		return stats.NewZipf(vm.ndFloat(a[0]), vm.argFloatOr(a, 1, 0))
	})
	disc("binom", 2, 3, func(vm *VM, a []object.Value) (stats.Discrete, error) {
		return stats.NewBinomial(vm.argIntAt(a, 0), vm.ndFloat(a[1]), vm.argFloatOr(a, 2, 0))
	})
	disc("nbinom", 2, 3, func(vm *VM, a []object.Value) (stats.Discrete, error) {
		return stats.NewNegativeBinomial(vm.ndFloat(a[0]), vm.ndFloat(a[1]), vm.argFloatOr(a, 2, 0))
	})
	disc("randint", 2, 3, func(vm *VM, a []object.Value) (stats.Discrete, error) {
		return stats.NewDiscreteUniform(vm.argIntAt(a, 0), vm.argIntAt(a, 1), vm.argFloatOr(a, 2, 0))
	})
	disc("hypergeom", 3, 4, func(vm *VM, a []object.Value) (stats.Discrete, error) {
		return stats.NewHypergeometric(vm.argIntAt(a, 0), vm.argIntAt(a, 1), vm.argIntAt(a, 2), vm.argFloatOr(a, 3, 0))
	})
}
