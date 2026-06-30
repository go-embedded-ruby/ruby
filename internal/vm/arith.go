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

	// BigDecimal arithmetic: + - * / % delegate to the go-ruby-bigdecimal library
	// (MRI-exact arbitrary-precision decimal). A BigDecimal operand wins the
	// numeric tower in either position (BigDecimal + Rational and Rational +
	// BigDecimal are both BigDecimal), so this is checked before the Rational
	// fast path; the non-BigDecimal operand is coerced to BigDecimal.
	if ab, ok := a.(*BigDecimal); ok {
		return bigDecimalOp(op, ab, b)
	}
	if bb, ok := b.(*BigDecimal); ok {
		return bigDecimalRightOp(op, a, bb)
	}

	// Rational fast paths. A Float operand makes the result Float (Float wins the
	// numeric tower); an Integer/Bignum stays exact.
	if _, ok := a.(*object.Rational); ok {
		return rationalOp(op, a, b)
	}
	if _, ok := b.(*object.Rational); ok {
		return rationalOp(op, a, b)
	}

	// Set algebra: + (union) and - (difference) reach the operator fast path
	// (the other combinators — & | << — dispatch as methods). The right operand
	// must be a Set.
	if as, ok := a.(*Set); ok {
		return setOp(op, as, b)
	}

	// IPAddr arithmetic: ip + n / ip - n (shift the address by a whole-number
	// offset) reach the operator fast path (the bitwise & | ~ combinators dispatch
	// as methods). The right operand is an integer offset, mirroring MRI's
	// IPAddr#+ / IPAddr#-.
	if ai, ok := a.(*IPAddr); ok {
		return ipaddrOp(op, ai, b)
	}

	// Matrix / Vector arithmetic: + and - reach the operator fast path (the other
	// operators — * / ** -@ — dispatch as methods). The right operand must be the
	// same wrapper type.
	if _, ok := a.(*Matrix); ok {
		return matrixOp(op, a, b)
	}
	if _, ok := a.(*Vector); ok {
		return matrixOp(op, a, b)
	}

	// Time arithmetic: t + secs / t - secs (shift by a Duration) and t - other
	// (the seconds between two instants) reach the operator fast path.
	if at, ok := a.(*Time); ok {
		return timeOp(op, at, b)
	}

	// Date arithmetic: d + n / d - n (shift by a whole number of days) and
	// d - other (the day count between two dates) reach the operator fast path.
	if ad, ok := a.(*Date); ok {
		return dateOp(op, ad, b)
	}

	// Bag (multiset) algebra: + (Sum, additive union) and - (Difference) reach
	// the operator fast path; the other combinators — & | — dispatch as methods.
	// The right operand must be a Bag.
	if ab, ok := a.(*Bag); ok {
		return bagOp(op, ab, b)
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
	// An instance of a user subclass of a built-in value type uses that value's
	// own operators (so a String-subclass "+", an Array-subclass "*", and the
	// comparisons all work), on either side of the operator.
	if o, ok := a.(*RObject); ok && o.builtin != nil {
		a = o.builtin
	}
	if o, ok := b.(*RObject); ok && o.builtin != nil {
		b = o.builtin
	}
	switch op {
	case bytecode.OpEq, bytecode.OpNeq:
		// Objects dispatch `==` (so Object identity, a user `==`, or
		// Comparable#== all apply); a builtin instance whose class defines its own
		// `==` (e.g. Digest::Instance#==, which compares hex digests) dispatches it
		// too; the remaining value types keep structural equality.
		if _, isObj := a.(*RObject); isObj || hasCustomEq(vm, a) {
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
		// A user object (RObject with no builtin backing) that defines an
		// arithmetic operator dispatches to it, so `Pathname + str`, a Money `+`,
		// etc. work. Built-in value types keep the inline path (and its coercion
		// errors).
		if _, isObj := a.(*RObject); isObj {
			return vm.send(a, arithOpName(op), []object.Value{b}, nil)
		}
		// A URI dispatches its arithmetic operator (only + is defined, resolving a
		// reference) as a method, so the binding's merge — which needs the VM to
		// wrap the result — runs with a live VM rather than the VM-less binary path.
		if _, isURI := a.(*URI); isURI {
			return vm.send(a, arithOpName(op), []object.Value{b}, nil)
		}
		// A Benchmark::Tms dispatches its memberwise/scalar + - * / as methods
		// (defined in internal/vm/benchmark.go), so the library's Tms arithmetic
		// runs rather than the numeric coercion path.
		if _, isTms := a.(*Tms); isTms {
			return vm.send(a, arithOpName(op), []object.Value{b}, nil)
		}
		return binary(op, a, b)
	}
}

// arithOpName names the arithmetic/modulo operator behind an opcode for method
// dispatch on a user object. Only the five arithmetic opcodes reach binaryOp's
// default branch, so each maps to exactly one operator name.
func arithOpName(op bytecode.Op) string {
	switch op {
	case bytecode.OpAdd:
		return "+"
	case bytecode.OpSub:
		return "-"
	case bytecode.OpMul:
		return "*"
	case bytecode.OpDiv:
		return "/"
	default: // bytecode.OpMod
		return "%"
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
			return object.IntValue(c)
		}
		return object.NormInt(new(big.Int).Add(big.NewInt(a), big.NewInt(b)))
	case bytecode.OpSub:
		if c := a - b; (c <= a) == (b >= 0) {
			return object.IntValue(c)
		}
		return object.NormInt(new(big.Int).Sub(big.NewInt(a), big.NewInt(b)))
	case bytecode.OpMul:
		if c := a * b; a == 0 || (c/a == b && !(a == -1 && b == minInt64)) {
			return object.IntValue(c)
		}
		return object.NormInt(new(big.Int).Mul(big.NewInt(a), big.NewInt(b)))
	case bytecode.OpDiv:
		if b == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		return object.IntValue(floorDiv(a, b))
	case bytecode.OpMod:
		if b == 0 {
			raise("ZeroDivisionError", "divided by 0")
		}
		return object.IntValue(floorMod(a, b))
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
		return &object.String{B: out, Enc: a.Enc} // result keeps the receiver's encoding
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
		return &object.String{B: out, Enc: a.Enc} // result keeps the receiver's encoding
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
// curried returns a lambda that accumulates arguments across calls until it holds
// at least `need` of them, then invokes p with all of them; otherwise it returns
// a further curried lambda. Backs Proc#curry.
func (vm *VM) curried(p *Proc, need int, got []object.Value) *Proc {
	return &Proc{isLambda: true, nativeArity: -1, native: func(vm *VM, args []object.Value) object.Value {
		all := append(append([]object.Value{}, got...), args...)
		if len(all) >= need {
			return vm.callBlock(p, all)
		}
		return vm.curried(p, need, all)
	}}
}

// arrayUniq returns elems with duplicates removed, keeping first-seen order and
// comparing with eql?. With a block, elements are distinguished by the block's
// return value rather than the element itself.
func (vm *VM) arrayUniq(elems []object.Value, blk *Proc) []object.Value {
	var out, keys []object.Value
	for _, e := range elems {
		key := e
		if blk != nil {
			key = vm.callBlock(blk, []object.Value{e})
		}
		seen := false
		for _, k := range keys {
			if valueEql(key, k) {
				seen = true
				break
			}
		}
		if !seen {
			keys = append(keys, key)
			out = append(out, e)
		}
	}
	return out
}

// arrayIncludes backs the set operators &, | and - (difference), which compare
// with eql? — so e.g. 1 and 1.0 are distinct members. Membership tests like
// include?/index/count use == instead and do not go through here.
func arrayIncludes(elems []object.Value, v object.Value) bool {
	for _, e := range elems {
		if valueEql(e, v) {
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
	case *BigDecimal:
		return &BigDecimal{d: n.d.Neg()}
	case *Matrix:
		return &Matrix{m: n.m.Neg()}
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
	// BigDecimal compares by value, coercing a numeric operand (2 == BigDecimal("2"),
	// BigDecimal("1.5") == Rational(3, 2)), in either operand order. Checked before
	// Rational so a BigDecimal operand drives the (decimal-precise) comparison.
	if ab, ok := a.(*BigDecimal); ok {
		return bigDecimalEqual(ab, b)
	}
	if bb, ok := b.(*BigDecimal); ok {
		return bigDecimalEqual(bb, a)
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
	case *Set:
		bv, ok := b.(*Set)
		return ok && av.s.EqualQ(bv.s)
	case *IPAddr:
		bv, ok := b.(*IPAddr)
		return ok && av.ip.Eql(bv.ip)
	case *Matrix:
		return eqMatrix(av, b)
	case *Vector:
		return eqVector(av, b)
	case *Bag:
		bv, ok := b.(*Bag)
		return ok && av.b.Equal(bv.b)
	case *Time:
		return timeEqual(av, b)
	case *Date:
		return dateEqual(av, b)
	case *URI:
		return uriEqual(av, b)
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

// valueEql implements Object#eql?: like ==, but without numeric coercion, so an
// Integer is never eql? a Float (1.eql?(1.0) is false) and Array/Hash compare
// their members with eql? too. A built-in value subclass instance is compared as
// the value it wraps; everything else falls back to object identity.
func valueEql(a, b object.Value) bool {
	if o, ok := a.(*RObject); ok && o.builtin != nil {
		a = o.builtin
	}
	if o, ok := b.(*RObject); ok && o.builtin != nil {
		b = o.builtin
	}
	switch av := a.(type) {
	case object.Integer:
		bv, ok := b.(object.Integer)
		return ok && av == bv
	case object.Float:
		bv, ok := b.(object.Float)
		return ok && av == bv
	case *object.Bignum:
		bv, ok := b.(*object.Bignum)
		return ok && av.I.Cmp(bv.I) == 0
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
			if !valueEql(av.Elems[i], bv.Elems[i]) {
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
			v1, _ := av.Get(k)
			v2, present := bv.Get(k)
			if !present || !valueEql(v1, v2) {
				return false
			}
		}
		return true
	}
	return a == b // identity for nil/true/false and other reference types
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
