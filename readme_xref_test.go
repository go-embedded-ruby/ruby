// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package ruby_test

import (
	"regexp"
	"strings"
	"testing"
)

// TestReadmeCrossReferencesResolve.
//
// The README sent readers to a section that was never written. Under
// Kernel#fork it said:
//
//	fork { ... } *does* exist and runs the block in the same process — see the
//	table under *Known divergences*, because code that forks to isolate or to
//	drop privileges gets neither here.
//
// ⛔ There is no "Known divergences" table. `git grep -i divergenc` over every
// markdown file in the repository returned that citation and nothing else. The
// sentence names the stake exactly right — a program that forks to drop
// privileges drops none here — and then points at somewhere that does not
// exist, which is the one place a reader would have gone to find out.
//
// MEASURED, because a guard aimed at prose is worthless if it fires on prose
// that is fine (cf. go-desktop/catalog#26, where the obvious version of a number
// guard gave four must-never-fire hits against two real findings). The
// population was 8 references before this change and is 7 after, and every one
// is a genuine pointer at another part of the document — no emphasis caught by
// accident:
//
//	*Set*  *Supported today* (x3)  *Running Puppet*  *Runtime conformance*
//	*Status*   — and, until this change, *Known divergences*
//
// Seven resolved; *Known divergences* was the one that did not, and the fix
// alongside this test says the thing inline instead of pointing anywhere.
//
// ⛔ I first counted that population BY HAND with a grep and got 4. The regexp
// finds 8: the hand pattern only matched capitalised titles in one phrasing and
// silently missed three repeats of *Supported today* and *Status*. A population
// counted by eye is the thing this test exists to replace, so what it prints is
// the count — and it prints the dangling tally too, rather than a verdict that
// could disagree with the failures above it.
//
// ⛔ The README is flattened first: "see *Set* under\n*Supported today*" wraps
// across two source lines, so a line-based check sees neither half.
func TestReadmeCrossReferencesResolve(t *testing.T) {
	readme := readFile(t, "README.md")
	flat := strings.Join(strings.Fields(readme), " ")

	// An italic phrase introduced by "see" or "under" is a reference to another
	// part of this document. Emphasis is written the same way, which is why the
	// pattern requires the italic to follow one of those two words IMMEDIATELY:
	// `fork { ... } *does* exist` is emphasis and must not be matched.
	ref := regexp.MustCompile(`(?i)\b(?:see|under)\s+\*([^*]{2,60})\*`)
	found := ref.FindAllStringSubmatch(flat, -1)

	// ⛔ A reworded README that matches nothing would make this test pass by
	// having stopped looking. The population is the first thing asserted.
	if len(found) == 0 {
		t.Fatalf("README.md: no %q-style cross-reference matched at all; "+
			"the wording changed shape, so teach the regexp rather than "+
			"deleting the check", "see *Section*")
	}

	// A reference resolves to a heading that names it, or to a bold run-in item
	// such as "**Set:**" — both are how this README labels a place.
	lines := strings.Split(readme, "\n")
	resolves := func(title string) bool {
		lower := strings.ToLower(title)
		for _, ln := range lines {
			if strings.HasPrefix(strings.TrimSpace(ln), "#") &&
				strings.Contains(strings.ToLower(ln), lower) {
				return true
			}
		}
		return strings.Contains(strings.ToLower(flat), "**"+lower)
	}

	// Reporting the denominator, not just the verdict: a reader of a green run
	// should be able to see WHICH references were checked, so a population that
	// quietly shrinks is visible.
	for _, m := range found {
		t.Logf("  reference: *%s*", strings.TrimSpace(m[1]))
	}
	dangling := 0
	for _, m := range found {
		title := strings.TrimSpace(m[1])
		if !resolves(title) {
			dangling++
			t.Errorf("README.md refers to *%s*, and no heading or bold item in "+
				"README.md names it.\nEither write that section or say the thing "+
				"inline — a reference is a promise, and this one carried the "+
				"consequence a reader needed.", title)
		}
	}
	// ⛔ The verdict in this line has to agree with the run. A first draft said
	// "all resolve" unconditionally and printed it directly under a failure it
	// had just reported — a summary that contradicts its own output is how a
	// reader learns to stop reading summaries.
	t.Logf("%d cross-reference(s) checked, %d dangling", len(found), dangling)
}
