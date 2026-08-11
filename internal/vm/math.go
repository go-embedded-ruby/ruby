package vm

import (
	"math"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The Math module wraps Go's math package: the standard transcendental, power,
// gamma and error functions plus the PI / E constants (read via ::, e.g.
// Math::PI). Arguments coerce through Float() semantics (Integer/Float/Bignum/
// Rational directly, any other Numeric via #to_f), and the domain restrictions,
// DomainError messages and edge-case results all mirror MRI exactly.

// mathFloat coerces a Math argument to float64 with MRI's Float() semantics: the
// built-in numeric types convert directly, any other Numeric coerces through
// #to_f, and everything else raises a TypeError naming the value like MRI.
func mathFloat(vm *VM, v object.Value) float64 {
	if f, ok := toFloat(v); ok {
		return f
	}
	if num, ok := vm.consts["Numeric"].(*RClass); ok && classIsA(vm.classOf(v), num) {
		if f, ok := toFloat(vm.send(v, "to_f", nil, nil)); ok {
			return f
		}
	}
	name := classNameOf(v)
	if b, ok := v.(object.Bool); ok {
		name = b.ToS() // MRI names true/false by value, not class.
	}
	raise("TypeError", "can't convert %s into Float", name)
	return 0
}

// mathDomainError raises Math::DomainError with MRI's exact message, which names
// the offending function (e.g. "Numerical argument is out of domain - sqrt").
func mathDomainError(fn string) {
	raise("Math::DomainError", "Numerical argument is out of domain - %s", fn)
}

// mathLdexpExp coerces Math.ldexp's second argument with Integer() semantics and
// narrows it to a C int, reproducing MRI's RangeError/TypeError messages: an
// Integer/Float out of the 32-bit int range, a NaN/Infinity, or a value too big
// even for a machine long each raise a distinct RangeError, while nil and
// non-Integer-convertible objects raise a TypeError.
func mathLdexpExp(vm *VM, v object.Value) int {
	switch n := v.(type) {
	case object.Integer:
		return mathInt32(int64(n))
	case *object.Bignum:
		return mathBignumRange()
	case object.Float:
		return mathFloatToInt(float64(n))
	case object.Nil:
		raise("TypeError", "no implicit conversion from nil to integer")
	}
	if vm.respondsTo(v, "to_int") {
		switch r := vm.send(v, "to_int", nil, nil).(type) {
		case object.Integer:
			return mathInt32(int64(r))
		case *object.Bignum:
			return mathBignumRange()
		default:
			raise("TypeError", "can't convert %s to Integer (%s#to_int gives %s)",
				vm.classOf(v).name, vm.classOf(v).name, vm.classOf(r).name)
		}
	}
	raise("TypeError", "no implicit conversion of %s into Integer", vm.classOf(v).name)
	return 0
}

// mathInt32 narrows x to a C int, raising RangeError like MRI's NUM2INT when it
// overflows the signed 32-bit range.
func mathInt32(x int64) int {
	if x > math.MaxInt32 {
		raise("RangeError", "integer %d too big to convert to 'int'", x)
	}
	if x < math.MinInt32 {
		raise("RangeError", "integer %d too small to convert to 'int'", x)
	}
	return int(x)
}

// mathBignumRange always raises: a Bignum exceeds a machine long, so MRI's
// NUM2INT fails at the long stage with this message (same for either sign).
func mathBignumRange() int {
	raise("RangeError", "bignum too big to convert into 'long'")
	return 0
}

// mathFloatToInt truncates a Float exponent toward zero and narrows it, matching
// MRI's staged conversion: NaN/Infinity and values beyond a machine long report
// "float ... out of range of integer", while a value that fits a long but not a
// C int reports "integer ... too big/small to convert to 'int'".
func mathFloatToInt(f float64) int {
	if math.IsNaN(f) {
		raise("RangeError", "float NaN out of range of integer")
	}
	if math.IsInf(f, 0) {
		s := "Inf"
		if f < 0 {
			s = "-Inf"
		}
		raise("RangeError", "float %s out of range of integer", s)
	}
	if f >= 9223372036854775808.0 || f < -9223372036854775808.0 {
		raise("RangeError", "float %v out of range of integer", f)
	}
	return mathInt32(int64(math.Trunc(f)))
}

// registerMath installs the Math module, its constants and its functions.
func (vm *VM) registerMath() {
	mod := newClass("Math", nil)
	mod.isModule = true
	mod.consts["PI"] = object.Float(math.Pi)
	mod.consts["E"] = object.Float(math.E)
	vm.consts["Math"] = mod

	// install exposes fn as both a module method (Math.fn) and a private instance
	// method, mirroring Ruby's module_function.
	install := func(name string, fn func(*VM, object.Value, []object.Value, *Proc) object.Value) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
		mod.methods[name] = &Method{name: name, owner: mod, native: fn, vis: visPrivate}
	}

	// unary installs a single-argument function with no domain restriction.
	unary := func(name string, f func(float64) float64) {
		install(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.Float(f(mathFloat(vm, args[0])))
		})
	}
	for name, f := range map[string]func(float64) float64{
		"cbrt": math.Cbrt, "exp": math.Exp,
		"sin": math.Sin, "cos": math.Cos, "tan": math.Tan,
		"atan": math.Atan,
		"sinh": math.Sinh, "cosh": math.Cosh, "tanh": math.Tanh,
		"asinh": math.Asinh, "erf": math.Erf, "erfc": math.Erfc,
	} {
		unary(name, f)
	}

	// unaryDom installs a single-argument function that raises DomainError when
	// bad reports the argument is outside the function's real domain.
	unaryDom := func(name string, f func(float64) float64, bad func(float64) bool) {
		install(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			x := mathFloat(vm, args[0])
			if bad(x) {
				mathDomainError(name)
			}
			return object.Float(f(x))
		})
	}
	neg := func(x float64) bool { return x < 0 }            // log family: x < 0
	unit := func(x float64) bool { return x < -1 || x > 1 } // asin/acos/atanh: |x| > 1
	unaryDom("log2", math.Log2, neg)
	unaryDom("log10", math.Log10, neg)
	unaryDom("asin", math.Asin, unit)
	unaryDom("acos", math.Acos, unit)
	unaryDom("atanh", math.Atanh, unit)
	unaryDom("acosh", math.Acosh, func(x float64) bool { return x < 1 })

	// sqrt raises for a negative argument and normalises sqrt(-0.0) to +0.0 (Go
	// returns -0.0, MRI returns +0.0).
	install("sqrt", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		x := mathFloat(vm, args[0])
		if x < 0 {
			mathDomainError("sqrt")
		}
		r := math.Sqrt(x)
		if r == 0 {
			r = 0
		}
		return object.Float(r)
	})

	// log(x) is the natural log; log(x, base) divides by log(base). A negative x
	// or base raises DomainError - log; log(0) is -Infinity.
	install("log", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		x := mathFloat(vm, args[0])
		if x < 0 {
			mathDomainError("log")
		}
		lx := math.Log(x)
		if len(args) > 1 {
			base := mathFloat(vm, args[1])
			if base < 0 {
				mathDomainError("log")
			}
			return object.Float(lx / math.Log(base))
		}
		return object.Float(lx)
	})

	// binary installs a two-argument function with no domain restriction.
	binary := func(name string, f func(a, b float64) float64) {
		install(name, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.Float(f(mathFloat(vm, args[0]), mathFloat(vm, args[1])))
		})
	}
	binary("atan2", math.Atan2)
	binary("hypot", math.Hypot)

	// gamma raises for a negative integer or -Infinity; gamma(0) is +Infinity and
	// gamma(-0.0) is -Infinity (both fall out of Go's math.Gamma directly).
	install("gamma", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		x := mathFloat(vm, args[0])
		if math.IsInf(x, -1) || (x < 0 && x == math.Trunc(x)) {
			mathDomainError("gamma")
		}
		return object.Float(math.Gamma(x))
	})

	// lgamma returns [log|gamma(x)|, sign]; only -Infinity is out of domain. Go
	// reports sign +1 for -0.0 where MRI reports -1 (gamma(-0.0) = -Infinity).
	install("lgamma", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		x := mathFloat(vm, args[0])
		if math.IsInf(x, -1) {
			mathDomainError("lgamma")
		}
		val, sign := math.Lgamma(x)
		if x == 0 && math.Signbit(x) {
			sign = -1
		}
		return object.NewArray(object.Float(val), object.Integer(sign))
	})

	// frexp splits x into [fraction, exponent] with x == fraction * 2**exponent.
	install("frexp", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		frac, exp := math.Frexp(mathFloat(vm, args[0]))
		return object.NewArray(object.Float(frac), object.Integer(exp))
	})

	// ldexp computes fraction * 2**exponent, coercing exponent with Integer().
	install("ldexp", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		frac := mathFloat(vm, args[0])
		exp := mathLdexpExp(vm, args[1])
		return object.Float(math.Ldexp(frac, exp))
	})
}
