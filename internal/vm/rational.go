package vm

import (
	"math"
	"math/big"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Rational numbers, held as a normalised math/big.Rat. They complete the numeric
// tower (Integer / Rational / Float / Complex): arithmetic with another Rational
// or an Integer stays exact, while a Float operand promotes the result to Float
// (Float wins). Ordering and equality coerce the same way.

// toRat converts an exact integer or Rational to *big.Rat; ok is false for a
// Float or non-numeric value.
func toRat(v object.Value) (*big.Rat, bool) {
	{
		__sw129 := v
		switch {
		case object.IsInt(__sw129):
			n := object.AsInteger(__sw129)
			_ = n
			return new(big.Rat).SetInt64(int64(n)), true
		case object.IsKind[*object.Bignum](__sw129):
			n := object.Kind[*object.Bignum](__sw129)
			_ = n
			return new(big.Rat).SetInt(n.I), true
		case object.IsKind[*object.Rational](__sw129):
			n := object.Kind[*object.Rational](__sw129)
			_ = n
			return n.R, true
		}
	}
	return nil, false
}

// newRational builds Rational(num, den) from integer components, raising on a
// zero denominator.
func newRational(num, den object.Value) object.Value {
	nb, ok := object.BigOf(num)
	if !ok {
		return raise("TypeError", "can't convert %s into Rational", num.Inspect())
	}
	db, ok := object.BigOf(den)
	if !ok {
		return raise("TypeError", "can't convert %s into Rational", den.Inspect())
	}
	if db.Sign() == 0 {
		return raise("ZeroDivisionError", "divided by 0")
	}
	return object.Wrap(&object.Rational{R: new(big.Rat).SetFrac(nb, db)})
}

// rationalOp applies an arithmetic operator when at least one operand is a
// Rational. A Float operand drops to float arithmetic; otherwise the result is
// an exact Rational.
func rationalOp(op bytecode.Op, a, b object.Value) object.Value {
	if _, ok := object.AsFloatOK(a); ok {
		return floatOp(op, complexFloat(a), complexFloat(b))
	}
	if _, ok := object.AsFloatOK(b); ok {
		return floatOp(op, complexFloat(a), complexFloat(b))
	}
	ra, aok := toRat(a)
	rb, bok := toRat(b)
	if !aok {
		return raise("TypeError", "%s can't be coerced into Rational", a.Inspect())
	}
	if !bok {
		return raise("TypeError", "%s can't be coerced into Rational", b.Inspect())
	}
	return ratArith(op, ra, rb)
}

func ratArith(op bytecode.Op, ra, rb *big.Rat) object.Value {
	switch op {
	case bytecode.OpAdd:
		return object.Wrap(&object.Rational{R: new(big.Rat).Add(ra, rb)})
	case bytecode.OpSub:
		return object.Wrap(&object.Rational{R: new(big.Rat).Sub(ra, rb)})
	case bytecode.OpMul:
		return object.Wrap(&object.Rational{R: new(big.Rat).Mul(ra, rb)})
	case bytecode.OpDiv:
		if rb.Sign() == 0 {
			return raise("ZeroDivisionError", "divided by 0")
		}
		return object.Wrap(&object.Rational{R: new(big.Rat).Quo(ra, rb)})
	case bytecode.OpMod:
		if rb.Sign() == 0 {
			return raise("ZeroDivisionError", "divided by 0")
		}
		// a - b·floor(a/b): the remainder takes the divisor's sign (Ruby's %).
		q := new(big.Rat).Quo(ra, rb)
		fl := new(big.Int).Div(q.Num(), q.Denom()) // Euclidean ⇒ floor (denom > 0)
		m := new(big.Rat).Sub(ra, new(big.Rat).Mul(rb, new(big.Rat).SetInt(fl)))
		return object.Wrap(&object.Rational{R: m})
	}
	return raise("NoMethodError", "undefined method '%s' for a Rational", op)
}

// ratPow raises ra to rb. An integer exponent gives an exact Rational; a
// fractional exponent falls to float.
func ratPow(ra, rb *big.Rat) object.Value {
	if !rb.IsInt() {
		af, _ := ra.Float64()
		bf, _ := rb.Float64()
		return object.FloatValue(float64(object.Float(math.Pow(af, bf))))
	}
	e := rb.Num()
	base := ra
	if e.Sign() < 0 {
		if ra.Sign() == 0 {
			return raise("ZeroDivisionError", "divided by 0")
		}
		base = new(big.Rat).Inv(ra)
		e = new(big.Int).Neg(e)
	}
	pn := new(big.Int).Exp(base.Num(), e, nil)
	pd := new(big.Int).Exp(base.Denom(), e, nil)
	return object.Wrap(&object.Rational{R: new(big.Rat).SetFrac(pn, pd)})
}

// rationalEqual reports equality, coercing an Integer (Rational(2,1) == 2) or a
// Float (Rational(1,2) == 0.5).
func rationalEqual(r *object.Rational, other object.Value) bool {
	if orat, ok := toRat(other); ok {
		return r.R.Cmp(orat) == 0
	}
	if of, ok := object.AsFloatOK(other); ok {
		rf, _ := r.R.Float64()
		return rf == float64(of)
	}
	return false
}

// ratCompare returns -1/0/1 comparing a Rational to another numeric, or nil for
// a non-numeric (Ruby's <=>).
func (vm *VM) ratCompare(r *object.Rational, other object.Value) object.Value {
	if orat, ok := toRat(other); ok {
		return object.IntValue(int64(r.R.Cmp(orat)))
	}
	if of, ok := object.AsFloatOK(other); ok {
		rf, _ := r.R.Float64()
		switch {
		case rf < float64(of):
			return object.IntValue(-1)
		case rf > float64(of):
			return object.IntValue(1)
		default:
			return object.IntValue(0)
		}
	}
	return object.NilVal()
}

// registerRational installs Kernel#Rational and the Rational instance methods.
func (vm *VM) registerRational() {
	vm.cObject.define("Rational", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		den := object.Value(object.IntValue(1))
		if len(args) > 1 {
			den = args[1]
		}
		return newRational(args[0], den)
	})

	rval := func(self object.Value) *object.Rational { return object.Kind[*object.Rational](self) }

	vm.cRational.define("numerator", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NormInt(rval(self).R.Num())
	})
	vm.cRational.define("denominator", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NormInt(rval(self).R.Denom())
	})
	vm.cRational.define("to_f", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		f, _ := rval(self).R.Float64()
		return object.FloatValue(float64(object.Float(f)))
	})
	toI := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		r := rval(self).R
		return object.NormInt(new(big.Int).Quo(r.Num(), r.Denom())) // truncate toward zero
	}
	vm.cRational.define("to_i", toI)
	vm.cRational.define("to_int", toI)
	vm.cRational.define("to_r", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cRational.define("abs", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&object.Rational{R: new(big.Rat).Abs(rval(self).R)})
	})
	vm.cRational.define("<=>", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.ratCompare(rval(self), args[0])
	})
	vm.cRational.define("**", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := rval(self).R
		if ef, ok := object.AsFloatOK(args[0]); ok { // Float exponent → Float result
			rf, _ := r.Float64()
			return object.FloatValue(float64(object.Float(math.Pow(rf, float64(ef)))))
		}
		eb, ok := toRat(args[0])
		if !ok {
			return raise("TypeError", "%s can't be coerced into Rational", args[0].Inspect())
		}
		return ratPow(r, eb)
	})
	vm.cRational.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(rval(self).ToS()))
	})
	vm.cRational.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(rval(self).Inspect()))
	})
}
