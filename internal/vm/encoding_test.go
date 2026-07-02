package vm_test

import (
	"strings"
	"testing"
)

// TestEncoding covers the String encoding tag (default UTF-8, ASCII-8BIT binary)
// and the Encoding class. Asserted against MRI Ruby 4.0.5.
func TestEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		// Default encoding and the Encoding objects (interned, == by identity).
		{`p ["abc".encoding.name, "café".encoding.to_s, "x".encoding == Encoding::UTF_8]`, "[\"UTF-8\", \"UTF-8\", true]\n"},
		{`p [Encoding::UTF_8, Encoding::ASCII_8BIT, Encoding::BINARY == Encoding::ASCII_8BIT]`, "[#<Encoding:UTF-8>, #<Encoding:BINARY (ASCII-8BIT)>, true]\n"},
		// length counts characters for UTF-8 but bytes for a binary string.
		{`p ["café".length, "café".b.length, "café".bytesize]`, "[4, 5, 5]\n"},
		{`s = "café".dup; s.force_encoding("ASCII-8BIT"); p [s.encoding.name, s.length]`, "[\"ASCII-8BIT\", 5]\n"},
		// force_encoding accepts a name (case-insensitively / aliases) or an Encoding.
		{`p ["x".dup.force_encoding("utf8").encoding.name, "x".dup.force_encoding(Encoding::BINARY).encoding.name, "x".dup.force_encoding("US-ASCII").encoding.name]`, "[\"UTF-8\", \"ASCII-8BIT\", \"US-ASCII\"]\n"},
		// b returns a binary copy; the original is untouched.
		{`s = "café"; b = s.b; p [b.encoding.name, b.length, s.encoding.name, s.length]`, "[\"ASCII-8BIT\", 5, \"UTF-8\", 4]\n"},
		// ascii_only? and valid_encoding?.
		{`p ["abc".ascii_only?, "café".ascii_only?, "café".valid_encoding?, "café".b.valid_encoding?]`, "[true, false, true, true]\n"},
		// Encoding#name / to_s (via puts) / inspect method / == with a non-encoding /
		// a pass-through name.
		{`puts Encoding::UTF_8`, "UTF-8\n"},
		{`p [Encoding::UTF_8.inspect, Encoding::UTF_8 == :foo]`, "[\"#<Encoding:UTF-8>\", false]\n"},
		{`p ["x".dup.force_encoding("ISO-8859-1").encoding.name, (Encoding::UTF_8 ? :t : :f)]`, "[\"ISO-8859-1\", :t]\n"},
		// random_bytes is binary, so length == the requested count.
		{`require "securerandom"; b = SecureRandom.random_bytes(5); p [b.length, b.encoding.name]`, "[5, \"ASCII-8BIT\"]\n"},
		// String#[] / #slice on a binary (ASCII-8BIT) string indexes by BYTES and
		// keeps the result binary — matching MRI, where index/length are byte
		// offsets on an ASCII-8BIT string. "café".b is 5 bytes; [3, 2] takes the
		// two bytes of "é", and each slice stays ASCII-8BIT.
		{`s = "café".b; p [s[3, 2].bytesize, s[3, 2].encoding.name]`, "[2, \"ASCII-8BIT\"]\n"},
		{`s = "café".b; p [s.slice(0, 5).bytesize, s.slice(0, 5).encoding.name]`, "[5, \"ASCII-8BIT\"]\n"},
		{`s = "é".b; p [s[0].bytesize, s[1].bytesize, s[1].encoding.name]`, "[1, 1, \"ASCII-8BIT\"]\n"},
		{`s = "café".b; p [s[0..2].bytesize, s[0..2].encoding.name]`, "[3, \"ASCII-8BIT\"]\n"},
		// The UTF-8 default is unchanged: [] indexes by character.
		{`p ["café"[3, 1], "café"[0, 3], "café"[3].bytesize]`, "[\"é\", \"caf\", 2]\n"},
		// A binary index past the byte length is nil (byte-range, not char-range).
		{`s = "é".b; p s[2]`, "nil\n"},
		// The substring form s[sub] is byte-wise Contains either way: present →
		// the substring, absent → nil.
		{`s = "café".b; p [s["af"], s["zz"]]`, "[\"af\", nil]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// force_encoding with a non-String/Encoding argument raises TypeError.
	if err := runErr(t, `"x".dup.force_encoding(123)`); err == nil || !strings.Contains(err.Error(), "into String") {
		t.Errorf("force_encoding(123) err=%v, want a TypeError", err)
	}
}
