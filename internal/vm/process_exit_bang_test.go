// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import "testing"

// ⛔ ONE METHOD, TWO SPELLINGS, TWO BEHAVIOURS.
//
// MRI implements Kernel#exit! and Process.exit! with the same C function
// (rb_f_exit_bang), so they cannot differ. rbgo defined them separately:
// kernel_introspect.go reached raiseSystemExitImmediate and process.go reached
// raiseSystemExit — the non-immediate helper — so Process.exit! ran the at_exit
// handlers that its whole point is to skip.
//
// Measured against MRI 4.0.7 through cmd/oracle before the fix:
//
//	at_exit { puts "at_exit ran" }; puts "start"; Process.exit! 4
//	  MRI    status 4, stdout EMPTY
//	  rbgo   status 4, "start\nat_exit ran"
//
// ⛔ The EXIT STATUS was already correct, which is why nothing caught it: the
// status is what an exit test naturally asserts, and it is identical through
// both helpers. Only at_exit distinguishes them. So this test asserts the
// handler, and uses `exit` as its own control — a test that only checked
// `exit!` could pass against an implementation where NEITHER runs handlers.
func TestProcessExitBangSkipsAtExitLikeKernelExitBang(t *testing.T) {
	// Process.exit! — no handler.
	got := runSrc(t, `
at_exit { puts "handler ran" }
puts "before"
Process.exit! 4
puts "unreachable"
`)
	if got != "before" {
		t.Errorf("Process.exit! should skip at_exit:\ngot  %q\nwant %q", got, "before")
	}

	// Kernel#exit! — the spelling that was already right, asserted so a future
	// change cannot fix one door by breaking the other.
	got = runSrc(t, `
at_exit { puts "handler ran" }
puts "before"
exit! 4
puts "unreachable"
`)
	if got != "before" {
		t.Errorf("Kernel#exit! should skip at_exit:\ngot  %q\nwant %q", got, "before")
	}

	// ⛔ THE CONTROL. Without it, an implementation where at_exit never runs at
	// all would pass both assertions above.
	got = runSrc(t, `
at_exit { puts "handler ran" }
puts "before"
Process.exit 4
puts "unreachable"
`)
	if got != "before\nhandler ran" {
		t.Errorf("Process.exit should RUN at_exit:\ngot  %q\nwant %q", got, "before\nhandler ran")
	}
}
