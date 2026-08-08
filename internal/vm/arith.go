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

	// Exact Integer/Float ordering: a large integer converted to a double loses
	// precision, so ordering it against a Float via float arithmetic can give the
	// wrong answer (e.g. (2**64+32) <= (2**64+32).to_f is false, since the float
	// rounds down to 2**64). Compare exactly via a rational instead — MRI does the
	// same. Arithmetic (+, -, *, /, %) still goes through the float path below.
	if isCompareOp(op) {
		if ai, ok := object.BigOf(a); ok {
			if bf, ok := b.(object.Float); ok {
				return intFloatCompare(op, ai, float64(bf), false)
			}
		}
		if bi, ok := object.BigOf(b); ok {
			if af, ok := a.(object.Float); ok {
				return intFloatCompare(op, bi, float64(af), true)
			}
		}
	}

	af, aIsNum := toFloat(a)
	bf, bIsNum := toFloat(b)
	if aIsNum && bIsNum {
		return floatOp(op, af, bf)
	}

	return raise("TypeError", "%s can't be coerced for %s", b.Inspect(), op)
}

// isCompareOp reports whether op is one of the four ordering comparisons.
func isCompareOp(op bytecode.Op) bool {
	switch op {
	case bytecode.OpLt, bytecode.OpGt, bytecode.OpLe, bytecode.OpGe:
		return true
	}
	return false
}

// intFloatCompare evaluates an ordering comparison between an exact integer and a
// float without precision loss. cmpBigFloat gives the sign of (int - float); when
// the float is the left operand that sign is inverted. A NaN operand makes every
// ordering comparison false, matching IEEE-754 / MRI.
func intFloatCompare(op bytecode.Op, a *big.Int, f float64, floatLeft bool) object.Value {
	c, ok := cmpBigFloat(a, f)
	if !ok { // NaN
		return object.Bool(false)
	}
	if floatLeft {
		c = -c
	}
	switch op {
	case bytecode.OpLt:
		return object.Bool(c < 0)
	case bytecode.OpGt:
		return object.Bool(c > 0)
	case bytecode.OpLe:
		return object.Bool(c <= 0)
	default: // bytecode.OpGe
		return object.Bool(c >= 0)
	}
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
	if o, ok := a.(*RObject); ok && !object.IsNil(o.builtin) {
		a = o.builtin
	}
	if o, ok := b.(*RObject); ok && !object.IsNil(o.builtin) {
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
			// A built-in number compared against a non-numeric right operand runs
			// MRI's coercion protocol for relational operators: other.coerce(self)
			// then re-dispatch the operator on the pair. A missing/invalid #coerce
			// raises ArgumentError ("comparison ... failed"); an exception inside
			// #coerce propagates. (String keeps its own ArgumentError from stringOp.)
			if isNumericValue(a) && !isNumericValue(b) {
				return vm.numericCoerceRelop(op, a, b)
			}
			return binary(op, a, b)
		}
		return vm.send(a, compareOpName(op), []object.Value{b}, nil)
	default:
		// Array#* with a String (or #to_str-convertible) right operand is Array#join
		// with that separator; it needs a live VM for the element/separator coercion
		// and encoding negotiation, so route it through join rather than the VM-less
		// arrayOp path. An Integer right operand keeps the fast repeat path below.
		if aa, ok := a.(*object.Array); ok && op == bytecode.OpMul {
			if _, isInt := b.(object.Integer); !isInt {
				if _, isStr := b.(*object.String); isStr || vm.respondsToDynamic(b, "to_str") {
					return vm.arrayJoin(aa, vm.joinSeparator(b), map[*object.Array]bool{})
				}
				// A non-String argument is coerced to an Integer via #to_int for the
				// repeat case; the coerced value falls through to arrayOp below.
				if vm.respondsToDynamic(b, "to_int") {
					b = vm.send(b, "to_int", nil, nil)
				}
			}
		}
		// String % args is Kernel#sprintf with the format as receiver; it needs a
		// live VM for MRI argument coercion (%s#to_s, %p#inspect, %d#to_int/#to_i,
		// %f#to_f, %{name}#to_s), so route it through the VM-aware formatter rather
		// than the VM-less stringOp path.
		if as, ok := a.(*object.String); ok && op == bytecode.OpMod {
			return object.NewString(vm.formatString(as.Str(), formatArgs(b)))
		}
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
		// A Money dispatches its arithmetic (+ - * over the go-ruby-money library,
		// raising Money::DifferentCurrencyError on a currency mismatch) as a method
		// rather than falling into the numeric coercion path.
		if _, isMoney := a.(*Money); isMoney {
			return vm.send(a, arithOpName(op), []object.Value{b}, nil)
		}
		// A Google::Protobuf::RepeatedField dispatches its + (a new list = the
		// receiver's elements followed by the other list's) as a method rather than
		// falling into the numeric coercion path.
		if _, isRF := a.(*ProtobufRepeatedField); isRF {
			return vm.send(a, arithOpName(op), []object.Value{b}, nil)
		}
		// An ActiveSupport::SafeBuffer dispatches its + (a new html-safe buffer with
		// the right operand escaped-unless-safe) as a method, so a view can build
		// markup with `f.label + f.text_field` rather than hitting the coercion path.
		if _, isSB := a.(*SafeBufferVal); isSB {
			return vm.send(a, arithOpName(op), []object.Value{b}, nil)
		}
		// Numeric coercion protocol: when a built-in number is combined with a
		// non-numeric object that answers #coerce, MRI calls other.coerce(self)
		// — expecting a [a2, b2] pair of same-type numbers — then re-runs the
		// operator on that pair. This lets user Numeric-likes (and spec mocks)
		// interoperate with Integer/Float/Rational/Complex arithmetic.
		if isNumericValue(a) && !isNumericValue(b) {
			if res, ok := vm.tryNumericCoerce(arithOpName(op), a, b); ok {
				return res
			}
		}
		return binary(op, a, b)
	}
}

