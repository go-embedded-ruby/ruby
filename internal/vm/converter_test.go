package vm_test

import (
	"strings"
	"testing"
)

// TestConverterMetadata covers Encoding::Converter.new and the read-only
// accessors (source/destination encoding, inspect, replacement) plus the
// constants. Expectations were cross-checked against MRI Ruby 3.4/4.0.
func TestConverterMetadata(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Encoding::Converter.new("us-ascii","utf-8").source_encoding.name`, "\"US-ASCII\"\n"},
		{`p Encoding::Converter.new("us-ascii","utf-8").destination_encoding.name`, "\"UTF-8\"\n"},
		{`p Encoding::Converter.new(Encoding::US_ASCII, Encoding::UTF_8).source_encoding == Encoding::US_ASCII`, "true\n"},
		{`p Encoding::Converter.new("utf-8","utf-16le").inspect`, "\"#<Encoding::Converter: UTF-8 to UTF-16LE>\"\n"},
		// Replacement defaults: U+FFFD (UTF-8) for a UTF-8 destination, "?" (US-ASCII) else.
		{`c = Encoding::Converter.new("us-ascii","utf-8"); p [c.replacement, c.replacement.encoding.name]`, "[\"\uFFFD\", \"UTF-8\"]\n"},
		{`c = Encoding::Converter.new("us-ascii","us-ascii".dup.force_encoding("us-ascii").encoding == Encoding::US_ASCII ? "binary" : "binary"); p [c.replacement, c.replacement.encoding.name]`, "[\"?\", \"US-ASCII\"]\n"},
		// :replace option (String) → stored in the destination encoding.
		{`c = Encoding::Converter.new("us-ascii","utf-8", replace: "fubar"); p [c.replacement, c.replacement.encoding.name]`, "[\"fubar\", \"UTF-8\"]\n"},
		{`c = Encoding::Converter.new("us-ascii","binary", replace: "x"); p c.replacement.encoding.name`, "\"ASCII-8BIT\"\n"},
		{`c = Encoding::Converter.new("us-ascii","utf-8", replace: nil); p c.replacement`, "\"\uFFFD\"\n"},
		{`c = Encoding::Converter.new("us-ascii","utf-8", replace: ""); p c.replacement`, "\"\"\n"},
		// :replace via #to_str.
		{`class R; def to_str; "z"; end; end; p Encoding::Converter.new("us-ascii","utf-8", replace: R.new).replacement`, "\"z\"\n"},
		// replacement= and its transcoding.
		{`c = Encoding::Converter.new("utf-8","us-ascii"); c.replacement = "!"; p c.replacement`, "\"!\"\n"},
		{`c = Encoding::Converter.new("us-ascii","utf-8"); c.replacement = "?".encode("utf-8"); p c.replacement`, "\"?\"\n"},
		// Constants exist and are Integers.
		{`p Encoding::Converter::INVALID_REPLACE.class`, "Integer\n"},
		{`p Encoding::Converter::UNDEF_REPLACE.class`, "Integer\n"},
		{`p Encoding::Converter::CRLF_NEWLINE_DECORATOR.class`, "Integer\n"},
		{`p Encoding::Converter::XML_ATTR_QUOTE_DECORATOR.class`, "Integer\n"},
		// Integer options argument (ORed flags) is accepted.
		{`c = Encoding::Converter.new("utf-8","us-ascii", Encoding::Converter::UNDEF_REPLACE); p c.convert("é")`, "\"?\"\n"},
		// Options object coerced via #to_hash (positional, avoiding ** kwsplat).
		{`class H; def to_hash; {replace: "Q"}; end; end; p Encoding::Converter.new("utf-8","us-ascii", H.new).replacement`, "\"Q\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errCases := []struct{ src, substr string }{
		{`Encoding::Converter.new("utf-8","utf-8")`, "ConverterNotFoundError"},
		{`Encoding::Converter.new("utf-8")`, "wrong number of arguments"},
		{`Encoding::Converter.new("us-ascii","utf-8", replace: 1)`, "no implicit conversion"},
		{`Encoding::Converter.new("us-ascii","utf-8", replace: true)`, "no implicit conversion"},
		{`class R; def to_str; 1; end; end; Encoding::Converter.new("us-ascii","utf-8", replace: R.new)`, "no implicit conversion"},
		{`Encoding::Converter.new("us-ascii","utf-8", Object.new)`, "into Hash"},
		{`c = Encoding::Converter.new("utf-8","us-ascii"); c.replacement = nil`, "no implicit conversion"},
		{`c = Encoding::Converter.new("sjis","ascii"); c.replacement = "\u{986}"`, "UndefinedConversionError"},
	}
	for _, c := range errCases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.substr) {
			t.Errorf("src=%q err=%v, want containing %q", c.src, err, c.substr)
		}
	}
}

// TestConverterConvpath covers #convpath, .search_convpath and
// .asciicompat_encoding across the direct, pivoted, crlf and error paths.
func TestConverterConvpath(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Encoding::Converter.new("ASCII","UTF-8").convpath`, "[[#<Encoding:US-ASCII>, #<Encoding:UTF-8>]]\n"},
		{`p Encoding::Converter.new("ascii","Big5").convpath.map { |a| a.map(&:name) rescue a }`,
			"[[\"US-ASCII\", \"UTF-8\"], [\"UTF-8\", \"Big5\"]]\n"},
		{`p Encoding::Converter.new("ISO-8859-1","EUC-JP", crlf_newline: true).convpath.last`, "\"crlf_newline\"\n"},
		{`p Encoding::Converter.search_convpath("ASCII","UTF-8")`, "[[#<Encoding:US-ASCII>, #<Encoding:UTF-8>]]\n"},
		{`p Encoding::Converter.search_convpath("ASCII","UTF-8", crlf_newline: false).last == "crlf_newline"`, "false\n"},
		// asciicompat_encoding: nil for compatible / unknown / "internal"; mapped else.
		{`p Encoding::Converter.asciicompat_encoding("UTF-8")`, "nil\n"},
		{`p Encoding::Converter.asciicompat_encoding("string")`, "nil\n"},
		{`p Encoding::Converter.asciicompat_encoding("internal")`, "nil\n"},
		{`p Encoding::Converter.asciicompat_encoding("UTF-16BE")`, "#<Encoding:UTF-8>\n"},
		{`p Encoding::Converter.asciicompat_encoding(Encoding::UTF_16LE)`, "#<Encoding:UTF-8>\n"},
		{`p Encoding::Converter.asciicompat_encoding("ISO-2022-JP").name`, "\"stateless-ISO-2022-JP\"\n"},
		{`p Encoding::Converter.asciicompat_encoding("UTF-7")`, "#<Encoding:UTF-8>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	if err := runErr(t, `Encoding::Converter.search_convpath(Encoding::BINARY, Encoding::Emacs_Mule)`); err == nil ||
		!strings.Contains(err.Error(), "ConverterNotFoundError") {
		t.Errorf("emacs-mule path: err=%v", err)
	}
}

