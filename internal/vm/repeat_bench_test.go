// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// Array#* and String#* are hot, and #777's guards sit directly on their fast
// path: one division and one extra call (allocValues / allocBytes, which carry a
// deferred recover so a refused allocation becomes NoMemoryError). These measure
// what that costs at the sizes real code uses, so the bound is paid for
// knowingly rather than assumed free.
//
// Array#* also moved from the VM-less arrayOp to vm.arrayTimes, which replaces an
// inline `b.(object.Integer)` with repeatLong -- that is the other thing these
// watch.

func BenchmarkArrayTimesSmall(b *testing.B) {
	benchProgram(b, `a = [1, 2, 3]; i = 0; while i < 1000; a * 4; i += 1; end`)
}

func BenchmarkArrayTimesWide(b *testing.B) {
	benchProgram(b, `a = (1..64).to_a; i = 0; while i < 200; a * 8; i += 1; end`)
}

func BenchmarkArrayTimesZero(b *testing.B) {
	// The short-circuit path: no allocation, no loop.
	benchProgram(b, `a = [1, 2, 3]; i = 0; while i < 1000; a * 0; i += 1; end`)
}

func BenchmarkStringTimesSmall(b *testing.B) {
	benchProgram(b, `s = "ab"; i = 0; while i < 1000; s * 8; i += 1; end`)
}

func BenchmarkStringTimesWide(b *testing.B) {
	benchProgram(b, `s = "abcdefghijklmnopqrstuvwxyz"; i = 0; while i < 200; s * 64; i += 1; end`)
}
