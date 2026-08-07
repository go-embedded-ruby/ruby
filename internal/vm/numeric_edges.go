// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"math/big"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file fills a batch of Integer/Float/Numeric edge methods to MRI 3.4/4.0
// semantics: Integer#[] (bit reference, incl. two-arg and Range forms),
// Integer#size/#ord/#integer?, Float#next_float/#prev_float, and MRI-faithful
// Float#round / Integer#round with the `half:` rounding-mode keyword
// (:up/:down/:even). The rounding is ported verbatim from CRuby numeric.c
// (round_half_up/down/even, flo_round, rb_int_round) so the float-representation
// correction steps match MRI bit-for-bit.

// The `half:` rounding-mode keyword and the roundMode type (modeHalfUp/Down/Even)
// are shared with the Rational rounding path (see rational.go): roundArgs peels
// the ndigits and mode with the same ratPrecision / ratHalfMode helpers.

// roundArgs splits a round argument list into its integer ndigits (default 0)
// and the `half:` rounding mode, validating the mode even when ndigits >= 0.
func roundArgs(args []object.Value) (ndigits int, mode roundMode) {
	return int(ratPrecision(args)), ratHalfMode(args)
}

// --- Float rounding, ported from CRuby numeric.c ---------------------------

// roundHalfUp rounds x*s to an integer with ties away from zero, applying MRI's
// division-based correction for the float representation of x*s.
func roundHalfUp(x, s float64) float64 {
	xs := x * s
	f := math.Round(xs)
	if s == 1.0 {
		return f
	}
	if x > 0 {
		if (f+0.5)/s <= x {
			f += 1
		}
		return f
	}
	if (f-0.5)/s >= x {
		f -= 1
	}
	return f
}

// roundHalfDown rounds x*s to an integer with ties toward zero.
func roundHalfDown(x, s float64) float64 {
	xs := x * s
	f := math.Round(xs)
	if x > 0 {
		if (f-0.5)/s >= x {
			f -= 1
		}
		return f
	}
	if (f+0.5)/s <= x {
		f += 1
	}
	return f
}

// roundHalfEven rounds x*s to an integer with ties to the even neighbour
// (banker's rounding), ported from CRuby's round_half_even.
func roundHalfEven(x, s float64) float64 {
	var u, v, f, d, uf float64
	v = math.Mod(x, 1) // fractional part, keeping sign
	u = x - v
	us := u * s
	vs := v * s

	if x > 0.0 {
		f = math.Floor(vs)
		uf = us + f
		d = vs - f
		if d > 0.5 {
			d = 1.0
		} else if d == 0.5 || (uf+0.5)/s <= x {
			d = math.Mod(uf, 2.0)
		} else {
			d = 0.0
		}
		return us + f + d
	}
	if x < 0.0 {
		f = math.Ceil(vs)
		uf = us + f
		d = f - vs
		if d > 0.5 {
			d = 1.0
		} else if d == 0.5 || (uf-0.5)/s >= x {
			d = math.Mod(-uf, 2.0)
		} else {
			d = 0.0
		}
		return us + f - d
	}
	return us
}

// applyRoundMode dispatches to the selected float rounding function.
func applyRoundMode(mode roundMode, x, s float64) float64 {
	switch mode {
	case modeHalfDown:
		return roundHalfDown(x, s)
	case modeHalfEven:
		return roundHalfEven(x, s)
	default:
		return roundHalfUp(x, s)
	}
}

const dblDig = 15 // DBL_DIG

// floatRoundOverflow reports when ndigits exceeds the precision the double can
// carry (ported from CRuby float_round_overflow); the value is then unchanged.
func floatRoundOverflow(ndigits, binexp int) bool {
	const floatDig = dblDig + 2
	adj := binexp / 3
	if binexp > 0 {
		adj = binexp / 4
	} else {
		adj = binexp/3 - 1
	}
	return ndigits >= floatDig-adj
}

// floatRoundUnderflow reports when ndigits is so negative the result is 0
// (ported from CRuby float_round_underflow).
func floatRoundUnderflow(ndigits, binexp int) bool {
	var lim int
	if binexp > 0 {
		lim = binexp/3 + 1
	} else {
		lim = binexp / 4
	}
	return ndigits < -lim
}

// dbl2ival converts an already-integral double to an Integer (Integer/Bignum),
// mirroring CRuby's dbl2ival.
func dbl2ival(x float64) object.Value {
	if x >= math.MinInt64 && x <= math.MaxInt64 {
		return object.IntValue(int64(x))
	}
	bi, _ := big.NewFloat(x).Int(nil)
	return object.NormInt(bi)
}

