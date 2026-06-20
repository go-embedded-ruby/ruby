package vm

import (
	"math"
	"math/big"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

const minInt64 = math.MinInt64

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

	// String fast paths: "a" + "b" and "a" * 3.
	if as, ok := a.(*object.String); ok {
		return stringOp(op, as, b)
	}

	// Array fast paths: [1] + [2], [1, 2, 1] - [1], [1, 2] * 3 / [1, 2] * ",".
	if aa, ok := a.(*object.Array); ok {
		return arrayOp(op, aa, b)
	}

	// Complex fast paths, both Complex⊕x and x⊕Complex (a real number coerces to
	// a Complex with zero imaginary part).
	if ac, ok := a.(*object.Complex); ok {
		return complexOp(op, ac, b)
	}
	if bc, ok := b.(*object.Complex); ok {
		ac, ok := asComplexVal(a)
		if !ok {
			return raise("TypeError", "%s can't be coerced into Complex", a.Inspect())
		}
		return complexOp(op, ac, bc)
	}

	// Rational fast paths. A Float operand makes the result Float (Float wins the
	// numeric tower); an Integer/Bignum stays exact.
	if _, ok := a.(*object.Rational); ok {
		return rationalOp(op, a, b)
	}
	if _, ok := b.(*object.Rational); ok {
		return rationalOp(op, a, b)
	}

	// NDArray element-wise / scalar arithmetic, in either operand order.
	if _, ok := a.(*NDArray); ok {
		return ndarrayOp(op, a, b)
	}
	if _, ok := b.(*NDArray); ok {
		return ndarrayOp(op, a, b)
	}

	ai, aok := a.(object.Integer)
	bi, bok := b.(object.Integer)
	if aok && bok {
		return intOp(op, int64(ai), int64(bi))
	}

	// Bignum, or an Integer/Bignum mix → arbitrary-precision arithmetic.
	if abig, ok := object.BigOf(a); ok {
		if bbig, ok := object.BigOf(b); ok {
			return bigOp(op, abig, bbig)
		}
	}

	af, aIsNum := toFloat(a)
	bf, bIsNum := toFloat(b)
	if aIsNum && bIsNum {
		return floatOp(op, af, bf)
	}

	return raise("TypeError", "%s can't be coerced for %s", b.Inspect(), op)
}

// bigOp performs an arbitrary-precision integer operation, normalizing the
// result back to an Integer when it fits. big.Int Div/Mod are Euclidean, which
// matches Ruby's floored division (a non-negative modulus).
func bigOp(op bytecode.Op, a, b *big.Int) object.Value {
	switch op {
	case bytecode.OpAdd:
		return object.NormInt(new(big.Int).Add(a, b))
	case bytecode.OpSub:
		return object.NormInt(new(big.Int).Sub(a, b))
	case bytecode.OpMul:
		return object.NormInt(new(big.Int).Mul(a, b))
	case bytecode.OpDiv:
		if b.Sign() == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		return object.NormInt(new(big.Int).Div(a, b))
	case bytecode.OpMod:
		if b.Sign() == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		return object.NormInt(new(big.Int).Mod(a, b))
	case bytecode.OpLt:
		return object.Bool(a.Cmp(b) < 0)
	case bytecode.OpGt:
		return object.Bool(a.Cmp(b) > 0)
	case bytecode.OpLe:
		return object.Bool(a.Cmp(b) <= 0)
	case bytecode.OpGe:
		return object.Bool(a.Cmp(b) >= 0)
	}
	return raise("VMError", "bad integer op %s", op)
}

// binaryOp evaluates an operator opcode. Arithmetic and numeric/string
// comparisons keep the Phase 0 fast path; everything else dispatches as a
// method so user classes (and the embedded-Ruby Comparable mixin) can define
// `<`, `<=`, `>`, `>=` and `==`.
func (vm *VM) binaryOp(op bytecode.Op, a, b object.Value) object.Value {
	switch op {
	case bytecode.OpEq, bytecode.OpNeq:
		// Objects dispatch `==` (so Object identity, a user `==`, or
		// Comparable#== all apply); value types keep structural equality.
		if _, isObj := a.(*RObject); isObj {
			eq := vm.send(a, "==", []object.Value{b}, nil).Truthy()
			if op == bytecode.OpNeq {
				eq = !eq
			}
			return object.Bool(eq)
		}
		return binary(op, a, b)
	case bytecode.OpLt, bytecode.OpGt, bytecode.OpLe, bytecode.OpGe:
		if hasFastOrdering(a) {
			return binary(op, a, b)
		}
		return vm.send(a, compareOpName(op), []object.Value{b}, nil)
	default:
		return binary(op, a, b)
	}
}

