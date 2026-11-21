// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import "testing"

// ⛔ THE BRANCH THE COVERAGE RATCHET CAUGHT, AND IT WAS WORTH CATCHING.
//
// interpreterSafepoint has a fast path: with one live thread there is nobody to
// hand the GVL to, so threadPass — an unlock, a Gosched, a relock and two
// context copies — is skipped. The gate on #942 reported
//
//	internal/vm/thread.go interpreterSafepoint  66.7% (was fully covered)
//
// 2 blocks of 3, and the missing one is `vm.threadPass(runtime.Gosched)`: the
// branch that actually hands the GVL over. Every test in the suite that spins a
// Ruby loop does it with ONE thread, so the fast path was exercised and the slow
// path — the one the whole change exists for — was not.
//
// The three-engine oracle probes on #940 did exercise it (that is how
// `Thread.new { loop { } }` started working), but those are not in this suite,
// so nothing here would have noticed it breaking. A local `go test ./internal/vm`
// was green on exactly that gap, because the ratchet is a separate CI step.
//
// This test spins the MAIN thread through enough backward jumps to cross
// safepointTicks while a second thread is parked, which is the only shape that
// reaches the branch.
func TestInterpreterSafepointHandsTheGVLOverWhenAnotherThreadIsLive(t *testing.T) {
	// 200_000 backward jumps against safepointTicks = 4096 is ~48 safepoints,
	// every one of them with two live threads. The sleeping thread is parked, so
	// it stays in vm.threads and len(vm.threads) > 1 holds throughout.
	got := runSrc(t, `
other = Thread.new { sleep 30 }
sleep 0.05

i = 0
while i < 200_000
  i += 1
end
p i

# The peer is still alive and still asleep: passing the GVL to a parked thread
# must not wake it, run it, or kill it -- a safepoint is a hand-off offer, not a
# resumption.
p other.alive?
p other.status

other.kill
other.join
p :joined
`)
	want := "200000\ntrue\n\"sleep\"\n:joined"
	if got != want {
		t.Errorf("safepoint with a second live thread:\ngot  %q\nwant %q", got, want)
	}
}

// And the companion property, which is what makes the fast path defensible
// rather than merely cheap: with ONE thread the same loop must behave
// identically. If the skip were wrong, this is where it would show.
func TestInterpreterSafepointIsATransparentNoOpWithOneThread(t *testing.T) {
	got := runSrc(t, `
i = 0
while i < 200_000
  i += 1
end
p i
p Thread.list.size
`)
	want := "200000\n1"
	if got != want {
		t.Errorf("safepoint with one thread:\ngot  %q\nwant %q", got, want)
	}
}
