package vm

import (
	"math"
	"testing"
)

// wantDeopt asserts fn panics the aotDeopt sentinel (the unboxed fast path
// abandoning to the level-1 body).
func wantDeopt(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if _, ok := r.(aotDeopt); !ok {
			t.Fatalf("expected aotDeopt, got %#v", r)
		}
	}()
	fn()
}

func TestAOTIntPrimitives(t *testing.T) {
	if aotAdd(2, 3) != 5 {
		t.Error("aotAdd")
	}
	wantDeopt(t, func() { aotAdd(math.MaxInt64, 1) })

	if aotSub(5, 3) != 2 {
		t.Error("aotSub")
	}
	wantDeopt(t, func() { aotSub(math.MinInt64, 1) })

	if aotMul(0, 5) != 0 || aotMul(3, 4) != 12 {
		t.Error("aotMul")
	}
	wantDeopt(t, func() { aotMul(math.MaxInt64, 2) })
	wantDeopt(t, func() { aotMul(-1, math.MinInt64) }) // -1 * MinInt64 overflows

	if aotDiv(17, 5) != 3 || aotDiv(-7, 2) != -4 { // floor division
		t.Error("aotDiv")
	}
	wantDeopt(t, func() { aotDiv(10, 0) })

	if aotMod(17, 5) != 2 {
		t.Error("aotMod")
	}
	wantDeopt(t, func() { aotMod(10, 0) })

	if aotNeg(5) != -5 {
		t.Error("aotNeg")
	}
	wantDeopt(t, func() { aotNeg(math.MinInt64) })
}
