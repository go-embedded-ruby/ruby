package object

import "testing"

// sink defeats dead-store elimination in the alloc benchmarks below.
var sink Value

// TestSymValIdentity covers the interning contract: a given name yields a stable
// Value that compares equal to a plainly boxed Symbol of the same name (so
// :foo.equal?(:foo) and symbol-keyed Hash lookups behave identically whether the
// key came from SymVal or object.Symbol), and stays distinct from a String of
// the same name.
func TestSymValIdentity(t *testing.T) {
	a := SymVal("greeting")
	b := SymVal("greeting") // second call: table hit
	if a != b {
		t.Fatalf("SymVal(%q) not stable: %v vs %v", "greeting", a, b)
	}
	if a != SymVal(string(Symbol("greeting"))) {
		t.Fatal("interned symbol must equal a plainly boxed Symbol of the same name")
	}
	if a == Wrap(NewString("greeting")) {
		t.Fatal("a symbol must never equal a string of the same name")
	}
	// Empty symbol interns too and stays stable.
	if SymVal("") != SymVal("") || SymVal("") != SymVal(string(Symbol(""))) {
		t.Fatal("empty symbol interning is inconsistent")
	}
}

// TestSymValZeroAllocOnHit proves the mechanism: once a name is interned, boxing
// it again allocates nothing.
func TestSymValZeroAllocOnHit(t *testing.T) {
	SymVal("warm") // prime the table
	if n := testing.AllocsPerRun(1000, func() { sink = SymVal("warm") }); n != 0 {
		t.Fatalf("SymVal on a warm name allocated %v/op, want 0", n)
	}
}

// TestBoolNilSingletonsZeroAlloc confirms the true/false/nil boxing floor: the
// shared True/False/NilV values box into a Value without allocating, so no cache
// beyond these singletons is needed.
func TestBoolNilSingletonsZeroAlloc(t *testing.T) {
	if n := testing.AllocsPerRun(1000, func() {
		sink = BoolValue(bool(True))
		sink = BoolValue(bool(False))
		sink = NilVal()
	}); n != 0 {
		t.Fatalf("boxing true/false/nil allocated %v/op, want 0", n)
	}
}

// TestSymValHashInterop confirms a Hash keyed through the interned SymVal box is
// found by a plainly boxed Symbol and vice versa: interning changes the box
// identity, not the map key (hashKey compares by dynamic type + value).
func TestSymValHashInterop(t *testing.T) {
	h := NewHash()
	h.Set(SymVal("kw"), IntValue(int64(Integer(5))))
	if v, ok := h.Get(SymVal(string(Symbol("kw")))); !ok || v != IntValue(int64(Integer(5))) {
		t.Fatalf("SymVal-keyed entry not found via plain Symbol: %v,%v", v, ok)
	}
	h.Set(SymVal(string(Symbol("kw"))), IntValue(int64(Integer(6)))) // overwrite via the other box: still one entry
	if v, ok := h.Get(SymVal("kw")); !ok || v != IntValue(int64(Integer(6))) || h.Len() != 1 {
		t.Fatalf("interned/plain symbol keys did not collapse: v=%v ok=%v len=%d", v, ok, h.Len())
	}
}

// TestHashImmediateKeyDeleteAndDistinct extends the immediate-key coverage of
// the re-box removal: Integer 1 and Float 1.0 stay distinct keys, a symbol and a
// same-named string stay distinct, and every immediate key type deletes cleanly.
func TestHashImmediateKeyDeleteAndDistinct(t *testing.T) {
	h := NewHash()
	h.Set(IntValue(int64(Integer(1))), SymVal(string(Symbol("int"))))
	h.Set(FloatValue(float64(Float(1.0))), SymVal(string(Symbol("float"))))
	if h.Len() != 2 {
		t.Fatalf("Integer(1) and Float(1.0) collapsed to one key: len=%d", h.Len())
	}
	if v, _ := h.Get(IntValue(int64(Integer(1)))); v != SymVal(string(Symbol("int"))) {
		t.Fatalf("Integer(1) key = %v want :int", v)
	}
	if v, _ := h.Get(FloatValue(float64(Float(1.0)))); v != SymVal(string(Symbol("float"))) {
		t.Fatalf("Float(1.0) key = %v want :float", v)
	}

	h.Set(SymVal(string(Symbol("x"))), IntValue(int64(Integer(1))))
	h.Set(Wrap(NewString("x")), IntValue(int64(Integer(2))))
	if sv, _ := h.Get(SymVal(string(Symbol("x")))); sv != IntValue(int64(Integer(1))) {
		t.Fatalf("symbol x = %v want 1 (must not alias string \"x\")", sv)
	}
	if strv, _ := h.Get(Wrap(NewString("x"))); strv != IntValue(int64(Integer(2))) {
		t.Fatalf("string x = %v want 2", strv)
	}

	for _, k := range []Value{IntValue(int64(Integer(1))), FloatValue(float64(Float(1.0))), SymVal(string(Symbol("x"))), BoolValue(bool(True)), BoolValue(bool(False)), NilVal()} {
		h.Set(k, IntValue(int64(Integer(99))))
		if v, ok := h.Delete(k); !ok || v != IntValue(int64(Integer(99))) {
			t.Fatalf("delete %v: v=%v ok=%v", k, v, ok)
		}
		if _, ok := h.Get(k); ok {
			t.Fatalf("delete %v: key still present", k)
		}
	}
}
