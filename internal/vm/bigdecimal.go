package vm

import (
	bigfloat "github.com/go-composites/bigfloat/src"
	goresult "github.com/go-composites/result/src"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// BigDecimal binds github.com/go-composites/bigfloat — the third real consumer
// of the go-composites family (after Set and Time) — into Ruby. A go-composites
// BigFloat is an arbitrary-precision decimal (256-bit mantissa over a
// math/big.Float), so sums such as 0.1 + 0.2 are exact rather than subject to
// binary float64 rounding. It fills the gap in the numeric tower: the VM already
// has Integer / Float / Rational / Complex but no arbitrary-precision decimal.
//
// BigDecimal holds the go-composites bigfloat.Interface directly (mirroring how
// Set holds its go-composites Set and Time its go-composites Time), so every
// Ruby BigDecimal is a thin shell over the composite value and all behaviour
// flows through that one interface — construction (FromString / FromFloat64),
// arithmetic (Add/Sub/Mul/Div, each a fallible Result), the unary Abs / Neg, the
// conversions (ToFloat64 / ToGoString) and the comparisons (Equal / LessThan /
// GreaterThan).

// BigDecimal is the Ruby wrapper around a go-composites BigFloat.
type BigDecimal struct {
	f bigfloat.Interface
}

// repr renders a clean decimal string via the composite's ToGoString, which is
// the shortest decimal that round-trips the value (math/big.Float Text('g',-1)).
// MRI's BigDecimal#to_s uses a scientific "0.3e1" form by default; this binding
// deliberately renders the plain decimal ("3"/"0.3") instead — exact, but in the
// conventional notation rather than MRI's mantissa/exponent split.
func (b *BigDecimal) repr() string { return b.f.ToGoString() }

func (b *BigDecimal) ToS() string     { return b.repr() }
func (b *BigDecimal) Inspect() string { return b.repr() }
func (b *BigDecimal) Truthy() bool    { return true }

// asBigDecimal coerces a Ruby operand to a BigDecimal: a BigDecimal passes
// through, while an Integer / Bignum / Float / Rational is promoted through its
// decimal string (so the promotion is exact for the value the operand prints).
// ok is false for a non-numeric, non-BigDecimal value.
func asBigDecimal(v object.Value) (*BigDecimal, bool) {
	switch x := v.(type) {
	case *BigDecimal:
		return x, true
	case object.Integer:
		return &BigDecimal{f: payloadBigFloat(bigfloat.FromString(x.ToS()))}, true
	case *object.Bignum:
		return &BigDecimal{f: payloadBigFloat(bigfloat.FromString(x.ToS()))}, true
	case object.Float:
		return &BigDecimal{f: bigfloat.FromFloat64(float64(x))}, true
	case *object.Rational:
		f, _ := x.R.Float64()
		return &BigDecimal{f: bigfloat.FromFloat64(f)}, true
	}
	return nil, false
}

// bigDecimalArg coerces an operand to a BigDecimal, raising TypeError otherwise
// — the BigDecimal counterpart of setArg / timeArg.
func bigDecimalArg(v object.Value) *BigDecimal {
	b, ok := asBigDecimal(v)
	if !ok {
		raise("TypeError", "%s can't be coerced into BigDecimal", v.Inspect())
	}
	return b
}

// payloadBigFloat unwraps a go-composites Result whose payload is a BigFloat,
// raising a Ruby ArgumentError carrying the composite's Error message when the
// parse failed (so a malformed literal is a Ruby exception, not a Go panic) —
// mirroring time.go's payloadTime.
func payloadBigFloat(r goresult.Interface) bigfloat.Interface {
	if r.HasError() {
		raise("ArgumentError", "%s", r.Error().Message())
	}
	return r.Payload().(bigfloat.Interface)
}

// bigDecimalOp implements the BigDecimal operator fast path reached from
// binary(): +/-/*/ delegate to the composite's Add/Sub/Mul/Div, coercing the
// right operand to a BigDecimal. Division by zero surfaces the composite's error
// Result as a Ruby ZeroDivisionError (Div is the only arithmetic that can fail).
func bigDecimalOp(op bytecode.Op, a *BigDecimal, b object.Value) object.Value {
	rhs := bigDecimalArg(b)
	switch op {
	case bytecode.OpAdd:
		return &BigDecimal{f: payloadBigFloat(a.f.Add(rhs.f))}
	case bytecode.OpSub:
		return &BigDecimal{f: payloadBigFloat(a.f.Sub(rhs.f))}
	case bytecode.OpMul:
		return &BigDecimal{f: payloadBigFloat(a.f.Mul(rhs.f))}
	case bytecode.OpDiv:
		r := a.f.Div(rhs.f)
		if r.HasError() {
			raise("ZeroDivisionError", "%s", r.Error().Message())
		}
		return &BigDecimal{f: r.Payload().(bigfloat.Interface)}
	}
	return raise("NoMethodError", "undefined method '%s' for a BigDecimal", op)
}

// bigDecimalEqual reports BigDecimal equality for valueEqual / the == operator
// fast path, coercing a numeric operand (BigDecimal("2") == 2) and reporting
// false for a non-numeric one.
func bigDecimalEqual(a *BigDecimal, other object.Value) bool {
	b, ok := asBigDecimal(other)
	return ok && a.f.Equal(b.f)
}

// bigDecimalCmp returns -1/0/1 ordering two BigDecimals through the composite's
// LessThan / GreaterThan (Equal otherwise).
func bigDecimalCmp(a, b *BigDecimal) int64 {
	switch {
	case a.f.LessThan(b.f):
		return -1
	case a.f.GreaterThan(b.f):
		return 1
	default:
		return 0
	}
}

// registerBigDecimal installs Kernel#BigDecimal and the BigDecimal instance
// methods, all delegating to the go-composites bigfloat.Interface.
func (vm *VM) registerBigDecimal() {
	vm.cBigDecimal = newClass("BigDecimal", vm.cObject)
	vm.consts["BigDecimal"] = vm.cBigDecimal

	// Kernel#BigDecimal(value): MRI's constructor is the global method, not
	// BigDecimal.new. A String is parsed at full precision (bad → ArgumentError);
	// an Integer/Bignum is parsed exactly through its decimal string; a Float is
	// taken via FromFloat64; a BigDecimal passes through. (Mirrors how
	// Complex(...) / Rational(...) register a Kernel conversion on Object.)
	vm.cObject.define("BigDecimal", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		switch v := args[0].(type) {
		case *BigDecimal:
			return v
		case *object.String:
			return &BigDecimal{f: payloadBigFloat(bigfloat.FromString(v.Str()))}
		case object.Integer, *object.Bignum:
			return &BigDecimal{f: payloadBigFloat(bigfloat.FromString(v.ToS()))}
		case object.Float:
			return &BigDecimal{f: bigfloat.FromFloat64(float64(v))}
		}
		return raise("TypeError", "can't convert %s into BigDecimal", args[0].Inspect())
	})

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

	// abs (→ Abs) and unary -@ (→ Neg): non-failing Results that always carry a
	// payload.
	d("abs", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &BigDecimal{f: payloadBigFloat(self(v).f.Abs())}
	})
	d("-@", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &BigDecimal{f: payloadBigFloat(self(v).f.Neg())}
	})

	// to_f (→ ToFloat64): the nearest float64.
	d("to_f", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(self(v).f.ToFloat64())
	})
	// to_s / inspect: the clean decimal string (see repr — plain decimal, not
	// MRI's "0.3e1" scientific form).
	toSFn := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).repr())
	}
	d("to_s", toSFn)
	d("inspect", toSFn)

	// Comparison: <=> via LessThan/GreaterThan/Equal (nil for a non-numeric, as in
	// MRI), and the boolean operators / == built on it (each coercing a numeric
	// operand to BigDecimal).
	d("<=>", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := asBigDecimal(args[0])
		if !ok {
			return object.NilV
		}
		return object.Integer(bigDecimalCmp(self(v), other))
	})
	d("<", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).f.LessThan(bigDecimalArg(args[0]).f))
	})
	d(">", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).f.GreaterThan(bigDecimalArg(args[0]).f))
	})
	d("<=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(!self(v).f.GreaterThan(bigDecimalArg(args[0]).f))
	})
	d(">=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(!self(v).f.LessThan(bigDecimalArg(args[0]).f))
	})
	d("==", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(bigDecimalEqual(self(v), args[0]))
	})
}
