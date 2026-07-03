package vm

import (
	"math"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The Math module wraps Go's math package: the standard transcendental and
// power functions plus the PI / E constants (read via ::, e.g. Math::PI).
// Arguments are coerced through toFloat, so Integer/Float/Bignum all work.

// mathFloat coerces a Math argument to float64, raising TypeError otherwise.
func mathFloat(v object.Value) float64 {
	f, ok := toFloat(v)
	if !ok {
		raise("TypeError", "can't convert %s into Float", v.Inspect())
	}
	return f
}

// registerMath installs the Math module, its constants and its functions.
func (vm *VM) registerMath() {
	mod := newClass("Math", nil)
	mod.isModule = true
	mod.consts["PI"] = object.FloatValue(float64(object.Float(math.Pi)))
	mod.consts["E"] = object.FloatValue(float64(object.Float(math.E)))
	vm.consts["Math"] = object.Wrap(mod)

	unary := func(name string, f func(float64) float64) {
		mod.smethods[name] = &Method{name: name, owner: mod,
			native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return object.FloatValue(float64(object.Float(f(mathFloat(args[0])))))
			}}
	}
	for name, f := range map[string]func(float64) float64{
		"sqrt": math.Sqrt, "cbrt": math.Cbrt, "exp": math.Exp,
		"log2": math.Log2, "log10": math.Log10,
		"sin": math.Sin, "cos": math.Cos, "tan": math.Tan,
		"asin": math.Asin, "acos": math.Acos, "atan": math.Atan,
		"sinh": math.Sinh, "cosh": math.Cosh, "tanh": math.Tanh,
	} {
		unary(name, f)
	}

	binary := func(name string, f func(a, b float64) float64) {
		mod.smethods[name] = &Method{name: name, owner: mod,
			native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
				return object.FloatValue(float64(object.Float(f(mathFloat(args[0]), mathFloat(args[1])))))
			}}
	}
	binary("atan2", math.Atan2)
	binary("hypot", math.Hypot)
	binary("pow", math.Pow)

	// log(x) is the natural log; log(x, base) divides by log(base).
	mod.smethods["log"] = &Method{name: "log", owner: mod,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			x := math.Log(mathFloat(args[0]))
			if len(args) > 1 {
				return object.FloatValue(float64(object.Float(x / math.Log(mathFloat(args[1])))))
			}
			return object.FloatValue(float64(object.Float(x)))
		}}
}
