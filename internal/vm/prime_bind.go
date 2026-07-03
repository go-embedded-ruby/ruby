// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"

	prime "github.com/go-ruby-prime/prime"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-prime/prime — the pure-Go, MRI-4.0.5
// faithful port of Ruby's `prime` stdlib — into rbgo. The library owns the
// primality test (Baillie–PSW), the unbounded prime generator and the prime
// factorisation; this file is the thin shell that maps rbgo's Integer/Bignum
// value model onto the library's *big.Int model and registers the Ruby-visible
// surface: the Prime module's singleton methods and the Integer#prime? /
// Integer#prime_division core extensions. rbgo had no prior Prime implementation,
// so this is a pure addition.

// registerPrime records the require "prime" feature hook. Nothing is installed
// eagerly: the hook (run once by doRequire on the first `require "prime"`)
// creates the Prime module and adds Integer#prime? / Integer#prime_division,
// mirroring MRI where lib/prime.rb defines them only when loaded. Before then
// `defined?(Prime)` is nil and Integer does not respond to #prime?. It runs
// during VM construction after Integer exists so the hook can close over it.
func (vm *VM) registerPrime() {
	if vm.featureHooks == nil {
		vm.featureHooks = map[string]func(){}
	}
	vm.featureHooks["prime"] = vm.installPrime
}

// installPrime installs the Prime module and the Integer core extensions — the
// body MRI's lib/prime.rb runs on load. The module's singleton methods mirror
// MRI's Prime.prime?, Prime.take/first, Prime.each, Prime.prime_division and
// Prime.int_from_prime_division; Integer#prime? and Integer#prime_division are
// the like-named core extensions the library backs identically.
func (vm *VM) installPrime() {
	mod := newClass("Prime", nil)
	mod.isModule = true
	vm.consts["Prime"] = mod
	sm := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// Prime.prime?(n) — primality via the library's deterministic BPSW test.
	sm("prime?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(prime.IsPrime(primeBig(args[0])))
	})

	// Prime.take(n) / Prime.first(n) — the first n primes as an Array.
	takeFn := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		n := int(intArg(args[0]))
		return primesToArray(prime.Take(n))
	}
	sm("take", takeFn)
	sm("first", takeFn)

	// Prime.each(ubound=nil) { |p| ... } — yield primes in ascending order. With
	// an upper bound only primes <= ubound are yielded; without one (or with a nil
	// bound) the generator runs until the block breaks. Called without a block it
	// returns an Enumerator (so Prime.each.first(5) works), like MRI.
	sm("each", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return enumFor(self, "each", args...)
		}
		// Determine the bound: a nil / absent argument means unbounded.
		bounded := false
		var ubound int64
		if len(args) > 0 {
			if _, isNil := object.AsNilOK(args[0]); !isNil {
				bounded = true
				ubound = intArg(args[0])
			}
		}
		gen := prime.EachPrime()
		for {
			p := gen()
			if bounded && p.Cmp(big.NewInt(ubound)) > 0 {
				break
			}
			vm.callBlock(blk, []object.Value{object.NormInt(p)})
		}
		return self
	})

	// Prime.prime_division(n) — the [prime, exponent] factorisation as an Array of
	// 2-element Arrays. Zero raises ZeroDivisionError, matching MRI.
	sm("prime_division", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return primeDivision(primeBig(args[0]))
	})

	// Prime.int_from_prime_division(pairs) — the inverse: rebuild the integer from
	// an Array of [prime, exponent] pairs.
	sm("int_from_prime_division", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NormInt(prime.Int(primePairs(args[0])))
	})

	// Integer#prime? and Integer#prime_division — the core extensions `require
	// "prime"` adds. They delegate to the same library functions on the receiver.
	vm.cInteger.define("prime?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(prime.IsPrime(bigVal(self)))
	})
	vm.cInteger.define("prime_division", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return primeDivision(bigVal(self))
	})
}

// primeBig coerces a Prime singleton-method argument to *big.Int, raising
// TypeError for a non-integer (matching MRI's Integer coercion).
func primeBig(v object.Value) *big.Int {
	if b, ok := object.BigOf(v); ok {
		return b
	}
	raise("TypeError", "no implicit conversion of %s into Integer", v.Inspect())
	return nil
}

// primesToArray boxes a slice of library primes into a Ruby Array of Integers.
func primesToArray(ps []*big.Int) object.Value {
	out := make([]object.Value, len(ps))
	for i, p := range ps {
		out[i] = object.NormInt(p)
	}
	return object.NewArrayFromSlice(out)
}

// primeDivision returns the factorisation of n as an Array of [prime, exponent]
// Arrays, raising ZeroDivisionError for zero exactly as MRI does (the library's
// non-panicking PrimeDivisionErr reports a ZeroError that we surface here).
func primeDivision(n *big.Int) object.Value {
	pairs, err := prime.PrimeDivisionErr(n)
	if err != nil {
		raise("ZeroDivisionError", "divided by 0")
	}
	out := make([]object.Value, len(pairs))
	for i, pr := range pairs {
		out[i] = object.NewArray(object.NormInt(pr[0]), object.NormInt(pr[1]))
	}
	return object.NewArrayFromSlice(out)
}

// primePairs reads an Array of [prime, exponent] Arrays into the library's pair
// slice, raising TypeError on a malformed shape (matching MRI, which coerces).
func primePairs(v object.Value) [][2]*big.Int {
	arr, ok := object.KindOK[*object.Array](v)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Array", v.Inspect())
	}
	pairs := make([][2]*big.Int, 0, len(arr.Elems))
	for _, e := range arr.Elems {
		pair, ok := object.KindOK[*object.Array](e)
		if !ok || len(pair.Elems) != 2 {
			raise("TypeError", "each prime-division entry must be a [prime, exponent] pair")
		}
		pairs = append(pairs, [2]*big.Int{primeBig(pair.Elems[0]), primeBig(pair.Elems[1])})
	}
	return pairs
}
