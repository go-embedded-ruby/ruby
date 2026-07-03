package object

import "math"

// This file defines the Value accessor/constructor SEAM. Every function here has
// a signature that was stable across the interface→tagged-struct flip: the
// mechanical rewriter converted every type-switch / type-assertion / implicit
// heap→Value conversion site to call these seams while Value was still an
// interface (behaviour-neutral, testable), and the flip then swapped only the
// bodies below plus the Value definition in object.go.
//
// Accessors are free functions (not methods) deliberately: a method cannot be
// declared on an interface type, so `AsInt(v)` (not `v.AsInt()`) was the only
// form that compiled both before and after the flip with one signature.

// Tag values classify a Value's payload. TagNil is 0 so the zero Value{} is Ruby
// nil. Immediates (Int/Float/Bool) live in the scalar word; every heap-backed
// value (Symbol and pointer types) lives in the object word. The fine-grained
// heap tags exist for TagOf fidelity/documentation; a value produced by Wrap
// carries TagObject and every heap accessor identifies the concrete type from
// the object word, so a heap tag is never load-bearing.
const (
	TagNil uint8 = iota
	TagInt
	TagFloat
	TagBool
	TagSym
	TagString
	TagArray
	TagHash
	TagBignum
	TagComplex
	TagRational
	TagRange
	TagMain
	TagObject // RObject and all VM-defined heap types (generic)
)

// RubyObj is the behaviour every heap-backed Ruby value provides. Every concrete
// value type satisfies it; the tagged-struct Value stores a RubyObj in its
// object word for heap-backed values (and Value itself also satisfies it via its
// ToS/Inspect/Truthy methods).
type RubyObj interface {
	ToS() string
	Inspect() string
	Truthy() bool
}

// --- Constructors (return Value) -------------------------------------------

// FloatValue boxes f as a Value. The float is stored as its IEEE-754 bit
// pattern, which round-trips identically on little- and big-endian hosts.
func FloatValue(f float64) Value { return Value{tag: TagFloat, i: int64(math.Float64bits(f))} }

// BoolValue boxes b as a Value.
func BoolValue(b bool) Value {
	if b {
		return Value{tag: TagBool, i: 1}
	}
	return Value{tag: TagBool, i: 0}
}

// Wrap boxes a heap-backed Ruby object (Symbol, any pointer value type, RObject
// or any VM binding type) as a Value. It must NOT be used for the immediate
// value types (Integer/Float/Bool/Nil) — those have dedicated constructors so
// they never allocate. Symbol is heap-backed here (stored in the object word),
// but SymVal should be preferred for symbols so the box is interned.
func Wrap(o RubyObj) Value { return Value{tag: TagObject, obj: o} }

// --- Predicates + accessors (free functions) --------------------------------

// TagOf returns v's tag.
func TagOf(v Value) uint8 { return v.tag }

// IsInt reports whether v is an Integer (fixnum).
func IsInt(v Value) bool { return v.tag == TagInt }

// AsInt returns v's int64 value (undefined if v is not an Integer).
func AsInt(v Value) int64 { return v.i }

// AsInteger returns v as object.Integer.
func AsInteger(v Value) Integer { return Integer(v.i) }

// IsFloat reports whether v is a Float.
func IsFloat(v Value) bool { return v.tag == TagFloat }

// AsFloat returns v's float64 value.
func AsFloat(v Value) float64 { return math.Float64frombits(uint64(v.i)) }

// AsFloatV returns v as object.Float.
func AsFloatV(v Value) Float { return Float(math.Float64frombits(uint64(v.i))) }

// IsBool reports whether v is a Bool.
func IsBool(v Value) bool { return v.tag == TagBool }

// AsBool returns v's bool value.
func AsBool(v Value) bool { return v.i != 0 }

// AsBoolV returns v as object.Bool.
func AsBoolV(v Value) Bool { return Bool(v.i != 0) }

// IsSym reports whether v is a Symbol.
func IsSym(v Value) bool { return v.tag == TagSym }

