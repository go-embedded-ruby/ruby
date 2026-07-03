package object

import (
	"math"
	"math/big"
	"testing"
)

func TestToSAndInspect(t *testing.T) {
	tests := []struct {
		v            Value
		toS, inspect string
		truthy       bool
	}{
		{IntValue(int64(Integer(42))), "42", "42", true},
		{IntValue(int64(Integer(-7))), "-7", "-7", true},
		{FloatValue(float64(Float(1.0))), "1.0", "1.0", true},
		{FloatValue(float64(Float(3.5))), "3.5", "3.5", true},
		{FloatValue(float64(Float(math.Inf(1)))), "Infinity", "Infinity", true},
		{FloatValue(float64(Float(math.Inf(-1)))), "-Infinity", "-Infinity", true},
		{FloatValue(float64(Float(math.NaN()))), "NaN", "NaN", true},
		{Wrap(NewString("hi")), "hi", `"hi"`, true},
		{SymVal(string(Symbol("hi"))), "hi", ":hi", true},
		{Wrap(&Array{}), "[]", "[]", true},
		{Wrap(&Array{Elems: []Value{IntValue(int64(Integer(1))), Wrap(NewString("x")), SymVal(string(Symbol("y")))}}), `[1, "x", :y]`, `[1, "x", :y]`, true},
		{Wrap(NewHash()), "{}", "{}", true},
		{Wrap(&Range{Lo: IntValue(int64(Integer(1))), Hi: IntValue(int64(Integer(5)))}), "1..5", "1..5", true},
		{Wrap(&Range{Lo: IntValue(int64(Integer(1))), Hi: IntValue(int64(Integer(5))), Exclusive: true}), "1...5", "1...5", true},
		{Wrap(&Range{Lo: Wrap(NewString("a")), Hi: Wrap(NewString("c"))}), "a..c", `"a".."c"`, true},
		{Wrap(NewString("a\"b\\c\nd\te")), "a\"b\\c\nd\te", `"a\"b\\c\nd\te"`, true},
		{BoolValue(bool(Bool(true))), "true", "true", true},
		{BoolValue(bool(Bool(false))), "false", "false", false},
		{NilVal(), "", "nil", false},
		{Wrap(NewMain()), "main", "main", true},
	}
	for _, tc := range tests {
		if got := tc.v.ToS(); got != tc.toS {
			t.Errorf("%#v ToS = %q, want %q", tc.v, got, tc.toS)
		}
		if got := tc.v.Inspect(); got != tc.inspect {
			t.Errorf("%#v Inspect = %q, want %q", tc.v, got, tc.inspect)
		}
		if got := tc.v.Truthy(); got != tc.truthy {
			t.Errorf("%#v Truthy = %v, want %v", tc.v, got, tc.truthy)
		}
	}
}

func TestHashOps(t *testing.T) {
	h := NewHash()
	if h.Len() != 0 || h.repr() != "{}" {
		t.Fatal("empty hash")
	}
	h.Set(SymVal(string(Symbol("a"))), IntValue(int64(Integer(1))))
	h.Set(Wrap(NewString("b")), IntValue(int64(Integer(2))))
	h.Set(SymVal(string(Symbol("a"))), IntValue(int64(Integer(9)))) // update keeps order, no new key
	if h.Len() != 2 {
		t.Fatalf("len = %d want 2", h.Len())
	}
	if v, ok := h.Get(SymVal(string(Symbol("a")))); !ok || v != IntValue(int64(Integer(9))) {
		t.Fatalf("get a = %v,%v", v, ok)
	}
	if _, ok := h.Get(SymVal(string(Symbol("z")))); ok {
		t.Fatal("missing key should be absent")
	}
	if h.Inspect() != `{a: 9, "b" => 2}` {
		t.Fatalf("inspect = %q", h.Inspect())
	}
}

// TestHashValueTypeKeys covers the immediate-value-key fast path in hashKey:
// Integer, Float, Symbol, true/false and nil each round-trip and update in place,
// and distinct value types never collide.
func TestHashValueTypeKeys(t *testing.T) {
	h := NewHash()
	keys := []Value{IntValue(int64(Integer(7))), FloatValue(float64(Float(1.5))), SymVal(string(Symbol("s"))), BoolValue(bool(True)), BoolValue(bool(False)), NilVal()}
	for i, k := range keys {
		h.Set(k, IntValue(int64(Integer(int64(i)))))
	}
	if h.Len() != len(keys) {
		t.Fatalf("len = %d want %d", h.Len(), len(keys))
	}
	for i, k := range keys {
		if v, ok := h.Get(k); !ok || v != IntValue(int64(Integer(int64(i)))) {
			t.Fatalf("get %v = %v,%v want %d", k, v, ok, i)
		}
	}
	// Updating an existing Integer key keeps the entry count (the found branch of
	// Set), confirming the fast-path key is stable across Get/Set.
	h.Set(IntValue(int64(Integer(7))), IntValue(int64(Integer(70))))
	if v, ok := h.Get(IntValue(int64(Integer(7)))); !ok || v != IntValue(int64(Integer(70))) || h.Len() != len(keys) {
		t.Fatalf("update Integer key: %v,%v len=%d", v, ok, h.Len())
	}
}

