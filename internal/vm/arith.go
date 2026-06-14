package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// binary applies a Phase 0 fast-path operator. Integer⊕Integer stays integer;
// any Float makes it float. Phase 1 reroutes these through method dispatch so
// that e.g. Integer#+ can be redefined.
func binary(op bytecode.Op, a, b object.Value) object.Value {
	switch op {
	case bytecode.OpEq:
		return object.Bool(valueEqual(a, b))
	case bytecode.OpNeq:
		return object.Bool(!valueEqual(a, b))
	}

	// String fast paths: "a" + "b" and "a" * 3. Phase 2 fleshes out String.
	if as, ok := a.(object.String); ok {
		return stringOp(op, as, b)
	}

	ai, aok := a.(object.Integer)
	bi, bok := b.(object.Integer)
	if aok && bok {
		return intOp(op, int64(ai), int64(bi))
	}

	af, aIsNum := toFloat(a)
	bf, bIsNum := toFloat(b)
	if aIsNum && bIsNum {
		return floatOp(op, af, bf)
	}

	return raise("TypeError", "%s can't be coerced for %s", b.Inspect(), op)
}

func intOp(op bytecode.Op, a, b int64) object.Value {
	switch op {
	case bytecode.OpAdd:
		return object.Integer(a + b)
	case bytecode.OpSub:
		return object.Integer(a - b)
	case bytecode.OpMul:
		return object.Integer(a * b)
	case bytecode.OpDiv:
		if b == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		return object.Integer(floorDiv(a, b))
	case bytecode.OpMod:
		if b == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		return object.Integer(floorMod(a, b))
	case bytecode.OpLt:
		return object.Bool(a < b)
	case bytecode.OpGt:
		return object.Bool(a > b)
	case bytecode.OpLe:
		return object.Bool(a <= b)
	case bytecode.OpGe:
		return object.Bool(a >= b)
	}
	return raise("VMError", "bad integer op %s", op)
}

func floatOp(op bytecode.Op, a, b float64) object.Value {
	switch op {
	case bytecode.OpAdd:
		return object.Float(a + b)
	case bytecode.OpSub:
		return object.Float(a - b)
	case bytecode.OpMul:
		return object.Float(a * b)
	case bytecode.OpDiv:
		return object.Float(a / b) // matches Ruby: 1.0/0 => Infinity
	case bytecode.OpMod:
		return object.Float(floatMod(a, b))
	case bytecode.OpLt:
		return object.Bool(a < b)
	case bytecode.OpGt:
		return object.Bool(a > b)
	case bytecode.OpLe:
		return object.Bool(a <= b)
	case bytecode.OpGe:
		return object.Bool(a >= b)
	}
	return raise("VMError", "bad float op %s", op)
}

func stringOp(op bytecode.Op, a object.String, b object.Value) object.Value {
	switch op {
	case bytecode.OpAdd:
		bs, ok := b.(object.String)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into String", b.Inspect())
		}
		return a + bs
	case bytecode.OpMul:
		n, ok := b.(object.Integer)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Integer", b.Inspect())
		}
		if n < 0 {
			raise("ArgumentError", "negative argument")
		}
		out := make([]byte, 0, len(a)*int(n))
		for i := int64(0); i < int64(n); i++ {
			out = append(out, a...)
		}
		return object.String(out)
	case bytecode.OpLt, bytecode.OpGt, bytecode.OpLe, bytecode.OpGe:
		bs, ok := b.(object.String)
		if !ok {
			raise("ArgumentError", "comparison of String with %s failed", b.Inspect())
		}
		switch op {
		case bytecode.OpLt:
			return object.Bool(a < bs)
		case bytecode.OpGt:
			return object.Bool(a > bs)
		case bytecode.OpLe:
			return object.Bool(a <= bs)
		default:
			return object.Bool(a >= bs)
		}
	}
	return raise("NoMethodError", "undefined method '%s' for a String", op)
}

func negate(v object.Value) object.Value {
	switch n := v.(type) {
	case object.Integer:
		return object.Integer(-n)
	case object.Float:
		return object.Float(-n)
	}
	return raise("NoMethodError", "undefined method '-@' for %s", v.Inspect())
}

func valueEqual(a, b object.Value) bool {
	switch av := a.(type) {
	case object.Integer:
		if bv, ok := b.(object.Integer); ok {
			return av == bv
		}
		if bv, ok := b.(object.Float); ok {
			return float64(av) == float64(bv)
		}
	case object.Float:
		if bf, ok := toFloat(b); ok {
			return float64(av) == bf
		}
	case object.String:
		bv, ok := b.(object.String)
		return ok && av == bv
	case object.Symbol:
		bv, ok := b.(object.Symbol)
		return ok && av == bv
	case *object.Array:
		bv, ok := b.(*object.Array)
		if !ok || len(av.Elems) != len(bv.Elems) {
			return false
		}
		for i := range av.Elems {
			if !valueEqual(av.Elems[i], bv.Elems[i]) {
				return false
			}
		}
		return true
	case object.Bool:
		bv, ok := b.(object.Bool)
		return ok && av == bv
	case object.Nil:
		_, ok := b.(object.Nil)
		return ok
	}
	return false
}

func toFloat(v object.Value) (float64, bool) {
	switch n := v.(type) {
	case object.Integer:
		return float64(n), true
	case object.Float:
		return float64(n), true
	}
	return 0, false
}

// floorDiv / floorMod implement Ruby's floor-division semantics (the remainder
// takes the sign of the divisor), unlike Go's truncating / and %.
func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 {
	m := a % b
	if m != 0 && ((m < 0) != (b < 0)) {
		m += b
	}
	return m
}

func floatMod(a, b float64) float64 {
	m := a - b*float64(int64(a/b))
	if m != 0 && ((m < 0) != (b < 0)) {
		m += b
	}
	return m
}
