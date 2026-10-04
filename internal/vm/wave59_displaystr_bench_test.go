// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// BenchmarkDisplayStrRoutes puts a number on what making #puts dispatch #to_s
// costs PER CALL, by timing the two routes side by side on the same receiver.
//
// It is a decomposition on purpose. A whole-program `puts` loop could not
// answer the question on this machine: at load ~15, five runs of 200000 puts
// gave main 0.61-0.77s and the branch 0.64-0.71s -- distributions that overlap
// completely, so neither a cost nor a saving was measurable. The per-call delta
// is small enough to time directly, and the call COUNT is known by
// construction (one per argument), so the product is the honest answer.
func BenchmarkDisplayStrRoutes(b *testing.B) {
	vm := New(io.Discard)
	v := object.IntValue(42)
	b.Run("go-level ToS (the old fast path)", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = v.ToS()
		}
	})
	b.Run("dispatched to_s (MRI's rb_obj_as_string)", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = vm.displayStr(v)
		}
	})
	b.Run("String, which takes neither", func(b *testing.B) {
		s := object.NewString("s")
		for i := 0; i < b.N; i++ {
			_ = vm.displayStr(s)
		}
	})
}
