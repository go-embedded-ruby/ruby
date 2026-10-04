// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strconv"
	"testing"
)

// TestOperatorGuardCostsNoResolutionPerIteration: the redefinition guard sits on
// every operator evaluation, so the question that decides whether it is
// affordable is how often it RESOLVES a method -- once per loop, or once per
// iteration.
//
// It is asserted as a count rather than timed on purpose. A wall-clock or even
// a CPU-time comparison on this machine is worthless while other work is
// running (measured at load 127, the same binary varied by 3x run to run), and
// the property being claimed is not "fast" but "does no work per iteration",
// which a counter settles exactly and a clock cannot.
//
// The bound is deliberately loose: what matters is that it does not scale with
// the iteration count. 30000 iterations over three operators would cost 90000
// resolutions without the cache.
func TestOperatorGuardCostsNoResolutionPerIteration(t *testing.T) {
	const iters = 30000
	src := `t = 0
i = 0
while i < ` + strconv.Itoa(iters) + `
  t = t + i * 2
  i = i + 1
end
p t
`
	before := basicOpSlowPath.Load()
	eval(t, src)
	misses := basicOpSlowPath.Load() - before

	// Four operators appear (+, *, <, and the loop's own +), each on Integer, so
	// a correct cache warms a handful of entries and then stops resolving.
	const bound = 64
	if misses > bound {
		t.Errorf("guard resolved a method %d times over %d iterations; want <= %d\n"+
			"a count that scales with the iteration count means the per-operator cache is not holding",
			misses, iters, bound)
	}
	t.Logf("%d resolutions for %d iterations (%d operator evaluations)", misses, iters, iters*4)
}

// TestOperatorGuardResolvesAgainAfterARedefinition is the other half: the cache
// must not be so sticky that it misses a change. A redefinition bumps
// globalMethodSerial, which invalidates every entry, so the count MUST move.
//
// It is an A/B over two programs of the same shape differing in one line,
// because each eval builds a FRESH VM: comparing a "warm" run against a later
// one would compare two cold caches and prove nothing. That is how the first
// version of this test passed its subject and failed its own premise.
func TestOperatorGuardResolvesAgainAfterARedefinition(t *testing.T) {
	loop := `i = 0
while i < 500
  i = i + 1
end
p i
`
	count := func(src string) uint64 {
		before := basicOpSlowPath.Load()
		eval(t, src)
		return basicOpSlowPath.Load() - before
	}
	plain := count(loop + loop)
	redefined := count(loop + "class Integer; def +(o); self.-(-o); end; end\n" + loop)

	if redefined <= plain {
		t.Errorf("same program with a redefinition in the middle cost %d resolutions, without it %d;\n"+
			"a redefinition must invalidate the cache and force at least one more",
			redefined, plain)
	}
	t.Logf("resolutions: %d without a redefinition, %d with one", plain, redefined)
}