// TestNewHashCap covers the pre-sized hash constructor: a positive capacity
// builds an empty, fully usable hash, and a negative capacity clamps to zero
// (behaving like NewHash) rather than panicking.
func TestNewHashCap(t *testing.T) {
	h := NewHashCap(4)
	if h.Len() != 0 {
		t.Fatalf("pre-sized hash len = %d want 0", h.Len())
	}
	h.Set(Wrap(NewString("k")), IntValue(int64(Integer(1))))
	if v, ok := h.Get(Wrap(NewString("k"))); !ok || v != IntValue(int64(Integer(1))) {
		t.Fatalf("pre-sized hash get = %v,%v", v, ok)
	}
	if neg := NewHashCap(-1); neg.Len() != 0 {
		t.Fatalf("negative-cap hash len = %d want 0", neg.Len())
	}
}

// TestHashContentKeysAndClear covers content-addressed Array/Hash/Bignum keys,
// Clear, Delete, and (with no CustomKeyHook installed) the identity fallback for a
// plain reference key.
func TestHashContentKeysAndClear(t *testing.T) {
	h := NewHash()
	// Array key by content.
	h.Set(Wrap(&Array{Elems: []Value{IntValue(int64(Integer(1))), IntValue(int64(Integer(2)))}}), Wrap(NewString("a")))
	if v, ok := h.Get(Wrap(&Array{Elems: []Value{IntValue(int64(Integer(1))), IntValue(int64(Integer(2)))}})); !ok || Kind[*String](v).Str() != "a" {
		t.Fatalf("array key get = %v,%v", v, ok)
	}
	// Nested Hash key by content (exercises valKey on the value side).
	inner := NewHash()
	inner.Set(SymVal(string(Symbol("x"))), Wrap(&Array{Elems: []Value{IntValue(int64(Integer(3)))}}))
	hk := NewHash()
	hk.Set(SymVal(string(Symbol("x"))), Wrap(&Array{Elems: []Value{IntValue(int64(Integer(3)))}}))
	h.Set(Wrap(inner), Wrap(NewString("b")))
	if v, ok := h.Get(Wrap(hk)); !ok || Kind[*String](v).Str() != "b" {
		t.Fatalf("hash key get = %v,%v", v, ok)
	}
	// Bignum key by content.
	big1, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	big2, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	h.Set(Wrap(&Bignum{I: big1}), IntValue(int64(Integer(7))))
	if v, ok := h.Get(Wrap(&Bignum{I: big2})); !ok || v != IntValue(int64(Integer(7))) {
		t.Fatalf("bignum key get = %v,%v", v, ok)
	}
	// A plain reference key (no hook) is identity-keyed: a distinct Range misses.
	r := &Range{Lo: IntValue(int64(Integer(1))), Hi: IntValue(int64(Integer(2)))}
	h.Set(Wrap(r), Wrap(NewString("rng")))
	if v, ok := h.Get(Wrap(r)); !ok || Kind[*String](v).Str() != "rng" {
		t.Fatalf("identity key get = %v,%v", v, ok)
	}
	if _, ok := h.Get(Wrap(&Range{Lo: IntValue(int64(Integer(1))), Hi: IntValue(int64(Integer(2)))})); ok {
		t.Fatal("distinct reference key should miss without a hook")
	}
	// Delete an Array key by content.
	if _, ok := h.Delete(Wrap(&Array{Elems: []Value{IntValue(int64(Integer(1))), IntValue(int64(Integer(2)))}})); !ok {
		t.Fatal("delete array key")
	}
	if _, ok := h.Get(Wrap(&Array{Elems: []Value{IntValue(int64(Integer(1))), IntValue(int64(Integer(2)))}})); ok {
		t.Fatal("array key still present after delete")
	}
	// Clear empties everything.
	h.Clear()
	if h.Len() != 0 {
		t.Fatalf("len after clear = %d", h.Len())
	}
	if _, ok := h.Get(Wrap(hk)); ok {
		t.Fatal("hash key present after clear")
	}
}

