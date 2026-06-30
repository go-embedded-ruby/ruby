// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import (
	"path/filepath"
	"testing"
)

// TestPStoreFlockFileUnix covers the Unix flockFile: a successful exclusive lock +
// unlock, a successful shared lock, the OpenFile error path for an unopenable path,
// and the flock-failure branch via the flockSyscall seam. (The Windows flockFile is
// a no-op covered by its own test.)
func TestPStoreFlockFileUnix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.pstore")

	unlock, err := flockFile(path, false) // LOCK_EX
	if err != nil {
		t.Fatalf("flockFile EX: %v", err)
	}
	unlock()

	unlock, err = flockFile(path, true) // LOCK_SH
	if err != nil {
		t.Fatalf("flockFile SH: %v", err)
	}
	unlock()

	// An unopenable path (a missing parent directory) fails at OpenFile.
	if _, err := flockFile(filepath.Join(dir, "nope", "x"), false); err == nil {
		t.Fatal("flockFile missing dir: want error, got nil")
	}

	// flock itself failing (injected) closes the fd and surfaces the error.
	restore := flockSyscall
	flockSyscall = func(int, int) error { return errInjected }
	if _, err := flockFile(path, false); err != errInjected {
		t.Fatalf("flockFile flock-fail = %v, want errInjected", err)
	}
	flockSyscall = restore
}
