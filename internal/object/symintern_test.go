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
	if a != Value(Symbol("greeting")) {
		t.Fatal("interned symbol must equal a plainly boxed Symbol of the same name")
	}
	if a == Value(NewString("greeting")) {
		t.Fatal("a symbol must never equal a string of the same name")
	}
	// Empty symbol interns too and stays stable.
	if SymVal("") != SymVal("") || SymVal("") != Value(Symbol("")) {
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
		sink = True
		sink = False
		sink = NilV
	}); n != 0 {
		t.Fatalf("boxing true/false/nil allocated %v/op, want 0", n)
	}
}

// TestSymValHashInterop confirms a Hash keyed through the interned SymVal box is
// found by a plainly boxed Symbol and vice versa: interning changes the box
// identity, not the map key (hashKey compares by dynamic type + value).
func TestSymValHashInterop(t *testing.T) {
	h := NewHash()
	h.Set(SymVal("kw"), Integer(5))
	if v, ok := h.Get(Symbol("kw")); !ok || v != Integer(5) {
		t.Fatalf("SymVal-keyed entry not found via plain Symbol: %v,%v", v, ok)
	}
	h.Set(Symbol("kw"), Integer(6)) // overwrite via the other box: still one entry
	if v, ok := h.Get(SymVal("kw")); !ok || v != Integer(6) || h.Len() != 1 {
		t.Fatalf("interned/plain symbol keys did not collapse: v=%v ok=%v len=%d", v, ok, h.Len())
	}
}

// TestHashImmediateKeyDeleteAndDistinct extends the immediate-key coverage of
// the re-box removal: Integer 1 and Float 1.0 stay distinct keys, a symbol and a
// same-named string stay distinct, and every immediate key type deletes cleanly.
func TestHashImmediateKeyDeleteAndDistinct(t *testing.T) {
	h := NewHash()
	h.Set(Integer(1), Symbol("int"))
	h.Set(Float(1.0), Symbol("float"))
	if h.Len() != 2 {
		t.Fatalf("Integer(1) and Float(1.0) collapsed to one key: len=%d", h.Len())
	}
	if v, _ := h.Get(Integer(1)); v != Symbol("int") {
		t.Fatalf("Integer(1) key = %v want :int", v)
	}
	if v, _ := h.Get(Float(1.0)); v != Symbol("float") {
		t.Fatalf("Float(1.0) key = %v want :float", v)
	}

	h.Set(Symbol("x"), Integer(1))
	h.Set(NewString("x"), Integer(2))
	if sv, _ := h.Get(Symbol("x")); sv != Integer(1) {
		t.Fatalf("symbol x = %v want 1 (must not alias string \"x\")", sv)
	}
	if strv, _ := h.Get(NewString("x")); strv != Integer(2) {
		t.Fatalf("string x = %v want 2", strv)
	}

	for _, k := range []Value{Integer(1), Float(1.0), Symbol("x"), True, False, NilV} {
		h.Set(k, Integer(99))
		if v, ok := h.Delete(k); !ok || v != Integer(99) {
			t.Fatalf("delete %v: v=%v ok=%v", k, v, ok)
		}
		if _, ok := h.Get(k); ok {
			t.Fatalf("delete %v: key still present", k)
		}
	}
}
