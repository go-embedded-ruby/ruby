// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
)

// TestCloseHalfReleasesTheFlock: #close_read on a read-only File and
// #close_write on a write-only one close the whole stream, and with it the
// descriptor File#flock holds, as close(2) does in MRI (#979). Before, the
// lock outlived the stream: every LOCK_NB below answered false, and a blocking
// flock on the file hung. The want is MRI 4.0.7's output for the same program
// (arm64-darwin25, rubydiff, 2026-11-22). Every lock is LOCK_NB so that a
// regression fails here rather than hangs.
func TestCloseHalfReleasesTheFlock(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOARCH == "wasm" || runtime.GOOS == "plan9" {
		t.Skip("no flock(2) on this platform")
	}
	d := filepath.ToSlash(t.TempDir())
	src := fmt.Sprintf(`path = File.join(%q, "f")
File.write(path, "abc")
r = File.open(path, "r"); r.flock(File::LOCK_EX); r.close_read
a = File.open(path, "r") { |g| g.flock(File::LOCK_EX | File::LOCK_NB) }
w = File.open(path, "w"); c = w.flock(File::LOCK_EX | File::LOCK_NB); w.close_write
b = File.open(path, "r") { |g| g.flock(File::LOCK_EX | File::LOCK_NB) }
p [a, c, b, r.closed?, w.closed?]
`, d)
	if got, want := eval(t, src), "[0, 0, 0, true, true]\n"; got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}
