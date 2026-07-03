package object

// This file defines the Value accessor/constructor SEAM used by the migration of
// Value from an interface to a concrete tagged struct (plan-rbgo.md §4). Every
// function here has a signature that is stable across the flip: while Value is
// still an interface each body is a type assertion; after the flip each body
// reads a struct field or a tag. Because the signatures do not change, the
// mechanical rewriter can convert every type-switch / type-assertion / implicit
// heap→Value conversion site to call these seams *while Value is still an
// interface* (behaviour-neutral, testable), and the representation flip then
// swaps only the bodies below plus the Value definition in object.go.
//
// Accessors are free functions (not methods) deliberately: a method cannot be
// declared on an interface type, so `AsInt(v)` (not `v.AsInt()`) is the only
// form that compiles both before and after the flip with one signature.

// Tag values classify a Value's payload. tagNil is 0 so the zero Value{} is Ruby
// nil (dissolving the interface-nil-vs-Nil hazard). Immediates (Int/Float/Bool)
// live in the scalar word; every heap-backed value (Symbol, *String, *Array,
// *Hash, *Bignum, *Complex, *Rational, *Range, *Main, RObject and every VM
// binding type) lives in the object word.
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

// RubyObj is the behaviour every heap-backed Ruby value provides. It is exactly
// the current Value method set, so every concrete value type satisfies it both
// before and after the flip. After the flip the tagged-struct Value stores a
// RubyObj in its object word for heap-backed values.
type RubyObj interface {
	ToS() string
	Inspect() string
	Truthy() bool
}

// --- Constructors (return Value) -------------------------------------------

// FloatValue boxes f as a Value.
func FloatValue(f float64) Value { return Float(f) }

// BoolValue boxes b as the shared True/False Value.
func BoolValue(b bool) Value {
	if b {
		return True
	}
	return False
}

// Str wraps a *String as a Value. NewString already returns *String; use Str at
// the point a *String enters a Value slot.
func Str(s *String) Value { return s }

// StringValue is an alias of Str for symmetry with the other *T constructors.
func StringValue(s *String) Value { return s }

// ArrayValue wraps a *Array as a Value.
func ArrayValue(a *Array) Value { return a }

// HashValue wraps a *Hash as a Value.
func HashValue(h *Hash) Value { return h }

// BignumValue wraps a *Bignum as a Value.
func BignumValue(b *Bignum) Value { return b }

// ComplexValue wraps a *Complex as a Value.
func ComplexValue(c *Complex) Value { return c }

// RationalValue wraps a *Rational as a Value.
func RationalValue(r *Rational) Value { return r }

// RangeValue wraps a *Range as a Value.
func RangeValue(r *Range) Value { return r }

// MainValue wraps a *Main as a Value.
func MainValue(m *Main) Value { return m }

// Wrap boxes an arbitrary heap-backed Ruby object (RObject or any VM binding
// type) as a Value. It must NOT be used for the immediate value types
// (Integer/Float/Bool/Nil/Symbol) — those have dedicated constructors so they do
// not allocate an interface box. The parameter is Value (identical method set to
// RubyObj) while Value is an interface; the flip changes it to RubyObj.
func Wrap(o Value) Value { return o }

// --- Predicates + accessors (free functions) --------------------------------

// TagOf returns v's tag. While Value is an interface it is computed from the
// dynamic type; after the flip it reads the tag word.
func TagOf(v Value) uint8 {
	switch x := v.(type) {
	case nil:
		return TagNil
	case Nil:
		return TagNil
	case Integer:
		return TagInt
	case Float:
		return TagFloat
	case Bool:
		return TagBool
	case Symbol:
		return TagSym
	case *String:
		return TagString
	case *Array:
		return TagArray
	case *Hash:
		return TagHash
	case *Bignum:
		return TagBignum
	case *Complex:
		return TagComplex
	case *Rational:
		return TagRational
	case *Range:
		return TagRange
	case *Main:
		return TagMain
	default:
		_ = x
		return TagObject
	}
}

// IsInt reports whether v is an Integer (fixnum).
func IsInt(v Value) bool { _, ok := v.(Integer); return ok }

// AsInt returns v's int64 value; it panics if v is not an Integer.
func AsInt(v Value) int64 { return int64(v.(Integer)) }

