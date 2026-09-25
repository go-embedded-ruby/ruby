// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"
)

// TestSubEncBuilderNegotiation drives subEncBuilder directly over every arm of
// string.c v3_4_0 rb_enc_cr_str_buf_cat, including the ones no ruby/spec example
// reaches: an empty append into a non-ASCII-compatible buffer, and an empty
// buffer adopting a non-ASCII-compatible encoding wholesale.
func TestSubEncBuilderNegotiation(t *testing.T) {
	cases := []struct {
		name string
		enc  string                 // the buffer's seed encoding
		do   func(b *subEncBuilder) // the appends
		want string                 // resulting bytes
		enc2 string                 // resulting encoding
	}{
		{"same encoding stays 7bit", "UTF-8",
			func(b *subEncBuilder) { b.cat("he", "UTF-8"); b.cat("llo", "UTF-8") }, "hello", "UTF-8"},
		{"same encoding scan stops once non-7bit", "UTF-8",
			func(b *subEncBuilder) { b.cat("é", "UTF-8"); b.cat("x", "UTF-8") }, "éx", "UTF-8"},
		{"7bit buffer keeps its encoding for a 7bit piece", "US-ASCII",
			func(b *subEncBuilder) { b.cat("ab", "UTF-8") }, "ab", "US-ASCII"},
		{"7bit buffer adopts a non-7bit piece's encoding", "UTF-8",
			func(b *subEncBuilder) { b.cat("he", "UTF-8"); b.cat("\xC3", "ASCII-8BIT") }, "he\xC3", "ASCII-8BIT"},
		{"non-7bit buffer keeps its encoding for a 7bit piece", "UTF-8",
			func(b *subEncBuilder) { b.cat("é", "UTF-8"); b.cat("x", "US-ASCII") }, "éx", "UTF-8"},
		{"empty enc defaults to UTF-8", "UTF-8",
			func(b *subEncBuilder) { b.cat("a", "") }, "a", "UTF-8"},
		{"empty append into an incompatible buffer is a no-op", "UTF-16LE",
			func(b *subEncBuilder) { b.cat("a\x00", "UTF-16LE"); b.cat("", "UTF-8") }, "a\x00", "UTF-16LE"},
		{"empty buffer takes an incompatible piece whole", "UTF-8",
			func(b *subEncBuilder) { b.cat("a\x00", "UTF-16LE") }, "a\x00", "UTF-16LE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newSubEncBuilder(c.enc)
			c.do(b)
			got := b.result()
			if got.Str() != c.want || got.EncName() != c.enc2 {
				t.Errorf("got %q/%s, want %q/%s", got.Str(), got.EncName(), c.want, c.enc2)
			}
		})
	}
}

// TestSubEncBuilderIncompatible covers both `incompatible:` exits: the
// non-ASCII-compatible pairing with a non-empty buffer, and two ASCII-compatible
// encodings whose coderanges are both non-7BIT.
func TestSubEncBuilderIncompatible(t *testing.T) {
	cases := []struct{ name, dstEnc, seed, seedEnc, add, addEnc string }{
		{"not ascii-compatible", "UTF-8", "a", "UTF-8", "b\x00", "UTF-16LE"},
		{"both non-7bit", "UTF-8", "é", "UTF-8", "\xD0\xA0", "ISO-8859-5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected Encoding::CompatibilityError")
				}
				re, ok := r.(RubyError)
				if !ok || re.Class != "Encoding::CompatibilityError" {
					t.Fatalf("got %#v", r)
				}
			}()
			b := newSubEncBuilder(c.dstEnc)
			b.cat(c.seed, c.seedEnc)
			b.cat(c.add, c.addEnc)
		})
	}
}