// isNumericValue reports whether v is one of the built-in numeric value types
// that drive the arithmetic fast paths (and never trigger the coercion
// protocol as a left operand's partner).
func isNumericValue(v object.Value) bool {
	switch v.(type) {
	case object.Integer, object.Float, *object.Bignum, *object.Rational, *object.Complex, *BigDecimal:
		return true
	}
	return false
}

// tryNumericCoerce runs MRI's numeric coercion protocol for `a op b` where a is
// a built-in number and b is a non-numeric object. It returns (result, true)
// when b responds to #coerce; the pair b returns is validated (a two-element
// Array) and the operator re-dispatched on it. Any exception raised by #coerce
// propagates (MRI does not rescue it). When b does not respond to #coerce it
// returns (nil, false) so the caller falls back to its own coercion error.
func (vm *VM) tryNumericCoerce(op string, a, b object.Value) (object.Value, bool) {
	if !vm.respondsToDynamic(b, "coerce") {
		return nil, false
	}
	pair := vm.send(b, "coerce", []object.Value{a}, nil)
	arr, ok := pair.(*object.Array)
	if !ok || len(arr.Elems) != 2 {
		raise("TypeError", "coerce must return [x, y]")
	}
	return vm.send(arr.Elems[0], op, []object.Value{arr.Elems[1]}, nil), true
}

// numericCoerceRelop runs MRI's coercion protocol for a relational operator
// (< <= > >=) where a is a built-in number and b is a non-numeric object. When b
// answers #coerce it is called (exceptions propagate, unrescued) and the operator
// is re-dispatched on the returned [x, y] pair. A missing #coerce, or one that
// returns something other than a two-element Array, raises ArgumentError with the
// "comparison of A with B failed" message — matching MRI's rb_num_coerce_relop.
func (vm *VM) numericCoerceRelop(op bytecode.Op, a, b object.Value) object.Value {
	if vm.respondsToDynamic(b, "coerce") {
		pair := vm.send(b, "coerce", []object.Value{a}, nil)
		if arr, ok := pair.(*object.Array); ok && len(arr.Elems) == 2 {
			return vm.send(arr.Elems[0], compareOpName(op), []object.Value{arr.Elems[1]}, nil)
		}
	}
	return raise("ArgumentError", "comparison of %s with %s failed", classNameOf(a), cmperrOperand(b))
}

// cmperrOperand renders the right operand of a failed comparison the way MRI's
// rb_cmperr does: immediates (nil/true/false) and Symbols are inspected; every
// other object is named by its class. (Numeric operands never reach here — they
// take the arithmetic fast path — so no Float/Integer case is needed.)
func cmperrOperand(b object.Value) string {
	switch b.(type) {
	case object.Symbol, object.Nil, object.Bool:
		return b.Inspect()
	default:
		return classNameOf(b)
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
		out := make([]byte, 0, len(a.Bytes())+len(bs.Bytes()))
		out = append(append(out, a.Bytes()...), bs.Bytes()...)
		return object.NewStringBytesEnc(out, a.Enc) // result keeps the receiver's encoding
	case bytecode.OpMul:
		n, ok := b.(object.Integer)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Integer", b.Inspect())
		}
		if n < 0 {
			raise("ArgumentError", "negative argument")
		}
		out := make([]byte, 0, len(a.Bytes())*int(n))
		for i := int64(0); i < int64(n); i++ {
			out = append(out, a.Bytes()...)
		}
		return object.NewStringBytesEnc(out, a.Enc) // result keeps the receiver's encoding
	// String % args (OpMod) is intercepted in binaryOp and routed through the
	// VM-aware formatter, so it never reaches this VM-less path.
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
		return object.NewArrayFromSlice(out)
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
		return object.NewArrayFromSlice(out)
	case bytecode.OpMul:
		// Array * String (join with a separator) is intercepted in binaryOp and
		// routed through Array#join for full coercion/encoding handling, so only the
		// Integer repeat case reaches here.
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
		return object.NewArrayFromSlice(out)
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
		return object.IntValue(int64(-n))
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
	case *Money:
		// -money negates the amount via the go-ruby-money library.
		return &Money{m: n.m.Neg()}
	}
	return raise("NoMethodError", "undefined method '-@' for %s", v.Inspect())
}