// TestConverterConvert covers #convert round-trips (exercising every UTF-8 lead
// class and every destination codec), the error classes, the replace flags, and
// #finish.
func TestConverterConvert(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Encoding::Converter.new("ascii","utf-8").convert("glark").encoding.name`, "\"UTF-8\"\n"},
		{`p Encoding::Converter.new("utf-8","euc-jp").convert("\u3042").bytes`, "[164, 162]\n"},
		// UTF-8 source, every multibyte lead class → UTF-16LE (representable) round trip.
		{`c = Encoding::Converter.new("utf-8","utf-16le"); p ["é","\u0800","€","\uD7FF","\uF000","😀","\u{50000}","\u{10FFFF}"].map { |s| c.convert(s.dup); c.convert("").empty? }.uniq`, "[true]\n"},
		{`p Encoding::Converter.new("utf-8","utf-16le").convert("A").bytes`, "[65, 0]\n"},
		{`p Encoding::Converter.new("utf-8","utf-16be").convert("A").bytes`, "[0, 65]\n"},
		{`p Encoding::Converter.new("utf-8","utf-32le").convert("A").bytes`, "[65, 0, 0, 0]\n"},
		{`p Encoding::Converter.new("utf-8","utf-32be").convert("A").bytes`, "[0, 0, 0, 65]\n"},
		{`p Encoding::Converter.new("utf-8","us-ascii").convert("A").bytes`, "[65]\n"},
		{`p Encoding::Converter.new("utf-8","binary").convert("A").bytes`, "[65]\n"},
		{`p Encoding::Converter.new("utf-8","iso-8859-1").convert("é").bytes`, "[233]\n"},
		{`p Encoding::Converter.new("utf-8","shift_jis").convert("あ").bytes`, "[130, 160]\n"},
		// Non-UTF-8 sources through the byte-level steppers.
		{`p Encoding::Converter.new("iso-8859-1","utf-8").convert("\xE9".b.force_encoding("iso-8859-1"))`, "\"é\"\n"},
		{`p Encoding::Converter.new("shift_jis","utf-8").convert("\x82\xa0".b)`, "\"あ\"\n"},
		{`p Encoding::Converter.new("shift_jis","utf-8").convert("a".b)`, "\"a\"\n"},
		{`p Encoding::Converter.new("euc-jp","utf-8").convert("\xa4\xa2".b)`, "\"あ\"\n"},
		{`p Encoding::Converter.new("euc-jp","utf-8").convert("\x8e\xb1".b).bytes`, "[239, 189, 177]\n"},
		{`p Encoding::Converter.new("euc-jp","utf-8").convert("a".b)`, "\"a\"\n"},
		{`p Encoding::Converter.new("utf-16le","utf-8").convert("😀".encode("utf-16le"))`, "\"😀\"\n"},
		{`p Encoding::Converter.new("utf-16be","utf-8").convert("A".encode("utf-16be"))`, "\"A\"\n"},
		{`p Encoding::Converter.new("utf-32le","utf-8").convert("A".encode("utf-32le"))`, "\"A\"\n"},
		{`p Encoding::Converter.new("utf-32be","utf-8").convert("A".encode("utf-32be"))`, "\"A\"\n"},
		// replace flags.
		{`c = Encoding::Converter.new("utf-8","us-ascii", invalid: :replace, undef: :replace); c.replacement = "!"; d = String.new; c.primitive_convert("中文123", d); p d`, "\"!!123\"\n"},
		{`p Encoding::Converter.new("utf-8","us-ascii", undef: :replace).convert("é")`, "\"?\"\n"},
		{`p(Encoding::Converter.new("utf-8","utf-8", invalid: :replace) rescue "same")`, "\"same\"\n"},
		// finish: stateless codecs emit nothing more.
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); c.convert("hi"); p c.finish`, "\"\"\n"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); p c.finish.encoding.name`, "\"ISO-8859-1\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errCases := []struct{ src, substr string }{
		{`Encoding::Converter.new("utf-8","iso-8859-1").convert("\u9899")`, "UndefinedConversionError"},
		{`Encoding::Converter.new("utf-8","iso-8859-1").convert("\xf1abcd".b)`, "InvalidByteSequenceError"},
		{`Encoding::Converter.new("utf-8","iso-8859-1").convert("\x80".b)`, "InvalidByteSequenceError"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); c.finish; c.convert("a")`, "convert after finish"},
		// A trailing incomplete sequence buffers, then #finish raises.
		{`c = Encoding::Converter.new("euc-jp","iso-8859-1"); c.convert("\xa4".b); c.finish`, "incomplete"},
		// Destination without a codec: MRI rejects the pair at construction time
		// (rb_econv_open) rather than at convert time.
		{`Encoding::Converter.new("utf-8","UTF-7").convert("a")`, "ConverterNotFoundError"},
		{`Encoding::Converter.new("utf-8","shift_jis").convert("😀")`, "UndefinedConversionError"},
	}
	for _, c := range errCases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.substr) {
			t.Errorf("src=%q err=%v, want containing %q", c.src, err, c.substr)
		}
	}
}

