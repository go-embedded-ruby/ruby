// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"path/filepath"
	"testing"
)

// eucFixture writes the EUC-JP bytes of "ありがとう\n" to a temp file and returns
// a Ruby prelude binding @p to its (forward-slash) path. The bytes are written
// raw so the test does not depend on a UTF-8→EUC-JP encoder.
func eucFixture(t *testing.T) (path, prelude string) {
	t.Helper()
	p := filepath.ToSlash(filepath.Join(t.TempDir(), "euc.txt"))
	// A4A2 A4EA A4AC A4C8 A4A6 = ありがとう in EUC-JP, then a newline.
	return p, fmt.Sprintf("@p=%q; File.binwrite(@p, \"\\xA4\\xA2\\xA4\\xEA\\xA4\\xAC\\xA4\\xC8\\xA4\\xA6\\n\"); ", p)
}

// TestIOWave20FileOpenHashOptions covers File.open / File.new / Kernel#open
// treating a trailing Hash as options (mode / :encoding / :external_encoding /
// :internal_encoding) rather than a mode String — io.c rb_scan_args "12:" +
// rb_io_extract_modeenc. Asserted against MRI Ruby 4.0.5.
func TestIOWave20FileOpenHashOptions(t *testing.T) {
	_, pre := eucFixture(t)
	cases := []struct{ src, want string }{
		// String mode with an "ext:int" suffix records both encodings.
		{pre + `f=File.open(@p,"r:euc-jp:utf-8"); p [f.external_encoding.name, f.internal_encoding.name]`,
			"[\"EUC-JP\", \"UTF-8\"]\n"},
		// mode: option (a trailing Hash) is options, not the mode String.
		{pre + `f=File.open(@p, mode: "r:euc-jp:utf-8"); p [f.external_encoding.name, f.internal_encoding.name]`,
			"[\"EUC-JP\", \"UTF-8\"]\n"},
		// :external_encoding / :internal_encoding options.
		{pre + `f=File.open(@p, mode: "r", external_encoding: "euc-jp", internal_encoding: "utf-8"); p f.internal_encoding.name`,
			"\"UTF-8\"\n"},
		// :encoding "ext:int" option.
		{pre + `f=File.open(@p, mode: "r", encoding: "euc-jp:utf-8"); p f.external_encoding.name`,
			"\"EUC-JP\"\n"},
		// File.new goes through the same resolver.
		{pre + `f=File.new(@p, mode: "r:euc-jp"); p f.external_encoding.name`, "\"EUC-JP\"\n"},
		// Kernel#open routes to File.open.
		{pre + `f=open(@p, mode: "r:euc-jp"); p f.external_encoding.name`, "\"EUC-JP\"\n"},
		// Integer mode via File::RDONLY, and a to_int object.
		{pre + `p File.open(@p, File::RDONLY).read.bytesize`, "11\n"},
		{pre + `o=Object.new; def o.to_int; File::RDONLY; end; p File.open(@p, o).read.bytesize`, "11\n"},
		// to_str object as the mode.
		{pre + `o=Object.new; def o.to_str; "rb"; end; p File.open(@p, o).binmode?`, "true\n"},
		// nil positional mode falls back to the :mode option.
		{pre + `f=File.open(@p, nil, mode: "r:euc-jp"); p f.external_encoding.name`, "\"EUC-JP\"\n"},
		// nil :mode option leaves the default read mode.
		{pre + `f=File.open(@p, mode: nil); p f.external_encoding.name`, "\"UTF-8\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// A bad (non-String, non-Integer, non-coercible) mode raises TypeError.
	if cls, msg := evalErr(t, pre+`File.open(@p, Object.new)`); cls != "TypeError" || msg != "no implicit conversion of Object into String" {
		t.Errorf("bad mode: got %s: %q", cls, msg)
	}
	// A Hash-only call (no path) is a wrong-number-of-arguments error.
	if cls, _ := evalErr(t, `File.open(mode: "r")`); cls != "ArgumentError" {
		t.Errorf("hash-only File.open: got %s", cls)
	}
}

// TestIOWave20ReadTranscoding covers IO#read honouring the external/internal
// encoding: a full read transcodes external→internal and tags the result; a
// length read stays BINARY; a passed buffer is retagged only on a full read
// (io.c io_read / io_enc_str). Asserted against MRI Ruby 4.0.5.
func TestIOWave20ReadTranscoding(t *testing.T) {
	_, pre := eucFixture(t)
	cases := []struct{ src, want string }{
		// Whole read transcodes EUC-JP→UTF-8.
		{pre + `f=File.open(@p,"r:euc-jp:utf-8"); s=f.read; p [s, s.encoding.name]`,
			"[\"ありがとう\\n\", \"UTF-8\"]\n"},
		// External-only: no transcoding, tagged EUC-JP.
		{pre + `f=File.open(@p,"r:euc-jp"); p f.read.encoding.name`, "\"EUC-JP\"\n"},
		// read(size) returns BINARY, position advances.
		{pre + `f=File.open(@p,"r:euc-jp:utf-8"); p [f.read(2).bytes, f.read(2).encoding.name]`,
			"[[164, 162], \"ASCII-8BIT\"]\n"},
		// read(nil, buf): buffer is retagged to the internal encoding and equal to result.
		{pre + `f=File.open(@p,"r:euc-jp:utf-8"); b="".dup.force_encoding("ISO-8859-1"); r=f.read(nil,b); p [b.equal?(r), b.encoding.name]`,
			"[true, \"UTF-8\"]\n"},
		// read(size, buf): buffer bytes set, its encoding left unchanged.
		{pre + `f=File.open(@p,"r:euc-jp:utf-8"); b="".dup.force_encoding("ISO-8859-1"); f.read(2,b); p [b.bytes, b.encoding.name]`,
			"[[164, 162], \"ISO-8859-1\"]\n"},
		// read(size) at EOF clears a passed buffer, leaving its encoding, returns nil.
		{pre + `f=File.open(@p,"r:euc-jp"); f.read; b="abc".dup.force_encoding("ISO-8859-1"); r=f.read(1,b); p [r, b.size, b.encoding.name]`,
			"[nil, 0, \"ISO-8859-1\"]\n"},
		// read(size) at EOF with no buffer is nil; read(0) is "".
		{pre + `f=File.open(@p,"r:euc-jp"); f.read; p [f.read(1), f.read(0)]`, "[nil, \"\"]\n"},
		// Pure-ASCII content is retagged to the internal encoding without a converter.
		{`File.write(@q="` + filepathTmp(t, "ascii.txt") + `", "line"); f=File.open(@q, mode: "r", external_encoding: "IBM866", internal_encoding: "utf-8"); s=f.read; p [s, s.encoding.name]`,
			"[\"line\", \"UTF-8\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// A frozen output buffer on a full read is a FrozenError (io.c rb_str_modify).
	if cls, msg := evalErr(t, pre+`f=File.open(@p,"r:euc-jp:utf-8"); f.read(nil, "x".freeze)`); cls != "FrozenError" || msg != `can't modify frozen String: "x"` {
		t.Errorf("frozen read buffer: got %s: %q", cls, msg)
	}
}

// filepathTmp returns a forward-slash temp path for name in a fresh temp dir.
func filepathTmp(t *testing.T, name string) string {
	t.Helper()
	return filepath.ToSlash(filepath.Join(t.TempDir(), name))
}

// TestIOWave20Readchar covers IO#readchar decoding one character of the external
// encoding and transcoding to the internal one, plus the EOFError path (io.c
// io_getc). Asserted against MRI Ruby 4.0.5.
func TestIOWave20Readchar(t *testing.T) {
	_, pre := eucFixture(t)
	cases := []struct{ src, want string }{
		// One EUC-JP character transcoded to UTF-8.
		{pre + `f=File.open(@p,"r:euc-jp:utf-8"); c=f.readchar; p [c, c.encoding.name]`, "[\"あ\", \"UTF-8\"]\n"},
		// External-only: the two raw EUC-JP bytes tagged EUC-JP.
		{pre + `f=File.open(@p,"r:euc-jp"); c=f.readchar; p [c.bytes, c.encoding.name]`, "[[164, 162], \"EUC-JP\"]\n"},
		// A plain UTF-8 file yields one UTF-8 character.
		{`File.write(@p="` + filepathTmp(t, "u.txt") + `", "aé"); f=File.open(@p); p [f.readchar, f.readchar]`, "[\"a\", \"é\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// readchar at EOF raises EOFError.
	if cls, msg := evalErr(t, pre+`f=File.open(@p,"r:euc-jp"); f.read; f.readchar`); cls != "EOFError" || msg != "end of file reached" {
		t.Errorf("readchar EOF: got %s: %q", cls, msg)
	}
	// An invalid lead byte is returned as a one-byte character.
	if got := eval(t, `File.binwrite(@p="`+filepathTmp(t, "bad.txt")+`", "\xFF"); f=File.open(@p, "r:utf-8"); c=f.readchar; p c.bytes`); got != "[255]\n" {
		t.Errorf("readchar invalid lead: got %q", got)
	}
	// A truncated multi-byte lead at EOF is also a one-byte character (sz<1 branch).
	if got := eval(t, `File.binwrite(@p="`+filepathTmp(t, "tr.txt")+`", "\xE3"); f=File.open(@p, "r:utf-8"); p f.readchar.bytes`); got != "[227]\n" {
		t.Errorf("readchar truncated lead: got %q", got)
	}
}

// TestIOWave20GetsEncoding covers the gets family tagging/transcoding each line
// per the stream's external/internal encoding, including the BINARY suppression
// of the internal encoding (io.c rb_io_getline_1). Asserted against MRI 4.0.5.
func TestIOWave20GetsEncoding(t *testing.T) {
	q := filepathTmp(t, "line.txt")
	pre := fmt.Sprintf("@p=%q; File.write(@p, \"line\"); ", q)
	cases := []struct{ src, want string }{
		{pre + `p File.open(@p,"r").gets.encoding.name`, "\"UTF-8\"\n"},
		{pre + `f=File.open(@p,"r"); f.set_encoding("US-ASCII"); p f.gets.encoding.name`, "\"US-ASCII\"\n"},
		// default_internal transcodes an ASCII line without a converter.
		{pre + `Encoding.default_internal="US-ASCII"; f=File.open(@p,"r"); e=f.gets.encoding.name; Encoding.default_internal=nil; p e`, "\"US-ASCII\"\n"},
		// A BINARY external encoding suppresses the internal one.
		{pre + `f=File.open(@p,"r"); f.set_encoding("BINARY","UTF-8"); p f.gets.encoding.name`, "\"ASCII-8BIT\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestIOWave20SetEncodingByBOM covers IO#set_encoding_by_bom (io.c
// rb_io_set_encoding_by_bom + io_strip_bom): each BOM's detection and encoding,
// the truncated / absent BOM nil paths, the not-readable nil, and the binmode /
// encoding-already-set / conversion-set ArgumentErrors and closed IOError.
// Asserted against MRI Ruby 4.0.5.
func TestIOWave20SetEncodingByBOM(t *testing.T) {
	p := filepathTmp(t, "bom.txt")
	open := fmt.Sprintf("@p=%q; File.binwrite(@p,\"\"); touch=->(s){File.binwrite(@p,s)}; ", p)
	// Each BOM is detected, stripped, and names the external encoding.
	boms := []struct{ bytes, enc, rest string }{
		{`\xEF\xBB\xBFabc`, "UTF-8", "abc"},
		{`\xFF\xFEabc`, "UTF-16LE", "abc"},
		{`\xFE\xFFabcd`, "UTF-16BE", "abcd"},
		{`\xFF\xFE\x00\x00abc`, "UTF-32LE", "abc"},
		{`\x00\x00\xFE\xFFabcd`, "UTF-32BE", "abcd"},
	}
	for _, b := range boms {
		src := open + fmt.Sprintf("io=File.open(@p,\"rb\"); touch.(\"%s\"); r=io.set_encoding_by_bom; p [r.name, io.external_encoding.name, io.read.b]", b.bytes)
		want := fmt.Sprintf("[%q, %q, %q]\n", b.enc, b.enc, b.rest)
		if got := eval(t, src); got != want {
			t.Errorf("BOM %s: got=%q want=%q", b.enc, got, want)
		}
	}
	// FF FE followed by a single NUL is UTF-16LE (not the incomplete UTF-32LE).
	if got := eval(t, open+`io=File.open(@p,"rb"); touch.("\xFF\xFE\x00"); r=io.set_encoding_by_bom; p [r.name, io.read.b]`); got != "[\"UTF-16LE\", \"\\x00\"]\n" {
		t.Errorf("FFFE00: got %q", got)
	}
	// No / truncated / absent BOM returns nil and leaves ASCII-8BIT + all bytes.
	nilCases := []struct{ bytes, rest string }{
		{`\xEF`, "\\xEF"}, {`\xEF\xBB`, "\\xEF\\xBB"}, {`\xFE`, "\\xFE"},
		{`\xFF`, "\\xFF"}, {`\x00`, "\\x00"}, {`\x00\x00\xFE`, "\\x00\\x00\\xFE"},
		{`abc`, "abc"}, {``, ""},
	}
	for _, c := range nilCases {
		src := open + fmt.Sprintf("io=File.open(@p,\"rb\"); touch.(\"%s\"); r=io.set_encoding_by_bom; p [r, io.external_encoding.name, io.read.b]", c.bytes)
		want := "[nil, \"ASCII-8BIT\", \"" + c.rest + "\"]\n"
		if got := eval(t, src); got != want {
			t.Errorf("nil BOM %q: got=%q want=%q", c.bytes, got, want)
		}
	}
	// A write-only stream is not readable: nil, encoding untouched.
	if got := eval(t, open+`touch.(""); io=File.open(@p,"wb"); p [io.set_encoding_by_bom, io.external_encoding.name]`); got != "[nil, \"ASCII-8BIT\"]\n" {
		t.Errorf("wb set_encoding_by_bom: got %q", got)
	}
	// Error paths.
	errCases := []struct{ src, class, msg string }{
		{open + `touch.(""); File.open(@p,"r").set_encoding_by_bom`, "ArgumentError", "ASCII incompatible encoding needs binmode"},
		{open + `touch.(""); io=File.open(@p,"rb"); io.set_encoding("utf-8"); io.set_encoding_by_bom`, "ArgumentError", "encoding is set to UTF-8 already"},
		{open + `touch.(""); io=File.open(@p,"rb"); io.set_encoding("utf-8","utf-16be"); io.set_encoding_by_bom`, "ArgumentError", "encoding conversion is set"},
		{open + `touch.(""); io=File.open(@p,"rb"); io.close; io.set_encoding_by_bom`, "IOError", "closed stream"},
	}
	for _, c := range errCases {
		cls, msg := evalErr(t, c.src)
		if cls != c.class || msg != c.msg {
			t.Errorf("src=%q\n got=%s: %q\nwant=%s: %q", c.src, cls, msg, c.class, c.msg)
		}
	}
}

// TestIOWave20ReadBOMMode covers IO.read / File.read with a "rb:BOM|enc" mode
// stripping a leading BOM (io.c) and modeHasBOM's ':'-less path. Asserted
// against MRI Ruby 4.0.5.
func TestIOWave20ReadBOMMode(t *testing.T) {
	p := filepathTmp(t, "r.txt")
	pre := fmt.Sprintf("@p=%q; ", p)
	cases := []struct{ src, want string }{
		// UTF-8 BOM stripped.
		{pre + `File.binwrite(@p,"\xEF\xBB\xBFhi"); p File.read(@p, mode: "rb:BOM|utf-16le").b`, "\"hi\"\n"},
		// UTF-16LE BOM stripped; remaining bytes kept.
		{pre + `File.binwrite(@p,"\xFF\xFEhi"); p File.read(@p, mode: "rb:BOM|utf-8").b`, "\"hi\"\n"},
		// No BOM present: BOM mode is a no-op on the bytes.
		{pre + `File.binwrite(@p,"hi"); p File.read(@p, mode: "rb:BOM|utf-8").b`, "\"hi\"\n"},
		// A plain mode (no ':') does not strip.
		{pre + `File.binwrite(@p,"\xEF\xBB\xBFhi"); p File.read(@p, mode: "rb").b`, "\"\\xEF\\xBB\\xBFhi\"\n"},
		// BOM strip only applies at offset 0: with an offset it is not consulted.
		{pre + `File.binwrite(@p,"\xEF\xBB\xBFhi"); p File.read(@p, 2, 1, mode: "rb:BOM|utf-8").b`, "\"\\xBB\\xBF\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
