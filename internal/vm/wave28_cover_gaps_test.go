// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestWave28TruncateReadOnly covers IO#truncate's read-only guard. rb_io_truncate
// goes through GetOpenFile and then rb_io_check_writable (io.c), so a stream
// opened for reading answers IOError "not opened for writing" rather than
// attempting the syscall. MRI 4.0.5 prints the same pair.
func TestWave28TruncateReadOnly(t *testing.T) {
	dir := t.TempDir()
	src := `p1 = ` + rq(dir+"/ro.txt") + `
File.write(p1, "abc")
f = File.open(p1, "r")
begin
  f.truncate(1)
rescue => e
  p [e.class.to_s, e.message]
ensure
  f.close
end
`
	if got, want := runFS(t, src), "[\"IOError\", \"not opened for writing\"]\n"; got != want {
		t.Errorf("truncate on a read-only stream: got %q want %q", got, want)
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
