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
	// The badge is shields.io with the `+` URL-encoded: go-1.27.1%2B
	badge := regexp.MustCompile(`badge/go-(\d+\.\d+(?:\.\d+)?)%2B`).FindSubmatch(readme)
	if badge == nil {
		t.Fatalf("README.md has no Go version badge; expected one claiming %s", floor)
	}
	if got := string(badge[1]); got != floor {
		t.Errorf("README Go badge claims %s, go.mod requires %s\n  update the badge to go-%s%%2B",
			got, floor, floor)
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
	if found < 2 {
		t.Errorf("found %d statements of the Go minimum in README.md, expected at least 2 "+
			"(the badge and the prose requirement); if one was removed on purpose, lower this", found)
	}
}