// AsSym returns v's symbol name.
func AsSym(v Value) string { return string(v.obj.(Symbol)) }

// AsSymbol returns v as object.Symbol.
func AsSymbol(v Value) Symbol { return v.obj.(Symbol) }

// IsString reports whether v is a *String.
func IsString(v Value) bool { _, ok := v.obj.(*String); return ok }

// AsString returns v as *String; it panics if v is not a *String.
func AsString(v Value) *String { return v.obj.(*String) }

// AsStringOK returns v as *String and whether it is one.
func AsStringOK(v Value) (*String, bool) { s, ok := v.obj.(*String); return s, ok }

// IsArray reports whether v is a *Array.
func IsArray(v Value) bool { _, ok := v.obj.(*Array); return ok }

// AsArray returns v as *Array; it panics if v is not a *Array.
func AsArray(v Value) *Array { return v.obj.(*Array) }

// AsArrayOK returns v as *Array and whether it is one.
func AsArrayOK(v Value) (*Array, bool) { a, ok := v.obj.(*Array); return a, ok }

// IsHash reports whether v is a *Hash.
func IsHash(v Value) bool { _, ok := v.obj.(*Hash); return ok }

// AsHash returns v as *Hash; it panics if v is not a *Hash.
func AsHash(v Value) *Hash { return v.obj.(*Hash) }

// AsHashOK returns v as *Hash and whether it is one.
func AsHashOK(v Value) (*Hash, bool) { h, ok := v.obj.(*Hash); return h, ok }

// IsBignum reports whether v is a *Bignum.
func IsBignum(v Value) bool { _, ok := v.obj.(*Bignum); return ok }

// AsBignum returns v as *Bignum; it panics if v is not a *Bignum.
func AsBignum(v Value) *Bignum { return v.obj.(*Bignum) }

// AsBignumOK returns v as *Bignum and whether it is one.
func AsBignumOK(v Value) (*Bignum, bool) { b, ok := v.obj.(*Bignum); return b, ok }

// IsComplex reports whether v is a *Complex.
func IsComplex(v Value) bool { _, ok := v.obj.(*Complex); return ok }

// AsComplex returns v as *Complex; it panics if v is not a *Complex.
func AsComplex(v Value) *Complex { return v.obj.(*Complex) }

// AsComplexOK returns v as *Complex and whether it is one.
func AsComplexOK(v Value) (*Complex, bool) { c, ok := v.obj.(*Complex); return c, ok }

// IsRational reports whether v is a *Rational.
func IsRational(v Value) bool { _, ok := v.obj.(*Rational); return ok }

// AsRational returns v as *Rational; it panics if v is not a *Rational.
func AsRational(v Value) *Rational { return v.obj.(*Rational) }

// AsRationalOK returns v as *Rational and whether it is one.
func AsRationalOK(v Value) (*Rational, bool) { r, ok := v.obj.(*Rational); return r, ok }

// IsRange reports whether v is a *Range.
func IsRange(v Value) bool { _, ok := v.obj.(*Range); return ok }

// AsRange returns v as *Range; it panics if v is not a *Range.
func AsRange(v Value) *Range { return v.obj.(*Range) }

// AsRangeOK returns v as *Range and whether it is one.
func AsRangeOK(v Value) (*Range, bool) { r, ok := v.obj.(*Range); return r, ok }

// IsMain reports whether v is a *Main.
func IsMain(v Value) bool { _, ok := v.obj.(*Main); return ok }

// AsMain returns v as *Main; it panics if v is not a *Main.
func AsMain(v Value) *Main { return v.obj.(*Main) }

// AsMainOK returns v as *Main and whether it is one.
func AsMainOK(v Value) (*Main, bool) { m, ok := v.obj.(*Main); return m, ok }

// IsNilV reports whether v is the Ruby nil object.
func IsNilV(v Value) bool { return v.tag == TagNil }

