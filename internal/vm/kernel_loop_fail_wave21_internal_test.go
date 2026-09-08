// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"
)

// TestKernelLoopStopIteration covers Kernel#loop's StopIteration handling: a
// StopIteration (or subclass) raised in the block ends the loop cleanly, and the
// loop's value is that exception's #result. Reference: ruby/ruby v3_4_0 eval.c
// rb_f_loop. Verified byte-for-byte against MRI 4.0.5.
func TestKernelLoopStopIteration(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// Plain StopIteration: loop returns nil (its #result is unset).
		{"bare_stop", `p(loop { raise StopIteration })`, "nil"},
		// A StopIteration subclass is rescued the same way.
		{"subclass_stop", `Sub = Class.new(StopIteration); p(loop { raise Sub })`, "nil"},
		// The loop value is the finished iterator's StopIteration#result.
		{"result_value", `e = Enumerator.new { |y| y << 1; y << 2; :done }; p(loop { e.next })`, ":done"},
		// A break unwinds to the call site with its value (the recover re-raises the
		// break signal untouched).
		{"break_value", `p(loop { break 7 })`, "7"},
		{"break_bare", `p(loop { break })`, "nil"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Fatalf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestKernelLoopPropagatesOtherErrors proves the recover re-raises a non-
// StopIteration Ruby exception rather than swallowing it (the `panic(r)` branch).
func TestKernelLoopPropagatesOtherErrors(t *testing.T) {
	err := runStragglerErr(t, `loop { raise "boom" }`)
	if err == nil {
		t.Fatal("expected loop to propagate a RuntimeError, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected the RuntimeError to propagate, got %v", err)
	}
}

// TestKernelLoopNoBlock covers the blockless branch: Kernel#loop returns an
// infinite Enumerator over itself whose #size is Float::INFINITY, and re-driving
// it with a block runs the loop.
func TestKernelLoopNoBlock(t *testing.T) {
	if got := runSrc(t, `p loop.instance_of?(Enumerator)`); got != "true" {
		t.Fatalf("loop.instance_of?(Enumerator): got %q want true", got)
	}
	if got := runSrc(t, `p loop.size`); got != "Infinity" {
		t.Fatalf("loop.size: got %q want Infinity", got)
	}
	// The returned Enumerator re-drives Kernel#loop with the given block.
	if got := runSrc(t, `c = 0; p(loop.each { c += 1; break c if c >= 5 })`); got != "5" {
		t.Fatalf("loop.each drive: got %q want 5", got)
	}
}

// TestKernelFailAliasOfRaise proves Kernel#fail shares Kernel#raise's method
// record on both the instance and singleton sides, and that it raises. Reference:
// ruby/ruby v3_4_0 eval.c (rb_f_raise registered under raise and fail).
func TestKernelFailAliasOfRaise(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"instance_method_eq", `p(Kernel.instance_method(:fail) == Kernel.instance_method(:raise))`, "true"},
		{"singleton_method_eq", `p(Kernel.method(:fail) == Kernel.method(:raise))`, "true"},
		{"private_on_instances", `p Kernel.private_instance_methods(false).include?(:fail)`, "true"},
		{"public_on_module", `p Kernel.public_methods(false).include?(:fail)`, "true"},
		{"raises_runtimeerror", `begin; fail "x"; rescue => e; p e.class; end`, "RuntimeError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Fatalf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
