// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"math/big"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// bigFloorDivmod returns Ruby's floored quotient and modulo of a and b (b != 0):
// the modulo takes the sign of the divisor, unlike Go's truncated QuoRem.
func bigFloorDivmod(a, b *big.Int) (*big.Int, *big.Int) {
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(a, b, r)
	if r.Sign() != 0 && (r.Sign() < 0) != (b.Sign() < 0) {
		q.Sub(q, big.NewInt(1))
		r.Add(r, b)
	}
	return q, r
}

// numCoerceDispatch runs Ruby's coerce protocol for an Integer operator whose
// right operand is neither an Integer/Bignum nor a Float: it calls arg.coerce
// (raising the canonical "X can't be coerced into Integer" TypeError when arg has
// none), then re-sends method on the returned [x, y] pair.
func (vm *VM) numCoerceDispatch(self, arg object.Value, method string) object.Value {
	if vm.respondsToDynamic(arg, "coerce") {
		pair := vm.send(arg, "coerce", []object.Value{self}, nil)
		if arr, ok := pair.(*object.Array); ok && len(arr.Elems) == 2 {
			return vm.send(arr.Elems[0], method, []object.Value{arr.Elems[1]}, nil)
		}
		raise("TypeError", "coerce must return [x, y]")
	}
	raise("TypeError", "%s can't be coerced into Integer", coerceName(arg))
	return nil
}

// integerDivmod implements Integer#divmod: an Integer/Bignum operand gives a
// floored [quotient, modulo] pair of Integers; a Float operand gives an Integer
// quotient and a Float modulo (like Float#divmod); anything else runs the coerce
// protocol. Division by zero raises ZeroDivisionError.
func (vm *VM) integerDivmod(self, arg object.Value) object.Value {
	if b, ok := object.BigOf(arg); ok {
		if b.Sign() == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		a, _ := object.BigOf(self)
		q, r := bigFloorDivmod(a, b)
		return object.NewArray(object.NormInt(q), object.NormInt(r))
	}
	if bf, ok := arg.(object.Float); ok {
		b := float64(bf)
		if b == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		a, _ := toFloat(self)
		q := math.Floor(a / b)
		return object.NewArray(object.IntValue(int64(q)), object.Float(a-b*q))
	}
	return vm.numCoerceDispatch(self, arg, "divmod")
}

// integerRemainder implements Integer#remainder: the remainder truncates toward
// zero (keeping the dividend's sign), for an Integer/Bignum or Float operand.
func (vm *VM) integerRemainder(self, arg object.Value) object.Value {
	if b, ok := object.BigOf(arg); ok {
		if b.Sign() == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		a, _ := object.BigOf(self)
		return object.NormInt(new(big.Int).Rem(a, b))
	}
	if bf, ok := arg.(object.Float); ok {
		b := float64(bf)
		if b == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		a, _ := toFloat(self)
		return object.Float(a - b*math.Trunc(a/b))
	}
	return vm.numCoerceDispatch(self, arg, "remainder")
}

// integerGcdArg reads the operand of #gcd / #lcm / #gcdlcm as a big integer,
// raising MRI's "not an integer" TypeError for a Float or any non-integer.
func integerGcdArg(arg object.Value) *big.Int {
	if b, ok := object.BigOf(arg); ok {
		return b
	}
	raise("TypeError", "not an integer")
	return nil
}

// bigGCD returns the non-negative greatest common divisor of a and b, with
// gcd(0, 0) == 0.
func bigGCD(a, b *big.Int) *big.Int {
	return new(big.Int).GCD(nil, nil, new(big.Int).Abs(a), new(big.Int).Abs(b))
}

// bigLCM returns the non-negative least common multiple of a and b, or 0 when
// either is 0.
func bigLCM(a, b *big.Int) *big.Int {
	if a.Sign() == 0 || b.Sign() == 0 {
		return big.NewInt(0)
	}
	g := bigGCD(a, b)
	l := new(big.Int).Quo(a, g)
	l.Mul(l, b)
	return l.Abs(l)
}