// AsInteger returns v as object.Integer; it panics if v is not an Integer.
func AsInteger(v Value) Integer { return v.(Integer) }

// IsFloat reports whether v is a Float.
func IsFloat(v Value) bool { _, ok := v.(Float); return ok }

// AsFloat returns v's float64 value; it panics if v is not a Float.
func AsFloat(v Value) float64 { return float64(v.(Float)) }

// AsFloatV returns v as object.Float; it panics if v is not a Float.
func AsFloatV(v Value) Float { return v.(Float) }

// IsBool reports whether v is a Bool.
func IsBool(v Value) bool { _, ok := v.(Bool); return ok }

// AsBool returns v's bool value; it panics if v is not a Bool.
func AsBool(v Value) bool { return bool(v.(Bool)) }

// AsBoolV returns v as object.Bool; it panics if v is not a Bool.
func AsBoolV(v Value) Bool { return v.(Bool) }

// IsSym reports whether v is a Symbol.
func IsSym(v Value) bool { _, ok := v.(Symbol); return ok }

// AsSym returns v's symbol name; it panics if v is not a Symbol.
func AsSym(v Value) string { return string(v.(Symbol)) }

// AsSymbol returns v as object.Symbol; it panics if v is not a Symbol.
func AsSymbol(v Value) Symbol { return v.(Symbol) }

// IsString reports whether v is a *String.
func IsString(v Value) bool { _, ok := v.(*String); return ok }

// AsString returns v as *String; it panics if v is not a *String.
func AsString(v Value) *String { return v.(*String) }

// AsStringOK returns v as *String and whether it is one.
func AsStringOK(v Value) (*String, bool) { s, ok := v.(*String); return s, ok }

// IsArray reports whether v is a *Array.
func IsArray(v Value) bool { _, ok := v.(*Array); return ok }

// AsArray returns v as *Array; it panics if v is not a *Array.
func AsArray(v Value) *Array { return v.(*Array) }

// AsArrayOK returns v as *Array and whether it is one.
func AsArrayOK(v Value) (*Array, bool) { a, ok := v.(*Array); return a, ok }

// IsHash reports whether v is a *Hash.
func IsHash(v Value) bool { _, ok := v.(*Hash); return ok }

// AsHash returns v as *Hash; it panics if v is not a *Hash.
func AsHash(v Value) *Hash { return v.(*Hash) }

// AsHashOK returns v as *Hash and whether it is one.
func AsHashOK(v Value) (*Hash, bool) { h, ok := v.(*Hash); return h, ok }

// IsBignum reports whether v is a *Bignum.
func IsBignum(v Value) bool { _, ok := v.(*Bignum); return ok }

// AsBignum returns v as *Bignum; it panics if v is not a *Bignum.
func AsBignum(v Value) *Bignum { return v.(*Bignum) }

// AsBignumOK returns v as *Bignum and whether it is one.
func AsBignumOK(v Value) (*Bignum, bool) { b, ok := v.(*Bignum); return b, ok }

// IsComplex reports whether v is a *Complex.
func IsComplex(v Value) bool { _, ok := v.(*Complex); return ok }

// AsComplex returns v as *Complex; it panics if v is not a *Complex.
func AsComplex(v Value) *Complex { return v.(*Complex) }

// AsComplexOK returns v as *Complex and whether it is one.
func AsComplexOK(v Value) (*Complex, bool) { c, ok := v.(*Complex); return c, ok }

// IsRational reports whether v is a *Rational.
func IsRational(v Value) bool { _, ok := v.(*Rational); return ok }

// AsRational returns v as *Rational; it panics if v is not a *Rational.
func AsRational(v Value) *Rational { return v.(*Rational) }

// AsRationalOK returns v as *Rational and whether it is one.
func AsRationalOK(v Value) (*Rational, bool) { r, ok := v.(*Rational); return r, ok }

// IsRange reports whether v is a *Range.
func IsRange(v Value) bool { _, ok := v.(*Range); return ok }

// AsRange returns v as *Range; it panics if v is not a *Range.
func AsRange(v Value) *Range { return v.(*Range) }

// AsRangeOK returns v as *Range and whether it is one.
func AsRangeOK(v Value) (*Range, bool) { r, ok := v.(*Range); return r, ok }

