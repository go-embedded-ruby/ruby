package vm

import (
	"math"
	"math/big"
	"math/cmplx"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Complex numbers. The components are numeric Values (Integer/Bignum/Float) so
// Complex(1, 2) keeps Integer parts (and MRI-identical "(1+2i)" inspect) while
// arithmetic reuses the existing numeric operators. There is no Rational type,
// so division produces Float components rather than exact Rationals.

// registerNumericComplexCompat installs the Complex-compatibility methods every
// real Numeric answers in MRI — phase/arg/angle, polar, rect/rectangular,
// conjugate/conj, imaginary/imag. A real number r has phase 0 when r >= 0 and π
// when r < 0; its polar form is [abs, phase]; its rectangular form is [r, 0];
// its conjugate is itself and its imaginary part is 0. They live on Numeric so
// Integer/Float/Rational/Complex subclasses inherit them (and a Numeric subclass
// such as Puppet's TimeData can undef_method them).
func registerNumericComplexCompat(vm *VM, cNumeric *RClass) {
	phaseOf := func(self object.Value) object.Value {
		if f, ok := toFloat(self); ok && f < 0 {
			return object.Float(math.Pi)
		}
		return object.IntValue(0)
	}
	phaseFn := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return phaseOf(self)
	}
	cNumeric.define("phase", phaseFn)
	cNumeric.define("arg", phaseFn)
	cNumeric.define("angle", phaseFn)
	cNumeric.define("polar", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArray(vm.send(self, "abs", nil, nil), phaseOf(self))
	})
	rectFn := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArray(self, object.IntValue(0))
	}
	cNumeric.define("rect", rectFn)
	cNumeric.define("rectangular", rectFn)
	conjFn := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	}
	cNumeric.define("conjugate", conjFn)
	cNumeric.define("conj", conjFn)
	imagFn := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(0)
	}
	cNumeric.define("imaginary", imagFn)
	cNumeric.define("imag", imagFn)

	registerNumericGeneric(vm, cNumeric)
}

// registerNumericGeneric installs the generic Numeric instance methods MRI
// defines at the abstract Numeric level — unary +/- , abs/magnitude/abs2,
// div/fdiv, %/modulo/divmod, negative?/positive?. Integer/Float keep their own
// faster overrides (which win, being nearer in the ancestor chain); these exist
// so a Numeric subclass (e.g. Puppet's Timestamp) inherits them and can
// undef_method them, and so `5.send(:-@)` style sends resolve. Each delegates
// through the receiver's own operators via vm.send, matching MRI's
// coercion-based definitions.
func registerNumericGeneric(vm *VM, cNumeric *RClass) {
	bin := func(self object.Value, op string, other object.Value) object.Value {
		return vm.send(self, op, []object.Value{other}, nil)
	}
	cmpZero := func(self object.Value) int {
		c := vm.send(self, "<=>", []object.Value{object.IntValue(0)}, nil)
		i, ok := c.(object.Integer)
		if !ok {
			raise("ArgumentError", "comparison of %s with 0 failed", vm.classOf(self).name)
		}
		return int(i)
	}
	absFn := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if cmpZero(self) < 0 {
			return vm.send(self, "-@", nil, nil)
		}
		return self
	}
	cNumeric.define("abs", absFn)
	cNumeric.define("magnitude", absFn)
	cNumeric.define("abs2", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bin(self, "*", self)
	})
	cNumeric.define("-@", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bin(object.IntValue(0), "-", self)
	})
	cNumeric.define("+@", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	cNumeric.define("negative?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(cmpZero(self) < 0)
	})
	cNumeric.define("positive?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(cmpZero(self) > 0)
	})
	cNumeric.define("fdiv", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		a := vm.send(self, "to_f", nil, nil)
		b := vm.send(args[0], "to_f", nil, nil)
		return bin(a, "/", b)
	})
	// integer division: floor of the true quotient (MRI Numeric#div).
	divOf := func(self, other object.Value) object.Value {
		return vm.send(bin(self, "/", other), "floor", nil, nil)
	}
	cNumeric.define("div", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return divOf(self, args[0])
	})
	// modulo: self - other * (self div other) (MRI Numeric#modulo).
	modOf := func(self, other object.Value) object.Value {
		return bin(self, "-", bin(other, "*", divOf(self, other)))
	}
	cNumeric.define("modulo", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return modOf(self, args[0])
	})
	cNumeric.define("%", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return modOf(self, args[0])
	})
	cNumeric.define("divmod", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewArray(divOf(self, args[0]), modOf(self, args[0]))
	})
	// nonzero? returns nil when self#zero? is true (dispatching through #zero? so
	// a Numeric subclass's own definition is honoured), otherwise self.
	cNumeric.define("nonzero?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.send(self, "zero?", nil, nil).Truthy() {
			return object.NilV
		}
		return self
	})
	// A generic Numeric is not an Integer; Integer overrides this with true.
	cNumeric.define("integer?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.False
	})
	// i returns the pure-imaginary Complex(0, self). It lives on Numeric so every
	// real number answers it; Complex (which is not a real number) undefs it.
	cNumeric.define("i", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Complex{Re: object.IntValue(0), Im: self}
	})
	// Every Numeric is a real number (Complex overrides this with false).
	cNumeric.define("real?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.True
	})
	// zero? is self == 0, dispatched so a subclass's own #== / #<=> decides. This
	// is also what the #nonzero? above relies on.
	cNumeric.define("zero?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.send(self, "==", []object.Value{object.IntValue(0)}, nil).Truthy())
	})
	// real returns self: a real number is its own real part.
	cNumeric.define("real", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	// floor / ceil / round / truncate fall back to operating on #to_f, so a Numeric
	// subclass that only defines #to_f still rounds; the optional digits argument is
	// forwarded to Float's own implementation.
	for _, name := range []string{"floor", "ceil", "round", "truncate"} {
		m := name
		cNumeric.define(m, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.send(vm.send(self, "to_f", nil, nil), m, args, nil)
		})
	}
}

