package vm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestZlib covers the Zlib module: crc32/adler32 (exact vs MRI, including a
// running initial value), and Deflate/Inflate round-trips and errors. The
// compressor's exact bytes are implementation-defined, so deflate is checked by
// round-trip and by inflating a genuine MRI-produced stream rather than by
// asserting the bytes. MRI Ruby 4.0.5.
func TestZlib(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Zlib.crc32("hello")`, "907060870\n"},
		{`p Zlib.crc32("hello", 12345)`, "1779074256\n"},
		{`p Zlib.crc32`, "0\n"},
		{`p Zlib.adler32("hello")`, "103547413\n"},
		{`p Zlib.adler32("hello", 999)`, "430573051\n"},
		{`p Zlib.adler32`, "1\n"},
		// Deflate -> Inflate round-trips (default level and an explicit one).
		{`p Zlib::Inflate.inflate(Zlib::Deflate.deflate("hello world hello world"))`, "\"hello world hello world\"\n"},
		{`p Zlib::Inflate.inflate(Zlib::Deflate.deflate("ABC" * 100, 9)).length`, "300\n"},
		{`p Zlib::Deflate.deflate("").class`, "String\n"},
	}
	for _, c := range cases {
		if got := eval(t, "require \"zlib\"\n"+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}

	// rbgo inflates a genuine zlib stream produced by MRI's compressor
	// (Zlib::Deflate.deflate("hello world hello world")).
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
	// header check but fails decompression — exercising the read-error path.
	badPath := filepath.ToSlash(filepath.Join(dir, "bad.bin"))
	if err := os.WriteFile(badPath, []byte{0x78, 0x9c, 'x', 'x', 'x', 'x'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runErr(t, fmt.Sprintf("require \"zlib\"\nZlib::Inflate.inflate(File.read(%q))", badPath)); err == nil || !strings.Contains(err.Error(), "Zlib::DataError") {
		t.Errorf("corrupt body: got %v want Zlib::DataError", err)
	}

	// Errors: an out-of-range level, a non-zlib header, and a truncated body.
	errs := []struct{ src, want string }{
		{`Zlib::Deflate.deflate("x", 99)`, "Zlib::Error"},
		{`Zlib::Deflate.deflate("x", -5)`, "Zlib::Error"},
		{`Zlib::Inflate.inflate("not a zlib stream")`, "Zlib::DataError"},
		{`g = Zlib::Deflate.deflate("hello world" * 10); Zlib::Inflate.inflate(g[0, 6])`, "Zlib::DataError"},
	}
	for _, c := range errs {
		if err := runErr(t, "require \"zlib\"\n"+c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got %v want %q", c.src, err, c.want)
		}
	}
}
