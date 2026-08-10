package vm_test

import (
	"strings"
	"testing"
)

// TestStringValidEncoding covers String#valid_encoding? across encodings: the
// structural UTF-8/16/32 validators, the US-ASCII 7-bit rule, the always-valid
// ASCII-8BIT, and the x/text-backed legacy encodings. Asserted against MRI 4.0.5.
func TestStringValidEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		// UTF-8 (default) and ASCII-8BIT.
		{`p ["café".valid_encoding?, [0x81].pack("C").force_encoding("utf-8").valid_encoding?, "\xFF".b.valid_encoding?]`, "[true, false, true]\n"},
		// US-ASCII: 7-bit only.
		{`p ["abc".force_encoding("US-ASCII").valid_encoding?, "é".force_encoding("US-ASCII").valid_encoding?]`, "[true, false]\n"},
		// UTF-16LE: a plain string, a surrogate pair, then invalid forms — lone low
		// surrogate, high surrogate + non-low, truncated high surrogate, odd length.
		{`p ["abc".encode("UTF-16LE").valid_encoding?, "𝔘".encode("UTF-16LE").valid_encoding?]`, "[true, true]\n"},
		{`p [[0x00,0xDC].pack("C*").force_encoding("UTF-16LE").valid_encoding?, [0x35,0xD8,0x41,0x00].pack("C*").force_encoding("UTF-16LE").valid_encoding?, [0x35,0xD8].pack("C*").force_encoding("UTF-16LE").valid_encoding?, [0x41].pack("C").force_encoding("UTF-16LE").valid_encoding?]`, "[false, false, false, false]\n"},
		// UTF-16BE.
		{`p ["abc".encode("UTF-16BE").valid_encoding?, [0xDC,0x00].pack("C*").force_encoding("UTF-16BE").valid_encoding?]`, "[true, false]\n"},
		// UTF-32LE: valid, out-of-range code point, surrogate code point, odd length.
		{`p ["A".encode("UTF-32LE").valid_encoding?, [0x00,0x00,0x11,0x00].pack("C*").force_encoding("UTF-32LE").valid_encoding?, [0x00,0xD8,0x00,0x00].pack("C*").force_encoding("UTF-32LE").valid_encoding?, [0x41,0x00,0x00].pack("C*").force_encoding("UTF-32LE").valid_encoding?]`, "[true, false, false, false]\n"},
		// UTF-32BE.
		{`p ["A".encode("UTF-32BE").valid_encoding?, [0x00,0x11,0x00,0x00].pack("C*").force_encoding("UTF-32BE").valid_encoding?]`, "[true, false]\n"},
		// Legacy x/text encodings: ISO-8859-1 accepts any byte; Big5 rejects the
		// ill-formed lead/trail pair.
		{`p [[0xE6,0x9D,0x94].pack("C*").force_encoding("ISO-8859-1").valid_encoding?, [0xE6,0x9D,0x94].pack("C*").force_encoding("Big5").valid_encoding?]`, "[true, false]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestStringForceEncodingInternal covers the "internal" special encoding name and
// the now-stateful Encoding.default_internal it resolves through.
func TestStringForceEncodingInternal(t *testing.T) {
	cases := []struct{ src, want string }{
		// default_internal set: "internal" resolves to it, and the reader reflects it.
		{`Encoding.default_internal = "US-ASCII"; p ["abc".force_encoding("internal").encoding.name, Encoding.default_internal.name]`, "[\"US-ASCII\", \"US-ASCII\"]\n"},
		// default_internal unset (nil): "internal" defaults to BINARY.
		{`Encoding.default_internal = nil; p ["abc".force_encoding("internal").encoding.name, Encoding.default_internal]`, "[\"ASCII-8BIT\", nil]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// force_encoding on a frozen string raises FrozenError.
	if err := runErr(t, `"x".freeze.force_encoding("UTF-8")`); err == nil || !strings.Contains(err.Error(), "FrozenError") {
		t.Errorf("force_encoding on frozen: got %v, want FrozenError", err)
	}
}

// TestStringScrub covers String#scrub / #scrub! across encodings, the replacement
// forms (default / explicit / block), and their error classes. Asserted against MRI.
func TestStringScrub(t *testing.T) {
	cases := []struct{ src, want string }{
		// Valid string: scrub returns an equal copy.
		{`p "foo".scrub`, "\"foo\"\n"},
		// UTF-8: default U+FFFD, explicit replacement, and a block over the bad bytes.
		{`p [0x61,0xE3,0x82,0x81,0x81].pack("C*").force_encoding("utf-8").scrub`, "\"aめ�\"\n"},
		{`p [0x61,0x81].pack("C*").force_encoding("utf-8").scrub("*")`, "\"a*\"\n"},
		{`p [0x61,0xE3,0x80].pack("C*").force_encoding("utf-8").scrub { |x| "<#{x.unpack("H*")[0]}>" }`, "\"a<e380>\"\n"},
		// ASCII-8BIT is always valid: a copy unchanged.
		{`p "foo".b.scrub`, "\"foo\"\n"},
		// US-ASCII: an ASCII byte is kept, each high byte becomes its own "?".
		{`p [0xE3,0x80].pack("C*").force_encoding("US-ASCII").scrub`, "\"??\"\n"},
		{`p [0x61,0xE3].pack("C*").force_encoding("US-ASCII").scrub`, "\"a?\"\n"},
		// An explicit nil replacement falls back to the default.
		{`p [0x61,0x81].pack("C*").force_encoding("utf-8").scrub(nil)`, "\"a�\"\n"},
		// UTF-16LE scrub token scan: a valid surrogate pair is kept, and the ill-formed
		// forms — truncated high surrogate, high surrogate + non-low, odd trailing byte —
		// each collapse to one replacement.
		{`p [0x35,0xD8,0x18,0xDD,0x00,0xDC].pack("C*").force_encoding("UTF-16LE").scrub.bytes`, "[53, 216, 24, 221, 253, 255]\n"},
		{`p [0x41,0x00,0x35,0xD8].pack("C*").force_encoding("UTF-16LE").scrub.bytes`, "[65, 0, 253, 255]\n"},
		{`p [0x35,0xD8,0x41,0x00].pack("C*").force_encoding("UTF-16LE").scrub.bytes`, "[253, 255, 65, 0]\n"},
		{`p [0x41,0x00,0x41].pack("C*").force_encoding("UTF-16LE").scrub.bytes`, "[65, 0, 253, 255]\n"},
		// UTF-32LE scrub token scan: a trailing partial code unit collapses to one.
		{`p [0x41,0x00,0x00,0x00,0x41,0x00].pack("C*").force_encoding("UTF-32LE").scrub.bytes`, "[65, 0, 0, 0, 253, 255, 0, 0]\n"},
		// scrub! on an already-valid ASCII-8BIT string is a no-op returning self.
		{`s = "foo".b; p s.scrub!.equal?(s)`, "true\n"},
		// UTF-16BE / UTF-16LE / UTF-32LE / UTF-32BE default replacements (byte-checked).
		{`p [0x00,0x41,0xDC,0x00].pack("C*").force_encoding("UTF-16BE").scrub.bytes`, "[0, 65, 255, 253]\n"},
		{`p [0x41,0x00,0x00,0xDC].pack("C*").force_encoding("UTF-16LE").scrub.bytes`, "[65, 0, 253, 255]\n"},
		{`p [0x41,0x00,0x00,0x00,0x00,0x00,0x11,0x00].pack("C*").force_encoding("UTF-32LE").scrub.bytes`, "[65, 0, 0, 0, 253, 255, 0, 0]\n"},
		{`p [0x00,0x00,0x00,0x41,0x00,0x11,0x00,0x00].pack("C*").force_encoding("UTF-32BE").scrub.bytes`, "[0, 0, 0, 65, 0, 0, 255, 253]\n"},
		// scrub! mutates self and returns it; on an already-valid string it is a no-op.
		{`s = [0x61,0x81].pack("C*").force_encoding("utf-8"); r = s.scrub!; p [s == "a�", r.equal?(s)]`, "[true, true]\n"},
		{`s = "ok".dup; p s.scrub!.equal?(s)`, "true\n"},
		{`s = [0x61,0x81].pack("C*").force_encoding("utf-8"); s.scrub! { |b| "<?>" }; p s`, "\"a<?>\"\n"},
		// scrub! keeps a frozen already-valid string frozen (no modification attempt).
		{`s = "a".freeze; s.scrub!; p s.frozen?`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// A non-String replacement is a TypeError.
	if err := runErr(t, `[0x61,0x81].pack("C*").force_encoding("utf-8").scrub(1)`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Errorf("scrub(1): got %v, want TypeError", err)
	}
	// A replacement with an invalid encoding is an ArgumentError.
	if err := runErr(t, `[0x61,0x81].pack("C*").force_encoding("utf-8").scrub([0xE4].pack("C").force_encoding("utf-8"))`); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
		t.Errorf("scrub(invalid): got %v, want ArgumentError", err)
	}
	// scrub! on a frozen INVALID string must modify it and so raises FrozenError.
	if err := runErr(t, `s = [0x61,0x81].pack("C*").force_encoding("utf-8").freeze; s.scrub!`); err == nil || !strings.Contains(err.Error(), "FrozenError") {
		t.Errorf("scrub! on frozen invalid: got %v, want FrozenError", err)
	}
}

// TestStringUnaryFreeze covers String#-@ (frozen copy) and #+@ (mutable copy),
// including the -"literal" operator path.
func TestStringUnaryFreeze(t *testing.T) {
	cases := []struct{ src, want string }{
		// -@ on a mutable string yields a frozen copy; on an already-frozen one, self.
		{`p (-"x").frozen?`, "true\n"},
		{`s = "y".freeze; p (-s).equal?(s)`, "true\n"},
		// +@ (via send) returns self when mutable and a mutable copy when frozen.
		{`s = "z"; p s.send(:"+@").equal?(s)`, "true\n"},
		{`p "w".freeze.send(:"+@").frozen?`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
