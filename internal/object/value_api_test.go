package object

import (
	"math"
	"math/big"
	"testing"
)

// TestValueConstructorsAndAccessors exercises every constructor / predicate /
// accessor in the Value seam across all payload kinds, restoring 100% coverage of
// the accessor surface after the interface→tagged-struct flip (many typed
// accessors are not reached by the generic Kind/IsKind the rewriter emits, so
// they are covered here directly).
func TestValueConstructorsAndAccessors(t *testing.T) {
	intV := IntValue(42)
	bigIntV := IntValue(1 << 40)
	floatV := FloatValue(2.5)
	trueV := BoolValue(true)
	falseV := BoolValue(false)
	nilV := NilVal()
	symV := SymVal("sym")
	strV := Wrap(NewString("hi"))
	arrV := Wrap(NewArray(intV))
	hashV := Wrap(NewHash())
	bigV := Wrap(&Bignum{I: big.NewInt(7)})
	cplxV := Wrap(&Complex{Re: intV, Im: intV})
	ratV := Wrap(&Rational{R: big.NewRat(1, 2)})
	rngV := Wrap(NewRange(intV, intV, false))
	mainV := Wrap(NewMain())

	// Tags.
	// Immediates and SymVal carry their own tag; every Wrap'd heap value carries
	// the generic TagObject (the fine-grained heap tags are documentation only —
	// heap accessors identify the concrete type from the object word).
	for _, tc := range []struct {
		v    Value
		want uint8
	}{
		{nilV, TagNil}, {intV, TagInt}, {floatV, TagFloat}, {trueV, TagBool},
		{symV, TagSym}, {strV, TagObject}, {arrV, TagObject}, {hashV, TagObject},
		{bigV, TagObject}, {cplxV, TagObject}, {ratV, TagObject},
		{rngV, TagObject}, {mainV, TagObject},
	} {
		if got := TagOf(tc.v); got != tc.want {
			t.Errorf("TagOf = %d want %d", got, tc.want)
		}
	}

	// Int.
	if !IsInt(intV) || IsInt(floatV) {
		t.Error("IsInt")
	}
	if AsInt(intV) != 42 || AsInteger(bigIntV) != Integer(1<<40) {
		t.Error("AsInt/AsInteger")
	}
	if n, ok := AsIntegerOK(intV); !ok || n != 42 {
		t.Error("AsIntegerOK hit")
	}
	if _, ok := AsIntegerOK(floatV); ok {
		t.Error("AsIntegerOK miss")
	}
	// Float.
	if !IsFloat(floatV) || IsFloat(intV) {
		t.Error("IsFloat")
	}
	if AsFloat(floatV) != 2.5 || AsFloatV(floatV) != Float(2.5) {
		t.Error("AsFloat/AsFloatV")
	}
	if f, ok := AsFloatOK(floatV); !ok || f != 2.5 {
		t.Error("AsFloatOK hit")
	}
	if _, ok := AsFloatOK(intV); ok {
		t.Error("AsFloatOK miss")
	}
	// Bool.
	if !IsBool(trueV) || IsBool(intV) {
		t.Error("IsBool")
	}
	if !AsBool(trueV) || AsBool(falseV) || AsBoolV(trueV) != Bool(true) {
		t.Error("AsBool/AsBoolV")
	}
	if b, ok := AsBoolOK(trueV); !ok || !bool(b) {
		t.Error("AsBoolOK hit")
	}
	if _, ok := AsBoolOK(intV); ok {
		t.Error("AsBoolOK miss")
	}
	// Sym.
	if !IsSym(symV) || IsSym(intV) {
		t.Error("IsSym")
	}
	if AsSym(symV) != "sym" || AsSymbol(symV) != Symbol("sym") {
		t.Error("AsSym/AsSymbol")
	}
	if s, ok := AsSymbolOK(symV); !ok || s != Symbol("sym") {
		t.Error("AsSymbolOK hit")
	}
	if _, ok := AsSymbolOK(intV); ok {
		t.Error("AsSymbolOK miss")
	}
	// Nil.
	if !IsNil(nilV) || !IsNilV(nilV) || !IsNilObj(nilV) || IsNil(intV) {
		t.Error("nil predicates")
	}
	if AsNil(nilV) != (Nil{}) || NilObj() != (Nil{}) {
		t.Error("AsNil/NilObj")
	}
	if _, ok := AsNilOK(nilV); !ok {
		t.Error("AsNilOK hit")
	}
	if _, ok := AsNilOK(intV); ok {
		t.Error("AsNilOK miss")
	}
	// String.
	if !IsString(strV) || IsString(intV) || AsString(strV).Str() != "hi" {
		t.Error("String accessors")
	}
	if s, ok := AsStringOK(strV); !ok || s.Str() != "hi" {
		t.Error("AsStringOK hit")
	}
	if _, ok := AsStringOK(intV); ok {
		t.Error("AsStringOK miss")
	}
	// Array / Hash.
	if !IsArray(arrV) || IsArray(intV) || len(AsArray(arrV).Elems) != 1 {
		t.Error("Array accessors")
	}
	if a, ok := AsArrayOK(arrV); !ok || len(a.Elems) != 1 {
		t.Error("AsArrayOK")
	}
	if _, ok := AsArrayOK(intV); ok {
		t.Error("AsArrayOK miss")
	}
	if !IsHash(hashV) || AsHash(hashV).Len() != 0 {
		t.Error("Hash accessors")
	}
	if _, ok := AsHashOK(hashV); !ok {
		t.Error("AsHashOK")
	}
	// Bignum.
	if !IsBignum(bigV) || AsBignum(bigV).I.Int64() != 7 {
		t.Error("Bignum accessors")
	}
	if b, ok := AsBignumOK(bigV); !ok || b.I.Int64() != 7 {
		t.Error("AsBignumOK")
	}
	if _, ok := AsBignumOK(intV); ok {
		t.Error("AsBignumOK miss")
	}
	// Complex / Rational / Range / Main.
	if !IsComplex(cplxV) || AsComplex(cplxV).Re != intV {
		t.Error("Complex")
	}
	if c, ok := AsComplexOK(cplxV); !ok || c.Re != intV {
		t.Error("AsComplexOK")
	}
	if _, ok := AsComplexOK(intV); ok {
		t.Error("AsComplexOK miss")
	}
	if !IsRational(ratV) || AsRational(ratV).R.Cmp(big.NewRat(1, 2)) != 0 {
		t.Error("Rational")
	}
	if r, ok := AsRationalOK(ratV); !ok || r.R.Cmp(big.NewRat(1, 2)) != 0 {
		t.Error("AsRationalOK")
	}
	if _, ok := AsRationalOK(intV); ok {
		t.Error("AsRationalOK miss")
	}
	if !IsRange(rngV) || AsRange(rngV).Lo != intV {
		t.Error("Range")
	}
	if rg, ok := AsRangeOK(rngV); !ok || rg.Lo != intV {
		t.Error("AsRangeOK")
	}
	if _, ok := AsRangeOK(intV); ok {
		t.Error("AsRangeOK miss")
	}
	if !IsMain(mainV) || AsMain(mainV) == nil {
		t.Error("Main")
	}
	if m, ok := AsMainOK(mainV); !ok || m == nil {
		t.Error("AsMainOK")
	}
	if _, ok := AsMainOK(intV); ok {
		t.Error("AsMainOK miss")
	}

	// AsObj: heap yields the object, immediate yields nil.
	if AsObj(strV) == nil || AsObj(intV) != nil {
		t.Error("AsObj")
	}
	// Kind / IsKind / KindOK.
	if !IsKind[*String](strV) || IsKind[*String](intV) {
		t.Error("IsKind")
	}
	if Kind[*String](strV).Str() != "hi" {
		t.Error("Kind")
	}
	if _, ok := KindOK[*Array](arrV); !ok {
		t.Error("KindOK")
	}
	// AsAny + interface assertions.
	for _, v := range []Value{nilV, intV, floatV, trueV, strV} {
		if AsAny(v) == nil && !IsNil(v) {
			t.Errorf("AsAny nil for non-nil %v", v)
		}
	}
	if !IsAny[RubyObj](strV) {
		t.Error("IsAny hit")
	}
	if CastAny[RubyObj](strV).ToS() != "hi" {
		t.Error("CastAny")
	}
	if _, ok := CastAnyOK[stubIface](strV); ok {
		t.Error("CastAnyOK miss")
	}

	// Equal: ints, floats (NaN never equal), and cross-type.
	if !Equal(intV, IntValue(42)) || Equal(intV, IntValue(43)) {
		t.Error("Equal int")
	}
	nan := FloatValue(math.NaN())
	if Equal(nan, nan) {
		t.Error("Equal NaN must be false")
	}
	if !Equal(floatV, FloatValue(2.5)) || Equal(intV, floatV) {
		t.Error("Equal float/cross")
	}

	// Value methods across tags (covers the immediate arms + heap delegation).
	if intV.ToS() != "42" || intV.Inspect() != "42" || !intV.Truthy() {
		t.Error("int methods")
	}
	if floatV.ToS() != "2.5" || !floatV.Truthy() {
		t.Error("float methods")
	}
	if trueV.ToS() != "true" || !trueV.Truthy() || falseV.Truthy() {
		t.Error("bool methods")
	}
	if nilV.ToS() != "" || nilV.Inspect() != "nil" || nilV.Truthy() {
		t.Error("nil methods")
	}
	if strV.ToS() != "hi" || strV.Inspect() != `"hi"` || !strV.Truthy() {
		t.Error("string methods")
	}
	if mainV.ToS() != "main" || mainV.Inspect() != "main" {
		t.Error("main methods")
	}
	// Dead-but-exported RubyObj methods on the concrete immediates (Value's own
	// methods special-case the immediates, so the concrete methods are otherwise
	// unreached).
	if !Integer(1).Truthy() || !Float(1).Truthy() {
		t.Error("concrete Truthy")
	}
	if NilV.ToS() != "" || NilV.Inspect() != "nil" {
		t.Error("concrete Nil methods")
	}
}

// TestAsNilPanics covers AsNil's non-nil panic arm.
func TestAsNilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("AsNil on non-nil did not panic")
		}
	}()
	_ = AsNil(IntValue(1))
}

// stubObj is an arbitrary heap RubyObj (stands in for a VM binding type) used to
// exercise the generic TagObject / Wrap path.
type stubObj struct{}

func (*stubObj) ToS() string     { return "stub" }
func (*stubObj) Inspect() string { return "stub" }
func (*stubObj) Truthy() bool    { return true }

// stubIface is an interface no built-in value implements, for the CastAnyOK miss.
type stubIface interface{ neverImplemented() }