// operatorOpcode maps a binary-operator method name to its opcode, for the
// fast-path operators that have no method-table entry. The ordering operators
// (< <= > >=) come from the Comparable prelude and == from Object#==, so those
// are reached by normal lookup; only the arithmetic operators (no method) and
// != (no method anywhere) fall through to here.
func operatorOpcode(name string) (bytecode.Op, bool) {
	switch name {
	case "+":
		return bytecode.OpAdd, true
	case "-":
		return bytecode.OpSub, true
	case "*":
		return bytecode.OpMul, true
	case "/":
		return bytecode.OpDiv, true
	case "%":
		return bytecode.OpMod, true
	case "!=":
		return bytecode.OpNeq, true
	}
	return 0, false
}

// hasFastOrdering reports whether the receiver is a built-in ordered type.
// Those keep the Phase 0 inline comparison (including its own coercion errors
// for a bad right operand); anything else dispatches `<`/`<=`/`>`/`>=`.
func hasFastOrdering(a object.Value) bool {
	switch a.(type) {
	case object.Integer, object.Float, *object.String, *object.Bignum:
		return true
	}
	return false
}

// compareOpName names the ordering operator behind an opcode for method
// dispatch. Only the four ordering opcodes reach it.
func compareOpName(op bytecode.Op) string {
	switch op {
	case bytecode.OpLt:
		return "<"
	case bytecode.OpGt:
		return ">"
	case bytecode.OpLe:
		return "<="
	}
	return ">=" // bytecode.OpGe
}

func intOp(op bytecode.Op, a, b int64) object.Value {
	switch op {
	case bytecode.OpAdd:
		if c := a + b; (c >= a) == (b >= 0) { // no signed overflow
			return object.Integer(c)
		}
		return object.NormInt(new(big.Int).Add(big.NewInt(a), big.NewInt(b)))
	case bytecode.OpSub:
		if c := a - b; (c <= a) == (b >= 0) {
			return object.Integer(c)
		}
		return object.NormInt(new(big.Int).Sub(big.NewInt(a), big.NewInt(b)))
	case bytecode.OpMul:
		if c := a * b; a == 0 || (c/a == b && !(a == -1 && b == minInt64)) {
			return object.Integer(c)
		}
		return object.NormInt(new(big.Int).Mul(big.NewInt(a), big.NewInt(b)))
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

func stringOp(op bytecode.Op, a *object.String, b object.Value) object.Value {
	switch op {
	case bytecode.OpAdd:
		bs, ok := b.(*object.String)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into String", b.Inspect())
		}
		out := make([]byte, 0, len(a.B)+len(bs.B))
		out = append(append(out, a.B...), bs.B...)
		return &object.String{B: out}
	case bytecode.OpMul:
		n, ok := b.(object.Integer)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Integer", b.Inspect())
		}
		if n < 0 {
			raise("ArgumentError", "negative argument")
		}
		out := make([]byte, 0, len(a.B)*int(n))
		for i := int64(0); i < int64(n); i++ {
			out = append(out, a.B...)
		}
		return &object.String{B: out}
	case bytecode.OpMod:
		return object.NewString(formatString(a.Str(), formatArgs(b)))
	case bytecode.OpLt, bytecode.OpGt, bytecode.OpLe, bytecode.OpGe:
		bs, ok := b.(*object.String)
		if !ok {
			raise("ArgumentError", "comparison of String with %s failed", b.Inspect())
		}
		as, bsv := a.Str(), bs.Str()
		switch op {
		case bytecode.OpLt:
			return object.Bool(as < bsv)
		case bytecode.OpGt:
			return object.Bool(as > bsv)
		case bytecode.OpLe:
			return object.Bool(as <= bsv)
		default:
			return object.Bool(as >= bsv)
		}
	}
	return raise("NoMethodError", "undefined method '%s' for a String", op)
}