// TestHashStringKeyFastPath exercises the allocation-free String-key fast path
// (strVals) end-to-end: insertion order, in-place overwrite keeping the original
// key + position, miss, delete-then-reinsert, Len, #keys returning the stored
// (frozen) snapshots, inspect, and the distinctness of a String key from a Symbol
// of the same name. It covers both the nil-strVals insert path (NewHash) and the
// pre-sized path (NewHashCap).
func TestHashStringKeyFastPath(t *testing.T) {
	for _, h := range []*Hash{NewHash(), NewHashCap(3)} {
		h.Set(Wrap(NewString("a")), IntValue(int64(Integer(1))))
		h.Set(SymVal(string(Symbol("a"))), IntValue(int64(Integer(100)))) // Symbol("a") is a DISTINCT key from "a".
		h.Set(Wrap(NewString("b")), IntValue(int64(Integer(2))))
		h.Set(Wrap(NewString("a")), IntValue(int64(Integer(9)))) // overwrite: keeps key + first position.
		if h.Len() != 3 {
			t.Fatalf("len = %d want 3", h.Len())
		}
		if v, ok := h.Get(Wrap(NewString("a"))); !ok || v != IntValue(int64(Integer(9))) {
			t.Fatalf(`get "a" = %v,%v want 9`, v, ok)
		}
		if v, ok := h.Get(SymVal(string(Symbol("a")))); !ok || v != IntValue(int64(Integer(100))) {
			t.Fatalf("get :a = %v,%v want 100 (String/Symbol must not collide)", v, ok)
		}
		if _, ok := h.Get(Wrap(NewString("z"))); ok {
			t.Fatal(`missing string key "z" should be absent`)
		}
		// Insertion order + frozen snapshots via #keys.
		wantOrder := []string{"a", ":a", "b"}
		if len(h.Keys) != 3 {
			t.Fatalf("keys len %d", len(h.Keys))
		}
		for i, k := range h.Keys {
			{
				__sw3 := k
				switch {
				case IsKind[*String](__sw3):
					kk := Kind[*String](__sw3)
					_ = kk
					if !kk.Frozen {
						t.Fatalf("stored string key %q not frozen", kk.Str())
					}
					if kk.Str() != wantOrder[i] {
						t.Fatalf("order[%d] = %q want %q", i, kk.Str(), wantOrder[i])
					}
				case IsKind[Symbol](__sw3):
					kk := Kind[Symbol](__sw3)
					_ = kk
					if ":"+string(kk) != wantOrder[i] {
						t.Fatalf("order[%d] = :%s want %q", i, string(kk), wantOrder[i])
					}
				}
			}
		}
		if h.Inspect() != `{"a" => 9, a: 100, "b" => 2}` {
			t.Fatalf("inspect = %q", h.Inspect())
		}
		// Delete a string key, then reinsert it: reinsertion appends at the end.
		if v, ok := h.Delete(Wrap(NewString("a"))); !ok || v != IntValue(int64(Integer(9))) {
			t.Fatalf(`delete "a" = %v,%v`, v, ok)
		}
		if _, ok := h.Get(Wrap(NewString("a"))); ok {
			t.Fatal(`"a" present after delete`)
		}
		if _, ok := h.Delete(Wrap(NewString("a"))); ok {
			t.Fatal("second delete should report absent")
		}
		h.Set(Wrap(NewString("a")), IntValue(int64(Integer(42))))
		if Kind[*String](h.Keys[len(h.Keys)-1]).Str() != "a" {
			t.Fatal("reinserted key should be last")
		}
	}
}

// TestHashStringKeyDupOnInsert covers Ruby's dup+freeze-on-insert: mutating the
// caller's String after insertion leaves the stored key (and the entry) intact,
// and the entry is still found by an equal String.
func TestHashStringKeyDupOnInsert(t *testing.T) {
	h := NewHash()
	s := NewString("k")
	h.Set(Wrap(s), IntValue(int64(Integer(1))))
	s.SetBytes([]byte("kx")) // mutate the original after insertion
	if got := Kind[*String](h.Keys[0]).Str(); got != "k" {
		t.Fatalf("stored key mutated to %q, want %q", got, "k")
	}
	if v, ok := h.Get(Wrap(NewString("k"))); !ok || v != IntValue(int64(Integer(1))) {
		t.Fatalf(`get "k" after source mutation = %v,%v`, v, ok)
	}
	if _, ok := h.Get(Wrap(s)); ok {
		t.Fatal(`mutated source "kx" should not be a key`)
	}
}

// keyUnwrap is a test double for KeyUnwrapper: an object that, used as a Hash key,
// hashes/compares as the value it wraps (wrap==true) or by identity (wrap==false).
type keyUnwrap struct {
	inner Value
	wrap  bool
}

func (k *keyUnwrap) HashUnwrap() (Value, bool) { return k.inner, k.wrap }
func (k *keyUnwrap) ToS() string               { return "unwrap" }
func (k *keyUnwrap) Inspect() string           { return "unwrap" }
func (k *keyUnwrap) Truthy() bool              { return true }

