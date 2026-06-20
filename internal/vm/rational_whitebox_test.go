package vm

import (
	"math/big"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
)

// TestRatArithDefault covers ratArith's defensive default. Only the arithmetic
// opcodes (+, -, *, /, %) reach it through the interpreter, so a unit call with
// another opcode is the only way to exercise the fallthrough.
func TestRatArithDefault(t *testing.T) {
	wantRaise(t, "NoMethodError", func() {
		ratArith(bytecode.OpLt, big.NewRat(1, 2), big.NewRat(1, 3))
	})
}
