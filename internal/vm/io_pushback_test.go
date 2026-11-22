// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestUngetcOnAFileIsNotTheFiles: bytes #ungetc pushes back onto a File are
// read-buffer bytes, never the file's, and the position counts them (#974).
// Every want is MRI 4.0.7's output for the same program (arm64-darwin25,
// rubydiff, 2026-11-22). Before the fix rbgo wrote "0AB23456789" for the
// first row — the write landed after the pushback instead of over it — and
// the seek-to-end row left the pushed-back Z in the file.
func TestUngetcOnAFileIsNotTheFiles(t *testing.T) {
	p := filepath.ToSlash(filepath.Join(t.TempDir(), "f"))
	cases := []struct{ name, act, want string }{
		{"a write lands where the pushback starts",
			`f.getc; f.ungetc("Z"); f.write("AB")`, `"AB23456789\nabc\n"`},
		{"after read(2)", `f.read(2); f.ungetc("Z"); f.write("AB")`, `"0AB3456789\nabc\n"`},
		{"after gets", `f.gets; f.ungetc("Z"); f.write("AB")`, `"0123456789ABbc\n"`},
		{"consumed pushback is not file content either",
			`f.read(3); f.ungetc("Z"); print f.read(2).inspect; f.write("AB")`, `"Z3""0123AB6789\nabc\n"`},
		{"partly consumed pushback", `f.getc; f.ungetc("ZY"); print f.getc, f.pos; f.write("AB")`,
			`Z0"AB23456789\nabc\n"`},
		{"two ungetc stack", `f.getc; f.ungetc("Z"); f.ungetc("Y"); print f.read(3).inspect; f.write("AB")`,
			`"YZ1""01AB456789\nabc\n"`},
		{"a seek drops the pushback", `f.getc; f.ungetc("Z"); f.seek(0, IO::SEEK_END); f.write("x")`,
			`"0123456789\nabc\nx"`},
		{"SEEK_CUR counts from the logical position", `f.getc; f.ungetc("Z"); f.seek(2, IO::SEEK_CUR); print f.read(2).inspect`,
			`"23""0123456789\nabc\n"`},
		{"rewind drops it", `f.getc; f.ungetc("Z"); f.rewind; print f.read(3).inspect`, `"012""0123456789\nabc\n"`},
		{"pos drops it when the position is valid", `f.getc; f.ungetc("Z"); print f.pos, f.getc.inspect`,
			`0"0""0123456789\nabc\n"`},
		{"pos keeps it and is negative when more was pushed back than read",
			`f.getc; f.ungetc("ZZ"); print f.pos, f.getc.inspect`, `-1"Z""0123456789\nabc\n"`},
		{"a negative position writes where the read-ahead stopped",
			`f.getc; f.ungetc("ZZ"); f.write("AB"); print f.pos`, `17"0123456789\nabc\nAB"`},
		{"nothing read: the write lands at 0", `f.ungetc("Z"); print f.pos; f.write("AB")`, `-1"AB23456789\nabc\n"`},
		{"flush drops it", `f.getc; f.ungetc("Z"); f.flush; print f.pos, f.read(2).inspect`, `0"01""0123456789\nabc\n"`},
		{"truncate drops it", `f.getc; f.ungetc("Z"); f.truncate(4); print f.pos, f.read.inspect`, `0"0123""0123"`},
		{"pwrite drops it", `f.getc; f.ungetc("Z"); f.pwrite("Q", 5); print f.pos, f.read(2).inspect`,
			`0"01""01234Q6789\nabc\n"`},
		{"syswrite drops it", `f.getc; f.ungetc("Z"); f.syswrite("AB")`, `"AB23456789\nabc\n"`},
		{"pread reads the file", `f.getc; f.ungetc("Z"); print f.pread(3, 0).inspect`, `"012""0123456789\nabc\n"`},
		{"size is the file's", `f.getc; f.ungetc("Z"); print f.size`, `15"0123456789\nabc\n"`},
		{"closing writes nothing", `f.getc; f.ungetc("Z")`, `"0123456789\nabc\n"`},
		{"sysread refuses buffered data", `f.getc; f.ungetc("Z"); begin; f.sysread(2); rescue IOError => e; print e.message; end`,
			`sysread for buffered IO"0123456789\nabc\n"`},
		{"sysseek refuses buffered data", `f.getc; f.ungetc("Z"); begin; f.sysseek(0); rescue IOError => e; print e.message; end`,
			`sysseek for buffered IO"0123456789\nabc\n"`},
		// A negative position after a direct read: io_bufread / io_getpartial
		// read straight into the string when rbuf is empty, so when io_unread's
		// lseek fails the descriptor is at the cursor, not at end-of-file.
		{"read(n) leaves the descriptor at the cursor", `f.read(1); f.ungetc("XYZ"); f.write("Q"); print f.pos`,
			`2"0Q23456789\nabc\n"`},
		{"readpartial too", `f.readpartial(2); f.ungetc("XYZ"); f.write("Q"); print f.pos`, `3"01Q3456789\nabc\n"`},
		{"read_nonblock too", `f.read_nonblock(1); f.ungetc("XYZ"); f.flush; print f.pos, f.read(2).inspect`,
			`1"12""0123456789\nabc\n"`},
		{"a seek empties rbuf", `f.seek(1); f.ungetc("XYZ"); f.write("Q")`, `"0Q23456789\nabc\n"`},
		{"pos= empties rbuf", `f.pos = 2; f.ungetc("WXYZ"); f.write("Q")`, `"01Q3456789\nabc\n"`},
		{"a write empties rbuf", `f.write("Q"); f.ungetc("XYZ"); f.write("R")`, `"QR23456789\nabc\n"`},
		{"a read past the pushback refills ahead to end-of-file",
			`f.read(1); f.ungetc("XY"); f.read(4); f.ungetc("STUVWXYZ"); f.write("Q"); print f.pos`,
			`16"0123456789\nabc\nQ"`},
		{"readpartial stops at the pushback", `f.read(1); f.ungetc("XY"); print f.readpartial(5)`, `XY"0123456789\nabc\n"`},
		{"read_nonblock stops at the pushback", `f.read(1); f.ungetc("XY"); print f.read_nonblock(5)`, `XY"0123456789\nabc\n"`},
		{"size drops the pushback of a writable stream", `f.read(1); f.ungetc("XYZ"); print [f.size, f.pos, f.read(2)].inspect`,
			`[15, 1, "12"]"0123456789\nabc\n"`},
		{"a pending write is flushed without the pushback", `f.write("AB"); f.getc; f.ungetc("ZY"); f.flush; print f.pos`,
			`1"AB23456789\nabc\n"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := fmt.Sprintf("p_ = %q; File.binwrite(p_, \"0123456789\\nabc\\n\")\n"+
				"File.open(p_, \"r+\") { |f| %s }\nprint File.binread(p_).inspect", p, c.act)
			if got := eval(t, src); got != c.want {
				t.Errorf("got=%s\nwant=%s", got, c.want)
			}
		})
	}
}

// TestReopenDropsThePushback: IO#reopen must not carry pushed-back bytes into
// the stream it rebinds, nor leave the region pointing into content that has
// been replaced. Wants: MRI 4.0.7 (arm64-darwin25, rubydiff, 2026-11-22).
func TestReopenDropsThePushback(t *testing.T) {
	d := filepath.ToSlash(t.TempDir())
	src := fmt.Sprintf(`a = %q + "/a"; b = %q + "/b"
File.binwrite(a, "0123456789"); File.binwrite(b, "abcdefghij")
f = File.open(a, "r+"); f.getc; f.ungetc("Z"); f.reopen(b, "r+"); print f.read(3).inspect; f.write("XY"); f.close
print File.binread(a).inspect, File.binread(b).inspect
File.binwrite(a, "0123456789"); File.binwrite(b, "abcdefghij")
f = File.open(a, "r+"); g = File.open(b, "r+"); g.read(2); g.ungetc("Z"); f.reopen(g); print f.pos, f.read(3).inspect; f.close; g.close
print File.binread(a).inspect, File.binread(b).inspect`, d, d)
	want := `"abc""0123456789""abcXYfghij"1"bcd""0123456789""abcdefghij"`
	if got := eval(t, src); got != want {
		t.Errorf("got=%s\nwant=%s", got, want)
	}
}

// TestStringIOUngetcWritesIntoTheString: StringIO's pushback is not the file
// model above — ext/stringio strio_unget_bytes writes it INTO the string, over
// the bytes just read, and moves the position back. Before #974 it was spliced
// in like a file's: the first row printed 1"0AB23". Every want is MRI 4.0.7's
// output (arm64-darwin25, rubydiff, 2026-11-22); the last column checks the
// caller's String object is the one changed.
func TestStringIOUngetcWritesIntoTheString(t *testing.T) {
	cases := []struct{ name, act, want string }{
		{"over the byte just read", `s.getc; s.ungetc("Z"); print s.pos; s.write("AB")`, `0"AB23"`},
		{"longer than the position: the string grows", `s.getc; s.ungetc("ZZZ"); print s.pos, s.read.inspect`, `0"ZZZ123""ZZZ123"`},
		{"at 0", `s.ungetc("XY"); print s.pos, s.read(3).inspect`, `0"XY0""XY0123"`},
		{"past the end: NUL-filled gap", `s.pos = 7; s.ungetc("Z"); print s.pos`, `6"0123\u0000\u0000Z"`},
		{"past the end and longer than the position", `s.pos = 6; s.ungetc("ZZZZZZZZZ"); print s.pos`, `0"ZZZZZZZZZ"`},
		{"ungetbyte", `s.read; s.ungetbyte(65); print s.pos, s.read.inspect`, `3"A""012A"`},
		{"multibyte", `s.read(2); s.ungetc("é"); print s.pos, s.read.inspect`, `0"é23""é23"`},
		{"codepoint", `s.read(3); s.ungetc(0x41); print s.pos`, `2"01A3"`},
		// strio_ungetc returns before strio_unget_bytes for an empty string, which
		// would otherwise NUL-fill the gap past the end.
		{"empty, past the end: nothing", `s.pos = 6; s.ungetc(""); s.ungetbyte(""); print s.pos`, `6"0123"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := fmt.Sprintf("require \"stringio\"; str = +\"0123\"; s = StringIO.new(str); %s; print str.inspect, str.equal?(s.string)", c.act)
			if got, want := eval(t, src), c.want+"true"; got != want {
				t.Errorf("got=%s\nwant=%s", got, want)
			}
		})
	}
}
