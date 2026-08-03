package vm_test

import (
	"strings"
	"testing"
)

// TestStringEncode covers String#encode / #encode! transcoding and options.
// Every expectation was verified against MRI Ruby (a byte-exact cross-check over
// the core encodings) and the ruby/spec core/string/encode suite.
func TestStringEncode(t *testing.T) {
	cases := []struct{ src, want string }{
		// No-arg encode returns a copy (rbgo's default_internal is always nil).
		{`s = "abc"; e = s.encode; p [e == s, e.equal?(s), e.encoding.name]`, "[true, false, \"UTF-8\"]\n"},
		// To the core Unicode encodings: byte-exact with MRI.
		{`p "abc".encode("UTF-16LE").bytes`, "[97, 0, 98, 0, 99, 0]\n"},
		{`p "abc".encode("UTF-16BE").bytes`, "[0, 97, 0, 98, 0, 99]\n"},
		{`p "A".encode("UTF-32LE").bytes`, "[65, 0, 0, 0]\n"},
		{`p "A".encode("UTF-32BE").bytes`, "[0, 0, 0, 65]\n"},
		{`p ["abc".encode("UTF-16LE").encoding.name, "abc".encode("utf-16be").encode("utf-8")]`, "[\"UTF-16LE\", \"abc\"]\n"},
		// Round trips through UTF-16/32.
		{`p "café".encode("utf-32be").encode("utf-8")`, "\"café\"\n"},
		{`p "café".encode("utf-16le").encode("utf-8")`, "\"café\"\n"},
		// Byte encodings: US-ASCII, ISO-8859-1, ASCII-8BIT.
		{`p "café".encode("iso-8859-1").bytes`, "[99, 97, 102, 233]\n"},
		{`p "café".encode("iso-8859-1").encode("utf-8")`, "\"café\"\n"},
		{`p "abc".encode("us-ascii").bytes`, "[97, 98, 99]\n"},
		{`p "abc".encode("binary").encoding.name`, "\"ASCII-8BIT\"\n"},
		{`p "ab".b.encode("utf-8")`, "\"ab\"\n"},
		// encode(to, from): reinterpret the receiver's bytes as `from`, transcode to `to`.
		{`p "abc".b.encode("utf-8", "utf-8")`, "\"abc\"\n"},
		{`p "\x30\x42".dup.force_encoding("utf-16be").encode(Encoding::UTF_8, Encoding::UTF_16BE)`, "\"あ\"\n"},
		// x/text bridge (Shift_JIS, EUC-JP, …): ASCII is identity; round-trips work.
		{`p "abc".encode("Shift_JIS").bytes`, "[97, 98, 99]\n"},
		{`p "ちわ".encode("shift_jis").encode("utf-8")`, "\"ちわ\"\n"},
		{`p "Ω".encode("iso-8859-7").encode("utf-8")`, "\"Ω\"\n"},
		// undef: :replace — default replacement is "?" for a byte target.
		{`p "é".encode("us-ascii", undef: :replace)`, "\"?\"\n"},
		{`p "é".encode("us-ascii", undef: :replace, replace: "foo")`, "\"foo\"\n"},
		{`p "\xff".b.encode("utf-8", undef: :replace).bytes`, "[239, 191, 189]\n"},
		// invalid: :replace on decode.
		{`p "ちX\xE3\x81".encode("utf-16le", invalid: :replace).encode("utf-8")`, "\"ちX�\"\n"},
		// Newline options.
		{`p "\r\nfoo".encode("utf-8", universal_newline: true)`, "\"\\nfoo\"\n"},
		{`p "\nfoo".encode("utf-8", crlf_newline: true).bytes`, "[13, 10, 102, 111, 111]\n"},
		{`p "\nfoo".encode("utf-8", cr_newline: true).bytes`, "[13, 102, 111, 111]\n"},
		// xml: :text / :attr.
		{`p "a<b>&".encode("utf-8", xml: :text)`, "\"a&lt;b&gt;&amp;\"\n"},
		{`p %{a<b>&"}.encode("utf-8", xml: :attr)`, "\"\\\"a&lt;b&gt;&amp;&quot;\\\"\"\n"},
		// encode! mutates in place, keeping identity.
		{`s = "abc"; r = s.encode!("utf-16le"); p [s.equal?(r), s.bytes, s.encoding.name]`, "[true, [97, 0, 98, 0, 99, 0], \"UTF-16LE\"]\n"},
		// Decode paths: ISO-8859-1, a UTF-16 surrogate pair, and invalid inputs with
		// invalid: :replace (odd trailing bytes, lone surrogate, out-of-range UTF-32).
		{`p "\xe9".dup.force_encoding("iso-8859-1").encode("utf-8")`, "\"é\"\n"},
		{`p "😀".encode("utf-16le").encode("utf-8")`, "\"😀\"\n"},
		{`p "\x61\x00\x62".dup.force_encoding("utf-16le").encode("utf-8", invalid: :replace)`, "\"a�\"\n"},
		{`p "\x00\xd8".dup.force_encoding("utf-16le").encode("utf-8", invalid: :replace)`, "\"�\"\n"},
		{`p "\x61\x00\x00\x00\x00".dup.force_encoding("utf-32le").encode("utf-8", invalid: :replace)`, "\"a�\"\n"},
		{`p "\xff\xff\xff\x00".dup.force_encoding("utf-32le").encode("utf-8", invalid: :replace)`, "\"�\"\n"},
		// scrub's maximal-ill-formed-subpart granularity (shared with encode's decode).
		{`p "\xC2\xE3\x81\x80".scrub`, "\"�぀\"\n"},
		{`p "\xF0abc".scrub`, "\"�abc\"\n"},
		{`p ["a\x80b".scrub, "a\xFFb".scrub, "abc".scrub]`, "[\"a�b\", \"a�b\", \"abc\"]\n"},
		// Decode FROM US-ASCII (valid and, with replace, invalid high bytes).
		{`p "abc".dup.force_encoding("us-ascii").encode("utf-16le").bytes`, "[97, 0, 98, 0, 99, 0]\n"},
		{`p "abcÿ".dup.force_encoding("us-ascii").encode("utf-8", invalid: :replace)`, "\"abc��\"\n"},
		// A lone low surrogate and out-of-range / surrogate UTF-32 scalars.
		{`p "\x00\xdc".dup.force_encoding("utf-16le").encode("utf-8", invalid: :replace)`, "\"�\"\n"},
		{`p "\x00\xd8\x62\x00".dup.force_encoding("utf-16le").encode("utf-8", invalid: :replace)`, "\"�b\"\n"},
		{`p "\x80\x00\x00\x00".dup.force_encoding("utf-32be").encode("utf-8", invalid: :replace)`, "\"�\"\n"},
		{`p "\x00\x00\xd8\x00".dup.force_encoding("utf-32be").encode("utf-8", invalid: :replace)`, "\"�\"\n"},
		// x/text encode with undef: :replace (default "?" and a custom replacement).
		{`p "é".encode("shift_jis", undef: :replace)`, "\"?\"\n"},
		{`p "Xé".encode("shift_jis", undef: :replace, replace: "Z").encode("utf-8")`, "\"XZ\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// Error semantics, verified against MRI.
	errCases := []struct{ src, substr string }{
		{`"é".encode("us-ascii")`, "UndefinedConversionError"},
		{`"é".encode("shift_jis")`, "UndefinedConversionError"},
		{`"\xff".b.encode("utf-8")`, "UndefinedConversionError"},
		{`"\x80".dup.force_encoding("utf-8").encode("utf-16le")`, "InvalidByteSequenceError"},
		{`"abc".encode("UTF-7")`, "ConverterNotFoundError"},
		{`"abc".dup.force_encoding("UTF-7").encode("utf-8")`, "ConverterNotFoundError"},
		{`"\xff".dup.force_encoding("us-ascii").encode("utf-8")`, "InvalidByteSequenceError"},
		{`"\x61\x00\x62".dup.force_encoding("utf-16le").encode("utf-8")`, "InvalidByteSequenceError"},
		{`"\x61\x00\x00".dup.force_encoding("utf-32le").encode("utf-8")`, "InvalidByteSequenceError"},
		{`"\x80\x00\x00\x00".dup.force_encoding("utf-32be").encode("utf-8")`, "InvalidByteSequenceError"},
		{`"\x00\xd8".dup.force_encoding("utf-16le").encode("utf-8")`, "InvalidByteSequenceError"},
		{`"abc".encode("utf-8", "no-such-enc")`, "unknown encoding name"},
		{`"abc".encode("utf-8", xml: :bogus)`, "unexpected value for xml option"},
	}
	for _, c := range errCases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.substr) {
			t.Errorf("src=%q err=%v, want containing %q", c.src, err, c.substr)
		}
	}
}
