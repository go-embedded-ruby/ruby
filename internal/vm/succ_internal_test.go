package vm

import "testing"

// TestSuccStringBinaryCarry covers succString's no-alphanumeric byte-carry path,
// including the all-0xff case that prepends a leading byte. This branch is not
// reachable from Ruby source (the lexer has no \xHH escape), so it is asserted
// here. MRI's behaviour on all-0xff binary strings is idiosyncratic; we use a
// plain carry (best-effort for non-text data).
func TestSuccStringBinaryCarry(t *testing.T) {
	cases := []struct{ in, want string }{
		{"##", "#$"},                 // no alnum: increment the last byte
		{"\xff", "\x01\x00"},         // single 0xff overflows -> prepend 0x01
		{"\xff\xff", "\x01\x00\x00"}, // all bytes overflow
		{"a\xff", "b\xff"},           // alnum present: carry handled by the alnum path
	}
	for _, c := range cases {
		if got := succString(c.in); got != c.want {
			t.Errorf("succString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