// arrayOp applies a fast-path operator with an Array receiver: + concatenates,
// - removes (set difference, keeping order/duplicates of the left), and * either
// repeats (Integer) or joins (String).
func arrayOp(op bytecode.Op, a *object.Array, b object.Value) object.Value {
	switch op {
	case bytecode.OpAdd:
		bb, ok := b.(*object.Array)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Array", b.Inspect())
		}
		out := make([]object.Value, 0, len(a.Elems)+len(bb.Elems))
		out = append(append(out, a.Elems...), bb.Elems...)
		return &object.Array{Elems: out}
	case bytecode.OpSub:
		bb, ok := b.(*object.Array)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Array", b.Inspect())
		}
		var out []object.Value
		for _, e := range a.Elems {
			if !arrayIncludes(bb.Elems, e) {
				out = append(out, e)
			}
		}
		return &object.Array{Elems: out}
	case bytecode.OpMul:
		if sep, ok := b.(*object.String); ok {
			return object.NewString(joinArray(a, sep.Str()))
		}
		n, ok := b.(object.Integer)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Integer", b.Inspect())
		}
		if n < 0 {
			raise("ArgumentError", "negative argument")
		}
		out := make([]object.Value, 0, len(a.Elems)*int(n))
		for i := int64(0); i < int64(n); i++ {
			out = append(out, a.Elems...)
		}
		return &object.Array{Elems: out}
	}
	return raise("NoMethodError", "undefined method '%s' for an Array", op)
}

// arrayIncludes reports whether v is in elems (by Ruby ==).
func arrayIncludes(elems []object.Value, v object.Value) bool {
	for _, e := range elems {
		if valueEqual(e, v) {
			return true
		}
	}
	return false
}

func negate(v object.Value) object.Value {
	switch n := v.(type) {
	case object.Integer:
		if n == minInt64 { // -minInt64 overflows int64 → promote
			return object.NormInt(new(big.Int).Neg(big.NewInt(int64(n))))
		}
		return object.Integer(-n)
	case object.Float:
		return object.Float(-n)
	case *object.Bignum:
		return object.NormInt(new(big.Int).Neg(n.I))
	case *object.Complex:
		return &object.Complex{Re: negate(n.Re), Im: negate(n.Im)}
	case *object.Rational:
		return &object.Rational{R: new(big.Rat).Neg(n.R)}
	}
	return raise("NoMethodError", "undefined method '-@' for %s", v.Inspect())
}

func valueEqual(a, b object.Value) bool {
	// Complex compares component-wise, and equals a real number when its
	// imaginary part is zero (Complex(2, 0) == 2), in either operand order.
	if ac, ok := a.(*object.Complex); ok {
		return complexEqual(ac, b)
	}
	if bc, ok := b.(*object.Complex); ok {
		return complexEqual(bc, a)
	}
	if ar, ok := a.(*object.Rational); ok {
		return rationalEqual(ar, b)
	}
	if br, ok := b.(*object.Rational); ok {
		return rationalEqual(br, a)
	}
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
	case *object.Bignum:
		// A Bignum is, by construction, outside int64 range, so it can only equal
		// another Bignum of the same magnitude.
		if bv, ok := b.(*object.Bignum); ok {
			return av.I.Cmp(bv.I) == 0
		}
	case *object.String:
		bv, ok := b.(*object.String)
		return ok && string(av.B) == string(bv.B)
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
	case *object.Hash:
		bv, ok := b.(*object.Hash)
		if !ok || av.Len() != bv.Len() {
			return false
		}
		for _, k := range av.Keys {
			ae, _ := av.Get(k)
			be, present := bv.Get(k)
			if !present || !valueEqual(ae, be) {
				return false
			}
		}
		return true
	case *object.Range:
		bv, ok := b.(*object.Range)
		return ok && av.Exclusive == bv.Exclusive && valueEqual(av.Lo, bv.Lo) && valueEqual(av.Hi, bv.Hi)
	case *Regexp:
		bv, ok := b.(*Regexp)
		return ok && av.source == bv.source && orderFlags(av.flags) == orderFlags(bv.flags)
	case object.Bool:
		bv, ok := b.(object.Bool)
		return ok && av == bv
	case object.Nil:
		_, ok := b.(object.Nil)
		return ok
	}
	// Reference types not handled above (classes, procs, …) compare by identity,
	// which is Ruby's default Object#==.
	return a == b
}

func toFloat(v object.Value) (float64, bool) {
	switch n := v.(type) {
	case object.Integer:
		return float64(n), true
	case object.Float:
		return float64(n), true
	case *object.Bignum:
		f, _ := new(big.Float).SetInt(n.I).Float64()
		return f, true
	case *object.Rational:
		f, _ := n.R.Float64()
		return f, true
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
