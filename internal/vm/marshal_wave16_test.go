// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestMarshalRestoreAliasWave16 checks that Marshal.restore is a true alias of
// Marshal.load: they share one method record, so Marshal.method(:restore) ==
// Marshal.method(:load) (as in MRI), and restore round-trips like load.
func TestMarshalRestoreAliasWave16(t *testing.T) {
	if got := runSrc(t, `p Marshal.method(:restore) == Marshal.method(:load)`); got != "true" {
		t.Fatalf("restore/load method equality: %q", got)
	}
	if got := runSrc(t, `p Marshal.restore(Marshal.dump([1, "x", :s]))`); got != `[1, "x", :s]` {
		t.Fatalf("restore round-trip: %q", got)
	}
}
