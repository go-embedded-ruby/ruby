package vm

import (
	"math"
	"math/big"
	"math/cmplx"

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
	switch n := v.(type) {
	case object.Integer:
		return new(big.Rat).SetInt64(int64(n)), true
	case *object.Bignum:
		return new(big.Rat).SetInt(n.I), true
	case *object.Rational:
		return n.R, true
	}
	return nil, false
}

// rationalComponent resolves a Kernel#Rational() argument to an exact *big.Rat,
// raising the MRI-matching error on failure. An Integer/Bignum is n/1; a Float
// uses its exact binary value (a non-finite Float is a FloatDomainError); a
// Rational passes through; a String is parsed with String#to_r's grammar (an
// invalid one is an ArgumentError, matching MRI's "invalid value for convert()");
// a Complex contributes its real part when the imaginary part is zero and is a
// RangeError otherwise. Any other object is converted through #to_r (which must
// return a Rational) or, failing that, #to_int; a value answering neither is a
// TypeError. Method lookup uses the static table so a BasicObject without the
// method fails cleanly rather than dispatching a respond_to? it lacks.
func (vm *VM) rationalComponent(v object.Value) *big.Rat {
	switch n := v.(type) {
	case object.Integer:
		return new(big.Rat).SetInt64(int64(n))
	case *object.Bignum:
		return new(big.Rat).SetInt(n.I)
	case object.Float:
		f := float64(n)
		if math.IsInf(f, 0) || math.IsNaN(f) {
			raise("FloatDomainError", "%s", object.Float(n).ToS())
		}
		return new(big.Rat).SetFloat64(f)
	case *object.Rational:
		return new(big.Rat).Set(n.R)
	case *object.String:
		r, ok := stringToR(n.Str(), true)
		if !ok {
			raise("ArgumentError", "invalid value for convert(): %s", n.Inspect())
		}
		return r.R
	case *object.Complex:
		if !imagNumericZero(n.Im) {
			raise("RangeError", "can't convert %s into Rational", n.Inspect())
		}
		return vm.rationalComponent(n.Re)
	case object.Nil, object.Bool:
		// MRI's Rational() rejects nil/true/false with a TypeError even though
		// NilClass#to_r (and the others) exist, so the #to_r fallback is skipped.
		raise("TypeError", "can't convert %s into Rational", vm.inspectName(v))
	}
	if vm.findMethod(v, "to_r") != nil {
		res := vm.send(v, "to_r", nil, nil)
		if r, ok := res.(*object.Rational); ok {
			return new(big.Rat).Set(r.R)
		}
		name := vm.classOf(v).name
		raise("TypeError", "can't convert %s to Rational (%s#to_r gives %s)", name, name, vm.classOf(res).name)
	}
	if vm.findMethod(v, "to_int") != nil {
		return vm.rationalComponent(vm.send(v, "to_int", nil, nil))
	}
	raise("TypeError", "can't convert %s into Rational", vm.inspectName(v))
	return nil
}

// makeRational builds the Rational for a validated Kernel#Rational() call: one
// argument is that value coerced to a Rational; two arguments divide the first by
// the second (a zero divisor is a ZeroDivisionError). Every failure path raises,
// so the constructor can wrap it for exception: false.
func (vm *VM) makeRational(args []object.Value) object.Value {
	num := vm.rationalComponent(args[0])
	if len(args) == 1 {
		return &object.Rational{R: num}
	}
	den := vm.rationalComponent(args[1])
	if den.Sign() == 0 {
		return raise("ZeroDivisionError", "divided by 0")
	}
	return &object.Rational{R: new(big.Rat).Quo(num, den)}
}

// inspectName names a value for a coercion error message: nil/true/false by
// their literal, and any other value by its class name (matching MRI's "can't
// convert X into Rational").
func (vm *VM) inspectName(v object.Value) string {
	switch v.(type) {
	case object.Nil:
		return "nil"
	case object.Bool:
		return v.ToS()
	}
	return vm.classOf(v).name
}

