// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	bigdecimal "github.com/go-ruby-bigdecimal/bigdecimal"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and github.com/go-ruby-bigdecimal/bigdecimal — an MRI-4.0.5-byte-exact
// arbitrary-precision decimal, a sibling of go-ruby-regexp / go-ruby-erb /
// go-ruby-yaml / go-ruby-json. The decimal semantics (the canonical 0.15e1
// scientific to_s, the BigDecimal::ROUND_* modes, floor/ceil/truncate at an
// arbitrary digit, div with an explicit precision, divmod/modulo/remainder)
// live in that library; rbgo only wraps a *bigdecimal.Decimal in the Ruby
// BigDecimal object and re-raises the library's typed errors as the matching
// Ruby exception (ErrZeroDivision -> ZeroDivisionError, a parse failure ->
// ArgumentError). This replaces rbgo's former internal/vm/bigdecimal.go, a thin
// shim over go-composites/bigfloat that rendered a plain decimal ("1.5") and
// rounded only to whole integers, rather than MRI's scientific "0.15e1" and the
// digit-and-mode-taking round family — so the binding is an upgrade to the real
// MRI behaviour.

// BigDecimal is the Ruby wrapper around a *bigdecimal.Decimal.
type BigDecimal struct {
	d *bigdecimal.Decimal
}

func (b *BigDecimal) ToS() string     { return b.d.ToS("") }
func (b *BigDecimal) Inspect() string { return b.d.ToS("") }
func (b *BigDecimal) Truthy() bool    { return true }

// roundModes maps the MRI BigDecimal::ROUND_* integer codes onto the library's
// RoundMode. The codes are MRI's own numbering (ROUND_UP == 1, ROUND_DOWN == 2,
// ...); registerBigDecimal installs them as the BigDecimal::ROUND_* constants.
var roundModes = map[int64]bigdecimal.RoundMode{
	1: bigdecimal.RoundUp,
	2: bigdecimal.RoundDown,
	3: bigdecimal.RoundHalfUp,
	4: bigdecimal.RoundHalfDown,
	5: bigdecimal.RoundHalfEven,
	6: bigdecimal.RoundCeiling,
	7: bigdecimal.RoundFloor,
}

// newDecimalString parses a String literal at full precision, raising a Ruby
// ArgumentError (carrying the library's message) when the literal is malformed —
// the BigDecimal counterpart of the date/json bindings re-raising a typed error.
func newDecimalString(s string) *BigDecimal {
	d, err := bigdecimal.New(s)
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	return &BigDecimal{d: d}
}

// asBigDecimal coerces a Ruby operand to a *bigdecimal.Decimal: a BigDecimal
// passes through, while an Integer / Bignum / Float / Rational is promoted
// through its exact decimal string. ok is false for a non-numeric, non-BigDecimal
// value.
func asBigDecimal(v object.Value) (*bigdecimal.Decimal, bool) {
	switch x := v.(type) {
	case *BigDecimal:
		return x.d, true
	case object.Integer:
		return bigdecimal.FromInt(int64(x)), true
	case *object.Bignum:
		return bigdecimal.FromBigInt(x.I), true
	case object.Float:
		return floatToDecimal(float64(x)), true
	case *object.Rational:
		f, _ := x.R.Float64()
		return floatToDecimal(f), true
	}
	return nil, false
}

// floatToDecimal coerces a Go float64 to a Decimal through its shortest decimal
// string (object.Float#to_s), so the promotion is exact for the value the float
// prints — the shared path for a Float operand and a Rational (taken at its
// float64 value, as MRI compares against the float). object.Float#to_s always
// renders a string the library parses (a finite literal, "NaN", "Infinity" or
// "-Infinity"), so the parse cannot fail.
func floatToDecimal(f float64) *bigdecimal.Decimal {
	d, _ := bigdecimal.New(object.Float(f).ToS())
	return d
}

// bigDecimalArg coerces an operand to a *bigdecimal.Decimal, raising TypeError
// otherwise — the BigDecimal counterpart of strArg / intArg.
func bigDecimalArg(v object.Value) *bigdecimal.Decimal {
	d, ok := asBigDecimal(v)
	if !ok {
		raise("TypeError", "%s can't be coerced into BigDecimal", v.Inspect())
	}
	return d
}

// divChecked is d / o via the library's fallible DivE, re-raising the library's
// ErrZeroDivision (finite divided by zero) as a Ruby ZeroDivisionError. prec is
// the requested significant-digit precision (<= 0 for the bare `/`).
func divChecked(d, o *bigdecimal.Decimal, prec int) *bigdecimal.Decimal {
	q, err := d.DivE(o, prec)
	if err != nil {
		raise("ZeroDivisionError", "%s", err.Error())
	}
	return q
}