// floRound is the full CRuby flo_round contract: it returns an Integer for
// ndigits <= 0 and a Float for ndigits > 0, applying the selected half mode.
func floRound(number float64, ndigits int, mode roundMode) object.Value {
	if number == 0.0 {
		if ndigits > 0 {
			return object.Float(number)
		}
		return object.IntValue(0)
	}
	if ndigits < 0 {
		t := math.Trunc(number) // flo_to_i truncates toward zero
		bi, _ := big.NewFloat(t).Int(nil)
		return object.NormInt(intRound(bi, ndigits, mode))
	}
	if ndigits == 0 {
		return dbl2ival(applyRoundMode(mode, number, 1.0))
	}
	if !math.IsInf(number, 0) && !math.IsNaN(number) {
		_, binexp := math.Frexp(number)
		if floatRoundOverflow(ndigits, binexp) {
			return object.Float(number)
		}
		if floatRoundUnderflow(ndigits, binexp) {
			return object.Float(0)
		}
		if ndigits >= dblDig { // !ACCURATE_POW10 — fall back to exact rational
			return floRoundByRational(number, ndigits, mode)
		}
		f := math.Pow(10, float64(ndigits))
		return object.Float(applyRoundMode(mode, number, f) / f)
	}
	return object.Float(number) // NaN / ±Infinity
}

// floRoundByRational rounds with ndigits beyond a double's accurate power of ten
// by working in exact rationals (CRuby's rb_flo_round_by_rational path).
func floRoundByRational(number float64, ndigits int, mode roundMode) object.Value {
	r := new(big.Rat).SetFloat64(number) // exact
	pow := new(big.Rat).SetInt(pow10Big(int64(ndigits)))
	scaled := new(big.Rat).Mul(r, pow)
	ri := ratRoundInt(scaled, mode) // shared Rational tie-break rounding
	res := new(big.Rat).SetInt(ri)
	res.Quo(res, pow)
	f, _ := res.Float64()
	return object.Float(f)
}

// --- Integer rounding, ported from CRuby numeric.c -------------------------

// intRoundZeroP reports when 10**(-ndigits)/2 exceeds |num| so the rounded value
// is 0 (ported from CRuby int_round_zero_p).
func intRoundZeroP(num *big.Int, ndigits int) bool {
	bytes := 8 // sizeof(long) for a fixnum-range value
	if !num.IsInt64() {
		bytes = len(num.Bytes())
	}
	return -0.415241*float64(ndigits)-0.125 > float64(bytes)
}

// intRound rounds an integer to a multiple of 10**(-ndigits) under mode
// (ported from CRuby rb_int_round; ndigits >= 0 leaves the value unchanged).
func intRound(num *big.Int, ndigits int, mode roundMode) *big.Int {
	if ndigits >= 0 {
		return new(big.Int).Set(num)
	}
	if num.Sign() == 0 || intRoundZeroP(num, ndigits) {
		return big.NewInt(0)
	}
	f := pow10Big(int64(-ndigits))
	h := new(big.Int).Rsh(f, 1) // f / 2
	r := new(big.Int).Mod(num, f)
	n := new(big.Int).Sub(num, r) // floor to a multiple of f
	cmp := r.Cmp(h)
	if cmp > 0 || (cmp == 0 && intHalfUp(mode, num, n, f)) {
		n.Add(n, f)
	}
	return n
}

// intHalfUp decides, for an exact half, whether to round up (toward +Infinity),
// matching CRuby's int_half_p_half_{up,down,even}.
func intHalfUp(mode roundMode, num, n, f *big.Int) bool {
	switch mode {
	case modeHalfDown:
		return num.Sign() < 0
	case modeHalfEven:
		q := new(big.Int).Quo(n, f) // n is a multiple of f, so exact
		return q.Bit(0) == 1
	default: // modeHalfUp
		return num.Sign() > 0
	}
}

// --- Integer#[] bit reference ----------------------------------------------

// arefToInt coerces a bit-index / range-boundary argument to a *big.Int through
// #to_int, mirroring MRI's conversion (a Float truncates toward zero; ±Infinity
// raises FloatDomainError; a non-convertible value raises TypeError).
func (vm *VM) arefToInt(v object.Value) *big.Int {
	switch x := v.(type) {
	case object.Integer:
		return big.NewInt(int64(x))
	case *object.Bignum:
		return new(big.Int).Set(x.I)
	case object.Float:
		f := float64(x)
		if math.IsInf(f, 1) {
			raise("FloatDomainError", "Infinity")
		}
		if math.IsInf(f, -1) {
			raise("FloatDomainError", "-Infinity")
		}
		if math.IsNaN(f) {
			raise("FloatDomainError", "NaN")
		}
		bi, _ := big.NewFloat(math.Trunc(f)).Int(nil)
		return bi
	}
	if vm.respondsToDynamic(v, "to_int") {
		r := vm.send(v, "to_int", nil, nil)
		switch y := r.(type) {
		case object.Integer:
			return big.NewInt(int64(y))
		case *object.Bignum:
			return new(big.Int).Set(y.I)
		}
		raise("TypeError", "can't convert %s to Integer (%s#to_int gives %s)",
			vm.classOf(v).name, vm.classOf(v).name, vm.classOf(r).name)
	}
	raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(v))
	return nil
}

// maxShift caps a shift/mask width to what is practical, standing in for a
// value larger than the most significant bit (the result is then all-sign).
const maxShift = 1 << 30