// rationalOp applies an arithmetic operator when at least one operand is a
// Rational. A Float operand drops to float arithmetic; otherwise the result is
// an exact Rational.
func rationalOp(op bytecode.Op, a, b object.Value) object.Value {
	if _, ok := a.(object.Float); ok {
		return ratFloatOp(op, complexFloat(a), complexFloat(b))
	}
	if _, ok := b.(object.Float); ok {
		return ratFloatOp(op, complexFloat(a), complexFloat(b))
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
		return &object.Rational{R: new(big.Rat).Add(ra, rb)}
	case bytecode.OpSub:
		return &object.Rational{R: new(big.Rat).Sub(ra, rb)}
	case bytecode.OpMul:
		return &object.Rational{R: new(big.Rat).Mul(ra, rb)}
	case bytecode.OpDiv:
		if rb.Sign() == 0 {
			return raise("ZeroDivisionError", "divided by 0")
		}
		return &object.Rational{R: new(big.Rat).Quo(ra, rb)}
	case bytecode.OpMod:
		if rb.Sign() == 0 {
			return raise("ZeroDivisionError", "divided by 0")
		}
		// a - b·floor(a/b): the remainder takes the divisor's sign (Ruby's %).
		q := new(big.Rat).Quo(ra, rb)
		fl := new(big.Int).Div(q.Num(), q.Denom()) // Euclidean ⇒ floor (denom > 0)
		m := new(big.Rat).Sub(ra, new(big.Rat).Mul(rb, new(big.Rat).SetInt(fl)))
		return &object.Rational{R: m}
	}
	return raise("NoMethodError", "undefined method '%s' for a Rational", op)
}

// ratFloatOp handles a Rational⊕Float (or Float⊕Rational) operation, where the
// Float wins the tower. It matches Float arithmetic except that Rational#% (and
// Rational#/ against an integer 0 is caught earlier) raises ZeroDivisionError on
// a zero divisor rather than yielding NaN — MRI's Rational modulo is strict even
// when the operand is a Float, unlike Float#%.
func ratFloatOp(op bytecode.Op, a, b float64) object.Value {
	if op == bytecode.OpMod && b == 0 {
		return raise("ZeroDivisionError", "divided by 0")
	}
	return floatOp(op, a, b)
}

// ratPow raises ra to rb. An integer exponent gives an exact Rational; a
// fractional exponent falls to float.
func ratPow(ra, rb *big.Rat) object.Value {
	if ra.Sign() == 0 && rb.Sign() < 0 {
		return raise("ZeroDivisionError", "divided by 0") // 0 ** negative
	}
	if !rb.IsInt() {
		af, _ := ra.Float64()
		bf, _ := rb.Float64()
		return realPow(af, bf)
	}
	e := rb.Num()
	base := ra
	if e.Sign() < 0 {
		// ra is non-zero here: the zero-base/negative-exponent case is caught above.
		base = new(big.Rat).Inv(ra)
		e = new(big.Int).Neg(e)
	}
	// e is now non-negative. A base of 0 or ±1 stays exact and cheap regardless of
	// exponent size (0**e = 0, (±1)**e = ±1). For any other base, an exponent that
	// does not fit a machine word makes the exact power astronomically large; Ruby
	// 3.4 refuses it with ArgumentError rather than attempt an unbounded
	// big.Int.Exp (which would exhaust memory).
	if e.BitLen() > 32 && base.Num().CmpAbs(base.Denom()) != 0 && base.Sign() != 0 {
		return raise("ArgumentError", "exponent is too large")
	}
	pn := new(big.Int).Exp(base.Num(), e, nil)
	pd := new(big.Int).Exp(base.Denom(), e, nil)
	return &object.Rational{R: new(big.Rat).SetFrac(pn, pd)}
}