// bigDecimalOp implements the BigDecimal operator fast path reached from
// binary(): + - * / % delegate to Add/Sub/Mul/DivE/Mod, coercing the right
// operand to a Decimal. Division or modulo by a finite zero surfaces
// ErrZeroDivision as a Ruby ZeroDivisionError. The bare % operator routes here
// (rather than through the `%` method) because it has a bytecode opcode.
func bigDecimalOp(op bytecode.Op, a *BigDecimal, b object.Value) object.Value {
	rhs := bigDecimalArg(b)
	switch op {
	case bytecode.OpAdd:
		return &BigDecimal{d: a.d.Add(rhs)}
	case bytecode.OpSub:
		return &BigDecimal{d: a.d.Sub(rhs)}
	case bytecode.OpMul:
		return &BigDecimal{d: a.d.Mul(rhs)}
	case bytecode.OpDiv:
		return &BigDecimal{d: divChecked(a.d, rhs, 0)}
	case bytecode.OpMod:
		return &BigDecimal{d: modChecked(a.d, rhs)}
	}
	return raise("NoMethodError", "undefined method '%s' for a BigDecimal", op)
}

// bigDecimalRightOp is the operator fast path when only the RIGHT operand is a
// BigDecimal (e.g. 2 + BigDecimal("1.5"), Rational(1, 2) + BigDecimal("1.5")):
// the left operand is coerced to a Decimal and the operation applied in the
// original order, so the result is a BigDecimal as MRI's numeric tower dictates.
func bigDecimalRightOp(op bytecode.Op, a object.Value, b *BigDecimal) object.Value {
	lhs, ok := asBigDecimal(a)
	if !ok {
		raise("TypeError", "%s can't be coerced into BigDecimal", a.Inspect())
	}
	return bigDecimalOp(op, &BigDecimal{d: lhs}, b)
}

// modChecked is the floored modulo a % o via the library's Mod, re-raising
// ErrZeroDivision (finite modulo zero) as a Ruby ZeroDivisionError.
func modChecked(a, o *bigdecimal.Decimal) *bigdecimal.Decimal {
	r, err := a.Mod(o)
	if err != nil {
		raise("ZeroDivisionError", "%s", err.Error())
	}
	return r
}

// bigDecimalEqual reports BigDecimal equality for valueEqual / the == operator
// fast path, coercing a numeric operand (BigDecimal("2") == 2) and reporting
// false for a non-numeric one or a NaN (Cmp == -2).
func bigDecimalEqual(a *BigDecimal, other object.Value) bool {
	b, ok := asBigDecimal(other)
	return ok && a.d.Cmp(b) == 0
}

// bigDecimalCmp returns the BigDecimal#<=> result for two operands: an Integer
// (-1/0/1) when comparable, or nil when either is NaN (Cmp == -2, MRI's nil).
func bigDecimalCmp(a, b *bigdecimal.Decimal) object.Value {
	c := a.Cmp(b)
	if c == -2 {
		return object.NilV
	}
	return object.IntValue(int64(c))
}

// decimalToInteger converts a Decimal known to be integer-valued to a Ruby
// Integer / Bignum via its exact big.Int (Int truncates toward zero), used for
// to_i and for the no-arg / n<=0 round family that MRI returns as an Integer.
func decimalToInteger(d *bigdecimal.Decimal) object.Value {
	return object.NormInt(d.Int())
}

// roundFamily implements floor / ceil / truncate: with NO argument MRI returns
// an Integer, and with ANY explicit digit count (including 0) it returns a
// BigDecimal at the requested place. (round differs at n == 0 / a negative n
// and is handled inline.) fn applies the named operation at digit n.
func roundFamily(d *bigdecimal.Decimal, args []object.Value, fn func(n int) *bigdecimal.Decimal) object.Value {
	if len(args) == 0 {
		return decimalToInteger(fn(0))
	}
	return &BigDecimal{d: fn(int(intArg(args[0])))}
}

