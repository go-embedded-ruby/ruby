// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestChmodKeepsTheSpecialBits pins setuid, setgid and sticky through every
// door that sets a mode, and through the writes that put the owner-write bit
// back for their length (#972). Every want is MRI 4.0.7's output for the same
// program (arm64-darwin25, rubydiff, 2026-11-21). Before the fix, rbgo printed
// the nine permission bits alone: os.Chmod reads Go's own ModeSticky, not
// 0o1000, so `File.chmod(0o1444, q)` set 0o444.
//
// The modes after a write are the KERNEL's: a write by a process without
// CAP_FSETID clears setuid, and setgid when the group may execute; sticky stays.
// So the rows are skipped as root, where nothing is cleared, and on Windows,
// which has none of these bits.
func TestChmodKeepsTheSpecialBits(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("POSIX special bits, cleared on write only for an unprivileged process")
	}
	dir := filepath.ToSlash(t.TempDir())
	cases := []struct{ name, src, want string }{
		{"File.chmod, File#chmod, FileUtils.chmod and chmod_R set them",
			fmt.Sprintf(`require "fileutils"; q = %q + "/m"; File.write(q, "x"); r = []
File.chmod(0o1644, q); r << File.stat(q).mode
File.open(q) { |f| f.chmod(0o4644) }; r << File.stat(q).mode
FileUtils.chmod(0o1600, q); r << File.stat(q).mode
FileUtils.chmod_R(0o2700, q); r << File.stat(q).mode
puts r.map { |m| format("%%o", m & 0o7777) }.join(" ")`, dir),
			"1644 4644 1600 2700\n"},
		{"a pwrite and a close through a read-only mode keep what the kernel keeps",
			fmt.Sprintf(`d = %q
[0o1444, 0o4444, 0o4755, 0o6755].each do |m|
  q = d + "/t" + m.to_s(8)
  File.write(q, "0123456789")
  f = File.open(q, "r+")
  File.chmod(m, q)
  b = File.stat(q).mode & 0o7777
  f.pwrite("AB", 0)
  w = File.stat(q).mode & 0o7777
  f.write("Q")
  f.close
  c = File.stat(q).mode & 0o7777
  printf("%%o: %%o %%o %%o %%p\n", m, b, w, c, File.binread(q))
end`, dir),
			"1444: 1444 1444 1444 \"QB23456789\"\n" +
				"4444: 4444 444 444 \"QB23456789\"\n" +
				"4755: 4755 755 755 \"QB23456789\"\n" +
				"6755: 6755 755 755 \"QB23456789\"\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("got=%q\nwant=%q", got, c.want)
			}
		})
	}
}

// TestPositionalOffsetsAreBounded: the offset of #pwrite and #pread comes from
// Ruby, and no offset may become a Go panic or an allocation the size of the
// offset (#973). Rows marked MRI are MRI 4.0.7's output (arm64-darwin25,
// rubydiff, 2026-11-21); the others are this model's documented limits.
func TestPositionalOffsetsAreBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sparse files and the pwrite(2) errnos are POSIX")
	}
	p := filepath.ToSlash(filepath.Join(t.TempDir(), "f"))
	reset := fmt.Sprintf("File.write(%q, \"0123456789\"); ", p)
	cases := []struct{ name, src, want string }{
		// MRI: Errno::EFBIG on darwin. Linux refuses the same overflow with
		// EINVAL, so the row asserts what both share: the kernel's errno,
		// labelled with the C function, and a file left as it was.
		{"MRI: a pwrite past the largest offset raises the kernel's errno",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); begin; f.pwrite("AB", 2**63-1); rescue SystemCallError => e; p [e.message.include?("@ rb_io_pwrite - "), File.binread(%q)]; end`, p, p),
			"[true, \"0123456789\"]\n"},
		{"MRI: a Bignum offset is RangeError, for pwrite and pread",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); [-> { f.pwrite("A", 2**64) }, -> { f.pread(1, -(2**64)) }].each { |c| begin; c.call; rescue RangeError => e; p e.message; end }`, p),
			"\"bignum too big to convert into 'long long'\"\n\"bignum too big to convert into 'long long'\"\n"},
		{"MRI: a pwrite far past the end reaches the file as a hole",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); p [f.pwrite("Z", 2**27), File.size(%q), f.pread(1, 2**27)]`, p, p),
			"[1, 134217729, \"Z\"]\n"},
		// The rows below fall back to the buffer, which is the only copy once
		// the path is gone, and a hole in it is memory.
		{"an unlinked file: a pread whose length would overflow is bounded by the buffer",
			reset + fmt.Sprintf(`f = File.open(%q, "r"); File.unlink(%q); p f.pread(2**63-1, 1)`, p, p),
			"\"123456789\"\n"},
		{"an unlinked file: a hole larger than the bound is EFBIG, not an allocation",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); File.unlink(%q); begin; f.pwrite("AB", 2**27); rescue Errno::EFBIG => e; p [e.message, f.pread(10, 0)]; end`, p, p),
			fmt.Sprintf("[\"File too large @ rb_io_pwrite - %s\", \"0123456789\"]\n", p)},
		{"an unlinked file: the largest offset is EFBIG, not a slice panic",
			reset + fmt.Sprintf(`f = File.open(%q, "r+"); File.unlink(%q); begin; f.pwrite("AB", 2**63-1); rescue Errno::EFBIG => e; p e.class; end`, p, p),
			"Errno::EFBIG\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}
