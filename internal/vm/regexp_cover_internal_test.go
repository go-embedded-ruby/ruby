package vm

import "testing"

// TestEscapeForwardSlashesInternal exercises escapeForwardSlashes directly,
// including the defensive branch where a backslash is the final byte (a lone
// trailing backslash never survives a compiled Regexp, so it is unreachable
// through the interpreter but must still be handled).
func TestEscapeForwardSlashesInternal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"a/b", "a\\/b"},
		{"\\/", "\\/"},     // already-escaped slash is not double-escaped
		{"\\d/", "\\d\\/"}, // backslash escapes 'd', then the slash is escaped
		{"a\\", "a\\"},     // trailing backslash: the i+1 < len guard is false
	}
	for _, c := range cases {
		if got := escapeForwardSlashes(c.in); got != c.want {
			t.Errorf("escapeForwardSlashes(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSingleWholeGroupInternal exercises singleWholeGroup directly, including the
// branches unreachable via the interpreter: an unterminated group (invalid, so it
// never compiles) and an escape inside the group body.
func TestSingleWholeGroupInternal(t *testing.T) {
	cases := []struct {
		src           string
		on, off, body string
		ok            bool
	}{
		{"(?i:a)", "i", "", "a", true},
		{"(?i-m:a)", "i", "m", "a", true},
		{"(?:x)", "", "", "x", true},
		{"(?:\\)x)", "", "", "\\)x", true}, // escaped ')' inside the body
		{"(?:[)])", "", "", "[)]", true},   // ')' inside a character class
		{"(?:(a))", "", "", "(a)", true},   // a nested group raises and lowers depth
		{"abc", "", "", "", false},         // no group prefix
		{"(?=5)", "", "", "", false},       // look-ahead: no option ':' spec
		{"(?i:a)b", "", "", "", false},     // group closes before the end
		{"(?i:abc", "", "", "", false},     // unterminated group (defensive path)
	}
	for _, c := range cases {
		on, off, body, ok := singleWholeGroup(c.src)
		if ok != c.ok || on != c.on || off != c.off || body != c.body {
			t.Errorf("singleWholeGroup(%q) = (%q,%q,%q,%v), want (%q,%q,%q,%v)",
				c.src, on, off, body, ok, c.on, c.off, c.body, c.ok)
		}
	}
}