// realPow raises the real base to the real exponent. A negative base with a
// non-whole exponent has no real value, so MRI returns the principal Complex
// root (exp(exp·ln base)); every other case yields a Float. This is why
// Rational(-3, 2) ** 1.5 and Rational(-3, 4) ** Rational(-4, 3) are Complex.
func realPow(base, exp float64) object.Value {
	if base < 0 && exp != math.Trunc(exp) {
		p := cmplx.Pow(complex(base, 0), complex(exp, 0))
		return &object.Complex{Re: object.Float(real(p)), Im: object.Float(imag(p))}
	}
	return object.Float(math.Pow(base, exp))
}

// rationalEqual reports equality, coercing an Integer (Rational(2,1) == 2) or a
// Float (Rational(1,2) == 0.5).
func rationalEqual(r *object.Rational, other object.Value) bool {
	if orat, ok := toRat(other); ok {
		return r.R.Cmp(orat) == 0
	}
	if of, ok := other.(object.Float); ok {
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
	if of, ok := other.(object.Float); ok {
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
	// A non-numeric object that answers #coerce is compared through the coerced
	// pair (MRI does not rescue an exception raised by #coerce); anything else is
	// incomparable and yields nil.
	if vm.respondsToDynamic(other, "coerce") {
		pair := vm.send(other, "coerce", []object.Value{r}, nil)
		if arr, ok := pair.(*object.Array); ok && len(arr.Elems) == 2 {
			return vm.send(arr.Elems[0], "<=>", []object.Value{arr.Elems[1]}, nil)
		}
	}
	return object.NilV
}

// roundMode selects how ratRoundInt collapses a Rational to an Integer.
type roundMode int

const (
	modeFloor roundMode = iota
	modeCeil
	modeTrunc
	modeHalfUp   // ties away from zero (Ruby default)
	modeHalfDown // ties toward zero
	modeHalfEven // ties to the even neighbour (banker's rounding)
)

// ratRoundInt rounds a normalised *big.Rat to a *big.Int according to mode.
func ratRoundInt(r *big.Rat, mode roundMode) *big.Int {
	num, den := r.Num(), r.Denom() // den > 0
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(num, den, rem) // q truncates toward zero; rem carries num's sign
	if rem.Sign() == 0 {
		return q
	}
	one := big.NewInt(1)
	switch mode {
	case modeTrunc:
		return q
	case modeFloor:
		if num.Sign() < 0 {
			q.Sub(q, one)
		}
		return q
	case modeCeil:
		if num.Sign() > 0 {
			q.Add(q, one)
		}
		return q
	}
	// Half modes: compare 2·|rem| with den to place the fraction against ½.
	twice := new(big.Int).Abs(rem)
	twice.Lsh(twice, 1)
	cmp := twice.Cmp(den)
	away := false
	switch mode {
	case modeHalfUp:
		away = cmp >= 0
	case modeHalfDown:
		away = cmp > 0
	case modeHalfEven:
		away = cmp > 0 || (cmp == 0 && q.Bit(0) == 1)
	}
	if away {
		if num.Sign() < 0 {
			q.Sub(q, one)
		} else {
			q.Add(q, one)
		}
	}
	return q
}

// pow10Big returns 10**n as a *big.Int (n >= 0).
func pow10Big(n int64) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(n), nil)
}