// registerBigDecimal installs Kernel#BigDecimal, the BigDecimal instance methods
// and the BigDecimal::ROUND_* / SIGN_* / NAN / INFINITY constants, all delegating
// to the go-ruby-bigdecimal library.
func (vm *VM) registerBigDecimal() {
	vm.cBigDecimal = newClass("BigDecimal", vm.cObject)
	vm.consts["BigDecimal"] = vm.cBigDecimal

	// Kernel#BigDecimal(value[, ndigits]): MRI's constructor is the global
	// method, not BigDecimal.new. A String is parsed at full precision (bad ->
	// ArgumentError); an Integer/Bignum is exact; a Float REQUIRES the ndigits
	// significant-digit argument (MRI raises ArgumentError without it); a
	// BigDecimal passes through.
	vm.cObject.define("BigDecimal", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		switch v := args[0].(type) {
		case *BigDecimal:
			return v
		case *object.String:
			return newDecimalString(v.Str())
		case object.Integer:
			return &BigDecimal{d: bigdecimal.FromInt(int64(v))}
		case *object.Bignum:
			return &BigDecimal{d: bigdecimal.FromBigInt(v.I)}
		case object.Float:
			if len(args) < 2 {
				return raise("ArgumentError", "can't omit precision for a Float")
			}
			d, err := bigdecimal.FromFloat(float64(v), int(intArg(args[1])))
			if err != nil {
				return raise("ArgumentError", "%s", err.Error())
			}
			return &BigDecimal{d: d}
		}
		return raise("TypeError", "can't convert %s into BigDecimal", args[0].Inspect())
	})

	// BigDecimal::ROUND_* (MRI's own numbering) and the SIGN_* / NAN / INFINITY
	// constants. NAN / INFINITY are themselves BigDecimal values.
	for name, code := range map[string]int64{
		"ROUND_UP": 1, "ROUND_DOWN": 2, "ROUND_HALF_UP": 3, "ROUND_HALF_DOWN": 4,
		"ROUND_HALF_EVEN": 5, "ROUND_CEILING": 6, "ROUND_FLOOR": 7,
		"SIGN_NaN": 0, "SIGN_POSITIVE_ZERO": 1, "SIGN_NEGATIVE_ZERO": -1,
		"SIGN_POSITIVE_FINITE": 2, "SIGN_NEGATIVE_FINITE": -2,
		"SIGN_POSITIVE_INFINITE": 3, "SIGN_NEGATIVE_INFINITE": -3,
	} {
		vm.cBigDecimal.consts[name] = object.IntValue(code)
	}
	vm.cBigDecimal.consts["NAN"] = newDecimalString("NaN")
	vm.cBigDecimal.consts["INFINITY"] = newDecimalString("Infinity")

	d := func(name string, fn NativeFn) { vm.cBigDecimal.define(name, fn) }
	self := func(v object.Value) *BigDecimal { return v.(*BigDecimal) }

	// Arithmetic mirrors the operator fast path (bigDecimalOp) so `send(:+, x)`
	// agrees with the `a + x` syntax.
	d("+", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return bigDecimalOp(bytecode.OpAdd, self(v), args[0])
	})
	d("-", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return bigDecimalOp(bytecode.OpSub, self(v), args[0])
	})
	d("*", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return bigDecimalOp(bytecode.OpMul, self(v), args[0])
	})
	d("/", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return bigDecimalOp(bytecode.OpDiv, self(v), args[0])
	})

	// abs (-> Abs) and unary -@ (-> Neg).
	d("abs", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &BigDecimal{d: self(v).d.Abs()}
	})
	d("-@", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &BigDecimal{d: self(v).d.Neg()}
	})

	// ** / pow (n) -> Pow(n): the receiver to an Integer exponent (negative ->
	// reciprocal; 0**0 == 1). The VM has no power opcode, so ** dispatches as an
	// ordinary method send. The exponent must be an Integer (intArg raises
	// TypeError otherwise).
	powFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &BigDecimal{d: self(v).d.Pow(int(intArg(args[0])))}
	}
	d("**", powFn)
	d("power", powFn) // MRI's named form
	d("pow", powFn)   // rbgo convenience alias, retained from the former binding

	// div(o[, prec]): with a precision argument, the quotient to that many
	// significant digits as a BigDecimal; with none, MRI's integer division
	// (floor of the quotient) as a BigDecimal. Finite-by-zero -> ZeroDivisionError.
	d("div", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		o := bigDecimalArg(args[0])
		if len(args) >= 2 {
			return &BigDecimal{d: divChecked(self(v).d, o, int(intArg(args[1])))}
		}
		q, err := self(v).d.IDiv(o)
		if err != nil {
			raise("ZeroDivisionError", "%s", err.Error())
		}
		return &BigDecimal{d: q}
	})
	// % / modulo (-> Mod): the floored remainder (sign of the divisor).
	modFn := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &BigDecimal{d: modChecked(self(v).d, bigDecimalArg(args[0]))}
	}
	d("%", modFn)
	d("modulo", modFn)
	// remainder (-> Remainder): the truncated remainder (sign of the receiver).
	d("remainder", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		r, err := self(v).d.Remainder(bigDecimalArg(args[0]))
		if err != nil {
			raise("ZeroDivisionError", "%s", err.Error())
		}
		return &BigDecimal{d: r}
	})
	// divmod (-> DivMod): [floored quotient, floored modulo], both BigDecimal.
	d("divmod", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		q, r, err := self(v).d.DivMod(bigDecimalArg(args[0]))
		if err != nil {
			raise("ZeroDivisionError", "%s", err.Error())
		}
		return &object.Array{Elems: []object.Value{&BigDecimal{d: q}, &BigDecimal{d: r}}}
	})

	// floor / ceil / truncate / round: no-arg or n<=0 -> Integer, n>0 -> BigDecimal
	// (round also takes an optional RoundMode; the default is half-up).
	d("floor", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return roundFamily(self(v).d, args, self(v).d.Floor)
	})
	d("ceil", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return roundFamily(self(v).d, args, self(v).d.Ceil)
	})
	d("truncate", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return roundFamily(self(v).d, args, self(v).d.Truncate)
	})
	d("round", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		// MRI: round with no argument, or a digit count <= 0 given WITHOUT an
		// explicit rounding mode, returns an Integer; a positive digit count, or
		// ANY digit count once an explicit mode is passed, returns a BigDecimal.
		if len(args) == 0 {
			return decimalToInteger(self(v).d.Round(0, bigdecimal.RoundHalfUp))
		}
		n := int(intArg(args[0]))
		mode := bigdecimal.RoundHalfUp
		hasMode := len(args) >= 2
		if hasMode {
			m, ok := roundModes[intArg(args[1])]
			if !ok {
				raise("ArgumentError", "invalid rounding mode")
			}
			mode = m
		}
		r := self(v).d.Round(n, mode)
		if n <= 0 && !hasMode {
			return decimalToInteger(r)
		}
		return &BigDecimal{d: r}
	})

	// frac (-> Frac) / fix (-> Fix): the fractional / integer part, each a BigDecimal.
	d("frac", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &BigDecimal{d: self(v).d.Frac()}
	})
	d("fix", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &BigDecimal{d: self(v).d.Fix()}
	})

	// to_f (-> Float64), to_i / to_int (-> Int, truncated toward zero, Integer/Bignum),
	// to_r (-> Rat, a Rational).
	d("to_f", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).d.Float64())
	})
	toIFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return decimalToInteger(self(v).d)
	}
	d("to_i", toIFn)
	d("to_int", toIFn)
	d("to_r", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Rational{R: self(v).d.Rat()}
	})

	// to_s(fmt) / inspect: the MRI to_s grammar; the bare form is the scientific
	// "0.15e1". inspect renders the same bare value (Array#inspect / p show it
	// unquoted).
	d("to_s", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		fmt := ""
		if len(args) >= 1 {
			fmt = strArg(args[0])
		}
		return object.NewString(self(v).d.ToS(fmt))
	})
	d("inspect", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).d.ToS(""))
	})

	// split (-> SplitParts): [sign, "digits", base(10), exponent].
	d("split", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		sign, digits, base, exp := self(v).d.SplitParts()
		return &object.Array{Elems: []object.Value{
			object.IntValue(int64(sign)), object.NewString(digits),
			object.IntValue(int64(base)), object.IntValue(int64(exp)),
		}}
	})
	// sign (-> Sign): MRI's BigDecimal#sign code, exponent (-> Exponent),
	// precision (-> Precision).
	d("sign", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).d.Sign()))
	})
	d("exponent", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).d.Exponent()))
	})
	d("precision", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).d.Precision()))
	})

	// nan? / infinite? / finite? / zero?: the IEEE-style specials. infinite?
	// returns 1 / -1 / nil (not a boolean), as MRI does.
	d("nan?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).d.IsNaN())
	})
	d("infinite?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if i := self(v).d.IsInfinite(); i != 0 {
			return object.IntValue(int64(i))
		}
		return object.NilV
	})
	d("finite?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).d.IsFinite())
	})
	d("zero?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).d.IsZero())
	})

	// Comparison: <=> (Integer, or nil for NaN / a non-numeric operand), the
	// boolean operators built on Cmp (each coercing a numeric operand), and ==.
	d("<=>", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := asBigDecimal(args[0])
		if !ok {
			return object.NilV
		}
		return bigDecimalCmp(self(v).d, other)
	})
	cmpBool := func(v object.Value, args []object.Value, want func(int) bool) object.Value {
		c := self(v).d.Cmp(bigDecimalArg(args[0]))
		if c == -2 {
			return object.NilV
		}
		return object.Bool(want(c))
	}
	d("<", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return cmpBool(v, args, func(c int) bool { return c < 0 })
	})
	d(">", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return cmpBool(v, args, func(c int) bool { return c > 0 })
	})
	d("<=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return cmpBool(v, args, func(c int) bool { return c <= 0 })
	})
	d(">=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return cmpBool(v, args, func(c int) bool { return c >= 0 })
	})
	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(bigDecimalEqual(self(v), args[0]))
	})
}