// TestHashStringKeyUnwrapper covers strContentKey's KeyUnwrapper branches: a
// wrapper of a String routes through the fast path and collides with a plain
// String of equal content; a wrapper of a non-String, and a wrapper that reports
// wrap==false, both fall through to the general (identity) path.
func TestHashStringKeyUnwrapper(t *testing.T) {
	h := NewHash()
	// Wrapper of a String -> fast path, same slot as a plain equal String.
	h.Set(Wrap(&keyUnwrap{inner: Wrap(NewString("w")), wrap: true}), IntValue(int64(Integer(1))))
	if v, ok := h.Get(Wrap(NewString("w"))); !ok || v != IntValue(int64(Integer(1))) {
		t.Fatalf("string-wrapper/plain collision = %v,%v", v, ok)
	}
	// Wrapper of a non-String (Array) -> general path (unwrapped to content key).
	aw := &keyUnwrap{inner: Wrap(&Array{Elems: []Value{IntValue(int64(Integer(3)))}}), wrap: true}
	h.Set(Wrap(aw), IntValue(int64(Integer(2))))
	if v, ok := h.Get(Wrap(&keyUnwrap{inner: Wrap(&Array{Elems: []Value{IntValue(int64(Integer(3)))}}), wrap: true})); !ok || v != IntValue(int64(Integer(2))) {
		t.Fatalf("array-wrapper key = %v,%v", v, ok)
	}
	// Wrapper reporting wrap==false -> identity key (no hook installed here).
	iw := &keyUnwrap{inner: Wrap(NewString("i")), wrap: false}
	h.Set(Wrap(iw), IntValue(int64(Integer(3))))
	if v, ok := h.Get(Wrap(iw)); !ok || v != IntValue(int64(Integer(3))) {
		t.Fatalf("identity-wrapper key = %v,%v", v, ok)
	}
	if _, ok := h.Get(Wrap(&keyUnwrap{inner: Wrap(NewString("i")), wrap: false})); ok {
		t.Fatal("distinct identity wrapper should miss")
	}
}

// TestHashStringInsideCompositeKey covers the recursive hashKey `case *String`:
// a String appearing INSIDE an Array/Hash key is still serialised by content, so
// two equal composite keys coincide even though the top-level fast path never
// sees the inner String.
func TestHashStringInsideCompositeKey(t *testing.T) {
	h := NewHash()
	h.Set(Wrap(&Array{Elems: []Value{Wrap(NewString("x")), IntValue(int64(Integer(1)))}}), IntValue(int64(Integer(7))))
	if v, ok := h.Get(Wrap(&Array{Elems: []Value{Wrap(NewString("x")), IntValue(int64(Integer(1)))}})); !ok || v != IntValue(int64(Integer(7))) {
		t.Fatalf("composite string-in-array key = %v,%v", v, ok)
	}
	// A nested Hash key whose value side is a String (exercises valKey -> case *String).
	inner := NewHash()
	inner.Set(SymVal(string(Symbol("k"))), Wrap(NewString("s")))
	probe := NewHash()
	probe.Set(SymVal(string(Symbol("k"))), Wrap(NewString("s")))
	h.Set(Wrap(inner), IntValue(int64(Integer(8))))
	if v, ok := h.Get(Wrap(probe)); !ok || v != IntValue(int64(Integer(8))) {
		t.Fatalf("nested-hash string-value key = %v,%v", v, ok)
	}
}

func TestSingletons(t *testing.T) {
	if !True.Truthy() || False.Truthy() || NilV.Truthy() {
		t.Fatal("singleton truthiness wrong")
	}
}

func TestNilSeam(t *testing.T) {
	// Nil() returns the shared nil singleton.
	if !IsNil(NilVal()) {
		t.Fatalf("NilVal() = %v, want NilV", NilVal())
	}
	if NilVal().Truthy() {
		t.Fatal("NilVal() must be falsy")
	}

	// IsNil folds a Go-nil Value and the Nil object together, and rejects every
	// other value — including ones whose dynamic type is not comparable (a
	// *Bignum holds a pointer, an Array holds a slice) to prove the NilV
	// comparison never panics.
	var absent Value
	cases := []struct {
		name string
		v    Value
		want bool
	}{
		{"go-nil", absent, true},
		{"nil-object", NilVal(), true},
		{"nil-via-constructor", NilVal(), true},
		{"integer", IntValue(int64(Integer(0))), false},
		{"false", BoolValue(bool(False)), false},
		{"bignum-ptr", Wrap(&Bignum{I: big.NewInt(1)}), false},
		{"array-slice", Wrap(&Array{Elems: []Value{IntValue(int64(Integer(1)))}}), false},
	}
	for _, c := range cases {
		if got := IsNil(c.v); got != c.want {
			t.Errorf("IsNil(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}