// TestConverterPrimitive covers #primitive_convert's status machine, the
// destination offset/bytesize handling, #primitive_errinfo, #last_error and
// #putback. Inline-assignment argument forms are avoided (a separate rbgo parser
// limitation), so buffers are pre-bound.
func TestConverterPrimitive(t *testing.T) {
	cases := []struct{ src, want string }{
		// status symbols + source-buffer consumption.
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); s = "glark".dup; r = c.primitive_convert(s, String.new); p [r, s]`, "[:finished, \"\"]\n"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); s = "\u9876abcd".dup; r = c.primitive_convert(s, String.new); p [r, s]`, "[:undefined_conversion, \"abcd\"]\n"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); s = "\xf1abcd".b; r = c.primitive_convert(s, String.new); p [r, s]`, "[:invalid_byte_sequence, \"bcd\"]\n"},
		{`c = Encoding::Converter.new("euc-jp","iso-8859-1"); s = "\xa4".b; r = c.primitive_convert(s, String.new); p [r, s]`, "[:incomplete_input, \"\"]\n"},
		{`c = Encoding::Converter.new("euc-jp","iso-8859-1"); r = c.primitive_convert("\xa4".b, String.new, nil, nil, partial_input: true); p r`, ":source_buffer_empty\n"},
		// destination offset (nil = append) and truncation past offset.
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); d = "aa".dup; c.primitive_convert("b", d, nil, 1); p d`, "\"aab\"\n"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); d = "xxx".dup; c.primitive_convert("b", d, 1); p d`, "\"xb\"\n"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); d = String.new; c.primitive_convert("glark", d, nil, 1); p d.bytesize`, "1\n"},
		// destination encoding is applied to the buffer.
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); d = String.new.force_encoding("utf-8"); c.primitive_convert("A", d); p d.encoding.name`, "\"ISO-8859-1\"\n"},
		// nil source accepted; accepts an options hash.
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); p c.primitive_convert(nil, String.new)`, ":finished\n"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); p c.primitive_convert(String.new, String.new, nil, nil, after_output: true)`, ":finished\n"},
		// #to_int coercion of offset/size (object pre-bound to avoid inline-assign).
		{`class I; def to_int; 2; end; end; c = Encoding::Converter.new("utf-8","iso-8859-1"); d = "  ".dup; o = I.new; p c.primitive_convert("abc", d, o)`, ":finished\n"},
		{`class I; def to_int; 2; end; end; c = Encoding::Converter.new("utf-8","iso-8859-1"); n = I.new; p c.primitive_convert("abc", String.new, 0, n)`, ":destination_buffer_full\n"},
		// errinfo.
		{`c = Encoding::Converter.new("ascii","utf-8"); p c.primitive_errinfo`, "[:source_buffer_empty, nil, nil, nil, nil]\n"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); c.primitive_convert("a", String.new); p c.primitive_errinfo`, "[:finished, nil, nil, nil, nil]\n"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); c.primitive_convert("\u9876".dup, String.new); e = c.primitive_errinfo; p [e[0], e[1], e[2], e[3].bytes, e[4].bytes]`,
			"[:undefined_conversion, \"UTF-8\", \"ISO-8859-1\", [233, 161, 182], []]\n"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); c.primitive_convert("\xf1abcd".b, String.new); e = c.primitive_errinfo; p [e[0], e[1], e[2], e[3].bytes, e[4].bytes]`,
			"[:invalid_byte_sequence, \"UTF-8\", \"ISO-8859-1\", [241], [97]]\n"},
		{`c = Encoding::Converter.new("EUC-JP","ISO-8859-1"); c.primitive_convert("\xa4".b, String.new, nil, 10); e = c.primitive_errinfo; p [e[0], e[1], e[2], e[3].bytes, e[4].bytes]`,
			"[:incomplete_input, \"EUC-JP\", \"UTF-8\", [164], []]\n"},
		// last_error.
		{`c = Encoding::Converter.new("ascii","utf-8"); p c.last_error`, "nil\n"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); c.primitive_convert("\xf1abcd".b, String.new); p c.last_error.class`, "Encoding::InvalidByteSequenceError\n"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); c.primitive_convert("\u9876".dup, String.new); p c.last_error.class`, "Encoding::UndefinedConversionError\n"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); c.primitive_convert("glark", String.new); p c.last_error`, "nil\n"},
		// putback: read-again bytes returned in the source encoding, then resumed.
		{`c = Encoding::Converter.new("EUC-JP","ISO-8859-1"); s = "abc\xa1def".b; c.primitive_convert(s, String.new, nil, 10); p [c.putback, c.putback]`, "[\"d\", \"\"]\n"},
		{`c = Encoding::Converter.new("EUC-JP","ISO-8859-1"); s = "abc\xa1def".b; c.primitive_convert(s, String.new, nil, 10); p c.putback.encoding.name`, "\"EUC-JP\"\n"},
		{`class I; def to_int; 1; end; end; c = Encoding::Converter.new("utf-16le","iso-8859-1"); s = "\x00\xd8\x61\x00".b; c.primitive_convert(s, String.new); n = I.new; p c.putback(n).bytes`, "[0]\n"},
		// read-again bytes are re-fed on the next primitive_convert.
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); s = "\xf1abcd".b; d = String.new; c.primitive_convert(s, d); r = c.primitive_convert(s, d); p [r, s, d]`, "[:finished, \"\", \"abcd\"]\n"},
		// UTF-32 source steppers.
		{`c = Encoding::Converter.new("utf-32le","utf-8"); p c.primitive_convert("A".encode("utf-32le"), String.new)`, ":finished\n"},
		{`c = Encoding::Converter.new("utf-16le","utf-8"); s = "\x00\xdc".b; p c.primitive_convert(s, String.new)`, ":invalid_byte_sequence\n"},
		{`c = Encoding::Converter.new("utf-16le","utf-8"); s = "\x00".b; p c.primitive_convert(s, String.new, nil, nil, partial_input: true)`, ":source_buffer_empty\n"},
		{`c = Encoding::Converter.new("utf-32le","utf-8"); s = "\x00\x00\x11\x00".b; p c.primitive_convert(s, String.new)`, ":invalid_byte_sequence\n"},
		{`c = Encoding::Converter.new("utf-32le","utf-8"); s = "\x00\x00\x00".b; p c.primitive_convert(s, String.new, nil, nil, partial_input: true)`, ":source_buffer_empty\n"},
		// invalid:replace fast paths at a full buffer (appendCapped over cap).
		{`c = Encoding::Converter.new("utf-8","iso-8859-1", invalid: :replace); p c.primitive_convert("\x80".b, String.new, 0, 0)`, ":finished\n"},
		{`c = Encoding::Converter.new("euc-jp","iso-8859-1", invalid: :replace); p c.primitive_convert("\xa4".b, String.new, 0, 0)`, ":finished\n"},
		// EUC-JP trail-byte validation and JIS X 0212 (0x8f) lead.
		{`c = Encoding::Converter.new("euc-jp","utf-8"); s = "\xa4\x20".b; p c.primitive_convert(s, String.new)`, ":invalid_byte_sequence\n"},
		{`c = Encoding::Converter.new("euc-jp","utf-8"); p c.primitive_convert("\x8f\xa1\xa1".b, String.new)`, ":finished\n"},
		{`c = Encoding::Converter.new("euc-jp","utf-8"); s = "\x80".b; p c.primitive_convert(s, String.new)`, ":invalid_byte_sequence\n"},
		// x/text source fallback: a bare invalid high byte and a truncated lead.
		{`c = Encoding::Converter.new("shift_jis","utf-8"); s = "\xfd\xfd\xfd\xfd".b; p c.primitive_convert(s, String.new)`, ":invalid_byte_sequence\n"},
		{`c = Encoding::Converter.new("shift_jis","utf-8"); p c.primitive_convert("\x82".b, String.new, nil, nil, partial_input: true)`, ":source_buffer_empty\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errCases := []struct{ src, substr string }{
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); c.primitive_convert(String.new, "am".dup, 3)`, "too big"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); c.primitive_convert(String.new, "x".freeze)`, "frozen"},
		{`c = Encoding::Converter.new("utf-8","iso-8859-1"); c.primitive_convert(String.new, Object.new)`, "into String"},
		{`class I; def to_int; "x"; end; end; c = Encoding::Converter.new("utf-8","iso-8859-1"); n = I.new; c.primitive_convert(String.new, String.new, n)`, "into Integer"},
	}
	for _, c := range errCases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.substr) {
			t.Errorf("src=%q err=%v, want containing %q", c.src, err, c.substr)
		}
	}
}