// AsObj returns the heap-backed object behind v as a RubyObj, for a subsequent
// type assertion to a concrete heap type. It returns nil for the immediate value
// types, so an assertion to a concrete heap type on an immediate simply fails.
func AsObj(v Value) RubyObj { return v.obj }

// AsIntegerOK returns v as object.Integer and whether it is one.
func AsIntegerOK(v Value) (Integer, bool) { return Integer(v.i), v.tag == TagInt }

// AsFloatOK returns v as object.Float and whether it is one.
func AsFloatOK(v Value) (Float, bool) {
	return Float(math.Float64frombits(uint64(v.i))), v.tag == TagFloat
}

// AsBoolOK returns v as object.Bool and whether it is one.
func AsBoolOK(v Value) (Bool, bool) { return Bool(v.i != 0), v.tag == TagBool }

// AsSymbolOK returns v as object.Symbol and whether it is one.
func AsSymbolOK(v Value) (Symbol, bool) { s, ok := v.obj.(Symbol); return s, ok }

// AsNil returns the Nil value; it panics if v is not the Nil object.
func AsNil(v Value) Nil {
	if v.tag != TagNil {
		panic("object: AsNil on non-nil value")
	}
	return NilV
}

// AsNilOK returns the Nil value and whether v is the Nil object.
func AsNilOK(v Value) (Nil, bool) { return NilV, v.tag == TagNil }

// NilObj returns the Nil value type (distinct from NilVal, which returns it as a
// Value).
func NilObj() Nil { return NilV }

// IsNilObj reports whether v is the Nil object. Identical to IsNil after the
// flip (Go-nil Values no longer exist).
func IsNilObj(v Value) bool { return v.tag == TagNil }

// IsKind reports whether v's heap object is a T (a pointer/heap concrete type, or
// Symbol). It is false for the immediate value types, whose payload is not in the
// object word.
func IsKind[T any](v Value) bool { _, ok := v.obj.(T); return ok }

// Kind returns v's heap object asserted to T, panicking on mismatch.
func Kind[T any](v Value) T { return v.obj.(T) }

// KindOK returns v's heap object asserted to T and whether it matched.
func KindOK[T any](v Value) (T, bool) { x, ok := v.obj.(T); return x, ok }

// AsAny materializes v as a plain interface value for a type assertion to an
// arbitrary Go interface type (e.g. v.(fmt.Stringer)). Unlike AsObj it does NOT
// drop the immediates: an Integer/Float/Bool/Nil is returned boxed, so it can
// still satisfy an interface it implements. The box is only paid on this
// interface-assertion path.
func AsAny(v Value) any {
	switch v.tag {
	case TagNil:
		return NilV
	case TagInt:
		return Integer(v.i)
	case TagFloat:
		return Float(math.Float64frombits(uint64(v.i)))
	case TagBool:
		return Bool(v.i != 0)
	default:
		return v.obj
	}
}

// IsAny reports whether v, materialized via AsAny, satisfies interface T.
func IsAny[T any](v Value) bool { _, ok := AsAny(v).(T); return ok }

// CastAny asserts v (materialized via AsAny) to interface T, panicking on
// mismatch.
func CastAny[T any](v Value) T { return AsAny(v).(T) }

// CastAnyOK asserts v (materialized via AsAny) to interface T and reports whether
// it matched.
func CastAnyOK[T any](v Value) (T, bool) { x, ok := AsAny(v).(T); return x, ok }

// Equal reports Ruby-representation equality of two Values with the exact
// semantics the interface `==` had: identity for reference types, value equality
// for immediates, and NaN-aware float inequality (Float(NaN) is never ==
// Float(NaN)). Raw `==` on two Values that may be Float must route through Equal
// so the struct's bitwise float compare does not make a NaN equal to itself.
func Equal(a, b Value) bool {
	if a.tag == TagFloat && b.tag == TagFloat {
		return math.Float64frombits(uint64(a.i)) == math.Float64frombits(uint64(b.i))
	}
	return a == b
}
