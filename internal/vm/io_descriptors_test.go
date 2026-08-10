// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestIODescriptorsStringIO covers the byte/char read protocol, gets limit/chomp,
// #lineno, half-close and sysread/syswrite on StringIO — asserted against MRI
// Ruby 4.0.5. StringIO exercises the buffer-backed path shared with File and the
// standard streams.
func TestIODescriptorsStringIO(t *testing.T) {
	cases := []struct{ src, want string }{
		// getbyte / readbyte.
		{`require "stringio"; s = StringIO.new("AB"); p [s.getbyte, s.getbyte, s.getbyte]`, "[65, 66, nil]\n"},
		{`require "stringio"; s = StringIO.new("A"); s.getbyte; p s.readbyte rescue p :eof`, ":eof\n"},
		{`require "stringio"; s = StringIO.new("ab"); p s.readbyte`, "97\n"},
		// readchar.
		{`require "stringio"; p StringIO.new("é").readchar`, "\"é\"\n"},
		// ungetbyte / ungetc.
		{`require "stringio"; s = StringIO.new("AB"); s.getbyte; s.ungetbyte(65); p s.read`, "\"AB\"\n"},
		{`require "stringio"; s = StringIO.new("Z"); s.getbyte; s.ungetbyte("AB"); p s.read`, "\"AB\"\n"},
		{`require "stringio"; s = StringIO.new("AB"); c = s.getc; s.ungetc(c); p s.read`, "\"AB\"\n"},
		{`require "stringio"; s = StringIO.new("ab"); s.ungetc("Z"); p s.read`, "\"Zab\"\n"},
		{`require "stringio"; s = StringIO.new("ab"); s.getc; s.ungetc(65); p s.read`, "\"Ab\"\n"},
		{`require "stringio"; s = StringIO.new("ab"); p s.ungetbyte(nil)`, "nil\n"},
		{`require "stringio"; s = StringIO.new("ab"); p s.ungetc(nil)`, "nil\n"},
		{`require "stringio"; s = StringIO.new("ab"); s.ungetbyte(""); p s.read`, "\"ab\"\n"},
		// each_byte / each.
		{`require "stringio"; s = StringIO.new("ab"); a = []; p s.each_byte { |b| a << b }.class; p a`, "StringIO\n[97, 98]\n"},
		{`require "stringio"; s = StringIO.new("x\ny\n"); a = []; p s.each { |l| a << l }.class; p a`, "StringIO\n[\"x\\n\", \"y\\n\"]\n"},
		// gets limit / chomp.
		{`require "stringio"; p StringIO.new("hello\nworld").gets(3)`, "\"hel\"\n"},
		{`require "stringio"; p StringIO.new("hello\nworld").gets("\n", 3)`, "\"hel\"\n"},
		{`require "stringio"; p StringIO.new("hello\nx").gets(chomp: true)`, "\"hello\"\n"},
		{`require "stringio"; p StringIO.new("a\r\nb").gets(chomp: true)`, "\"a\"\n"},
		{`require "stringio"; p StringIO.new("a;b;").gets(";", chomp: true)`, "\"a\"\n"},
		{`require "stringio"; p StringIO.new("abc").gets(nil, 2)`, "\"ab\"\n"},
		{`require "stringio"; p StringIO.new("abc").gets(chomp: true)`, "\"abc\"\n"},
		// a single gets with a 0 limit yields "" (it does not loop).
		{`require "stringio"; p StringIO.new("ab").gets(0)`, "\"\"\n"},
		{`require "stringio"; s = StringIO.new("a\nb\nc\n"); s.readlines; p s.lineno`, "3\n"},
		{`require "stringio"; s = StringIO.new("a\nb\n"); p s.readlines(chomp: true)`, "[\"a\", \"b\"]\n"},
		// lineno.
		{`require "stringio"; s = StringIO.new("x\ny\n"); s.gets; s.gets; p s.lineno`, "2\n"},
		{`require "stringio"; s = StringIO.new("x\ny\n"); s.lineno = 5; p s.lineno`, "5\n"},
		{`require "stringio"; p StringIO.new("x").lineno`, "0\n"},
		// sysread / syswrite.
		{`require "stringio"; p StringIO.new("hello").sysread(3)`, "\"hel\"\n"},
		{`require "stringio"; s = StringIO.new("hello"); b = "xxxx"; r = s.sysread(3, b); p [r, b, r.equal?(b)]`, "[\"hel\", \"hel\", true]\n"},
		{`require "stringio"; p StringIO.new("ab").sysread(0)`, "\"\"\n"},
		{`require "stringio"; s = StringIO.new; p s.syswrite("abc"); p s.string`, "3\n\"abc\"\n"},
		// half-close.
		{`require "stringio"; s = StringIO.new("ab"); s.close_read; p [s.closed_read?, s.closed?]`, "[true, false]\n"},
		{`require "stringio"; s = StringIO.new("ab"); s.close_write; p [s.closed_write?, s.closed?]`, "[true, false]\n"},
		{`require "stringio"; s = StringIO.new("ab"); s.close_read; s.close_write; p s.closed?`, "true\n"},
		{`require "stringio"; s = StringIO.new("ab"); s.close_write; s.close_read; p s.closed?`, "true\n"},
		{`require "stringio"; s = StringIO.new("ab"); s.close_read; s.write("x"); p s.string`, "\"xb\"\n"},
		{`require "stringio"; p [StringIO.new("").closed_read?, StringIO.new("").closed_write?]`, "[false, false]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestIODescriptorsFile covers the IO-only positioned methods (pread/pwrite/
// sysseek) and the descriptor accessors on a File, using a per-test temp dir so
// no real machine files are touched.
func TestIODescriptorsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.ToSlash(filepath.Join(dir, "io.txt"))
	q := func(s string) string { return `"` + s + `"` }
	cases := []struct{ src, want string }{
		// pread does not disturb the cursor.
		{fmt.Sprintf(`File.write(%s, "hello world"); File.open(%s, "r+") { |f| x = f.pread(5, 0); y = f.pread(5, 6); z = f.read(2); p [x, y, z] }`, q(path), q(path)),
			"[\"hello\", \"world\", \"he\"]\n"},
		// pread into a supplied buffer.
		{fmt.Sprintf(`File.write(%s, "abcdef"); File.open(%s) { |f| b = "  "; r = f.pread(3, 0, b); p [r, b, r.equal?(b)] }`, q(path), q(path)),
			"[\"abc\", \"abc\", true]\n"},
		// zero-length pread is "" without hitting EOF.
		{fmt.Sprintf(`File.write(%s, "abc"); File.open(%s) { |f| p f.pread(0, 0) }`, q(path), q(path)), "\"\"\n"},
		// pwrite writes at an offset, returns the count, leaves the cursor alone.
		{fmt.Sprintf(`File.write(%s, "0123456789"); File.open(%s, "r+") { |f| f.read(3); p f.pwrite("XX", 8); p f.read }; p File.read(%s)`, q(path), q(path), q(path)),
			"2\n\"34567XX\"\n\"01234567XX\"\n"},
		// pwrite extending past the current end grows the file.
		{fmt.Sprintf(`File.write(%s, "ab"); File.open(%s, "r+") { |f| f.pwrite("Z", 5) }; p File.read(%s).bytes`, q(path), q(path), q(path)),
			"[97, 98, 0, 0, 0, 90]\n"},
		// sysseek returns the resulting absolute position for each whence.
		{fmt.Sprintf(`File.write(%s, "hello"); File.open(%s) { |f| a = [f.sysseek(1)]; a << f.sysseek(2, 1); a << f.sysseek(-1, 2); p a }`, q(path), q(path)),
			"[1, 3, 4]\n"},
		// descriptor accessors.
		{fmt.Sprintf(`File.write(%s, "x"); File.open(%s) { |f| p [f.binmode?, f.autoclose?, (f.autoclose = false), f.fdatasync] }`, q(path), q(path)),
			"[false, true, false, 0]\n"},
		// pread/pwrite are IO-only: StringIO does not answer them.
		{`require "stringio"; p StringIO.new("ab").respond_to?(:pread)`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestIODescriptorsErrors covers the error branches (EOFError, IOError for a
// closed / half-closed stream, ArgumentError / Errno::EINVAL / TypeError /
// FrozenError) at MRI-exact classes and messages.
func TestIODescriptorsErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.ToSlash(filepath.Join(dir, "e.txt"))
	q := func(s string) string { return `"` + s + `"` }
	errs := []struct{ src, want string }{
		// EOFError at end of input.
		{`require "stringio"; StringIO.new("").readbyte`, "end of file reached"},
		{`require "stringio"; StringIO.new("").readchar`, "end of file reached"},
		{`require "stringio"; StringIO.new("").sysread(1)`, "end of file reached"},
		// sysread argument validation.
		{`require "stringio"; StringIO.new("ab").sysread(-1)`, "negative length"},
		// a 0 limit on a line-iterating read is rejected (it would never advance).
		{`require "stringio"; StringIO.new("ab").readlines(0)`, "invalid limit: 0 for readlines"},
		{`require "stringio"; StringIO.new("ab").each_line(0) { |x| }`, "invalid limit: 0 for each_line"},
		{`require "stringio"; StringIO.new("ab").each(0) { |x| }`, "invalid limit: 0 for each_line"},
		// unget type errors.
		{`require "stringio"; StringIO.new("ab").ungetbyte([])`, "into Integer"},
		{`require "stringio"; StringIO.new("ab").ungetc([])`, "into String"},
		// half-close then read / write.
		{`require "stringio"; s = StringIO.new("ab"); s.close_read; s.read`, "not opened for reading"},
		{`require "stringio"; s = StringIO.new("ab"); s.close_read; s.gets`, "not opened for reading"},
		{`require "stringio"; s = StringIO.new("ab"); s.close_read; s.getbyte`, "not opened for reading"},
		{`require "stringio"; s = StringIO.new("ab"); s.close_write; s.write("x")`, "not opened for writing"},
		{`require "stringio"; s = StringIO.new("ab"); s.close_write; s.syswrite("x")`, "not opened for writing"},
		// fully closed stream.
		{`require "stringio"; s = StringIO.new("ab"); s.close; s.getc`, "closed stream"},
		{`require "stringio"; s = StringIO.new("ab"); s.close; s.read`, "closed stream"},
		// pread / pwrite validation on a File.
		{fmt.Sprintf(`File.write(%s, "abc"); File.open(%s) { |f| f.pread(-1, 0) }`, q(path), q(path)), "negative string size"},
		{fmt.Sprintf(`File.write(%s, "abc"); File.open(%s) { |f| f.pread(2, -1) }`, q(path), q(path)), "Invalid argument"},
		{fmt.Sprintf(`File.write(%s, "abc"); File.open(%s) { |f| f.pread(2, 9) }`, q(path), q(path)), "end of file reached"},
		{fmt.Sprintf(`File.write(%s, "abc"); File.open(%s, "r+") { |f| f.pwrite("x", -1) }`, q(path), q(path)), "Invalid argument"},
		// pread into a frozen buffer.
		{fmt.Sprintf(`File.write(%s, "abc"); File.open(%s) { |f| f.pread(2, 0, "xx".freeze) }`, q(path), q(path)), "frozen String"},
		// class-method line iterators reject a 0 limit too.
		{fmt.Sprintf(`File.write(%s, "a\nb\n"); IO.foreach(%s, 0) { |x| }`, q(path), q(path)), "invalid limit: 0 for foreach"},
		{fmt.Sprintf(`File.write(%s, "a\nb\n"); IO.readlines(%s, 0)`, q(path), q(path)), "invalid limit: 0 for readlines"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