// TestScanCoderange covers coderange_scan's three outcomes, including the one a
// non-ASCII-compatible encoding can never reach (7BIT).
func TestScanCoderange(t *testing.T) {
	cases := []struct {
		b, enc string
		want   coderange
	}{
		{"abc", "UTF-8", cr7Bit},
		{"", "UTF-8", cr7Bit},
		{"é", "UTF-8", crValid},
		{"\xFF", "UTF-8", crBroken},
		{"a\x00", "UTF-16LE", crValid}, // ASCII bytes, but never 7BIT here
		{"a", "UTF-16LE", crBroken},    // half a code unit
	}
	for _, c := range cases {
		if got := scanCoderange([]byte(c.b), c.enc); got != c.want {
			t.Errorf("scanCoderange(%q, %s) = %v, want %v", c.b, c.enc, got, c.want)
		}
	}
}

// TestGsubEncodingNegotiation is the Ruby-visible half: each line was checked
// byte for byte against ruby 4.0.5.
func TestGsubEncodingNegotiation(t *testing.T) {
	cases := []struct{ src, want string }{
		// A gsub that matches nothing keeps the receiver's encoding.
		{`r = 'abc'.force_encoding(Encoding::US_ASCII).gsub('é', 'è'); p [r, r.encoding]`,
			`["abc", #<Encoding:US-ASCII>]`},
		// A 7-bit buffer adopts the encoding of the first non-ASCII replacement.
		{`r = "hello".gsub(/l/) { 195.chr }; p [r.bytes, r.encoding]`,
			`[[104, 101, 195, 195, 111], #<Encoding:BINARY (ASCII-8BIT)>]`},
		// A String pattern never reaches rb_reg_prepare_enc, so a BINARY receiver
		// with a BINARY literal pattern is legal.
		{`s = "#{195.chr}#{192.chr}#{195.chr}"; r = s.gsub("#{192.chr}") { "hello" }; p r.encoding`,
			`#<Encoding:BINARY (ASCII-8BIT)>`},
		// Once the buffer carries non-ASCII bytes it keeps its encoding.
		{`p "hllëllo".gsub(/ë/) { "Русский".force_encoding("iso-8859-5") }.encoding`,
			`#<Encoding:ISO-8859-5>`},
		// gsub! carries dest's encoding onto the receiver (str_shared_replace).
		{`s = +"hello"; s.gsub!(/l/) { 195.chr }; p s.encoding`,
			`#<Encoding:BINARY (ASCII-8BIT)>`},
		// A replacement template is appended in the REPLACEMENT's encoding.
		{`r = "hello".force_encoding("US-ASCII").gsub(/l/, "é"); p [r, r.encoding]`,
			`["heééo", #<Encoding:UTF-8>]`},
		// A Hash replacement negotiates the same way, through rb_obj_as_string.
		{`r = "hello".gsub(/l/, "l" => 195.chr); p r.encoding`,
			`#<Encoding:BINARY (ASCII-8BIT)>`},
		// A block result that is not a String goes through #to_s (objAsStringVal).
		{`r = "hello".gsub(/l/) { :Z }; p r`, `"heZZo"`},
		// …and a #to_s that does not return a String falls back to the identity form.
		{`class B; undef to_s; def to_s; 1; end; end` + "\n" +
			`p "l".gsub(/l/) { B.new }.start_with?("#<B")`, `true`},
		// An empty match still advances one character, in the receiver's encoding.
		{`r = "été".gsub(//, "-"); p [r, r.encoding]`, `["-é-t-é-", #<Encoding:UTF-8>]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestGsubEncodingErrors covers the two raising paths stringSub adds: the
// buffer-level incompatibility and rb_reg_prepare_enc's BROKEN-subject refusal.
func TestGsubEncodingErrors(t *testing.T) {
	cases := []struct{ src, class, msgPart string }{
		{`"hllëllo".gsub(/l/) { "Русский".force_encoding("iso-8859-5") }`,
			"Encoding::CompatibilityError", "incompatible character encodings"},
		{`"hellö".gsub(/l/) { "Русский".force_encoding("iso-8859-5") }`,
			"Encoding::CompatibilityError", "incompatible character encodings"},
		{`x92 = [0x92].pack('C').force_encoding('utf-8'); "a#{x92}b".gsub(/[^\x00-\x7f]/u, '')`,
			"ArgumentError", "invalid byte sequence in UTF-8"},
		{`x92 = [0x92].pack('C').force_encoding('utf-8'); "a#{x92}b".sub(/b/, '')`,
			"ArgumentError", "invalid byte sequence in UTF-8"},
	}
	for _, c := range cases {
		class, msg := evalErr(t, c.src)
		if class != c.class || !strings.Contains(msg, c.msgPart) {
			t.Errorf("src=%q got %s: %q, want %s containing %q", c.src, class, msg, c.class, c.msgPart)
		}
	}
}

// TestChompSmartEncoding covers chompped_length's smart_chomp arm in both
// widths, plus the encMinLen / encASCIIChar tables underneath it.
func TestChompSmartEncoding(t *testing.T) {
	for _, c := range []struct {
		enc  string
		want int
	}{{"UTF-8", 1}, {"US-ASCII", 1}, {"ASCII-8BIT", 1}, {"ISO-8859-1", 1},
		{"UTF-16", 2}, {"UTF-16LE", 2}, {"UTF-16BE", 2},
		{"UTF-32", 4}, {"UTF-32LE", 4}, {"UTF-32BE", 4}} {
		if got := encMinLen(c.enc); got != c.want {
			t.Errorf("encMinLen(%s) = %d, want %d", c.enc, got, c.want)
		}
		if got := len(encASCIIChar('\n', c.enc)); got != c.want {
			t.Errorf("len(encASCIIChar('\\n', %s)) = %d, want %d", c.enc, got, c.want)
		}
	}
	// Byte order: the newline sits in the low unit for LE, the high one for BE.
	for _, c := range []struct{ enc, want string }{
		{"UTF-16BE", "\x00\n"}, {"UTF-16", "\x00\n"}, {"UTF-16LE", "\n\x00"},
		{"UTF-32BE", "\x00\x00\x00\n"}, {"UTF-32", "\x00\x00\x00\n"}, {"UTF-32LE", "\n\x00\x00\x00"},
		{"UTF-8", "\n"},
	} {
		if got := encASCIIChar('\n', c.enc); got != c.want {
			t.Errorf("encASCIIChar('\\n', %s) = %q, want %q", c.enc, got, c.want)
		}
	}
	// chompSmart itself: one newline character, then a '\r' character before it.
	for _, c := range []struct{ in, enc, want string }{
		{"abc\r\n", "UTF-8", "abc"},
		{"abc\r", "UTF-8", "abc"},
		{"abc", "UTF-8", "abc"},
		{"", "UTF-8", ""},
		{"a\x00\r\x00\n\x00", "UTF-16LE", "a\x00"},
		{"a\x00\n\x00", "UTF-16LE", "a\x00"},
		{"a\x00\r\x00", "UTF-16LE", "a\x00"},
		{"a\x00", "UTF-16LE", "a\x00"},
		{"\x00a\x00\r\x00\n", "UTF-16BE", "\x00a"},
	} {
		if got := chompSmart(c.in, c.enc); got != c.want {
			t.Errorf("chompSmart(%q, %s) = %q, want %q", c.in, c.enc, got, c.want)
		}
	}
	// Reachable from Ruby, including through the $/ path.
	if got := eval(t, `p "abc\r\n".encode("utf-32be").chomp.bytes.length`); got != "12\n" {
		t.Errorf("utf-32be chomp: %q", got)
	}
	if got := eval(t, "$VERBOSE = nil\n$/ = \"\\n\".encode(\"utf-8\")\np \"abc\\r\\n\".encode(\"utf-32be\").chomp.bytes.length"); got != "12\n" {
		t.Errorf("utf-32be chomp under an assigned $/: %q", got)
	}
	if got := eval(t, `s = +"abc\r\n".encode("utf-16le"); s.chomp!; p s.bytes.length`); got != "6\n" {
		t.Errorf("utf-16le chomp!: %q", got)
	}
}

// TestDefaultRecordSeparator covers the rb_default_rs seed: read through either
// spelling, as one frozen US-ASCII object, until an assignment replaces it.
func TestDefaultRecordSeparator(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p $/`, `"\n"`},
		{`p $-0`, `"\n"`},
		{`p $/.frozen?`, `true`},
		{`p $/.encoding`, `#<Encoding:US-ASCII>`},
		{`p $/.equal?($-0)`, `true`},
		{"$VERBOSE = nil\n$/ = nil\np [$/, $-0]", `[nil, nil]`},
		{"$VERBOSE = nil\n$/ = \"x\"\np [$/, $-0]", `["x", "x"]`},
		{"$VERBOSE = nil\n$-0 = \"y\"\np $/", `"y"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestLinesSeparatorResolution covers rb_str_enumerate_lines' separator
// resolution: the absent-argument read of $/, the nil short-circuit (which
// never chomps), the empty-receiver case, paragraph mode, and the re-encoding of
// the default separator for a non-ASCII-compatible receiver.
func TestLinesSeparatorResolution(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "a\nb\n".lines`, `["a\n", "b\n"]`},
		{`p "".lines`, `[]`},
		{`p "a\nb\n".lines(nil)`, `["a\nb\n"]`},
		{`p "a\nb\n".lines(nil, chomp: true)`, `["a\nb\n"]`},
		{"$VERBOSE = nil\n$/ = nil\np \"a\\nb\\n\".lines", `["a\nb\n"]`},
		{"$VERBOSE = nil\n$/ = nil\np \"a\\nb\\n\".lines(chomp: true)", `["a\nb\n"]`},
		{"$VERBOSE = nil\n$/ = \"b\"\np \"a\\nb\\n\".lines", `["a\nb", "\n"]`},
		{"$VERBOSE = nil\n$/ = \"\"\np \"a\\n\\n\\nb\\n\".lines", `["a\n\n", "b\n"]`},
		{`p "a\nb".encode(Encoding::UTF_16).lines.size`, `1`},
		{`p "a\x00\n\x00".dup.force_encoding("UTF-16LE").lines.size`, `1`},
		{`a = []; "a\nb\n".each_line { |l| a << l }; p a`, `["a\n", "b\n"]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// A destination with no converter at all is the ConverterNotFoundError arm.
	if class, _ := evalErr(t, `"a\nb".dup.force_encoding(Encoding::UTF_7).lines`); class != "Encoding::ConverterNotFoundError" {
		t.Errorf("dummy UTF-7 lines: got %s", class)
	}
}

// TestBOMEncodingTranscode covers transcode.c's to_/from_ UTF-16 and UTF-32
// converters and the helpers behind them.
func TestBOMEncodingTranscode(t *testing.T) {
	if !isBOMEncoding("UTF-16") || !isBOMEncoding("UTF-32") || isBOMEncoding("UTF-16LE") {
		t.Error("isBOMEncoding")
	}
	for _, c := range []struct {
		name string
		be   bool
		want string
	}{
		{"UTF-16", true, "\xFE\xFF"}, {"UTF-16", false, "\xFF\xFE"},
		{"UTF-32", true, "\x00\x00\xFE\xFF"}, {"UTF-32", false, "\xFF\xFE\x00\x00"},
	} {
		if got := string(bomFor(c.name, c.be)); got != c.want {
			t.Errorf("bomFor(%s, %v) = %q, want %q", c.name, c.be, got, c.want)
		}
	}
	for _, c := range []struct {
		src, name, body string
		be, ok          bool
	}{
		{"\xFE\xFF\x00a", "UTF-16", "\x00a", true, true},
		{"\xFF\xFE" + "a\x00", "UTF-16", "a\x00", false, true},
		{"\x00\x00\xFE\xFF\x00\x00\x00a", "UTF-32", "\x00\x00\x00a", true, true},
		{"\xFF\xFE\x00\x00a\x00\x00\x00", "UTF-32", "a\x00\x00\x00", false, true},
		{"\x00a", "UTF-16", "", false, false},
		{"\xFE", "UTF-16", "", false, false},
	} {
		body, be := splitBOM([]byte(c.src), c.name)
		if !c.ok {
			if body != nil {
				t.Errorf("splitBOM(%q, %s) = %q, want nil", c.src, c.name, body)
			}
			continue
		}
		if string(body) != c.body || be != c.be {
			t.Errorf("splitBOM(%q, %s) = %q/%v, want %q/%v", c.src, c.name, body, be, c.body, c.be)
		}
	}
	// The error text names one code unit, or the whole (short) string.
	for _, c := range []struct{ src, name, want string }{
		{"\x00ab", "UTF-16", `"\x00a"`},
		{"\x00", "UTF-16", `"\x00"`},
		{"\x00\x00\x00abcd", "UTF-32", `"\x00\x00\x00a"`},
	} {
		if got := bomErrorBytes([]byte(c.src), c.name); got != c.want {
			t.Errorf("bomErrorBytes(%q, %s) = %s, want %s", c.src, c.name, got, c.want)
		}
	}
	cases := []struct{ src, want string }{
		{`p "abé".encode("UTF-16").bytes`, `[254, 255, 0, 97, 0, 98, 0, 233]`},
		{`p "abé".encode("UTF-32").bytes`, `[0, 0, 254, 255, 0, 0, 0, 97, 0, 0, 0, 98, 0, 0, 0, 233]`},
		{`p "abé".encode("UTF-16").encode("UTF-8")`, `"abé"`},
		{`p "abé".encode("UTF-32").encode("UTF-8")`, `"abé"`},
		{`p "\xFF\xFE\x61\x00".dup.force_encoding("UTF-16").encode("UTF-8")`, `"a"`},
		{`p "abc".encode("UTF-16").encode("UTF-16LE").bytes`, `[97, 0, 98, 0, 99, 0]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	if class, msg := evalErr(t, `"\x00a".dup.force_encoding("UTF-16").encode("UTF-8")`); class != "Encoding::InvalidByteSequenceError" || !strings.Contains(msg, "on UTF-16") {
		t.Errorf("BOM-less decode: %s %q", class, msg)
	}
}

// TestMustASCIICompat covers rb_must_asciicompat at each entry point that now
// applies it, and its two non-raising arms (a compatible String, a non-String).
func TestMustASCIICompat(t *testing.T) {
	for _, src := range []string{
		`"79+4i".encode("UTF-16").to_f`,
		`"79+4i".encode("UTF-16").to_i`,
		`"79+4i".encode("UTF-16").to_c`,
		`Float("79".encode("UTF-16"))`,
		`Float("79".encode("UTF-16"), exception: false)`,
		`Integer("79".encode("UTF-16"))`,
		`Integer("79".encode("UTF-16"), exception: false)`,
		`Complex("79+4i".encode("UTF-16"))`,
		`Complex("79+4i".encode("UTF-16"), exception: false)`,
	} {
		class, msg := evalErr(t, src)
		if class != "Encoding::CompatibilityError" || msg != "ASCII incompatible encoding: UTF-16" {
			t.Errorf("src=%q got %s: %q", src, class, msg)
		}
	}
	// The pass-through arms: an ASCII-compatible String, and a non-String operand.
	for _, c := range []struct{ src, want string }{
		{`p "79".to_f`, `79.0`},
		{`p "79".to_i`, `79`},
		{`p "79+4i".to_c`, `(79+4i)`},
		{`p Float(79)`, `79.0`},
		{`p Integer(79.5)`, `79`},
		{`p Complex(1, 2)`, `(1+2i)`},
	} {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestStringMatchFamily covers rb_str_match, rb_str_match_m and
// rb_str_match_m_p: the #=~ delegation, the dispatched #match with its block,
// the get_pat coercion ladder, and match?'s position argument.
func TestStringMatchFamily(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p("hello" =~ /l/)`, `2`},
		{`p("hello" =~ nil)`, `nil`},
		{"o = Object.new\ndef o.=~(x); \"got #{x}\"; end\np(\"zz\" =~ o)", `"got zz"`},
		{`p "hello".match(/l/)[0]`, `"l"`},
		{`p "hello".match("l", 3)[0]`, `"l"`},
		{`p "hello".match(/l/) { |m| m[0] * 2 }`, `"ll"`},
		{`p "hello".match(/z/) { :never }`, `nil`},
		{"o = Object.new\ndef o.to_str; \".\"; end\np \"hello\".match(o)[0]", `"h"`},
		{`p "string".match?(/str/i, 0)`, `true`},
		{`p "string".match?(/str/i, 1)`, `false`},
		{`p "string".match?(/tr/, -5)`, `true`},
		{`p "string".match?(/str/, 99)`, `false`},
		{`p "string".match?(/str/)`, `true`},
		{`p "string".match?(nil.to_s)`, `true`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	for _, c := range []struct{ src, class, msg string }{
		{`"a" =~ "b"`, "TypeError", "type mismatch: String given"},
		{`"a".match(Object.new)`, "TypeError", "wrong argument type Object (expected Regexp)"},
		{`"a".match?(Object.new)`, "TypeError", "wrong argument type Object (expected Regexp)"},
		{`"a".match`, "ArgumentError", "wrong number of arguments (given 0, expected 1..2)"},
		{`"a".match?`, "ArgumentError", "wrong number of arguments (given 0, expected 1..2)"},
		{`"a".match?(/a/, 1, 2)`, "ArgumentError", "wrong number of arguments (given 3, expected 1..2)"},
	} {
		class, msg := evalErr(t, c.src)
		if class != c.class || msg != c.msg {
			t.Errorf("src=%q got %s: %q, want %s: %q", c.src, class, msg, c.class, c.msg)
		}
	}
	// Regexp#match? routes through the same regexpMatchP.
	if got := eval(t, `p [/a/.match?(nil), /a/.match?("bab", 1), /a/.match?("bab", 9), /a/.match?("bab")]`); got != "[false, true, false, true]\n" {
		t.Errorf("Regexp#match?: %q", got)
	}
}

// TestBytesplicePromotesEncoding covers rb_str_bytesplice's rb_enc_check arm,
// the equal-encoding fast path that skips it, and the incompatible case.
func TestBytesplicePromotesEncoding(t *testing.T) {
	cases := []struct{ src, want string }{
		{`s = +"xxxxxx".force_encoding(Encoding::US_ASCII); s.bytesplice(0, 3, "こんにちは"); p s.encoding`,
			`#<Encoding:UTF-8>`},
		{`s = +"こんにちは"; s.bytesplice(0, 3, "xxxxxx".dup.force_encoding(Encoding::US_ASCII)); p s.encoding`,
			`#<Encoding:UTF-8>`},
		{`s = +"hello"; s.bytesplice(0, 1, "J"); p [s, s.encoding]`, `["Jello", #<Encoding:UTF-8>]`},
		{`s = +"xxx".force_encoding(Encoding::US_ASCII); s.bytesplice(0..2, "こんにちは", 0..2); p s.encoding`,
			`#<Encoding:UTF-8>`},
		{`s = +"xxx".force_encoding(Encoding::US_ASCII); s.bytesplice(0, 3, "こんにちは", 0, 3); p s.encoding`,
			`#<Encoding:UTF-8>`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	if class, _ := evalErr(t, `s = +"é"; s.bytesplice(0, 2, "a\x00".dup.force_encoding("UTF-16LE"))`); class != "Encoding::CompatibilityError" {
		t.Errorf("incompatible bytesplice: got %s", class)
	}
}

// TestIOParagraphSwallow covers rb_io_getline_1's post-read swallow(fptr, '\n'):
// a real IO advances past the whole newline run while the returned string keeps
// only the two separator newlines, and a StringIO does neither.
func TestIOParagraphSwallow(t *testing.T) {
	const setup = `require 'stringio'
D = "a\nb\n\n\n\nc\nd\n\ne\n"
path = File.join(Dir.tmpdir, "rbgo-para-#{Process.pid}.txt")
File.write(path, D)
`
	cases := []struct{ src, want string }{
		// The stream is left at the next paragraph, not inside the newline run.
		{setup + `f = File.open(path); f.gets(""); p [f.gets, f.pos]`, `["c\n", 9]`},
		// A limit-truncated read swallows too (c != EOF).
		{setup + `f = File.open(path); p [f.gets("", 3), f.pos]`, `["a\nb", 7]`},
		// A StringIO keeps the whole run in the result and swallows nothing.
		{setup + `s = StringIO.new(D); p s.gets("")`, `"a\nb\n\n\n\n"`},
		{setup + `s = StringIO.new(D); s.gets(""); p [s.gets, s.pos]`, `["c\n", 9]`},
		// A real IO keeps exactly the two separator newlines.
		{setup + `f = File.open(path); p f.gets("")`, `"a\nb\n\n"`},
		// chomp strips the separator run; EOF ends the last paragraph.
		{setup + `f = File.open(path); p [f.gets("", chomp: true), f.gets("", chomp: true), f.gets("", chomp: true), f.gets("")]`,
			`["a\nb", "c\nd", "e\n", nil]`},
		// A stream that is nothing but newlines yields nil.
		{`require 'stringio'` + "\n" + `p StringIO.new("\n\n\n").gets("")`, `nil`},
		// A nil $/ makes the no-argument reads take the whole remainder
		// (defaultGetsSep's rb_rs == Qnil arm).
		{setup + "$VERBOSE = nil\n$/ = nil\nf = File.open(path); p [f.gets, f.gets]",
			`["a\nb\n\n\n\nc\nd\n\ne\n", nil]`},
		{setup + "$VERBOSE = nil\n$/ = nil\np File.open(path).readlines.size", `1`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestEncCharLen covers rb_enc_mbclen for every family encCharLen models,
// including the truncated-unit arms and the charmap fallback.
func TestEncCharLen(t *testing.T) {
	cases := []struct {
		s, enc string
		want   int
	}{
		{"abc", "UTF-8", 1}, {"é", "UTF-8", 2}, {"\xFFa", "UTF-8", 1}, {"a", "", 1},
		{"\xF0\xA4\xAD\xA2", "ASCII-8BIT", 1},
		{"ab", "US-ASCII", 1},
		{"\xC3\xA9", "ISO-8859-1", 1},
		{"\xFE\xFF\x00a", "UTF-16", 1},
		{"\x00\x00\xFE\xFF", "UTF-32", 1},
		{"a\x00b\x00", "UTF-16LE", 2},
		{"\x00a\x00b", "UTF-16BE", 2},
		{"\x3C\xD8\xF3\xDF", "UTF-16LE", 4}, // surrogate pair
		{"\xD8\x3C\xDF\xF3", "UTF-16BE", 4},
		{"\x3C\xD8", "UTF-16LE", 2}, // high surrogate with no room for its partner
		{"a", "UTF-16LE", 1},        // truncated code unit
		{"\x00\x00\x00a", "UTF-32BE", 4},
		{"\x00\x00", "UTF-32BE", 2}, // truncated unit
		// Shift_JIS lead bytes, half-width katakana, and the stand-alone ranges.
		{"\xF0\xA4", "Shift_JIS", 2}, {"\xAD\xA2", "Shift_JIS", 1},
		{"\x81\x40", "Windows-31J", 2}, {"\x80\x40", "Shift_JIS", 1},
		{"\xA0\x40", "Shift_JIS", 1}, {"\xFD", "Shift_JIS", 1},
		{"\x81", "Shift_JIS", 1}, // lead byte with nothing after it
		// EUC-JP: SS2, SS3 and the two-byte lead range.
		{"\x8E\xB1", "EUC-JP", 2}, {"\x8F\xA1\xA1", "EUC-JP", 3},
		{"\xA4\xA2", "EUC-JP", 2}, {"A", "EUC-JP", 1}, {"\xFF", "EUC-JP", 1},
		{"\x8F\xA1", "EUC-JP", 2}, // SS3 truncated
		// A charmap codec is single-byte; a stateful one keeps the UTF-8 walk.
		{"\xC3\xA9", "Windows-1252", 1},
		{"é", "ISO-2022-JP", 2},
	}
	for _, c := range cases {
		if got := encCharLen(c.s, c.enc); got != c.want {
			t.Errorf("encCharLen(%q, %s) = %d, want %d", c.s, c.enc, got, c.want)
		}
	}
}

// TestGraphemeClustersByEncoding covers
// rb_str_enumerate_grapheme_clusters' two arms (non-Unicode falls back to
// characters; a real UTF-16/UTF-32 string is clustered in its own bytes), the
// block form, and decodeFixedWidthRune's rejection paths.
func TestGraphemeClustersByEncoding(t *testing.T) {
	const flag = `"ab\u{1f3f3}\u{fe0f}\u{200d}\u{1f308}\u{1F43E}"`
	cases := []struct{ src, want string }{
		{`p ` + flag + `.grapheme_clusters`, `["a", "b", "🏳️‍🌈", "🐾"]`},
		// The block form yields and returns the receiver.
		{`a = []; s = ` + flag + `; r = s.grapheme_clusters { |c| a << c }; p [a.size, r.equal?(s)]`, `[4, true]`},
		{`a = []; s = ` + flag + `; r = s.each_grapheme_cluster { |c| a << c }; p [a.size, r.equal?(s)]`, `[4, true]`},
		// Non-Unicode encodings enumerate characters instead.
		{`p "\xF0\xA4\xAD\xA2".b.grapheme_clusters`, `["\xF0", "\xA4", "\xAD", "\xA2"]`},
		{`p "a\xC3\xA9".dup.force_encoding("ISO-8859-1").grapheme_clusters`, `["a", "\xC3", "\xA9"]`},
		{`p "abc".dup.force_encoding("US-ASCII").grapheme_clusters`, `["a", "b", "c"]`},
		{`p "abc".encode("UTF-16").grapheme_clusters.size`, `8`},
		// A real UTF-16/UTF-32 string clusters in its own bytes.
		{`p ` + flag + `.encode(Encoding::UTF_16LE).grapheme_clusters.map { |c| c.bytes.size }`, `[2, 2, 12, 4]`},
		{`p ` + flag + `.encode(Encoding::UTF_16BE).grapheme_clusters.map { |c| c.bytes.size }`, `[2, 2, 12, 4]`},
		{`p ` + flag + `.encode(Encoding::UTF_32LE).grapheme_clusters.map { |c| c.bytes.size }`, `[4, 4, 16, 4]`},
		{`p ` + flag + `.encode(Encoding::UTF_32BE).grapheme_clusters.map { |c| c.bytes.size }`, `[4, 4, 16, 4]`},
		// The walk stops where onig_match would: at bytes that are not a character.
		{`p "a\x00\x00\xD8".dup.force_encoding("UTF-16LE").grapheme_clusters.map(&:bytes)`, `[[97, 0]]`},
		{`p "\x00\x00\x00a\x00\x11\x00\x00".dup.force_encoding("UTF-32BE").grapheme_clusters.size`, `1`},
		{`p "".dup.force_encoding("UTF-16LE").grapheme_clusters`, `[]`},
		{`p "a".dup.force_encoding("UTF-16LE").grapheme_clusters`, `[]`},
		// An enumerator is still produced without a block.
		{`p ` + flag + `.each_grapheme_cluster.to_a.size`, `4`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// decodeFixedWidthRune's direct rejections.
	for _, c := range []struct {
		b, enc string
		want   rune
	}{
		{"a\x00", "UTF-16LE", 'a'},
		{"\x00a", "UTF-16BE", 'a'},
		{"\x00\xD8", "UTF-16LE", -1},         // lone high surrogate
		{"\x3C\xD8\x41\x00", "UTF-16LE", -1}, // high surrogate + non-low
		{"\x3C\xD8\xF3", "UTF-16LE", -1},     // wrong length
		{"\x00\x00\x00a", "UTF-32BE", 'a'},
		{"a\x00\x00\x00", "UTF-32LE", 'a'},
		{"\x00\x00\x11\x00", "UTF-32LE", -1}, // above U+10FFFF
		{"\x00\x00\x00\xFF", "UTF-32LE", -1}, // negative once the top byte is set
		{"\x00\x00\xD8\x00", "UTF-32BE", -1}, // surrogate
		{"\x00\x00", "UTF-32BE", -1},         // wrong length
		{"ab", "UTF-8", -1},                  // not a fixed-width encoding
	} {
		if got := decodeFixedWidthRune(c.b, c.enc); got != c.want {
			t.Errorf("decodeFixedWidthRune(%q, %s) = %d, want %d", c.b, c.enc, got, c.want)
		}
	}
}
