package vm_test

import (
	"strings"
	"testing"
)

// TestConverterNewNotFound covers Encoding::Converter.new's endpoint resolution.
// MRI's rb_econv_open raises Encoding::ConverterNotFoundError — never
// ArgumentError — for an unknown name, an identical src/dst, or a known but
// undeliverable pair (any pair through the dummy UTF-7). A non-String,
// non-Encoding argument raises TypeError. Every expectation is verified against
// MRI 4.0.x.
func TestConverterNewNotFound(t *testing.T) {
	cases := []struct {
		name, src, wantSub string
	}{
		// Unknown source keeps its name verbatim; the known destination is
		// canonicalised ("utf-8" -> "UTF-8").
		{"unknown_src", `Encoding::Converter.new("foo-bar", "utf-8")`,
			"code converter not found (foo-bar to UTF-8)"},
		{"unknown_dst", `Encoding::Converter.new("utf-8", "foo-bar")`,
			"code converter not found (UTF-8 to foo-bar)"},
		{"both_unknown", `Encoding::Converter.new("foo", "bar")`,
			"code converter not found (foo to bar)"},
		// Known but undeliverable: the dummy UTF-7 has no codec in either direction.
		{"undeliverable_to_utf7", `Encoding::Converter.new("utf-8", "utf-7")`,
			"code converter not found (UTF-8 to UTF-7)"},
		{"undeliverable_from_utf7", `Encoding::Converter.new("utf-7", "utf-8")`,
			"code converter not found (UTF-7 to UTF-8)"},
		// Identical endpoints are their own not-found case.
		{"identical", `Encoding::Converter.new("utf-8", "utf-8")`,
			"code converter not found (UTF-8 to UTF-8)"},
		// An Encoding object resolves like its canonical name.
		{"encoding_object", `Encoding::Converter.new(Encoding::UTF_8, Encoding::UTF_7)`,
			"code converter not found (UTF-8 to UTF-7)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := runErr(t, c.src)
			if err == nil {
				t.Fatalf("src=%q: expected error, got none", c.src)
			}
			if !strings.Contains(err.Error(), "ConverterNotFoundError") {
				t.Errorf("src=%q: want ConverterNotFoundError, got %v", c.src, err)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("src=%q: want message containing %q, got %v", c.src, c.wantSub, err)
			}
		})
	}
}

func TestConverterNewTypeError(t *testing.T) {
	for _, src := range []string{
		`Encoding::Converter.new(123, "utf-8")`,
		`Encoding::Converter.new("utf-8", nil)`,
	} {
		err := runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "TypeError") {
			t.Errorf("src=%q: want TypeError, got %v", src, err)
		}
	}
}

// TestConverterNewValidPair confirms a genuinely deliverable pair still
// constructs (the deliverability check must not over-reject). The final case
// resolves an endpoint through #to_str.
func TestConverterNewValidPair(t *testing.T) {
	for _, src := range []string{
		`p Encoding::Converter.new("utf-8", "utf-16le").class.name`,
		`p Encoding::Converter.new("euc-jp", "utf-8").class.name`,
		`p Encoding::Converter.new(Encoding::UTF_8, Encoding::SHIFT_JIS).class.name`,
		`o = Object.new; def o.to_str; "utf-16le"; end; p Encoding::Converter.new(o, "utf-8").class.name`,
	} {
		if got := eval(t, src); got != "\"Encoding::Converter\"\n" {
			t.Errorf("src=%q: got %q", src, got)
		}
	}
}

// TestConverterNewToStrUnknown resolves an endpoint through #to_str to an
// unregistered name: the raw #to_str result is kept in the not-found message.
func TestConverterNewToStrUnknown(t *testing.T) {
	src := `o = Object.new; def o.to_str; "bogus-enc"; end; Encoding::Converter.new(o, "utf-8")`
	err := runErr(t, src)
	if err == nil || !strings.Contains(err.Error(), "ConverterNotFoundError") ||
		!strings.Contains(err.Error(), "code converter not found (bogus-enc to UTF-8)") {
		t.Errorf("src=%q: got %v", src, err)
	}
}

// TestConcatAliasedReceiver covers the snapshot fix for a receiver that also
// appears among its own concat arguments: MRI contributes the receiver's INITIAL
// value at each position.
func TestConcatAliasedReceiver(t *testing.T) {
	cases := []struct{ src, want string }{
		{`a = "hello"; a.concat(a, a); p a`, "\"hellohellohello\"\n"},
		{`a = "ab"; a.concat(a); p a`, "\"abab\"\n"},          // single arg, one append
		{`a = "x"; a.concat("y", a, "z"); p a`, "\"xyxz\"\n"}, // mixed, self in the middle
		{`p "ab".concat("c", "d")`, "\"abcd\"\n"},             // no aliasing, multi-arg
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q: got %q want %q", c.src, got, c.want)
		}
	}
}

// TestAsciiOnlyEncoding covers that #ascii_only? is the 7-bit coderange, not a
// bare byte scan: content in a non-ASCII-compatible encoding is never 7-bit,
// even when empty or all-ASCII in bytes.
func TestAsciiOnlyEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "abc".ascii_only?`, "true\n"},
		{`p "".ascii_only?`, "true\n"}, // empty UTF-8 is 7-bit
		{`p "".force_encoding("UTF-16LE").ascii_only?`, "false\n"},
		{`p "abc".force_encoding("UTF-16BE").ascii_only?`, "false\n"},
		{`p "\xe3\x81\x82".ascii_only?`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q: got %q want %q", c.src, got, c.want)
		}
	}
}

// TestConcatMultibyteCodepoint covers appending an Integer codepoint to a string
// in a legacy multibyte encoding: a valid codepoint is unpacked big-endian, an
// in-range but non-character byte (a lone lead byte) raises RangeError, and
// single-byte encodings still accept any 0..255 byte.
func TestConcatMultibyteCodepoint(t *testing.T) {
	ok := []struct{ src, want string }{
		{`s = "".force_encoding("euc-jp"); s.concat(0xa4a2); p s.bytes`, "[164, 162]\n"},
		{`s = "".force_encoding("shift_jis"); s.concat(0x82a0); p s.bytes`, "[130, 160]\n"},
		{`p 0xa4a2.chr("euc-jp").bytes`, "[164, 162]\n"},
		{`p 0.chr("euc-jp").bytes`, "[0]\n"}, // codepoint 0 unpacks to a single NUL byte
		{`s = "".force_encoding("iso-8859-1"); s.concat(0x81); p s.bytes`, "[129]\n"},
	}
	for _, c := range ok {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q: got %q want %q", c.src, got, c.want)
		}
	}
	bad := []struct{ src, wantSub string }{
		{`"".force_encoding("euc-jp").concat(0x81)`, "invalid codepoint 0x81 in EUC-JP"},
		{`"".force_encoding("shift_jis").concat(0x81)`, "invalid codepoint 0x81 in Shift_JIS"},
		{`0x81.chr("euc-jp")`, "invalid codepoint 0x81 in EUC-JP"},
		{`"".force_encoding("euc-jp").concat(-1)`, "invalid codepoint"},
		{`"".force_encoding("us-ascii").concat(256)`, "out of char range"},
	}
	for _, c := range bad {
		err := runErr(t, c.src)
		if err == nil || !strings.Contains(err.Error(), "RangeError") {
			t.Errorf("src=%q: want RangeError, got %v", c.src, err)
			continue
		}
		if !strings.Contains(err.Error(), c.wantSub) {
			t.Errorf("src=%q: want message containing %q, got %v", c.src, c.wantSub, err)
		}
	}
}