// ratTerminatesWithin reports whether r is exactly representable with at most n
// digits after the decimal point — i.e. its reduced denominator is 2**a·5**b
// with max(a, b) <= n. It lets floor/round/ceil/truncate short-circuit to r for
// a large precision without materialising an astronomically large 10**n.
func ratTerminatesWithin(r *big.Rat, n int64) bool {
	d := new(big.Int).Set(r.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	zero, rem := new(big.Int), new(big.Int)
	var a, b int64
	for {
		q, m := new(big.Int).QuoRem(d, two, rem)
		if m.Cmp(zero) != 0 {
			break
		}
		d, a = q, a+1
	}
	for {
		q, m := new(big.Int).QuoRem(d, five, rem)
		if m.Cmp(zero) != 0 {
			break
		}
		d, b = q, b+1
	}
	return d.Cmp(big.NewInt(1)) == 0 && a <= n && b <= n
}

// ratWithDigits applies floor/ceil/round/truncate at ndigits precision. With
// ndigits <= 0 it returns an Integer (rounded to the nearest 10**(-ndigits));
// with ndigits > 0 it returns a Rational carrying up to ndigits fractional
// digits, matching MRI.
func ratWithDigits(r *big.Rat, ndigits int64, mode roundMode) object.Value {
	if ndigits == 0 {
		return object.NormInt(ratRoundInt(r, mode))
	}
	if ndigits < 0 {
		pow := pow10Big(-ndigits)
		scaled := new(big.Rat).Quo(r, new(big.Rat).SetInt(pow))
		m := ratRoundInt(scaled, mode)
		return object.NormInt(new(big.Int).Mul(m, pow))
	}
	if ratTerminatesWithin(r, ndigits) {
		return &object.Rational{R: new(big.Rat).Set(r)}
	}
	pow := pow10Big(ndigits)
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt(pow))
	m := ratRoundInt(scaled, mode)
	return &object.Rational{R: new(big.Rat).SetFrac(m, pow)}
}

// ratHalfNames maps the recognised `half:` keyword symbols to their tie-breaking
// mode; a nil value (or an absent keyword) also selects modeHalfUp.
var ratHalfNames = map[object.Symbol]roundMode{
	"up":   modeHalfUp,
	"down": modeHalfDown,
	"even": modeHalfEven,
}

// ratHalfMode reads a `half:` keyword (nil/:up/:down/:even) from a trailing
// options Hash, defaulting to half-up. It raises ArgumentError on an unknown
// mode, matching MRI's "invalid rounding mode: <name>".
func ratHalfMode(args []object.Value) roundMode {
	h := lastHashOrNil(args)
	if h == nil {
		return modeHalfUp
	}
	v, ok := h.Get(object.Symbol("half"))
	if !ok {
		return modeHalfUp
	}
	if _, isNil := v.(object.Nil); isNil {
		return modeHalfUp
	}
	sym, isSym := v.(object.Symbol)
	m, known := ratHalfNames[sym]
	if !isSym || !known {
		name := v.Inspect()
		if isSym {
			name = string(sym)
		}
		raise("ArgumentError", "invalid rounding mode: %s", name)
	}
	return m
}

// ratPrecision extracts the positional ndigits argument (skipping a trailing
// keyword Hash), defaulting to 0. A non-Integer precision raises TypeError
// without consulting #to_int, matching MRI ("not an integer").
func ratPrecision(args []object.Value) int64 {
	for _, a := range args {
		if _, ok := a.(*object.Hash); ok {
			continue
		}
		if n, ok := a.(object.Integer); ok {
			return int64(n)
		}
		raise("TypeError", "not an integer")
	}
	return 0
}

