// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package ruby_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestReadmeGoBadgeMatchesTheModuleFloor keeps the README's Go badge honest
// against go.mod, in the spirit of TestReadmeConformanceCountMatchesTheBaseline.
//
// It exists because the badge went stale the moment the floor moved: #785
// raised go.mod from 1.26.4 to 1.27.1 and the badge kept claiming 1.26.4, which
// is the claim a reader checks before depending on the module. Nothing failed,
// because nothing was looking -- a version badge is exactly the kind of fact
// that rots silently, since it is read by people and by no test.
func TestReadmeGoBadgeMatchesTheModuleFloor(t *testing.T) {
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	gd := regexp.MustCompile(`(?m)^go (\d+\.\d+(?:\.\d+)?)`).FindSubmatch(mod)
	if gd == nil {
		t.Fatal("go.mod has no `go` directive; this test cannot judge the badge without one")
	}
	floor := string(gd[1])

	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	// A badge that READS go.mod cannot go stale, so it is not checked for a
	// value: the answer to a number that rots is to stop restating it, not to
	// keep correcting it. shields.io's github/go-mod/go-version endpoint renders
	// the directive directly -- verified against this repository, where it shows
	// "Go: v1.27.1" -- which takes the badge out of the maintenance loop.
	//
	// What turned that from a preference into a fix: on 2026-10-10 Renovate
	// FORCE-PUSHED its branch over a commit that had corrected the literal badge
	// and gone green on every lane, reinstating 1.27.1 against a 1.27.2
	// directive. A number a human has to retype is a number that comes back.
	dynamic := regexp.MustCompile(`img\.shields\.io/github/go-mod/go-version/`).Match(readme)
	literalBadge := regexp.MustCompile(`badge/go-(\d+\.\d+(?:\.\d+)?)%2B`).FindSubmatch(readme)
	switch {
	case dynamic:
		// Nothing to compare against. Guard the replacement instead: a literal
		// badge left beside the dynamic one would be a second, rottable copy.
		if literalBadge != nil {
			t.Errorf("README has BOTH a dynamic Go badge and a literal one claiming %s; "+
				"the literal one can go stale, which is the thing the dynamic one removes",
				literalBadge[1])
		}
	case literalBadge == nil:
		t.Fatalf("README.md has no Go version badge, literal or dynamic; expected one claiming %s", floor)
	default:
		if got := string(literalBadge[1]); got != floor {
			t.Errorf("README Go badge claims %s, go.mod requires %s\n  update the badge to go-%s%%2B",
				got, floor, floor)
		}
	}

	// The badge is not the only place the README states the floor, and a guard
	// scoped to the shape I happened to fix first would have missed the other
	// one: line 716 said "Requires **Go 1.26.4+**" and stayed stale through two
	// separate PRs that each corrected only the badge. So this judges the CLAIM
	// -- every statement of a minimum -- rather than the markup it is wearing.
	//
	// It deliberately does NOT fail on every mention of a Go version. A README
	// may name one for reasons that must not move: a dated measurement ("measured
	// on Go 1.26.4") would be falsified by updating it, and "Go 1.26 introduced X"
	// is a fact about when an API appeared. Only a stated MINIMUM tracks go.mod,
	// so only the two forms that state one are matched.
	minima := regexp.MustCompile(`(?:badge/go-|Go )\*{0,2}(\d+\.\d+(?:\.\d+)?)\+?\*{0,2}(?:%2B|\+)`)
	found := 0
	for _, m := range minima.FindAllStringSubmatch(string(readme), -1) {
		found++
		if m[1] != floor {
			t.Errorf("README states a Go minimum of %s in %q; go.mod requires %s",
				m[1], strings.TrimSpace(m[0]), floor)
		}
	}
	// How many hand-written statements must remain depends on whether the badge
	// is still one of them. With a dynamic badge there is exactly one copy left
	// to keep honest, and demanding two would force someone to add a second
	// rottable number back -- which is the opposite of the point.
	//
	// The floor is 1 either way, never 0: a README that states the minimum
	// NOWHERE in prose would pass a check for "no wrong statement" by saying
	// nothing, and a reader deciding whether they can depend on the module needs
	// the number on the page.
	want := 2
	if dynamic {
		want = 1
	}
	if found < want {
		t.Errorf("found %d statement(s) of the Go minimum in README.md, expected at least %d "+
			"(dynamic badge: %v); if one was removed on purpose, lower this", found, want, dynamic)
	}
}