// IsMain reports whether v is a *Main.
func IsMain(v Value) bool { _, ok := v.(*Main); return ok }

// AsMain returns v as *Main; it panics if v is not a *Main.
func AsMain(v Value) *Main { return v.(*Main) }

// AsMainOK returns v as *Main and whether it is one.
func AsMainOK(v Value) (*Main, bool) { m, ok := v.(*Main); return m, ok }

// IsNilV reports whether v is the Ruby nil object (or an absent Go-nil Value).
// It is a spelling of IsNil used by the rewriter for nil-typed switch arms.
func IsNilV(v Value) bool { return IsNil(v) }

// AsObj returns the heap-backed object behind v as a RubyObj, for a subsequent
// type assertion to a concrete heap type (used by the rewriter for VM binding
// types, e.g. object.AsObj(v).(*Time)). While Value is an interface it returns v
// itself; after the flip it returns the object word. It returns nil for the
// immediate value types, so a type assertion to a concrete heap type on an
// immediate simply fails (matching the old direct assertion on the interface).
func AsObj(v Value) RubyObj {
	switch v.(type) {
	case nil, Integer, Float, Bool, Nil:
		return nil
	default:
		return v
	}
}

// AsIntegerOK returns v as object.Integer and whether it is one.
func AsIntegerOK(v Value) (Integer, bool) { i, ok := v.(Integer); return i, ok }

// AsFloatOK returns v as object.Float and whether it is one.
func AsFloatOK(v Value) (Float, bool) { f, ok := v.(Float); return f, ok }

// AsBoolOK returns v as object.Bool and whether it is one.
func AsBoolOK(v Value) (Bool, bool) { b, ok := v.(Bool); return b, ok }

// AsSymbolOK returns v as object.Symbol and whether it is one.
func AsSymbolOK(v Value) (Symbol, bool) { s, ok := v.(Symbol); return s, ok }

// AsNil returns v as object.Nil; it panics if v is not the Nil object.
func AsNil(v Value) Nil { return v.(Nil) }

// AsNilOK returns v as object.Nil and whether it is one.
func AsNilOK(v Value) (Nil, bool) { n, ok := v.(Nil); return n, ok }

// NilObj returns the Nil value type (distinct from NilVal, which returns it as a
// Value). Used by the rewriter to bind a Nil-typed switch variable.
func NilObj() Nil { return NilV }

// IsNilObj reports whether v is specifically the Nil object (distinct, while
// Value is an interface, from an absent Go-nil Value — unlike IsNil/IsNilV which
// fold both). Used by the rewriter for a `case object.Nil` switch arm so the
// conversion is behaviour-neutral before the flip; after the flip (where Go-nil
// Values no longer exist) it is simply tag==TagNil.
func IsNilObj(v Value) bool { _, ok := v.(Nil); return ok }

// IsKind reports whether v's heap object is a T (a pointer/heap concrete type, or
// Symbol). It is false for the immediate value types (Integer/Float/Bool/Nil),
// whose payload is not in the object word. Used by the rewriter for the
// heap-typed arms of a converted type switch.
func IsKind[T any](v Value) bool { _, ok := AsObj(v).(T); return ok }

// Kind returns v's heap object asserted to T, panicking on mismatch (matching a
// bare X.(T) assertion). Used by the rewriter for single-value heap assertions.
func Kind[T any](v Value) T { return AsObj(v).(T) }

// KindOK returns v's heap object asserted to T and whether it matched. Used by
// the rewriter for comma-ok heap assertions.
func KindOK[T any](v Value) (T, bool) { x, ok := AsObj(v).(T); return x, ok }

// Equal reports Ruby-representation equality of two Values with the exact
// semantics the interface `==` had: identity for reference types, value equality
// for immediates, and — the one hazard the tagged struct would otherwise change
// — NaN-aware float inequality (Float(NaN) is never == Float(NaN)). Raw `==` on
// two Values that may be Float must route through Equal so the struct's bitwise
// float compare does not make NaN equal itself.
func Equal(a, b Value) bool {
	af, aok := a.(Float)
	bf, bok := b.(Float)
	if aok && bok {
		// NaN != NaN, matching IEEE and the old interface compare.
		return float64(af) == float64(bf)
	}
	if aok != bok {
		return false
	}
	return a == b
}