// rubyShiftRight computes n >> i with Ruby's semantics: a negative i shifts
// left, and an i past the value's magnitude yields the sign extension.
func rubyShiftRight(n, i *big.Int) *big.Int {
	if i.Sign() >= 0 {
		if !i.IsInt64() || i.Int64() > maxShift {
			if n.Sign() < 0 {
				return big.NewInt(-1)
			}
			return big.NewInt(0)
		}
		return new(big.Int).Rsh(n, uint(i.Int64()))
	}
	neg := new(big.Int).Neg(i)
	if !neg.IsInt64() || neg.Int64() > maxShift {
		raise("RangeError", "shift width too big")
	}
	return new(big.Int).Lsh(n, uint(neg.Int64()))
}

// bitAt returns bit i of n (two's complement for negatives); a negative index
// is 0, and an index past the most significant bit is the sign bit.
func bitAt(n, i *big.Int) *big.Int {
	if i.Sign() < 0 {
		return big.NewInt(0)
	}
	if !i.IsInt64() || i.Int64() > maxShift {
		if n.Sign() < 0 {
			return big.NewInt(1)
		}
		return big.NewInt(0)
	}
	return big.NewInt(int64(n.Bit(int(i.Int64()))))
}

// bitSlice computes (n >> i) & ((1 << length) - 1) with Ruby semantics; a
// negative length keeps every bit of the shifted value.
func bitSlice(n, i, length *big.Int) *big.Int {
	shifted := rubyShiftRight(n, i)
	if length.Sign() < 0 {
		return shifted
	}
	if !length.IsInt64() || length.Int64() > maxShift {
		return shifted
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(length.Int64())), big.NewInt(1))
	return new(big.Int).And(shifted, mask)
}

// integerBitRange evaluates Integer#[] with a Range argument.
func (vm *VM) integerBitRange(n *big.Int, rng *object.Range) object.Value {
	beginless := object.IsNil(rng.Lo)
	endless := object.IsNil(rng.Hi)
	if beginless {
		if endless {
			raise("ArgumentError", "The beginless range for Integer#[] results in infinity")
		}
		hi := vm.arefToInt(rng.Hi)
		length := new(big.Int).Set(hi)
		if !rng.Exclusive {
			length.Add(length, big.NewInt(1))
		}
		if bitSlice(n, big.NewInt(0), length).Sign() != 0 {
			raise("ArgumentError", "The beginless range for Integer#[] results in infinity")
		}
		return object.IntValue(0)
	}
	lo := vm.arefToInt(rng.Lo)
	if endless {
		return object.NormInt(rubyShiftRight(n, lo))
	}
	hi := vm.arefToInt(rng.Hi)
	if hi.Cmp(lo) < 0 { // upper boundary below lower — ignored (endless)
		return object.NormInt(rubyShiftRight(n, lo))
	}
	length := new(big.Int).Sub(hi, lo)
	if !rng.Exclusive {
		length.Add(length, big.NewInt(1))
	}
	return object.NormInt(bitSlice(n, lo, length))
}

// registerNumericEdges installs the batch of Integer/Float/Numeric edge methods.
// It runs at the end of bootstrap so its Float#round / Integer#round definitions
// win over the earlier registrations in the same class.
func (vm *VM) registerNumericEdges() {
	floatOf := func(self object.Value) float64 { return float64(self.(object.Float)) }

	// Integer#[] — bit reference, with two-arg (start, len) and Range forms.
	vm.cInteger.define("[]", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		n := bigVal(self)
		if len(args) == 1 {
			if rng, ok := args[0].(*object.Range); ok {
				return vm.integerBitRange(n, rng)
			}
		}
		i := vm.arefToInt(args[0])
		if len(args) >= 2 {
			return object.NormInt(bitSlice(n, i, vm.arefToInt(args[1])))
		}
		return object.NormInt(bitAt(n, i))
	})

	// Integer#size — bytes in the machine representation (8 for a fixnum-range
	// value; the exact byte count for a Bignum).
	vm.cInteger.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if _, ok := self.(object.Integer); ok {
			return object.IntValue(8)
		}
		abs := new(big.Int).Abs(bigVal(self))
		return object.IntValue(int64((abs.BitLen() + 7) / 8))
	})

	// Integer#ord — an integer is its own codepoint.
	vm.cInteger.define("ord", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})

	// Integer#integer? — always true (overrides Numeric#integer?).
	vm.cInteger.define("integer?", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.True
	})

	// Integer#round — MRI-faithful, honouring negative ndigits and `half:`.
	vm.cInteger.define("round", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ndigits, mode := roundArgs(args)
		if ndigits >= 0 {
			return self
		}
		return object.NormInt(intRound(bigVal(self), ndigits, mode))
	})

	// Float#next_float / #prev_float — the adjacent representable doubles.
	vm.cFloat.define("next_float", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(math.Nextafter(floatOf(self), math.Inf(1)))
	})
	vm.cFloat.define("prev_float", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(math.Nextafter(floatOf(self), math.Inf(-1)))
	})

	// Float#round — MRI-faithful, honouring `half:` and the ndigits contract.
	vm.cFloat.define("round", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ndigits, mode := roundArgs(args)
		return floRound(floatOf(self), ndigits, mode)
	})
}