// registerRational installs Kernel#Rational and the Rational instance methods.
func (vm *VM) registerRational() {
	vm.cObject.define("Rational", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		args, doRaise := popExceptionKwarg(args)
		return vm.numericCtor(doRaise, func() object.Value { return vm.makeRational(args) })
	})

	// String#to_r parses a rational literal leniently (unrecognised input yields
	// Rational(0, 1)); a zero denominator ("1/0") raises ZeroDivisionError.
	vm.cString.define("to_r", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		r, _ := stringToR(self.(*object.String).Str(), false)
		return r
	})

	rval := func(self object.Value) *object.Rational { return self.(*object.Rational) }

	vm.cRational.define("numerator", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NormInt(rval(self).R.Num())
	})
	vm.cRational.define("denominator", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NormInt(rval(self).R.Denom())
	})
	vm.cRational.define("to_f", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		f, _ := rval(self).R.Float64()
		return object.Float(f)
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
	vm.cRational.define("to_c", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Complex{Re: self, Im: object.IntValue(0)}
	})
	vm.cRational.define("abs", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Rational{R: new(big.Rat).Abs(rval(self).R)}
	})
	vm.cRational.define("<=>", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.ratCompare(rval(self), args[0])
	})
	vm.cRational.define("**", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := rval(self).R
		if ef, ok := args[0].(object.Float); ok { // Float exponent
			rf, _ := r.Float64()
			return realPow(rf, float64(ef))
		}
		eb, ok := toRat(args[0])
		if !ok {
			// A non-numeric exponent answering #coerce goes through the coercion
			// protocol (self ** other → a2 ** b2 on the coerced pair).
			if res, ok := vm.tryNumericCoerce("**", self, args[0]); ok {
				return res
			}
			return raise("TypeError", "%s can't be coerced into Rational", args[0].Inspect())
		}
		return ratPow(r, eb)
	})
	vm.cRational.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(rval(self).ToS())
	})
	vm.cRational.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(rval(self).Inspect())
	})

	// magnitude is a true alias of abs: it shares the identical Method record, so
	// Rational.instance_method(:magnitude) == Rational.instance_method(:abs).
	vm.cRational.methods["magnitude"] = vm.cRational.methods["abs"]

	vm.cRational.define("zero?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(rval(self).R.Sign() == 0)
	})
	vm.cRational.define("integer?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.False // a Rational is never an Integer, even Rational(2, 1)
	})

	vm.cRational.define("floor", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return ratWithDigits(rval(self).R, ratPrecision(args), modeFloor)
	})
	vm.cRational.define("ceil", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return ratWithDigits(rval(self).R, ratPrecision(args), modeCeil)
	})
	vm.cRational.define("truncate", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return ratWithDigits(rval(self).R, ratPrecision(args), modeTrunc)
	})
	vm.cRational.define("round", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return ratWithDigits(rval(self).R, ratPrecision(args), ratHalfMode(args))
	})

	// div / divmod / modulo work over an Integer, Rational or Float divisor
	// (Float promotes to Float results), each raising ZeroDivisionError on a zero
	// divisor — including a Float 0.0, which Float#div/#% would not.
	vm.cRational.define("div", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 1 {
			return raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		q, _ := ratDivmod(rval(self).R, args[0])
		return q
	})
	vm.cRational.define("divmod", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		q, m := ratDivmod(rval(self).R, args[0])
		return object.NewArray(q, m)
	})
	vm.cRational.define("modulo", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_, m := ratDivmod(rval(self).R, args[0])
		return m
	})
	vm.cRational.define("%", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_, m := ratDivmod(rval(self).R, args[0])
		return m
	})

	// Rational#/ is a real method (so Rational.instance_method(:/) resolves)
	// routed through the operator fast path, and #quo is its true alias sharing
	// the one Method record — Rational.instance_method(:quo) ==
	// Rational.instance_method(:/).
	vm.cRational.define("/", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.binaryOp(bytecode.OpDiv, self, args[0])
	})
	vm.cRational.methods["quo"] = vm.cRational.methods["/"]

	// marshal_dump is the private hook Marshal uses to serialise a Rational: the
	// two-element [numerator, denominator] array (MRI's Rational#marshal_dump).
	vm.cRational.define("marshal_dump", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		r := rval(self).R
		return object.NewArray(object.NormInt(r.Num()), object.NormInt(r.Denom()))
	})
	vm.setInstanceVisibility(vm.cRational, "marshal_dump", visPrivate)

	vm.cRational.define("coerce", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		other := args[0]
		if orat, ok := toRat(other); ok {
			return object.NewArray(&object.Rational{R: new(big.Rat).Set(orat)}, self)
		}
		if of, ok := other.(object.Float); ok {
			sf, _ := rval(self).R.Float64()
			return object.NewArray(of, object.Float(sf))
		}
		return raise("TypeError", "%s can't be coerced into Rational", vm.classOf(other).name)
	})

	vm.cRational.define("rationalize", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 1 {
			return raise("ArgumentError", "wrong number of arguments (given %d, expected 0..1)", len(args))
		}
		r := rval(self).R
		if len(args) == 0 {
			return self
		}
		eps, ok := toRatOrFloatRat(args[0])
		if !ok {
			return raise("TypeError", "%s can't be coerced into Rational", args[0].Inspect())
		}
		return &object.Rational{R: ratRationalize(r, eps)}
	})

	// Rational has no public allocator: Rational(...) is the only constructor, so
	// Rational.new is shadowed to raise NoMethodError (MRI undefines it).
	vm.cRational.smethods["new"] = &Method{name: "new", owner: vm.cRational,
		native: func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NoMethodError", "undefined method 'new' for class 'Rational'")
		}}
}