// eqPair keys the recursion guard used by valueEqual/valueEql: a container pair
// currently being compared. Re-encountering the same pair means the structures
// are mutually self-referential; MRI treats such a recursive comparison as equal
// (rb_exec_recursive) rather than looping forever.
type eqPair struct{ a, b object.Value }

func valueEqual(a, b object.Value) bool {
	return valueEqualRec(a, b, nil)
}

func valueEqualRec(a, b object.Value, seen map[eqPair]struct{}) bool {
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
	// Value types from the js/wasm-incompatible network/OS bindings (e.g. Arrow's
	// DataType, which compares through the go-ruby-arrow library) equate through
	// the build-tagged seam so this shared comparison compiles for wasm, where
	// those types can never be constructed. ok is false for every other value.
	if eq, ok := valueEqualExtBinding(a, b); ok {
		return eq
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
		return ok && string(av.Bytes()) == string(bv.Bytes())
	case object.Symbol:
		bv, ok := b.(object.Symbol)
		return ok && av == bv
	case *object.Array:
		bv, ok := b.(*object.Array)
		if !ok || len(av.Elems) != len(bv.Elems) {
			return false
		}
		key := eqPair{av, bv}
		if _, rec := seen[key]; rec {
			return true // recursive pair: MRI compares as equal
		}
		if seen == nil {
			seen = map[eqPair]struct{}{}
		}
		seen[key] = struct{}{}
		defer delete(seen, key)
		for i := range av.Elems {
			if !valueEqualRec(av.Elems[i], bv.Elems[i], seen) {
				return false
			}
		}
		return true
	case *object.Hash:
		bv, ok := b.(*object.Hash)
		if !ok || av.Len() != bv.Len() {
			return false
		}
		key := eqPair{av, bv}
		if _, rec := seen[key]; rec {
			return true
		}
		if seen == nil {
			seen = map[eqPair]struct{}{}
		}
		seen[key] = struct{}{}
		defer delete(seen, key)
		for _, k := range av.Keys {
			ae, _ := av.Get(k)
			be, present := bv.Get(k)
			if !present || !valueEqualRec(ae, be, seen) {
				return false
			}
		}
		return hashIdentityMatch(av, bv)
	case *object.Range:
		bv, ok := b.(*object.Range)
		return ok && av.Exclusive == bv.Exclusive && valueEqualRec(av.Lo, bv.Lo, seen) && valueEqualRec(av.Hi, bv.Hi, seen)
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
	return valueEqlRec(a, b, nil)
}

func valueEqlRec(a, b object.Value, seen map[eqPair]struct{}) bool {
	if o, ok := a.(*RObject); ok && !object.IsNil(o.builtin) {
		a = o.builtin
	}
	if o, ok := b.(*RObject); ok && !object.IsNil(o.builtin) {
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
		return ok && string(av.Bytes()) == string(bv.Bytes())
	case object.Symbol:
		bv, ok := b.(object.Symbol)
		return ok && av == bv
	case *object.Array:
		bv, ok := b.(*object.Array)
		if !ok || len(av.Elems) != len(bv.Elems) {
			return false
		}
		key := eqPair{av, bv}
		if _, rec := seen[key]; rec {
			return true // recursive pair: MRI compares as equal
		}
		if seen == nil {
			seen = map[eqPair]struct{}{}
		}
		seen[key] = struct{}{}
		defer delete(seen, key)
		for i := range av.Elems {
			if !valueEqlRec(av.Elems[i], bv.Elems[i], seen) {
				return false
			}
		}
		return true
	case *object.Hash:
		bv, ok := b.(*object.Hash)
		if !ok || av.Len() != bv.Len() {
			return false
		}
		key := eqPair{av, bv}
		if _, rec := seen[key]; rec {
			return true
		}
		if seen == nil {
			seen = map[eqPair]struct{}{}
		}
		seen[key] = struct{}{}
		defer delete(seen, key)
		for _, k := range av.Keys {
			v1, _ := av.Get(k)
			v2, present := bv.Get(k)
			if !present || !valueEqlRec(v1, v2, seen) {
				return false
			}
		}
		return hashIdentityMatch(av, bv)
	}
	return a == b // identity for nil/true/false and other reference types
}

// hashIdentityMatch reports whether two content-equal hashes also agree on their
// compare_by_identity state: MRI treats {1=>2}.compare_by_identity as unequal to
// a plain {1=>2}, but two empty hashes are equal regardless of the flag.
func hashIdentityMatch(a, b *object.Hash) bool {
	return a.Len() == 0 || a.Identity == b.Identity
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