// asComplexVal coerces a real number to a Complex (zero imaginary part); a
// Complex passes through. ok is false for a non-numeric value.
func asComplexVal(v object.Value) (*object.Complex, bool) {
	if c, ok := v.(*object.Complex); ok {
		return c, true
	}
	if _, ok := toFloat(v); ok {
		return &object.Complex{Re: v, Im: object.IntValue(0)}, true
	}
	return nil, false
}

// complexEqual reports Complex equality, including Complex(x, 0) == x.
func complexEqual(c *object.Complex, other object.Value) bool {
	if oc, ok := other.(*object.Complex); ok {
		return valueEqual(c.Re, oc.Re) && valueEqual(c.Im, oc.Im)
	}
	if _, ok := toFloat(other); ok {
		return valueEqual(c.Im, object.IntValue(0)) && valueEqual(c.Re, other)
	}
	return false
}

// complexOp applies an arithmetic operator with a Complex left operand; the
// right operand is coerced to Complex. +/-/* stay exact on the component types;
// / falls to float64. Other operators are undefined on Complex (as in MRI).
func complexOp(op bytecode.Op, a *object.Complex, b object.Value) object.Value {
	bc, ok := asComplexVal(b)
	if !ok {
		return raise("TypeError", "%s can't be coerced into Complex", b.Inspect())
	}
	switch op {
	case bytecode.OpAdd:
		return &object.Complex{Re: binary(bytecode.OpAdd, a.Re, bc.Re), Im: binary(bytecode.OpAdd, a.Im, bc.Im)}
	case bytecode.OpSub:
		return &object.Complex{Re: binary(bytecode.OpSub, a.Re, bc.Re), Im: binary(bytecode.OpSub, a.Im, bc.Im)}
	case bytecode.OpMul:
		// (ar + ai·i)(br + bi·i) = (ar·br − ai·bi) + (ar·bi + ai·br)i.
		re := binary(bytecode.OpSub, binary(bytecode.OpMul, a.Re, bc.Re), binary(bytecode.OpMul, a.Im, bc.Im))
		im := binary(bytecode.OpAdd, binary(bytecode.OpMul, a.Re, bc.Im), binary(bytecode.OpMul, a.Im, bc.Re))
		return &object.Complex{Re: re, Im: im}
	case bytecode.OpDiv:
		// (a+bi)/(c+di) = ((ac+bd) + (bc−ad)i)/(c²+d²). This stays exact on
		// Integer/Rational components (each final division is quo, so 3/2 becomes
		// Rational(3,2) rather than an integer floor) and drops to Float only when a
		// Float component is present. A real integer 0 divisor raises
		// ZeroDivisionError; a Float 0.0 divisor yields non-finite components.
		c, d := bc.Re, bc.Im
		den := binary(bytecode.OpAdd, binary(bytecode.OpMul, c, c), binary(bytecode.OpMul, d, d))
		reNum := binary(bytecode.OpAdd, binary(bytecode.OpMul, a.Re, c), binary(bytecode.OpMul, a.Im, d))
		imNum := binary(bytecode.OpSub, binary(bytecode.OpMul, a.Im, c), binary(bytecode.OpMul, a.Re, d))
		return &object.Complex{Re: numQuo(reNum, den), Im: numQuo(imNum, den)}
	}
	return raise("NoMethodError", "undefined method '%s' for a Complex", op)
}

