// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIOPipeEncodingsWave22 drives IO.pipe's encoding-argument resolution
// (encPairFromArgs / stripBOMPrefix): a combined "ext:int" name, a BOM-prefixed
// name, two separate names, an Encoding object, and a #to_str-coerced first
// argument, plus the write end carrying no encoding and the defaults captured at
// creation. Values verified against MRI Ruby 4.0.5.
func TestIOPipeEncodingsWave22(t *testing.T) {
	cases := []struct{ src, want string }{
		// "ext:int" combined name on a single argument.
		{`r,w=IO.pipe("UTF-8:UTF-16LE"); v=[r.external_encoding.name, r.internal_encoding.name]; r.close; w.close; p v`,
			"[\"UTF-8\", \"UTF-16LE\"]\n"},
		// BOM| marker stripped (case-insensitive) on the external part.
		{`r,w=IO.pipe("BOM|UTF-8:ISO-8859-1"); v=[r.external_encoding.name, r.internal_encoding.name]; r.close; w.close; p v`,
			"[\"UTF-8\", \"ISO-8859-1\"]\n"},
		// two separate String arguments (external, internal).
		{`r,w=IO.pipe("UTF-8","UTF-16LE"); v=[r.external_encoding.name, r.internal_encoding.name]; r.close; w.close; p v`,
			"[\"UTF-8\", \"UTF-16LE\"]\n"},
		// a single Encoding object → external only, no internal.
		{`r,w=IO.pipe(Encoding::UTF_8); v=[r.external_encoding.name, r.internal_encoding]; r.close; w.close; p v`,
			"[\"UTF-8\", nil]\n"},
		// a #to_str-convertible first argument, carrying "ext:int".
		{`o=Object.new; def o.to_str; "UTF-8:UTF-16BE"; end
r,w=IO.pipe(o); v=[r.external_encoding.name, r.internal_encoding.name]; r.close; w.close; p v`,
			"[\"UTF-8\", \"UTF-16BE\"]\n"},
		// the write end never carries an external/internal encoding.
		{`r,w=IO.pipe(Encoding::UTF_8); v=[w.external_encoding, w.internal_encoding]; r.close; w.close; p v`,
			"[nil, nil]\n"},
		// no arguments → the read end captures the default encodings at creation.
		{`Encoding.default_external=Encoding::ISO_8859_1; Encoding.default_internal=Encoding::UTF_8
r,w=IO.pipe; v=[r.external_encoding.name, r.internal_encoding.name]; r.close; w.close; p v`,
			"[\"ISO-8859-1\", \"UTF-8\"]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOWriteEncodeWave22 drives ioWriteEncode / asWriteString: transcoding a
// write to a stream's non-BINARY external encoding, the no-transcode shortcuts
// (no external encoding, BINARY external, a StringIO, an argument already in the
// target encoding), the #to_s coercion of a non-String argument, and the
// non-String-#to_s fallback. Values verified against MRI Ruby 4.0.5, except the
// StringIO case, which asserts rbgo's current behaviour (StringIO writes are not
// transcoded — a deliberate scope boundary of this wave).
func TestIOWriteEncodeWave22(t *testing.T) {
	dir := slash(t.TempDir())
	f := func(name string) string { return dir + "/" + name }
	cases := []struct{ src, want string }{
		// transcodes UTF-8 → UTF-16BE (external encoding set), returning the
		// transcoded byte count.
		{`n=File.open("` + f("e1") + `","w",external_encoding: Encoding::UTF_16BE){|io| io.write("hi")}; p n`, "4\n"},
		{`File.open("` + f("e1") + `","w",external_encoding: Encoding::UTF_16BE){|io| io.write("hi")}; p File.binread("` + f("e1") + `").bytes`, "[0, 104, 0, 105]\n"},
		// no external encoding → bytes written unchanged.
		{`n=File.open("` + f("e2") + `","w"){|io| io.write("hi")}; p n`, "2\n"},
		// BINARY external encoding → no transcoding.
		{`n=File.open("` + f("e3") + `","wb"){|io| io.write("h\xC3\xA9")}; p n`, "3\n"},
		// argument already in the target encoding → no double transcode.
		{`n=File.open("` + f("e4") + `","w",external_encoding: Encoding::UTF_16BE){|io| io.write("hi".encode("UTF-16BE"))}; p n`, "4\n"},
		// StringIO write is NOT transcoded in rbgo (scope boundary): 2 bytes, not 4.
		{`require "stringio"; s=StringIO.new; s.set_encoding(Encoding::UTF_16BE); p s.write("hi")`, "2\n"},
		// #to_s coercion of a non-String argument.
		{`require "stringio"; s=StringIO.new; s.write(123); p s.string`, "\"123\"\n"},
		// non-String #to_s falls back to the object's default string form.
		{`require "stringio"
class W22; def to_s; 42; end; end
s=StringIO.new; n=s.write(W22.new); p [n>0, s.string.start_with?("#<")]`, "[true, true]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIOWriteEmptyWave22 covers ioWriteAll's empty-write shortcut: writing "" to
// a read-only or closed stream returns 0 without raising, while a non-empty write
// raises IOError. Verified against MRI Ruby 4.0.5.
func TestIOWriteEmptyWave22(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/w"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	oks := []struct{ src, want string }{
		{`io=File.open("` + f + `"); p io.write("")`, "0\n"},           // read-only, empty
		{`io=File.open("` + f + `"); io.close; p io.write("")`, "0\n"}, // closed, empty
	}
	for _, c := range oks {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if got := runFSErr(t, `io=File.open("`+f+`"); io.write("x")`); got != "IOError" {
		t.Errorf("read-only non-empty write: got %q want IOError", got)
	}
}

// TestIOCopyStreamWave22 drives IO.copy_stream over every source/destination
// shape handled by copyStreamRead / copyStreamReadObject / copyStreamWrite: a
// File IO source (with and without a src_offset, the offset preserving its
// position), a path/#to_path source, duck-typed #read and #readpartial sources,
// and File IO / path / #to_path / duck-typed #write destinations, plus the
// zero-length and StringIO-offset guards. Verified against MRI Ruby 4.0.5.
func TestIOCopyStreamWave22(t *testing.T) {
	dir := slash(t.TempDir())
	src := dir + "/src.txt"
	if err := os.WriteFile(filepath.FromSlash(src), []byte("hello world duck"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ src, want string }{
		// path source → File IO destination (flushed to disk immediately).
		{`to=File.open("` + dir + `/d1","wb"); IO.copy_stream("` + src + `", to); to.close; p File.read("` + dir + `/d1")`,
			"\"hello world duck\"\n"},
		// File IO source, no offset: reads from the current position, advancing it.
		{`from=File.open("` + src + `","rb"); from.pos=6; n=IO.copy_stream(from, "` + dir + `/d2"); p [n, from.pos, File.read("` + dir + `/d2")]`,
			"[10, 16, \"world duck\"]\n"},
		// File IO source WITH offset: preads without moving the position.
		{`from=File.open("` + src + `","rb"); from.pos=3; n=IO.copy_stream(from, "` + dir + `/d3", 5, 6); p [n, from.pos, File.read("` + dir + `/d3")]`,
			"[5, 3, \"world\"]\n"},
		// #to_path source and #to_path destination.
		{`sp=Object.new; def sp.to_path; "` + src + `"; end
dp=Object.new; def dp.to_path; "` + dir + `/d4"; end
IO.copy_stream(sp, dp); p File.read("` + dir + `/d4")`, "\"hello world duck\"\n"},
		// duck-typed #read(size, buf) source.
		{`class SR22; def initialize(io); @io=io; end; def read(s,b); @io.read(s,b); end; end
IO.copy_stream(SR22.new(File.open("` + src + `","rb")), "` + dir + `/d5"); p File.read("` + dir + `/d5")`,
			"\"hello world duck\"\n"},
		// duck-typed #readpartial(size, buf) source, drained until EOFError.
		{`class SP22; def initialize(io); @io=io; end; def readpartial(s,b); @io.readpartial(s,b); end; end
IO.copy_stream(SP22.new(File.open("` + src + `","rb")), "` + dir + `/d6"); p File.read("` + dir + `/d6")`,
			"\"hello world duck\"\n"},
		// duck-typed #read source with a copy_length bound.
		{`class SR22b; def initialize(io); @io=io; end; def read(s,b); @io.read(s,b); end; end
n=IO.copy_stream(SR22b.new(File.open("` + src + `","rb")), "` + dir + `/d7", 5); p [n, File.read("` + dir + `/d7")]`,
			"[5, \"hello\"]\n"},
		// a File IO source with a src_offset beyond EOF copies nothing.
		{`from=File.open("` + src + `","rb"); p IO.copy_stream(from, "` + dir + `/d8", 5, 1000)`, "0\n"},
		// a path source with a src_offset but no length reads from the offset to EOF.
		{`n=IO.copy_stream("` + src + `", "` + dir + `/d9", nil, 6); p [n, File.read("` + dir + `/d9")]`,
			"[10, \"world duck\"]\n"},
		// a bare object answering only #write receives the bytes (duck-typed dst).
		{`class WD22; def initialize; @b=+""; end; def write(s); @b<<s; end; def read; @b; end; end
wd=WD22.new; IO.copy_stream("` + src + `", wd); p wd.read`, "\"hello world duck\"\n"},
		// zero copy_length transfers nothing and touches neither object.
		{`from=Object.new; to=Object.new; p IO.copy_stream(from, to, 0)`, "0\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// A src_offset on a StringIO (no descriptor) is rejected.
	if got := runFSErr(t, `require "stringio"; IO.copy_stream(StringIO.new("abcdef"), "`+dir+`/dx", 2, 1)`); got != "ArgumentError" {
		t.Errorf("StringIO src_offset: got %q want ArgumentError", got)
	}
	// A src_offset on a bare (non-IO, non-path) object is rejected.
	if got := runFSErr(t, `o=Object.new; def o.read(*a); ""; end; IO.copy_stream(o, "`+dir+`/dy", 2, 1)`); got != "ArgumentError" {
		t.Errorf("object src_offset: got %q want ArgumentError", got)
	}
	// A duck-typed #readpartial source that raises a non-EOFError propagates it.
	if got := runFSErr(t, `class SPE22; def readpartial(s,b); raise "boom"; end; end; IO.copy_stream(SPE22.new, "`+dir+`/dz")`); got != "RuntimeError" {
		t.Errorf("readpartial raising: got %q want RuntimeError", got)
	}
	// A duck-typed #readpartial that yields an empty read terminates (rbgo's
	// defensive guard against an unbounded loop); nothing is copied.
	if got := runFS(t, `class SPZ22; def readpartial(s,b); ""; end; end; p IO.copy_stream(SPZ22.new, "`+dir+`/dw")`); got != "0\n" {
		t.Errorf("readpartial empty: got %q want 0", got)
	}
}

// TestIOEncodingOptionBOMWave22 covers the "BOM|" marker handling in the
// encoding options (extractEncodingOption): it is stripped on the :encoding
// option (both the plain and the "ext:int" forms) but rejected on
// :external_encoding / :internal_encoding, matching MRI Ruby 4.0.5.
func TestIOEncodingOptionBOMWave22(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/bom"
	cases := []struct{ src, want string }{
		{`File.open("` + f + `","w",encoding: "bom|utf-8"){|io| io.write("x")}; p File.binread("` + f + `").bytes`, "[120]\n"},
		{`File.open("` + f + `","w",encoding: "bom|utf-8:iso-8859-1"){|io| p io.external_encoding.name, io.internal_encoding.name}`,
			"\"UTF-8\"\n\"ISO-8859-1\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// BOM| is not accepted on :external_encoding / :internal_encoding.
	if got := runFSErr(t, `File.open("`+f+`","w",external_encoding: "bom|utf-8"){}`); got != "ArgumentError" {
		t.Errorf("external_encoding bom: got %q want ArgumentError", got)
	}
	if got := runFSErr(t, `File.open("`+f+`","r",internal_encoding: "bom|utf-8"){}`); got != "ArgumentError" {
		t.Errorf("internal_encoding bom: got %q want ArgumentError", got)
	}
}

// TestIOWriteNonblockWave22 covers IO#write_nonblock: a file write returns the
// byte count like #write; a pipe write reports would-block once the modelled
// kernel buffer fills — returning :wait_writable with exception: false — and the
// no-argument ArgumentError. Verified against MRI Ruby 4.0.5 semantics.
func TestIOWriteNonblockWave22(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/wnb"
	cases := []struct{ src, want string }{
		// to a file: behaves like write.
		{`File.open("` + f + `","w"){|io| p io.write_nonblock("abcde")}`, "5\n"},
		// to a pipe with room: writes the bytes.
		{`r,w=IO.pipe; n=w.write_nonblock("hi"); w.close; v=r.read; r.close; p [n, v]`, "[2, \"hi\"]\n"},
		// a full pipe reports would-block as :wait_writable (exception: false).
		{`r,w=IO.pipe
res=nil
loop { break if (res = w.write_nonblock("a"*10_000, exception: false)) == :wait_writable }
p res
r.close; w.close`, ":wait_writable\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if got := runFSErr(t, `File.open("`+f+`","w"){|io| io.write_nonblock}`); got != "ArgumentError" {
		t.Errorf("write_nonblock arity: got %q want ArgumentError", got)
	}
}

// TestIOReadpartialWave22 covers IO#readpartial's guards over a pipe and a file:
// a negative length (ArgumentError), a zero length returning the cleared output
// buffer, a closed stream (IOError), EOF on a fully read file (EOFError), and the
// output buffer receiving the bytes and being returned. Verified against MRI
// Ruby 4.0.5.
func TestIOReadpartialWave22(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/rp"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ src, want string }{
		// length 0 returns the (cleared) output buffer immediately.
		{`r,w=IO.pipe; buf=+"existing"; v=r.readpartial(0, buf); r.close; w.close; p [v.equal?(buf), buf]`, "[true, \"\"]\n"},
		// the output buffer receives the read data and is returned by identity.
		{`r,w=IO.pipe; w.write("hello"); w.close; buf=+"old"; v=r.readpartial(10, buf); r.close; p [v.equal?(buf), buf]`,
			"[true, \"hello\"]\n"},
		// a fully read file raises EOFError on the next readpartial.
		{`io=File.open("` + f + `"); io.read; r = (io.readpartial(3) rescue $!.class); io.close; p r`, "EOFError\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// negative length and closed stream.
	if got := runFSErr(t, `r,w=IO.pipe; r.readpartial(-1)`); got != "ArgumentError" {
		t.Errorf("readpartial(-1): got %q want ArgumentError", got)
	}
	if got := runFSErr(t, `r,w=IO.pipe; r.close; r.readpartial(1)`); got != "IOError" {
		t.Errorf("readpartial closed: got %q want IOError", got)
	}
}

// TestIOWave22CoverageEdges exercises the remaining introduced branches: the
// IO.pipe internal==external drop, write_nonblock reporting a full pipe with the
// default exception: true, readpartial's arity guard and its would-block report
// on an empty open pipe, and the :encoding option given an Encoding object.
func TestIOWave22CoverageEdges(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/ce"
	// IO.pipe with a "ext:int" whose halves are equal drops the internal encoding.
	if got := runFS(t, `r,w=IO.pipe("UTF-8:UTF-8"); v=r.internal_encoding; r.close; w.close; p v`); got != "nil\n" {
		t.Errorf("pipe equal enc: got %q want nil", got)
	}
	// :encoding given an Encoding object (not a String) sets the external encoding.
	if got := runFS(t, `File.open("`+f+`","w",encoding: Encoding::UTF_8){|io| p io.external_encoding.name}`); got != "\"UTF-8\"\n" {
		t.Errorf("encoding Encoding obj: got %q want UTF-8", got)
	}
	// write_nonblock arity guard.
	if got := runFSErr(t, `r,w=IO.pipe; w.write_nonblock`); got != "ArgumentError" {
		t.Errorf("write_nonblock arity: got %q want ArgumentError", got)
	}
	// write_nonblock on a full pipe with the default exception: true raises an
	// error that is an Errno::EAGAIN (MRI raises IO::EAGAINWaitWritable, a subclass;
	// rbgo raises Errno::EAGAIN directly — both are caught as Errno::EAGAIN).
	if got := runFS(t, `r,w=IO.pipe
res = (loop { w.write_nonblock("a"*10_000) } rescue $!.is_a?(Errno::EAGAIN))
r.close; w.close; p res`); got != "true\n" {
		t.Errorf("write_nonblock full: got %q want true", got)
	}
	// readpartial arity guard.
	if got := runFSErr(t, `r,w=IO.pipe; r.readpartial`); got != "ArgumentError" {
		t.Errorf("readpartial arity: got %q want ArgumentError", got)
	}
	// readpartial on an empty pipe whose write end is still open reports would-block
	// (rbgo cannot block; MRI would block here).
	if got := runFSErr(t, `r,w=IO.pipe; r.readpartial(5)`); got != "Errno::EAGAIN" {
		t.Errorf("readpartial empty open pipe: got %q want Errno::EAGAIN", got)
	}
}

// TestIOOfftLimitsWave22 covers the C off_t RangeError raised for oversized
// integer limits/offsets on gets and sysseek, and the IOError sysseek raises on a
// closed stream. Verified against MRI Ruby 4.0.5.
func TestIOOfftLimitsWave22(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/off"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("abc\ndef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errCases := map[string]string{
		`io=File.open("` + f + `"); io.gets(2**128)`:         "RangeError",
		`io=File.open("` + f + `"); io.sysseek(2**128)`:      "RangeError",
		`io=File.open("` + f + `"); io.close; io.sysseek(0)`: "IOError",
	}
	for src, want := range errCases {
		if got := runFSErr(t, src); got != want {
			t.Errorf("%s: got %q want %q", src, got, want)
		}
	}
}
