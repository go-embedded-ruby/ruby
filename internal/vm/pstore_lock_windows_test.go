// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

import "testing"

// TestPStoreFlockFileWindows covers the Windows flockFile: it is a pure no-op that
// opens nothing, never errors, and returns a releasable (empty) closure — so the
// atomic-rename Store is never blocked by a still-open store handle.
func TestPStoreFlockFileWindows(t *testing.T) {
	unlock, err := flockFile(`C:\does\not\matter\store.pstore`, false)
	if err != nil {
		t.Fatalf("flockFile (windows no-op) EX: %v", err)
	}
	unlock()

	unlock, err = flockFile(`C:\does\not\matter\store.pstore`, true)
	if err != nil {
		t.Fatalf("flockFile (windows no-op) SH: %v", err)
	}
	unlock()
}
