// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"testing"
)

// ioScratchDir is t.TempDir() for tests that deliberately leave a File open.
//
// Several cases here have to: File.open(fh.fileno) needs fh alive to have a
// descriptor to reopen, and the permission cases hold a handle across a chmod.
// rbgo closes no IO at interpreter shutdown, where MRI closes every one — issue
// #643 — so those handles are still open when the test ends. On POSIX nothing
// notices, because unlinking an open file is allowed. On Windows the open
// handle makes t.TempDir()'s RemoveAll fail, and the test then reports a
// cleanup error instead of its assertion, which hides what it was measuring.
//
// So: remove the directory best-effort, and let the VM gap be tracked in #643
// rather than re-reported by every test that trips over it. One rule, one
// place — the previous shape had this explanation duplicated per helper and
// they would have drifted.
func ioScratchDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rbgo-ioscratch-")
	if err != nil {
		t.Fatalf("scratch: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