// bigFromFloor returns floor(f) as a *big.Int (used for Rational#div against a
// Float divisor, where the quotient is a whole Float).
func bigFromFloor(f float64) *big.Int {
	bf := big.NewFloat(math.Floor(f))
	z, _ := bf.Int(nil)
	return z
}

// ratDivmod computes the floored quotient and remainder of the Rational `ra`
// divided by `other`. The quotient is always an Integer; the remainder is a
// Rational for an Integer/Rational divisor and a Float for a Float divisor. A
// zero divisor raises ZeroDivisionError, and a non-numeric divisor TypeError.
func ratDivmod(ra *big.Rat, other object.Value) (q, m object.Value) {
	if rb, ok := toRat(other); ok {
		if rb.Sign() == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		quo := new(big.Rat).Quo(ra, rb)
		fl := ratRoundInt(quo, modeFloor)
		rem := new(big.Rat).Sub(ra, new(big.Rat).Mul(rb, new(big.Rat).SetInt(fl)))
		return object.NormInt(fl), &object.Rational{R: rem}
	}
	of, ok := other.(object.Float)
	if !ok {
		raise("TypeError", "%s can't be coerced into Rational", other.Inspect())
	}
	if of == 0 {
		raise("ZeroDivisionError", "divided by 0")
	}
	af, _ := ra.Float64()
	d := math.Floor(af / float64(of))
	// The quotient is a whole number (Integer, as MRI); the remainder is Float.
	return object.NormInt(bigFromFloor(d)), object.Float(af - float64(of)*d)
}

// toRatOrFloatRat converts an Integer/Rational (exact) or Float (via its exact
// binary value) argument to a *big.Rat; ok is false for anything else.
func toRatOrFloatRat(v object.Value) (*big.Rat, bool) {
	if r, ok := toRat(v); ok {
		return new(big.Rat).Set(r), true
	}
	if f, ok := v.(object.Float); ok {
		return new(big.Rat).SetFloat64(float64(f)), true
	}
	return nil, false
}

// ratRationalize returns the simplest Rational within |eps| of x — MRI's
// Rational#rationalize(eps). eps of 0 (or an interval collapsing to a point)
// yields x itself.
func ratRationalize(x, eps *big.Rat) *big.Rat {
	e := new(big.Rat).Abs(eps)
	lo := new(big.Rat).Sub(x, e)
	hi := new(big.Rat).Add(x, e)
	if lo.Cmp(hi) == 0 {
		return new(big.Rat).Set(x)
	}
	switch {
	case lo.Sign() <= 0 && hi.Sign() >= 0:
		return new(big.Rat) // 0 lies in the interval → simplest is 0
	case hi.Sign() < 0:
		// simplestRatBetween requires a non-negative interval, so reflect through 0.
		neg := simplestRatBetween(new(big.Rat).Neg(hi), new(big.Rat).Neg(lo))
		return neg.Neg(neg)
	default:
		return simplestRatBetween(lo, hi)
	}
}