// numQuo divides one real numeric by another with Ruby's exact #quo semantics:
// two exact operands (Integer/Bignum/Rational) give an Integer when the result
// is whole and a Rational otherwise, while any Float operand yields a Float. A
// zero exact divisor raises ZeroDivisionError (a Float 0.0 divisor instead
// produces the IEEE non-finite result).
func numQuo(num, den object.Value) object.Value {
	_, numFloat := num.(object.Float)
	_, denFloat := den.(object.Float)
	if !numFloat && !denFloat {
		rn, _ := toRat(num)
		rd, _ := toRat(den)
		if rd.Sign() == 0 {
			return raise("ZeroDivisionError", "divided by 0")
		}
		q := new(big.Rat).Quo(rn, rd)
		if q.IsInt() {
			return object.NormInt(q.Num())
		}
		return &object.Rational{R: q}
	}
	a, _ := toFloat(num)
	b, _ := toFloat(den)
	return object.Float(a / b)
}

// complexFloat returns a component as float64 (components are always numeric).
func complexFloat(v object.Value) float64 {
	f, _ := toFloat(v)
	return f
}

// makeComplex builds the Complex for a validated Kernel#Complex() call. A single
// String argument is parsed with String#to_c's grammar (an invalid one is an
// ArgumentError, matching MRI's "invalid value for convert()"). Otherwise the
// first argument is the real part and an optional second the imaginary part, each
// required to be a real number (a non-real part is a TypeError). Every failure
// path raises, so the constructor can wrap it for exception: false.
func makeComplex(args []object.Value) object.Value {
	if s, ok := args[0].(*object.String); ok && len(args) == 1 {
		c, ok := stringToC(s.Str(), true)
		if !ok {
			return raise("ArgumentError", "invalid value for convert(): %s", s.Inspect())
		}
		return c
	}
	re := args[0]
	im := object.Value(object.IntValue(0))
	if len(args) > 1 {
		im = args[1]
	}
	if _, ok := toFloat(re); !ok {
		return raise("TypeError", "can't convert %s into Complex", re.Inspect())
	}
	if _, ok := toFloat(im); !ok {
		return raise("TypeError", "can't convert %s into Complex", im.Inspect())
	}
	return &object.Complex{Re: re, Im: im}
}

