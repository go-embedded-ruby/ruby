package vm_test

import "testing"

// TestStringControlEscapes verifies the single-character control escapes in
// double-quoted strings decode to the right bytes (MRI Ruby 4.0.5) — \b and \f
// previously kept the letter instead of the control byte.
func TestStringControlEscapes(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "\a\b\v\f\s".bytes`, "[7, 8, 11, 12, 32]\n"},
		{`p "\n\t\r\e\0".bytes`, "[10, 9, 13, 27, 0]\n"},
		{`p "a\bc".length`, "3\n"},
		{`p "x\\y".bytes`, "[120, 92, 121]\n"}, // a literal backslash still works
		{`p "q\"z".length`, "3\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
