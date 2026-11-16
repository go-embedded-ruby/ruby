// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package ruby_test

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// baselineRecordCounts counts BASELINE's records by KIND. Its sibling
// baselinePassingTotal (readme_conformance_count_test.go) sums the passing
// EXAMPLES and skips FILEFAIL rows on purpose — "a FILEFAIL row carries no
// count" — so it cannot answer how many FILES there are, which is a different
// claim the README makes five times.
//
// ⛔ IT KEYS ON THE RECORD SHAPE, NOT ON THE WORD. `grep -c FILEFAIL` over this
// file answers 5 where there are 4 records: the fifth hit is the header line
// that DOCUMENTS the format —
//
//	# One record per spec file: OK<TAB>path<TAB>passing, or FILEFAIL<TAB>path.
//
// A comment that names a value is the oldest way a count goes wrong by one.
func baselineRecordCounts(t *testing.T) (ok, filefail int) {
	t.Helper()
	const path = "scripts/conformance/rubyspec/BASELINE"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var unknown []string
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch strings.Split(line, "\t")[0] {
		case "OK":
			ok++
		case "FILEFAIL":
			filefail++
		default:
			unknown = append(unknown, line)
		}
	}
	// ⛔ A record this cannot classify must not be dropped quietly from a
	// denominator the README quotes, and zero records is a format change rather
	// than an empty corpus.
	if len(unknown) > 0 {
		t.Fatalf("%s holds %d record(s) that are neither OK nor FILEFAIL, so no total here can answer for them; first: %q",
			path, len(unknown), unknown[0])
	}
	if ok+filefail == 0 {
		t.Fatalf("%s parsed to zero records: the format changed, and this test measured nothing", path)
	}
	return ok, filefail
}

// ⛔ THE README STATES THESE FILE COUNTS FIVE TIMES AND NOTHING DERIVED ANY OF
// THEM.
//
//	| spec files | 2,202 of 2,206 produced a result; 4 produced none |
//	… agreed on every one of the 2,206 per-file records except …
//	| spec files in the corpus | 3,814 — of which 2,206 are swept and 1,608 … |
//	count of each of the 2,206 spec files, and the run fails if any file passes
//	of the 2,206 files is compared with its own record, which needs no margin …
//
// TestReadmeConformanceCountMatchesTheBaseline already guards the passing
// EXAMPLE total, which is the number that moves most often — and precisely
// because that one is guarded, the FILE counts beside it read as equally
// measured. They were not. A corpus sweep that adds or retires spec files moves
// 2,206 in five places and 2,202 in one, by hand, and the only reader who could
// catch a missed edit is one who happens to count BASELINE.
//
// 3,814 and 1,608 are NOT checked here: they describe ruby/spec's whole tree,
// which is not in this repository, so this test cannot see them. It asserts
// that the swept figure inside that sentence is the same 2,206 as everywhere
// else, which is the part BASELINE can answer for. Saying so is the point —
// a gate that quietly judged a subset of a sentence would be worse than none.
func TestTheReadmeSpecFileCountsComeFromTheBaseline(t *testing.T) {
	ok, filefail := baselineRecordCounts(t)
	total := ok + filefail

	readmeBytes, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(readmeBytes), "\r", ""), "\n")

	// The one sentence that states all three numbers at once.
	triple := regexp.MustCompile(`([\d,]+) of ([\d,]+) produced a result; ([\d,]+) produced none`)
	// Every other place that quotes the corpus size.
	single := regexp.MustCompile(`([\d,]+) (?:per-file records|spec files|files is compared|are swept)`)

	tripleSeen, singleSeen := 0, 0
	for i, line := range lines {
		if m := triple.FindStringSubmatch(line); m != nil {
			tripleSeen++
			check(t, i+1, line, "files that produced a result", m[1], ok)
			check(t, i+1, line, "spec files in the baseline", m[2], total)
			check(t, i+1, line, "files that produced none", m[3], filefail)
		}
		for _, m := range single.FindAllStringSubmatch(line, -1) {
			singleSeen++
			check(t, i+1, line, "spec files in the baseline", m[1], total)
		}
	}

	// ⛔ BOTH DENOMINATORS ARE ASSERTED, because a rewording that moves a claim
	// out of a pattern's reach takes it out of this test without taking it out
	// of the README — and a test that then matched nothing would pass. The
	// numbers below are how many such statements the README held when this was
	// written; a drop means a claim went unread, not that the README got shorter.
	if tripleSeen != 1 {
		t.Errorf("expected exactly 1 %q sentence in README.md, matched %d: a reworded claim is an UNCHECKED claim, so reword the pattern with it",
			"N of M produced a result; K produced none", tripleSeen)
	}
	if singleSeen < 4 {
		t.Errorf("expected at least 4 other statements quoting the corpus size in README.md, matched %d: "+
			"one has been reworded out of this test's reach rather than out of the README", singleSeen)
	}
	t.Logf("BASELINE: OK=%d FILEFAIL=%d total=%d; README statements checked: 1 triple, %d single", ok, filefail, total, singleSeen)
}

// check compares one README figure, written with thousands separators, against
// the measurement.
func check(t *testing.T, line int, text, what, got string, want int) {
	t.Helper()
	n, err := strconv.Atoi(strings.ReplaceAll(got, ",", ""))
	if err != nil {
		t.Errorf("README.md:%d: %q is not a number I can read for %s", line, got, what)
		return
	}
	if n != want {
		t.Errorf("README.md:%d says %s is %s; BASELINE says %d\n  %s", line, what, got, want, strings.TrimSpace(text))
	}
}