// registerComplex installs Kernel#Complex and the Complex instance methods.
func (vm *VM) registerComplex() {
	vm.cObject.define("Complex", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		args, doRaise := popExceptionKwarg(args)
		return vm.numericCtor(doRaise, func() object.Value { return makeComplex(args) })
	})

	// Integer#to_c / Float#to_c wrap a real number as Complex(self, 0); String#to_c
	// parses a complex literal leniently (unrecognised input yields Complex(0, 0)).
	realToC := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Complex{Re: self, Im: object.IntValue(0)}
	}
	vm.cInteger.define("to_c", realToC)
	vm.cFloat.define("to_c", realToC)
	vm.cString.define("to_c", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c, _ := stringToC(self.(*object.String).Str(), false)
		return c
	})

	// Complex::I is the imaginary unit, Complex(0, 1).
	vm.cComplex.consts["I"] = &object.Complex{Re: object.IntValue(0), Im: object.IntValue(1)}

	cval := func(self object.Value) *object.Complex { return self.(*object.Complex) }

	vm.cComplex.define("real", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return cval(self).Re
	})
	imag := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return cval(self).Im }
	vm.cComplex.define("imaginary", imag)
	vm.cComplex.define("imag", imag)

	abs := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := cval(self)
		return object.Float(math.Hypot(complexFloat(c.Re), complexFloat(c.Im)))
	}
	vm.cComplex.define("abs", abs)
	vm.cComplex.define("magnitude", abs)

	vm.cComplex.define("abs2", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := cval(self)
		return binary(bytecode.OpAdd, binary(bytecode.OpMul, c.Re, c.Re), binary(bytecode.OpMul, c.Im, c.Im))
	})

	arg := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := cval(self)
		return object.Float(math.Atan2(complexFloat(c.Im), complexFloat(c.Re)))
	}
	vm.cComplex.define("arg", arg)
	vm.cComplex.define("angle", arg)
	vm.cComplex.define("phase", arg)

	conj := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := cval(self)
		return &object.Complex{Re: c.Re, Im: negate(c.Im)}
	}
	vm.cComplex.define("conjugate", conj)
	vm.cComplex.define("conj", conj)

	rect := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) != 0 {
			return raise("ArgumentError", "wrong number of arguments (given %d, expected 0)", len(args))
		}
		c := cval(self)
		return object.NewArray(c.Re, c.Im)
	}
	vm.cComplex.define("rectangular", rect)
	vm.cComplex.define("rect", rect)

	vm.cComplex.define("polar", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := cval(self)
		mag := object.Float(math.Hypot(complexFloat(c.Re), complexFloat(c.Im)))
		ang := object.Float(math.Atan2(complexFloat(c.Im), complexFloat(c.Re)))
		return object.NewArray(mag, ang)
	})

	vm.cComplex.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(cval(self).ToS())
	})
	vm.cComplex.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(cval(self).Inspect())
	})

	// A Complex is never real and never an integer, whatever its parts.
	vm.cComplex.define("real?", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.False
	})
	vm.cComplex.define("integer?", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.False
	})

	vm.cComplex.define("finite?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := cval(self)
		return object.Bool(finiteComponent(c.Re) && finiteComponent(c.Im))
	})
	vm.cComplex.define("infinite?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := cval(self)
		if infiniteComponent(c.Re) || infiniteComponent(c.Im) {
			return object.IntValue(1)
		}
		return object.NilV
	})

	// to_f / to_i / to_r convert the real part, but only when the imaginary part is
	// an exact zero (an Integer/Rational 0 — a Float 0.0 raises RangeError, as MRI).
	realOnly := func(vm *VM, self object.Value, conv string) object.Value {
		c := cval(self)
		if !imagExactZero(c.Im) {
			return raise("RangeError", "can't convert %s into %s", c.Inspect(), rangeErrTarget(conv))
		}
		return vm.send(c.Re, conv, nil, nil)
	}
	vm.cComplex.define("to_f", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return realOnly(vm, self, "to_f")
	})
	vm.cComplex.define("to_i", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return realOnly(vm, self, "to_i")
	})
	// to_r is looser than to_f/to_i: a numeric-zero imaginary part (including a
	// Float 0.0) converts, only a non-zero one raises RangeError.
	vm.cComplex.define("to_r", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := cval(self)
		if !imagNumericZero(c.Im) {
			return raise("RangeError", "can't convert %s into Rational", c.Inspect())
		}
		return vm.send(c.Re, "to_r", nil, nil)
	})
	vm.cComplex.define("to_c", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	vm.cComplex.define("rationalize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 1 {
			return raise("ArgumentError", "wrong number of arguments (given %d, expected 0..1)", len(args))
		}
		c := cval(self)
		if !imagExactZero(c.Im) {
			return raise("RangeError", "can't convert %s into Rational", c.Inspect())
		}
		return vm.send(c.Re, "rationalize", args, nil)
	})

	vm.cComplex.define("numerator", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return complexNumerator(cval(self))
	})
	vm.cComplex.define("denominator", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NormInt(complexDenominator(cval(self)))
	})

	vm.cComplex.define("fdiv", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		c := cval(self)
		return &object.Complex{
			Re: vm.send(c.Re, "fdiv", []object.Value{args[0]}, nil),
			Im: vm.send(c.Im, "fdiv", []object.Value{args[0]}, nil),
		}
	})

	vm.cComplex.define("coerce", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		other := args[0]
		if oc, ok := other.(*object.Complex); ok {
			return object.NewArray(oc, self)
		}
		if _, ok := toFloat(other); ok {
			return object.NewArray(&object.Complex{Re: other, Im: object.IntValue(0)}, self)
		}
		return raise("TypeError", "%s can't be coerced into Complex", vm.classOf(other).name)
	})

	vm.cComplex.define("quo", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.send(self, "/", []object.Value{args[0]}, nil)
	})

	vm.cComplex.define("**", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		exp := args[0]
		if isNumericValue(exp) {
			return complexPow(cval(self), exp)
		}
		// A non-numeric exponent answering #coerce goes through the coercion
		// protocol; otherwise it cannot be raised to.
		if res, ok := vm.tryNumericCoerce("**", self, exp); ok {
			return res
		}
		return raise("TypeError", "%s can't be coerced into Complex", vm.classOf(exp).name)
	})

	vm.cComplex.define("eql?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		c := cval(self)
		oc, ok := args[0].(*object.Complex)
		if !ok {
			return object.False
		}
		sameKind := vm.classOf(c.Re) == vm.classOf(oc.Re) && vm.classOf(c.Im) == vm.classOf(oc.Im)
		return object.Bool(sameKind && complexEqual(c, oc))
	})

	vm.cComplex.define("<=>", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		c := cval(self)
		if !imagNumericZero(c.Im) {
			return object.NilV // a non-real Complex is unordered
		}
		var oreal object.Value
		switch o := args[0].(type) {
		case *object.Complex:
			if !imagNumericZero(o.Im) {
				return object.NilV
			}
			oreal = o.Re
		default:
			if _, ok := toFloat(o); !ok {
				return object.NilV
			}
			oreal = args[0]
		}
		return vm.send(c.Re, "<=>", []object.Value{oreal}, nil)
	})

	// A Complex is unordered, so the sign predicates (negative?/positive?), the
	// Comparable ordering operators, and the pure-imaginary constructor #i are all
	// undefined on it — each raises NoMethodError rather than being inherited from
	// Numeric/Comparable (Complex keeps #<=>, which yields nil for a non-real pair,
	// and #==). A tombstone is installed directly (not via undef) because those
	// Numeric methods are registered after this hook runs.
	for _, name := range []string{"negative?", "positive?", "i", "<", "<=", ">", ">=", "clamp", "between?"} {
		vm.cComplex.methods[name] = &Method{name: name, owner: vm.cComplex, undefined: true}
	}

	// True aliases share one Method record so Complex.instance_method(:angle) ==
	// Complex.instance_method(:arg), matching MRI.
	for _, pair := range [][2]string{
		{"angle", "arg"}, {"phase", "arg"},
		{"conj", "conjugate"}, {"imag", "imaginary"},
		{"rect", "rectangular"}, {"magnitude", "abs"},
	} {
		vm.cComplex.methods[pair[0]] = vm.cComplex.methods[pair[1]]
	}

	// Class constructors Complex.rectangular / .rect / .polar.
	rectCtor := &Method{name: "rectangular", owner: vm.cComplex, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return complexRectangular(args)
	}}
	vm.cComplex.smethods["rectangular"] = rectCtor
	vm.cComplex.smethods["rect"] = rectCtor
	vm.cComplex.smethods["polar"] = &Method{name: "polar", owner: vm.cComplex, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return complexPolar(args)
	}}
}

