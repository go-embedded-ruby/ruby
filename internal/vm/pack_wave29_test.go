package vm_test

import (
	"strings"
	"testing"
)

// TestUnpackWave29 pins String#unpack behaviours corrected in wave 29. Every
// expected value was produced by running the same snippet under MRI 4.0.5 and
// capturing its output verbatim.
func TestUnpackWave29(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"unpack_aAZ_binary",
			"p \"abc\".unpack(\"a2A2Z2\").map { |s| s.encoding.to_s }",
			"[\"ASCII-8BIT\", \"ASCII-8BIT\", \"ASCII-8BIT\"]\n"},
		{"unpack_bB_usascii",
			"p \"abc\".unpack(\"B8b8\").map { |s| s.encoding.to_s }",
			"[\"US-ASCII\", \"US-ASCII\"]\n"},
		{"unpack_hH_usascii",
			"p \"abc\".unpack(\"H2h2\").map { |s| s.encoding.to_s }",
			"[\"US-ASCII\", \"US-ASCII\"]\n"},
		{"unpack_Zstar_walks",
			"p \"a\\x00\\x00 b \\x00\".unpack(\"Z*Z*Z*Z*\")",
			"[\"a\", \"\", \" b \", \"\"]\n"},
		{"unpack_Zstar_pair",
			"p \"abc\\x00def\".unpack(\"Z*Z*\")",
			"[\"abc\", \"def\"]\n"},
		{"unpack_Zstar_no_nul",
			"p \"abc\".unpack(\"Z*\")",
			"[\"abc\"]\n"},
		{"unpack_Z_fixed",
			"p \"abc\\x00def\".unpack(\"Z4Z3\")",
			"[\"abc\", \"def\"]\n"},
		{"unpack_X_fixed",
			"p \"abcd\".unpack(\"C2X1C\")",
			"[97, 98, 98]\n"},
		{"unpack_U_ok",
			"p \"\u00E9b\".unpack(\"U*\")",
			"[233, 98]\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}

// TestUnpackWave29Errors pins the refusals, each checked against MRI 4.0.5.
func TestUnpackWave29Errors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// utf8_to_uv raises rather than substituting U+FFFD.
		{"U_malformed", `"\xE3".unpack("U")`, "malformed UTF-8 character"},
		{"U_malformed_star", `"\xE3".unpack("U*")`, "malformed UTF-8 character"},
		// X* backs up by the bytes REMAINING, so it can run off the front.
		{"X_star_past_start", `"abcd".unpack("CX*C")`, "X outside of string"},
		{"X_star_alone", `"abcd".unpack("CX*")`, "X outside of string"},
		{"X_count_past_start", `"\x01\x02\x03\x04".unpack("C3X4C")`, "X outside of string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runErr(t, c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
			}
		})
	}
}
