package vm

import (
	"io"
	"math/big"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestSpaceshipNumericExact drives spaceshipNumeric (Integer#<=>) directly across
// its exact-integer branches. The `<=>` operator on two integers is inlined by
// the VM, so the Integer-Integer and Bignum-Bignum comparison paths are only
// reachable by calling the method function itself.
func TestSpaceshipNumericExact(t *testing.T) {
	vm := New(io.Discard)
	big70 := object.NormInt(new(big.Int).Lsh(big.NewInt(1), 70)) // 2**70
	big71 := object.NormInt(new(big.Int).Lsh(big.NewInt(1), 71)) // 2**71
	for _, c := range []struct {
		self, other object.Value
		want        int64
	}{
		{object.Integer(1), object.Integer(2), -1}, // fixnum <=> fixnum
		{object.Integer(5), object.Integer(5), 0},
		{object.Integer(9), object.Integer(3), 1},
		{big70, big71, -1}, // bignum <=> bignum
		{big71, big70, 1},
		{big70, big70, 0},
		{big70, object.Integer(7), 1}, // bignum <=> fixnum
	} {
		got := spaceshipNumeric(vm, c.self, []object.Value{c.other}, nil)
		gi, ok := got.(object.Integer)
		if !ok || int64(gi) != c.want {
			t.Errorf("spaceshipNumeric(%v, %v) = %v, want %d", c.self, c.other, got, c.want)
		}
	}
}
