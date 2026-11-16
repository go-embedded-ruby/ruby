// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

// ⛔ package ruby, not ruby_test, unlike its sibling README gates. It reuses
// requiredModules and unixEndings from bench_backing_test.go, which is the
// INTERNAL test package; the alternative was a second go.mod parser beside the
// one documented as the single answer to this question.
package ruby

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ⛔ THE README COUNTS THE BOUND FAMILY BY HAND, AND SAYS SO.
//
//	The `go.mod` currently **binds 178 such modules across 176 orgs** into rbgo
//	as native modules (count:
//	`grep -oE 'github\.com/go-ruby-[a-z0-9-]*/[a-z0-9-]*' go.mod | sort -u | wc -l`;
//	`go-ruby-widgets` contributes three)
//
// and then prints all 176 org names in a backticked list.
//
// Two numbers and a 176-element list, in a family that gains members weekly,
// with nothing checking any of it. The documented method is the problem as much
// as the absence of a gate: a `grep | sort -u | wc -l` cannot tell a CHANGED
// COUNT from a BROKEN PATTERN. Tighten the character class, mistype the host or
// point it at the wrong file and it prints a smaller number — never "I matched
// nothing of the shape you meant". The same pipeline is what a reader would run
// to check the figure, so it would confirm whatever it was mistyped into.
//
// The LIST is the part most likely to rot: adding a module means editing an
// alphabetical run of 176 names by hand, and a reader cannot tell a missing one
// from a name they simply did not notice.
//
// This derives all three from go.mod and compares BOTH WAYS. One direction is
// not enough — a list can be short OR carry a name that go.mod no longer has,
// and only the second direction sees the second kind
// (a-check-in-one-direction-cannot-see-something-extra).
//
// It reuses requiredModules from bench_backing_test.go rather than parsing
// go.mod again: that helper exists, is documented as "a parse of the text rather
// than a call to `go list`: the test has to answer the same question a reader of
// go.mod would", and a second parser beside it would be a second answer to one
// question.
func TestTheBoundFamilyListMatchesGoMod(t *testing.T) {
	gomod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	required := requiredModules(unixEndings(string(gomod)))
	if len(required) < 100 {
		t.Fatalf("go.mod parsed to %d requirements, which cannot be right: this test measured nothing", len(required))
	}

	// ⛔ ORGS AND MODULES ARE DIFFERENT POPULATIONS and the README states both.
	// go-ruby-widgets holds three modules (widgets, tui, mvvm), so modules =
	// orgs + 2 — and deriving one count from the other would make the pair
	// agree by construction instead of by measurement.
	goRuby := regexp.MustCompile(`^github\.com/go-ruby-([a-z0-9-]+)/([a-z0-9-]+)$`)
	orgs := map[string]bool{}
	modules := 0
	for path := range required {
		if m := goRuby.FindStringSubmatch(path); m != nil {
			orgs[m[1]] = true
			modules++
		}
	}
	if modules == 0 {
		t.Fatal("no github.com/go-ruby-*/* module required by go.mod: the pattern is wrong, not the repository")
	}

	readmeBytes, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	readme := unixEndings(string(readmeBytes))

	// The two stated counts.
	// ⛔ The emphasis opens BEFORE "binds": `**binds 178 such modules across 176
	// orgs**`. Written the other way round the pattern matched nothing, and this
	// test said so instead of passing — which is the only reason the mistake was
	// mine to fix rather than a silent hole.
	counts := regexp.MustCompile(`\*\*binds (\d+) such modules across (\d+) orgs\*\*`)
	cm := counts.FindStringSubmatch(readme)
	if cm == nil {
		t.Fatalf("README.md no longer states %q: this test cannot judge counts it cannot find. "+
			"If the sentence was reworded, reword it here too rather than deleting the gate.",
			"**binds N such modules across N orgs**")
	}
	if got, _ := strconv.Atoi(cm[1]); got != modules {
		t.Errorf("README.md says go.mod binds %d modules; it requires %d github.com/go-ruby-*/* module(s)", got, modules)
	}
	if got, _ := strconv.Atoi(cm[2]); got != len(orgs) {
		t.Errorf("README.md says those modules span %d orgs; they come from %d", got, len(orgs))
	}

	// The list itself: every backticked name between "The full bound family" and
	// the "each at `github.com/go-ruby-<name>/<name>`" that closes it.
	lines := strings.Split(readme, "\n")
	start, end := -1, -1
	for i, line := range lines {
		if start < 0 && strings.Contains(line, "The full bound family") {
			start = i
			continue
		}
		if start >= 0 && strings.Contains(line, "github.com/go-ruby-<name>/<name>") {
			end = i
			break
		}
	}
	if start < 0 || end < 0 {
		t.Fatalf("README.md: could not find the bound-family list (start=%d, end=%d). "+
			"Both delimiters must be present for this test to read the list at all.", start, end)
	}

	// A backticked token that is a plain name. The closing sentence also holds
	// `github.com/go-ruby-<name>/<name>`, which is a shape and not a member, so
	// anything with a slash or an angle bracket is skipped.
	tick := regexp.MustCompile("`([a-z0-9-]+)`")
	listed := map[string]bool{}
	for _, line := range lines[start+1 : end+1] {
		for _, m := range tick.FindAllStringSubmatch(line, -1) {
			listed[m[1]] = true
		}
	}
	if len(listed) == 0 {
		t.Fatal("README.md: the bound-family list yielded no names, so this test compared nothing")
	}

	var missing, extra []string
	for org := range orgs {
		if !listed[org] {
			missing = append(missing, org)
		}
	}
	for name := range listed {
		if !orgs[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("go.mod requires %d go-ruby org(s) the README's bound-family list does not name (%d):\n    %s\n\n"+
			"Add them to the alphabetical run and update the two counts in the same edit.",
			len(orgs), len(missing), strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("the README's bound-family list names %d entr(ies) go.mod no longer requires:\n    %s\n\n"+
			"A name left behind after a module is dropped is how the list stops describing go.mod. "+
			"⛔ This is the direction a one-way check cannot see.",
			len(extra), strings.Join(extra, ", "))
	}
	t.Logf("go.mod: %d go-ruby module(s) across %d org(s); README lists %d name(s)", modules, len(orgs), len(listed))
}
