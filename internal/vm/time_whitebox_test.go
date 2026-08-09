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