// rangeErrTarget names the conversion target in the Complex#to_f / #to_i
// RangeError (the only two conversions routed through realOnly; #to_r is looser
// and handled separately).
func rangeErrTarget(conv string) string {
	if conv == "to_f" {
		return "Float"
	}
	return "Integer"
}

// finiteComponent reports whether a Complex part is finite: any exact number is,
// and a Float is unless it is Infinity or NaN.
func finiteComponent(v object.Value) bool {
	if f, ok := v.(object.Float); ok {
		return !math.IsInf(float64(f), 0) && !math.IsNaN(float64(f))
	}
	return true
}

// infiniteComponent reports whether a Complex part is an infinite Float.
func infiniteComponent(v object.Value) bool {
	f, ok := v.(object.Float)
	return ok && math.IsInf(float64(f), 0)
}

// imagExactZero reports whether an imaginary part is an *exact* zero (Integer or
// Rational 0). A Float 0.0 is not exact, so Complex#to_f/#to_i/#to_r reject it.
func imagExactZero(im object.Value) bool {
	if _, ok := im.(object.Float); ok {
		return false
	}
	return valueEqual(im, object.IntValue(0))
}

// imagNumericZero reports whether an imaginary part is numerically zero (0 or
// 0.0), the looser test Complex#<=> uses to decide a Complex is real.
func imagNumericZero(im object.Value) bool {
	return valueEqual(im, object.IntValue(0))
}

// ratParts returns the numerator and denominator of an exact real component.
func ratParts(v object.Value) (num, den *big.Int) {
	r, _ := toRat(v)
	return r.Num(), r.Denom()
}

