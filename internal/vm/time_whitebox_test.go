package vm

import (
	"testing"
	stdtime "time"
)

// TestTimeNowSeam pins the nowWall seam so Time.now is deterministic in a test:
// the seam's instant must round-trip through the Time wrapper, seconds *and*
// nanoseconds. A seam that could only carry seconds is what made Time.now whole
// (#689), so the nanosecond leg is the part worth pinning.
func TestTimeNowSeam(t *testing.T) {
	saved := nowWall
	defer func() { nowWall = saved }()
	nowWall = func() stdtime.Time { return stdtime.Unix(1782045296, 123456789).UTC() }

	tm := &Time{t: nowWall()}
	if got := tm.t.Unix(); got != 1782045296 {
		t.Fatalf("seamed Time.now = %d, want 1782045296", got)
	}
	if got := tm.t.Nanosecond(); got != 123456789 {
		t.Fatalf("seamed Time.now nsec = %d, want 123456789", got)
	}
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
