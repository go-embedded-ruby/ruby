package vm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestZlib covers the Zlib module (require "zlib"), now backed by the pure-Go
// github.com/go-ruby-zlib/zlib library. Checksums are asserted byte-exact
// against MRI Ruby 4.0.5; the compressors are validated by round-trip and by
// inflating a genuine MRI-produced stream (a deflate/gzip byte stream is
// implementation-defined and need not equal MRI's), and the error classes and
// constants are asserted against MRI as well.
func TestZlib(t *testing.T) {
	cases := []struct{ src, want string }{
		// Checksums, exact vs MRI (incl. running initial value and the no-arg form).
		{`p Zlib.crc32("hello world")`, "222957957\n"},
		{`p Zlib.crc32("hello")`, "907060870\n"},
		{`p Zlib.crc32("hello", 12345)`, "1779074256\n"},
		{`p Zlib.crc32`, "0\n"},
		{`p Zlib.adler32("hello world")`, "436929629\n"},
		{`p Zlib.adler32("hello")`, "103547413\n"},
		{`p Zlib.adler32("hello", 999)`, "430573051\n"},
		{`p Zlib.adler32`, "1\n"},
		// Combine: crc32(a) + crc32(b) over len(b) == crc32(a+b).
		{`p Zlib.crc32_combine(Zlib.crc32("hello "), Zlib.crc32("world"), 5)`, "222957957\n"},
		{`p Zlib.adler32_combine(Zlib.adler32("hello "), Zlib.adler32("world"), 5)`, "436929629\n"},

		// Module-level and class one-shot deflate/inflate round-trips.
		{`p Zlib.inflate(Zlib.deflate("round trip via module"))`, "\"round trip via module\"\n"},
		{`p Zlib::Inflate.inflate(Zlib::Deflate.deflate("hello world hello world"))`, "\"hello world hello world\"\n"},
		{`p Zlib::Inflate.inflate(Zlib::Deflate.deflate("ABC" * 100, 9)).length`, "300\n"},
		{`p Zlib::Deflate.deflate("").class`, "String\n"},
		{`p Zlib.deflate("x", Zlib::NO_COMPRESSION).class`, "String\n"},

		// gzip / gunzip round-trip (payload compared, not raw bytes).
		{`p Zlib.gunzip(Zlib.gzip("gzip payload here"))`, "\"gzip payload here\"\n"},
		{`p Zlib.gunzip(Zlib.gzip("p", Zlib::BEST_SPEED))`, "\"p\"\n"},

		// Streaming Deflate: deflate(...) + finish round-trips, accessors work.
		{"z = Zlib::Deflate.new(Zlib::BEST_COMPRESSION)\n" +
			"s = z.deflate(\"abc\") + z.finish\n" +
			"p Zlib::Inflate.inflate(s)", "\"abc\"\n"},
		{"z = Zlib::Deflate.new\n" +
			"z << \"abc\"\n" +
			"s = z.deflate(\"\", Zlib::FINISH)\n" +
			"p Zlib::Inflate.inflate(s)", "\"abc\"\n"},
		{"z = Zlib::Deflate.new\n" +
			"z.deflate(\"abcabc\")\n" +
			"z.finish\n" +
			"p [z.total_in, z.finished?]", "[6, true]\n"},
		{"z = Zlib::Deflate.new\n" +
			"z.deflate(\"x\")\n" +
			"z.finish\n" +
			"p [z.adler.class, z.total_out > 0]", "[Integer, true]\n"},

		// Streaming Inflate: feed a complete stream, read accessors.
		{"d = Zlib::Deflate.deflate(\"streamed data\")\n" +
			"inf = Zlib::Inflate.new\n" +
			"p inf.inflate(d)", "\"streamed data\"\n"},
		{"d = Zlib::Deflate.deflate(\"abc\")\n" +
			"inf = Zlib::Inflate.new\n" +
			"inf << d\n" +
			"p [inf.finish, inf.finished?]", "[\"abc\", true]\n"},
		{"d = Zlib::Deflate.deflate(\"abcdef\")\n" +
			"inf = Zlib::Inflate.new\n" +
			"inf.inflate(d)\n" +
			"p [inf.total_out, inf.total_in > 0, inf.adler.class]", "[6, true, Integer]\n"},

		// Constants, exact vs MRI.
		{`p Zlib::NO_COMPRESSION`, "0\n"},
		{`p Zlib::BEST_SPEED`, "1\n"},
		{`p Zlib::BEST_COMPRESSION`, "9\n"},
		{`p Zlib::DEFAULT_COMPRESSION`, "-1\n"},
		{`p Zlib::DEFAULT_STRATEGY`, "0\n"},
		{`p Zlib::FILTERED`, "1\n"},
		{`p Zlib::HUFFMAN_ONLY`, "2\n"},
		{`p Zlib::RLE`, "3\n"},
		{`p Zlib::FIXED`, "4\n"},
		{`p Zlib::NO_FLUSH`, "0\n"},
		{`p Zlib::SYNC_FLUSH`, "2\n"},
		{`p Zlib::FULL_FLUSH`, "3\n"},
		{`p Zlib::FINISH`, "4\n"},
		{`p Zlib::VERSION`, "\"3.2.3\"\n"},
		{`p Zlib::ZLIB_VERSION.class`, "String\n"},

		// Error hierarchy, exact vs MRI.
		{`p Zlib::Error.ancestors.include?(StandardError)`, "true\n"},
		{`p Zlib::StreamError < Zlib::Error`, "true\n"},
		{`p Zlib::BufError < Zlib::Error`, "true\n"},
		{`p Zlib::DataError < Zlib::Error`, "true\n"},
		{`p Zlib::GzipFile < Zlib::Error`, "true\n"},
		{`p Zlib::GzipFile::Error < Zlib::Error`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, "require \"zlib\"\n"+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}

	// rbgo inflates a genuine zlib stream produced by MRI's compressor
	// (Zlib::Deflate.deflate("hello world hello world")) — interop, not byte-eq.
	dir := t.TempDir()
	zPath := filepath.ToSlash(filepath.Join(dir, "z.bin"))
	mriStream := []byte{120, 156, 203, 72, 205, 201, 201, 87, 40, 207, 47, 202, 73, 81, 200, 64, 176, 1, 105, 231, 8, 217}
	if err := os.WriteFile(zPath, mriStream, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, fmt.Sprintf("require \"zlib\"\np Zlib::Inflate.inflate(File.read(%q))", zPath)); got != "\"hello world hello world\"\n" {
		t.Errorf("inflate MRI stream: got %q", got)
	}

	// A valid zlib header (0x78 0x9c) followed by a garbage body passes the
	// header check but fails decompression — the read-error path -> Zlib::DataError.
	badPath := filepath.ToSlash(filepath.Join(dir, "bad.bin"))
	if err := os.WriteFile(badPath, []byte{0x78, 0x9c, 'x', 'x', 'x', 'x'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runErr(t, fmt.Sprintf("require \"zlib\"\nZlib::Inflate.inflate(File.read(%q))", badPath)); err == nil || !strings.Contains(err.Error(), "Zlib::DataError") {
		t.Errorf("corrupt body: got %v want Zlib::DataError", err)
	}

	// Error paths: each maps to the exact MRI class.
	errs := []struct{ src, want string }{
		// Out-of-range level -> Zlib::StreamError (one-shot, module, and stream new).
		{`Zlib::Deflate.deflate("x", 99)`, "Zlib::StreamError"},
		{`Zlib.deflate("x", -5)`, "Zlib::StreamError"},
		{`Zlib::Deflate.new(99)`, "Zlib::StreamError"},
		{`Zlib.gzip("x", 99)`, "Zlib::StreamError"},
		// Bad / truncated zlib stream -> Zlib::DataError.
		{`Zlib::Inflate.inflate("not a zlib stream")`, "Zlib::DataError"},
		{`Zlib.inflate("not a zlib stream")`, "Zlib::DataError"},
		{`g = Zlib::Deflate.deflate("hello world" * 10); Zlib::Inflate.inflate(g[0, 6])`, "Zlib::DataError"},
		{`Zlib::Inflate.new.inflate("not a zlib stream")`, "Zlib::DataError"},
		{`Zlib::Inflate.new << "not a zlib stream"`, "Zlib::DataError"}, // the #<< append path
		// Non-gzip input -> Zlib::GzipFile::Error.
		{`Zlib.gunzip("not gzip data at all")`, "Zlib::GzipFile::Error"},
		// Using a finished streaming compressor -> Zlib::StreamError (matches MRI).
		{`z = Zlib::Deflate.new; z.deflate("a", Zlib::FINISH); z.deflate("b")`, "Zlib::StreamError"},
		{`z = Zlib::Deflate.new; z.finish; z << "b"`, "Zlib::StreamError"},
	}
	for _, c := range errs {
		if err := runErr(t, "require \"zlib\"\n"+c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got %v want %q", c.src, err, c.want)
		}
	}
}
