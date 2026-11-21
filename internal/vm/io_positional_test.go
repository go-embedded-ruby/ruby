// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestPositionalIOReachesTheFile pins IO#pwrite and IO#pread to the FILE rather
// than to the stream's buffer (#949). Every want is MRI 4.0.7's output for the
// same program (arm64-darwin25, measured with rubydiff on 2026-11-21); before
// the fix rbgo diverged on the first five rows and on the 0444 row.
func TestPositionalIOReachesTheFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.ToSlash(filepath.Join(dir, "f"))
	reset := fmt.Sprintf("File.write(%q, \"0123456789\"); ", p)
	read := fmt.Sprintf("File.read(%q)", p)
	cases := []struct{ name, src, want string }{
		{"pwrite lands at once",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); f.pwrite("AB", 0); p %s`, p, read),
			"\"AB23456789\"\n"},
		{"pwrite pushes only its own range, not the pending buffered write",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); f.write("QQ"); f.pwrite("AB", 5); p %s; f.close; p %s`, p, read, read),
			"\"01234AB789\"\n\"QQ234AB789\"\n"},
		{"pwrite past the end leaves a hole",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); f.pwrite("Z", 12); p %s`, p, read),
			"\"0123456789\\u0000\\u0000Z\"\n"},
		{"pread sees another writer",
			reset + fmt.Sprintf(`f = File.open(%q, "r"); File.write(%q, "xyz3456789"); p f.pread(3, 0)`, p, p),
			"\"xyz\"\n"},
		{"pread sees this stream's own pending write",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); f.write("QQ"); p f.pread(2, 0)`, p),
			"\"QQ\"\n"},
		{"pwrite leaves the position alone",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); f.read(3); f.pwrite("AB", 0); p [f.pos, f.read(2)]`, p),
			"[3, \"34\"]\n"},
		{"a stream created 0444 still pwrites through its grant",
			fmt.Sprintf(`q = %q + "ro"; f = File.new(q, "w", 0444); f.pwrite("AB", 0); p [File.binread(q), format("%%o", File.stat(q).mode & 0o777)]`, p),
			"[\"AB\", \"444\"]\n"},
		// The stream's own descriptor in MRI is unaffected by a later chmod, so
		// it still reads the file; here the path is unreadable and pread falls
		// back to the buffer, which holds the same bytes.
		{"pread on a path made unreadable after open falls back to the buffer",
			reset + fmt.Sprintf(`f = File.open(%q, "r"); File.chmod(0, %q); r = f.pread(3, 2); File.chmod(0o644, %q); p r`, p, p, p),
			"\"234\"\n"},
		{"pread past the end of an unlinked file is still end of file",
			reset + fmt.Sprintf(`f = File.open(%q, "r"); File.unlink(%q); begin; f.pread(1, 10); rescue EOFError => e; p e.message; end`, p, p),
			"\"end of file reached\"\n"},
		{"pwrite on an unlinked path falls back to the buffer",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); File.unlink(%q); p [f.pwrite("AB", 0), f.pread(4, 0)]`, p, p),
			"[2, \"AB23\"]\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}

// TestPreadLengthIsBoundedByTheFile: the length comes from Ruby, and the read
// allocates what the file holds past the offset, never the length asked for.
// MRI 4.0.7 does allocate it — `f.pread(2**40, 7)` on a 10-byte file did not
// answer within 30 s — so this is a deliberate divergence in rbgo's favour, not
// a parity row.
func TestPreadLengthIsBoundedByTheFile(t *testing.T) {
	p := filepath.ToSlash(filepath.Join(t.TempDir(), "f"))
	if err := os.WriteFile(p, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf(`f = File.open(%q, "r"); p f.pread(2**40, 7)`, p)
	if got := eval(t, src); got != "\"789\"\n" {
		t.Errorf("got %q, want %q", got, "\"789\"\n")
	}
}
