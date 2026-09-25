// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// truncFrames shortens a per-frame tracking stack to the pushing frame's own
// depth. Both directions are load-bearing, and each failed in the field before
// the helper existed: asking for a shorter stack must pop (the ordinary case),
// and asking for a LONGER one must leave the stack alone rather than slice back
// up into entries a peer goroutine already abandoned (issue #615, where the
// arithmetic form reached frameNames[:-1] and killed the process).
func TestTruncFrames(t *testing.T) {
	t.Run("shortens", func(t *testing.T) {
		got := truncFrames([]string{"a", "b", "c"}, 1)
		if len(got) != 1 || got[0] != "a" {
			t.Fatalf("truncFrames([a b c], 1) = %v, want [a]", got)
		}
	})
	t.Run("leaves an equal length alone", func(t *testing.T) {
		got := truncFrames([]string{"a", "b"}, 2)
		if len(got) != 2 {
			t.Fatalf("truncFrames([a b], 2) = %v, want [a b]", got)
		}
	})
	t.Run("does not re-extend a shorter stack", func(t *testing.T) {
		// The capacity is deliberately larger than the length: a bare slice
		// expression s[:5] here would SUCCEED and hand back the stale entries,
		// which is the failure mode this guard exists for.
		s := make([]string, 0, 8)
		s = append(s, "gone", "also gone")
		s = s[:0]
		got := truncFrames(s, 5)
		if len(got) != 0 {
			t.Fatalf("truncFrames(len 0 cap 8, 5) = %v, want empty", got)
		}
	})
	t.Run("never slices negative", func(t *testing.T) {
		// frameNamesDepth-1 with an emptied stack: the arithmetic the helper
		// replaced evaluated s[:-1] here and panicked.
		got := truncFrames([]int{}, -1)
		if len(got) != 0 {
			t.Fatalf("truncFrames([], -1) = %v, want empty", got)
		}
	})
}
