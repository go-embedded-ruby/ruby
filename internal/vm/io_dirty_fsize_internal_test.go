// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin || linux

package vm

import (
	"path/filepath"
	"syscall"
	"testing"
)

// TestAFlushPastTheFileSizeLimitRaises: the file opened, and the write itself
// was refused. With RLIMIT_FSIZE below the span's offset, write(2) answers
// EFBIG (the Go runtime ignores SIGXFSZ, so the process survives it). This is
// TestAFlushThatCannotWriteRaises for platforms with no /dev/full: the error
// must be raised, not dropped while Close reports success.
func TestAFlushPastTheFileSizeLimitRaises(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	o := &IOObj{isStr: true, writable: true, dirty: []ioSpan{{8, 12}}, path: path, buf: []byte("0123456789ab")}
	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Skipf("getrlimit: %v", err)
	}
	limited := old
	limited.Cur = 4
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		t.Skipf("setrlimit: %v", err)
	}
	got := catchRaise(func() { ioFlush(o) })
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Fatalf("restoring RLIMIT_FSIZE: %v", err)
	}
	if got != "Errno::EFBIG" {
		t.Errorf("a flush past RLIMIT_FSIZE raised %q, want Errno::EFBIG", got)
	}
}
