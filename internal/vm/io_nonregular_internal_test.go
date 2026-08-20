// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestOpeningSomethingThatIsNotARegularFile covers the one path that cannot be
// read whole.
//
// A File stream here is backed by the file's bytes in memory — see IOObj, which
// says why — so opening used to read the whole path. Some paths do not end:
// File.open("/dev/zero") allocated 83 GB and killed a CI runner before the open
// returned, which is one of the three reasons the ruby/spec ratchet lane was red
// for three days.
//
// A device, a fifo or a socket now opens with an empty buffer, so the position
// arithmetic works — which is what core/io/seek_spec.rb asks of /dev/zero — and
// a read sees end-of-file rather than the machine going away.
func TestOpeningSomethingThatIsNotARegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("/dev/zero and fifos are not what Windows calls them")
	}
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skipf("/dev/zero is not here: %v", err)
	}

	// The case that used to take the machine: open, seek far past any real
	// file, read the position back.
	got := eval(t, `f = File.open("/dev/zero"); f.seek(2**33); p f.pos; f.close`)
	if want := "8589934592\n"; got != want {
		t.Errorf("seeking on /dev/zero gave %q, want %q", got, want)
	}
	// Reading sees end-of-file rather than an endless stream of zeros. That is
	// a limit of a buffered IO, and the test says so rather than leaving the
	// next reader to discover it.
	if got := eval(t, `f = File.open("/dev/zero"); p f.read; f.close`); got != "\"\"\n" {
		t.Errorf("reading /dev/zero gave %q, want the empty string", got)
	}

	// A fifo has the same shape and would block a reader forever.
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := mkfifoForTest(fifo); err != nil {
		t.Skipf("cannot make a fifo here: %v", err)
	}
	if got := eval(t, `f = File.open("`+fifo+`"); p f.pos; f.close`); got != "0\n" {
		t.Errorf("opening a fifo gave %q, want 0", got)
	}

	// A regular file is untouched by any of this: it is still read whole.
	reg := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(reg, []byte("abc\ndef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, `p File.open("`+reg+`").read`); got != "\"abc\\ndef\\n\"\n" {
		t.Errorf("reading a regular file gave %q", got)
	}
	// And a path that is not there still raises, rather than opening empty.
	missing := filepath.Join(dir, "nope.txt")
	got = eval(t, `begin
  File.open("`+missing+`")
rescue Errno::ENOENT => e
  p e.class
end`)
	if got != "Errno::ENOENT\n" {
		t.Errorf("opening a missing path gave %q, want Errno::ENOENT", got)
	}
}
