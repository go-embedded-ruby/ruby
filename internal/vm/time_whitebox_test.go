package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
)

// TestTimeNowSeam pins the nowUnix seam so Time.now is deterministic in a test:
// FromUnix(fixed) must round-trip to that instant.
func TestTimeNowSeam(t *testing.T) {
	saved := nowUnix
	defer func() { nowUnix = saved }()
	nowUnix = func() int64 { return 1782045296 }

	tm := unixTime(nowUnix())
	if got := tm.t.Unix(); got != 1782045296 {
		t.Fatalf("seamed Time.now = %d, want 1782045296", got)
	}
}

// TestTimeOpDefault covers timeOp's defensive default. Only the arithmetic
// opcodes (+, -) reach it through the interpreter, so a unit call with another
// opcode is the only way to exercise the fallthrough.
func TestTimeOpDefault(t *testing.T) {
	wantRaise(t, "NoMethodError", func() {
		timeOp(bytecode.OpMul, unixTime(0), unixTime(0))
	})
}

// TestModNegative covers mod's Euclidean-positive branch for a negative
// dividend — a defensive path the strftime callers (which only ever pass
// non-negative operands) never reach through the interpreter.
func TestModNegative(t *testing.T) {
	if got := mod(-5, 3); got != 1 {
		t.Errorf("mod(-5, 3) = %d, want 1", got)
	}
	if got := mod(-1, 12); got != 11 {
		t.Errorf("mod(-1, 12) = %d, want 11", got)
	}
}
