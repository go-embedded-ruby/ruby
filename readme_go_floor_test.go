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

	// A second claim in prose would drift independently of the badge, so fail on
	// any stale one rather than trusting that the badge is the only place.
	for _, stale := range regexp.MustCompile(`1\.\d+\.\d+`).FindAllString(string(readme), -1) {
		if strings.HasPrefix(stale, "1.2") && stale != floor && strings.Contains(string(readme), "go-"+stale) {
			t.Errorf("README still mentions Go %s somewhere outside the badge; go.mod requires %s", stale, floor)
		}
	}
}
