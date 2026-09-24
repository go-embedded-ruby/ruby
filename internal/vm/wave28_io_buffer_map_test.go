// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWave28IOBufferMap pins IO::Buffer.map against io_buffer.c:
//
//   - io_buffer_map / rb_io_buffer_map: rb_check_arity(argc, 1, 4), and with no
//     size the whole file — a file of zero length has no mapping to make;
//   - io_buffer_extract_size / _offset / _flags: three different refusals for
//     three different numbers ("not an Integer" for a size, "no implicit
//     conversion from X" for an offset, "Flags can't be negative!" for flags);
//   - io_buffer_map_file: MAP_SHARED with PROT_READ|PROT_WRITE unless PRIVATE or
//     READONLY was asked for, so a shared writable mapping of a stream that was
//     not opened for writing is the EACCES mmap(2) gives. A non-private mapping
//     is marked external and shared; a private one is neither.
//
// Every want string is the raw stdout of the same source under MRI ruby 4.0.5,
// compared byte for byte against a fixture file holding "abc\xC3\xA2def\n".
func TestWave28IOBufferMap(t *testing.T) {
	cases := []struct{ src, want string }{
		{`b = IO::Buffer.map(f); [b.size, b.get_string]`, "[9, \"abc\\xC3\\xA2def\\n\"]\n"},
		{`b = IO::Buffer.map(f); [b.internal?, b.mapped?, b.external?, b.empty?, b.null?, b.shared?, b.private?, b.readonly?, b.locked?, b.valid?]`,
			"[false, true, true, false, false, true, false, false, false, true]\n"},
		{`b = IO::Buffer.map(f, 4); [b.size, b.get_string]`, "[4, \"abc\\xC3\"]\n"},
		{`IO::Buffer.map(f, nil).size`, "9\n"},
		{`b = IO::Buffer.map(f, 4, 0); [b.size, b.get_string]`, "[4, \"abc\\xC3\"]\n"},

		// The three number refusals, each with its own wording.
		{`IO::Buffer.map(f, 0)`, "[ArgumentError, \"Size can't be zero!\"]\n"},
		{`IO::Buffer.map(f, -1)`, "[ArgumentError, \"Size can't be negative!\"]\n"},
		{`IO::Buffer.map(f, "10")`, "[TypeError, \"not an Integer\"]\n"},
		{`IO::Buffer.map(f, 10.0)`, "[TypeError, \"not an Integer\"]\n"},
		{`IO::Buffer.map(f, 8192)`, "[ArgumentError, \"Size can't be larger than file size!\"]\n"},
		{`IO::Buffer.map(f, 4, "4096")`, "[TypeError, \"no implicit conversion from string\"]\n"},
		{`IO::Buffer.map(f, 4, nil)`, "[TypeError, \"no implicit conversion from nil\"]\n"},
		{`IO::Buffer.map(f, 4, -1)`, "[ArgumentError, \"Offset can't be negative!\"]\n"},
		{`IO::Buffer.map(f, 8, 4)`, "[ArgumentError, \"Offset too large!\"]\n"},
		{`IO::Buffer.map`, "[ArgumentError, \"wrong number of arguments (given 0, expected 1..4)\"]\n"},
		{`IO::Buffer.map(f, 1, 0, 0, 0)`, "[ArgumentError, \"wrong number of arguments (given 5, expected 1..4)\"]\n"},

		// READONLY maps without asking for write access, and refuses writes.
		{`b = IO::Buffer.map(f, nil, 0, IO::Buffer::READONLY); [b.readonly?, b.get_string]`, "[true, \"abc\\xC3\\xA2def\\n\"]\n"},
		{`b = IO::Buffer.map(f, nil, 0, IO::Buffer::READONLY); b.set_string("test")`,
			"[IO::Buffer::AccessError, \"Buffer is not writable!\"]\n"},

		// PRIVATE is a copy: neither external nor shared, and the file never sees
		// the writes.
		{`b = IO::Buffer.map(f, nil, 0, IO::Buffer::PRIVATE); [b.private?, b.shared?, b.external?, b.mapped?, b.internal?]`,
			"[true, false, false, true, false]\n"},
		{`b = IO::Buffer.map(f, nil, 0, IO::Buffer::PRIVATE); b.set_string("test12345"); [b.get_string, f.read]`,
			"[\"test12345\", \"abc\\xC3\\xA2def\\n\"]\n"},

		// The mapping outlives the stream: mmap keeps the pages, close(2) does not
		// take them back.
		{`b = IO::Buffer.map(f); f.close; b.get_string`, "\"abc\\xC3\\xA2def\\n\"\n"},
	}
	for _, c := range cases {
		dir := bufferMapScratch(t)
		src := "f = File.open(" + rubyString(filepath.Join(dir, "read_text.txt")) + ", \"rb+\")\n" +
			"begin\n  p(begin\n" + c.src + "\nend)\nrescue => e\n  p [e.class, e.message]\nend\n"
		if got := eval(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestWave28IOBufferMapModeGuards covers the two file states that decide whether
// a mapping can be made at all: a stream not opened for writing cannot carry the
// default shared writable mapping (EACCES from mmap, which is the SystemCallError
// the spec names) but can carry a READONLY one; and a file of zero length has
// nothing to map. MRI 4.0.5.
func TestWave28IOBufferMapModeGuards(t *testing.T) {
	dir := bufferMapScratch(t)
	ro := rubyString(filepath.Join(dir, "read_text.txt"))
	empty := rubyString(filepath.Join(dir, "empty.txt"))
	cases := []struct{ src, want string }{
		{`f = File.open(` + ro + `, "rb"); IO::Buffer.map(f)`,
			"[Errno::EACCES, \"Permission denied - io_buffer_map_file:mmap\"]\n"},
		{`f = File.open(` + ro + `, "rb"); b = IO::Buffer.map(f, nil, 0, IO::Buffer::READONLY); [b.readonly?, b.get_string]`,
			"[true, \"abc\\xC3\\xA2def\\n\"]\n"},
		{`f = File.open(` + ro + `, "rb"); b = IO::Buffer.map(f, nil, 0, IO::Buffer::PRIVATE); b.set_string("test12345"); b.get_string`,
			"\"test12345\"\n"},
		{`f = File.open(` + empty + `, "wb+"); IO::Buffer.map(f)`,
			"[ArgumentError, \"Invalid negative or zero file size!\"]\n"},
		{`f = File.open(` + ro + `, "rb"); f.close; IO::Buffer.map(f)`,
			"[IOError, \"closed stream\"]\n"},
	}
	for _, c := range cases {
		src := "begin\n  p(begin\n" + c.src + "\nend)\nrescue => e\n  p [e.class, e.message]\nend\n"
		if got := eval(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestWave28IOBufferNewFlags pins IO::Buffer.new's allocation flags —
// rb_io_buffer_initialize + io_buffer_initialize + io_flags_for_size
// (io_buffer.c). With no flags the SIZE chooses: below a page the buffer is
// allocated internally, from a page upwards it is mapped. With flags, INTERNAL or
// MAPPED has to be among them because they are the only two ways to obtain the
// memory, and every other bit is simply recorded — INTERNAL|SHARED is an
// internally allocated buffer that also reports #shared?. A size of zero
// allocates nothing and the flags are never consulted at all. MRI 4.0.5.
func TestWave28IOBufferNewFlags(t *testing.T) {
	cases := []struct{ src, want string }{
		{`b = IO::Buffer.new(IO::Buffer::PAGE_SIZE); [b.internal?, b.mapped?, b.external?]`, "[false, true, false]\n"},
		{`b = IO::Buffer.new(8); [b.internal?, b.mapped?, b.external?]`, "[true, false, false]\n"},
		{`b = IO::Buffer.new; [b.size, b.internal?, b.mapped?]`, "[65536, false, true]\n"},
		{`b = IO::Buffer.new(0); [b.null?, b.internal?, b.mapped?, b.empty?, b.size]`, "[true, false, false, true, 0]\n"},
		{`b = IO::Buffer.new(0, 0xffff); [b.null?, b.empty?, b.internal?, b.mapped?, b.external?, b.shared?, b.private?, b.readonly?, b.locked?, b.valid?, b.size]`,
			"[true, true, false, false, false, false, false, false, false, true, 0]\n"},
		{`b = IO::Buffer.new(8, IO::Buffer::MAPPED); [b.mapped?, b.internal?]`, "[true, false]\n"},
		{`b = IO::Buffer.new(10, IO::Buffer::INTERNAL | IO::Buffer::SHARED | IO::Buffer::READONLY); [b.internal?, b.shared?, b.readonly?, b.mapped?, b.external?, b.private?]`,
			"[true, true, true, false, false, false]\n"},
		{`b = IO::Buffer.new(10, IO::Buffer::INTERNAL | IO::Buffer::PRIVATE); [b.internal?, b.private?]`, "[true, true]\n"},
		{`b = IO::Buffer.new(20000, IO::Buffer::MAPPED | IO::Buffer::EXTERNAL); [b.mapped?, b.external?, b.internal?]`, "[true, true, false]\n"},
		{`IO::Buffer.new(8, 0)`, "[IO::Buffer::AllocationError, \"Could not allocate buffer!\"]\n"},
		{`IO::Buffer.new(8, -1)`, "[ArgumentError, \"Flags can't be negative!\"]\n"},
		{`IO::Buffer.new(8, 0.0)`, "[TypeError, \"not an Integer\"]\n"},
		{`IO::Buffer.new(-1)`, "[ArgumentError, \"Size can't be negative!\"]\n"},
		{`IO::Buffer.new(1, 2, 3)`, "[ArgumentError, \"wrong number of arguments (given 3, expected 0..2)\"]\n"},
	}
	for _, c := range cases {
		src := "begin\n  p(begin\n" + c.src + "\nend)\nrescue => e\n  p [e.class, e.message]\nend\n"
		if got := eval(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// bufferMapScratch builds the fixture the map cases assume: "read_text.txt" with
// the nine bytes "abc\xC3\xA2def\n", and an empty "empty.txt".
func bufferMapScratch(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "read_text.txt"), []byte("abc\xc3\xa2def\n"), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.txt"), nil, 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return dir
}
