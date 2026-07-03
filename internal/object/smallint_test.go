package object

import "testing"

// TestIntValue checks that interning a small integer yields exactly the same
// value as boxing it directly, across and beyond the cached range — interning is
// a representation detail that must never change an integer's value.
func TestIntValue(t *testing.T) {
	for _, n := range []int64{
		smallIntMin - 1, smallIntMin, smallIntMin + 1,
		-1, 0, 1, 42,
		smallIntMax - 1, smallIntMax, smallIntMax + 1,
		1 << 40, -(1 << 40),
	} {
		got := IntValue(n)
		if got != IntValue(int64(Integer(n))) {
			t.Errorf("IntValue(%d) = %v, want Integer(%d)", n, got, n)
		}
	}
}
