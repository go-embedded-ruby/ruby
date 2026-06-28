package vm

import (
	"math"

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
		return object.Integer(0)
	}
	phaseFn := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return phaseOf(self)
	}
	cNumeric.define("phase", phaseFn)
	cNumeric.define("arg", phaseFn)
	cNumeric.define("angle", phaseFn)
	cNumeric.define("polar", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Array{Elems: []object.Value{vm.send(self, "abs", nil, nil), phaseOf(self)}}
	})
	rectFn := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Array{Elems: []object.Value{self, object.Integer(0)}}
	}
	cNumeric.define("rect", rectFn)
	cNumeric.define("rectangular", rectFn)
	conjFn := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	}
	cNumeric.define("conjugate", conjFn)
	cNumeric.define("conj", conjFn)
	imagFn := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(0)
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
		c := vm.send(self, "<=>", []object.Value{object.Integer(0)}, nil)
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
		return bin(object.Integer(0), "-", self)
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
		return &object.Array{Elems: []object.Value{divOf(self, args[0]), modOf(self, args[0])}}
	})
}

// asComplexVal coerces a real number to a Complex (zero imaginary part); a
// Complex passes through. ok is false for a non-numeric value.
func asComplexVal(v object.Value) (*object.Complex, bool) {
	if c, ok := v.(*object.Complex); ok {
		return c, true
	}
	if _, ok := toFloat(v); ok {
		return &object.Complex{Re: v, Im: object.Integer(0)}, true
	}
	return nil, false
}

// complexEqual reports Complex equality, including Complex(x, 0) == x.
func complexEqual(c *object.Complex, other object.Value) bool {
	if oc, ok := other.(*object.Complex); ok {
		return valueEqual(c.Re, oc.Re) && valueEqual(c.Im, oc.Im)
	}
	if _, ok := toFloat(other); ok {
		return valueEqual(c.Im, object.Integer(0)) && valueEqual(c.Re, other)
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
		ar, _ := toFloat(a.Re)
		ai, _ := toFloat(a.Im)
		br, _ := toFloat(bc.Re)
		bi, _ := toFloat(bc.Im)
		den := br*br + bi*bi
		return &object.Complex{
			Re: object.Float((ar*br + ai*bi) / den),
			Im: object.Float((ai*br - ar*bi) / den),
		}
	}
	return raise("NoMethodError", "undefined method '%s' for a Complex", op)
}

// complexFloat returns a component as float64 (components are always numeric).
func complexFloat(v object.Value) float64 {
	f, _ := toFloat(v)
	return f
}

// registerComplex installs Kernel#Complex and the Complex instance methods.
func (vm *VM) registerComplex() {
	vm.cObject.define("Complex", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		re := args[0]
		im := object.Value(object.Integer(0))
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
	})

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

	rect := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := cval(self)
		return &object.Array{Elems: []object.Value{c.Re, c.Im}}
	}
	vm.cComplex.define("rectangular", rect)
	vm.cComplex.define("rect", rect)

	vm.cComplex.define("polar", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		c := cval(self)
		mag := object.Float(math.Hypot(complexFloat(c.Re), complexFloat(c.Im)))
		ang := object.Float(math.Atan2(complexFloat(c.Im), complexFloat(c.Re)))
		return &object.Array{Elems: []object.Value{mag, ang}}
	})

	vm.cComplex.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(cval(self).ToS())
	})
	vm.cComplex.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(cval(self).Inspect())
	})
}
