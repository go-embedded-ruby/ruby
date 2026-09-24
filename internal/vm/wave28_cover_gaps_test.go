// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestWave28TruncateReadOnly covers StringIO#truncate's read-only guard — the
// one inside defStringIORead, which is a different implementation from File's.
// rb_io_truncate goes through GetOpenFile then rb_io_check_writable (io.c), so a
// stream opened for reading answers IOError "not opened for writing" rather than
// attempting the write. MRI 4.0.5 prints the same pair, and truncates to "a"
// when the stream IS writable.
func TestWave28TruncateReadOnly(t *testing.T) {
	src := `require "stringio"
s = StringIO.new("abc", "r")
begin
  s.truncate(1)
rescue => e
  p [e.class.to_s, e.message]
end
w = StringIO.new("abc")
w.truncate(1)
p w.string
`
	want := "[\"IOError\", \"not opened for writing\"]\n\"a\"\n"
	if got := runFS(t, src); got != want {
		t.Errorf("StringIO#truncate: got %q want %q", got, want)
	}
}

// TestWave28SelectNaNTimeout covers IO.select's timeout validation: a NaN
// interval is a RangeError before any waiting happens, which is what
// rb_time_interval's NaN check yields. Verified against MRI 4.0.5.
func TestWave28SelectNaNTimeout(t *testing.T) {
	src := `r, w = IO.pipe
begin
  IO.select([r], nil, nil, Float::NAN)
rescue => e
  p [e.class.to_s, e.message]
ensure
  r.close; w.close
end
`
	if got, want := runFS(t, src), "[\"RangeError\", \"NaN out of Time range\"]\n"; got != want {
		t.Errorf("IO.select with a NaN timeout: got %q want %q", got, want)
	}
}

// TestWave28TruncateClosedStream covers the other guard in the same truncate:
// rb_io_truncate calls GetOpenFile BEFORE rb_io_check_writable, so a CLOSED
// stream answers "closed stream" whichever mode it had. The guard excludes
// StringIO, which tolerates a closed stream — hence a File here, not a StringIO.
// MRI 4.0.5 prints the same pair.
func TestWave28TruncateClosedStream(t *testing.T) {
	dir := t.TempDir()
	src := `p1 = ` + rq(dir+"/closed.txt") + `
f = File.open(p1, "w")
f.write("abc")
f.close
begin
  f.truncate(0)
rescue => e
  p [e.class.to_s, e.message]
end
`
	if got, want := runFS(t, src), "[\"IOError\", \"closed stream\"]\n"; got != want {
		t.Errorf("truncate on a closed stream: got %q want %q", got, want)
	}
}

// TestWave28TruncateFileOnDisk covers the ftruncate half of IO#truncate: the
// file on disk is that size at once, which is what File.size and a second
// stream opened on the same path see straight afterwards. MRI 4.0.5 prints 3.
//
// The second case documents a DIVERGENCE rather than asserting a contract.
// MRI's rb_io_truncate is ftruncate(2) on the descriptor, so it succeeds after
// the path is unlinked; rbgo's IOObj is buffer-backed with a synthetic fd, so
// it truncates by path and answers ENOENT. That is the same root cause as
// File#stat on an unlinked file, and it is tracked in issue #635 — recorded
// here so the branch is exercised and the difference is visible at the point
// where someone would otherwise read rbgo's answer as correct.
func TestWave28TruncateFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	src := `p1 = ` + rq(dir+"/sz.txt") + `
File.write(p1, "abcdef")
f = File.open(p1, "r+")
f.truncate(3)
p File.size(p1)
f.close
p2 = ` + rq(dir+"/gone.txt") + `
File.write(p2, "abcdef")
g = File.open(p2, "r+")
File.delete(p2)
begin
  g.truncate(3)
  p :ok
rescue => e
  p e.class.to_s
end
`
	// MRI prints 3 then :ok. rbgo prints 3 then the errno, per #635.
	if got, want := runFS(t, src), "3\n\"Errno::ENOENT\"\n"; got != want {
		t.Errorf("truncate on disk: got %q want %q", got, want)
	}
}