// complexDenominator is the LCM of the two parts' denominators (MRI's rule).
func complexDenominator(c *object.Complex) *big.Int {
	_, rd := ratParts(c.Re)
	_, id := ratParts(c.Im)
	g := new(big.Int).GCD(nil, nil, rd, id)
	return new(big.Int).Mul(new(big.Int).Quo(rd, g), id) // lcm = rd/gcd*id
}

// complexNumerator scales both parts up by the common denominator, giving a
// Complex of whole-number parts (MRI's Complex#numerator).
func complexNumerator(c *object.Complex) object.Value {
	d := complexDenominator(c)
	rn, rd := ratParts(c.Re)
	in, id := ratParts(c.Im)
	re := new(big.Int).Mul(rn, new(big.Int).Quo(d, rd))
	im := new(big.Int).Mul(in, new(big.Int).Quo(d, id))
	return &object.Complex{Re: object.NormInt(re), Im: object.NormInt(im)}
}

// complexRectangular builds Complex(re, im=0) from real arguments (MRI's
// Complex.rectangular), raising TypeError on a non-real part.
func complexRectangular(args []object.Value) object.Value {
	re := args[0]
	im := object.Value(object.IntValue(0))
	if len(args) > 1 {
		im = args[1]
	}
	if _, ok := toFloat(re); !ok {
		return raise("TypeError", "not a real")
	}
	if _, ok := toFloat(im); !ok {
		return raise("TypeError", "not a real")
	}
	return &object.Complex{Re: re, Im: im}
}

// polarReal reads a real value for Complex.polar's modulus/argument: a real
// number yields its float value, and a Complex whose imaginary part is zero
// yields its real part's value. ok is false for a Complex with a non-zero
// imaginary part or any non-numeric value ("not a real").
func polarReal(v object.Value) (float64, bool) {
	if c, ok := v.(*object.Complex); ok {
		if !imagNumericZero(c.Im) {
			return 0, false
		}
		v = c.Re
	}
	return toFloat(v)
}

// complexPolar builds a Complex from an absolute value and angle (MRI's
// Complex.polar); the result carries Float parts. Each argument may be a real
// number or a Complex with a zero imaginary part.
func complexPolar(args []object.Value) object.Value {
	abs, ok := polarReal(args[0])
	if !ok {
		return raise("TypeError", "not a real")
	}
	ang := 0.0
	if len(args) > 1 {
		ang, ok = polarReal(args[1])
		if !ok {
			return raise("TypeError", "not a real")
		}
	}
	return &object.Complex{Re: object.Float(abs * math.Cos(ang)), Im: object.Float(abs * math.Sin(ang))}
}

// complexPow raises z to an exponent. A whole-number exponent is evaluated by
// exact repeated multiplication (a negative one via the exact reciprocal), so
// integer/rational components stay exact; any other exponent falls back to the
// polar (Float) form.
func complexPow(z *object.Complex, exp object.Value) object.Value {
	// Only a small (machine-word) integer exponent is evaluated exactly: a Bignum
	// is by construction out of int64 range, so an exact repeated product would be
	// infeasible — it falls through to the polar form below.
	if n, ok := exp.(object.Integer); ok {
		result := &object.Complex{Re: object.IntValue(1), Im: object.IntValue(0)}
		base := z
		k := int64(n)
		if k < 0 {
			base = complexReciprocal(z)
			k = -k
		}
		for ; k > 0; k-- {
			result = complexOp(bytecode.OpMul, result, base).(*object.Complex)
		}
		return result
	}
	// Non-integer (real or Complex) exponent: z**w = exp(w·ln z), in floating point.
	base := complex(complexFloat(z.Re), complexFloat(z.Im))
	var w complex128
	if ec, ok := exp.(*object.Complex); ok {
		w = complex(complexFloat(ec.Re), complexFloat(ec.Im))
	} else {
		ef, _ := toFloat(exp)
		w = complex(ef, 0)
	}
	p := cmplx.Pow(base, w)
	return &object.Complex{Re: object.Float(real(p)), Im: object.Float(imag(p))}
}

// complexReciprocal returns 1/z with exact components where possible.
func complexReciprocal(z *object.Complex) *object.Complex {
	one := &object.Complex{Re: object.IntValue(1), Im: object.IntValue(0)}
	return complexOp(bytecode.OpDiv, one, z).(*object.Complex)
}
