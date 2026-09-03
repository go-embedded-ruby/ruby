package vm

import (
	"math/big"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// mustRaiseVM asserts that fn triggers a raised (panicked) Ruby error.
func mustRaiseVM(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: expected a raised error, got none", name)
		}
	}()
	fn()
}

// TestNumericRangeEqualityCoverage drives complexEqual, rationalEqual, rangeInts,
// rangeSize and rangeElems across all of their branches directly. These helpers
// are reached from operator/opcode paths the rest of the suite only partly
// exercises, so the direct calls give the merged coverage gate real margin.
func TestNumericRangeEqualityCoverage(t *testing.T) {
	// complexEqual: Complex==Complex (equal / unequal), Complex==real (zero
	// imaginary true / non-zero imaginary false), Complex==non-numeric (false).
	c12 := &object.Complex{Re: object.Integer(1), Im: object.Integer(2)}
	c30 := &object.Complex{Re: object.Integer(3), Im: object.Integer(0)}
	if !complexEqual(c12, &object.Complex{Re: object.Integer(1), Im: object.Integer(2)}) {
		t.Error("complexEqual(1+2i, 1+2i) = false")
	}
	if complexEqual(c12, &object.Complex{Re: object.Integer(1), Im: object.Integer(3)}) {
		t.Error("complexEqual(1+2i, 1+3i) = true")
	}
	if !complexEqual(c30, object.Integer(3)) {
		t.Error("complexEqual(3+0i, 3) = false")
	}
	if complexEqual(c12, object.Integer(3)) {
		t.Error("complexEqual(1+2i, 3) = true")
	}
	if complexEqual(c12, object.NewString("x")) {
		t.Error("complexEqual(1+2i, \"x\") = true")
	}

	// rationalEqual: ==Rational (equal / unequal), ==Float (equal / unequal),
	// ==non-numeric (false).
	half := &object.Rational{R: big.NewRat(1, 2)}
	if !rationalEqual(half, &object.Rational{R: big.NewRat(1, 2)}) {
		t.Error("rationalEqual(1/2, 1/2) = false")
	}
	if rationalEqual(half, &object.Rational{R: big.NewRat(1, 3)}) {
		t.Error("rationalEqual(1/2, 1/3) = true")
	}
	if !rationalEqual(half, object.Float(0.5)) {
		t.Error("rationalEqual(1/2, 0.5) = false")
	}
	if rationalEqual(half, object.Float(0.6)) {
		t.Error("rationalEqual(1/2, 0.6) = true")
	}
	if rationalEqual(half, object.NewString("x")) {
		t.Error("rationalEqual(1/2, \"x\") = true")
	}

	// rangeInts: integer endpoints succeed; a non-integer endpoint fails.
	if lo, hi, ok := rangeInts(&object.Range{Lo: object.Integer(1), Hi: object.Integer(5)}); !ok || lo != 1 || hi != 5 {
		t.Errorf("rangeInts(1..5) = %d,%d,%v", lo, hi, ok)
	}
	if _, _, ok := rangeInts(&object.Range{Lo: object.NewString("a"), Hi: object.NewString("e")}); ok {
		t.Error("rangeInts(\"a\"..\"e\") ok = true")
	}

	// rangeSize: inclusive, exclusive, empty (negative span), non-integer raises.
	if n := rangeSize(&object.Range{Lo: object.Integer(1), Hi: object.Integer(5)}); n != 5 {
		t.Errorf("rangeSize(1..5) = %d", n)
	}
	if n := rangeSize(&object.Range{Lo: object.Integer(1), Hi: object.Integer(5), Exclusive: true}); n != 4 {
		t.Errorf("rangeSize(1...5) = %d", n)
	}
	if n := rangeSize(&object.Range{Lo: object.Integer(5), Hi: object.Integer(1)}); n != 0 {
		t.Errorf("rangeSize(5..1) = %d", n)
	}
	mustRaiseVM(t, "rangeSize non-integer", func() {
		rangeSize(&object.Range{Lo: object.NewString("a"), Hi: object.NewString("e")})
	})

	// rangeElems: String range, integer inclusive/exclusive/empty, non-integer raises.
	if got := rangeElems(&object.Range{Lo: object.NewString("a"), Hi: object.NewString("c")}); len(got) != 3 {
		t.Errorf("rangeElems(\"a\"..\"c\") len = %d", len(got))
	}
	if got := rangeElems(&object.Range{Lo: object.Integer(1), Hi: object.Integer(3)}); len(got) != 3 {
		t.Errorf("rangeElems(1..3) len = %d", len(got))
	}
	if got := rangeElems(&object.Range{Lo: object.Integer(1), Hi: object.Integer(3), Exclusive: true}); len(got) != 2 {
		t.Errorf("rangeElems(1...3) len = %d", len(got))
	}
	if got := rangeElems(&object.Range{Lo: object.Integer(5), Hi: object.Integer(1)}); got != nil {
		t.Errorf("rangeElems(5..1) = %v, want nil", got)
	}
	mustRaiseVM(t, "rangeElems non-integer", func() {
		rangeElems(&object.Range{Lo: object.Float(1.5), Hi: object.Float(2.5)})
	})
}
