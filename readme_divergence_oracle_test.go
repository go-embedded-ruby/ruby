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

// ⛔ THE SECTION NAMED ITS ORACLE TWICE AND THE TWO DISAGREED.
//
// "### What does not work yet" opens by saying what every entry under it was
// measured against:
//
//	Every entry below was re-checked on `fc8ca1a` (darwin/arm64, 2026-10-08) by
//	running it against **MRI 4.0.7 on the same host**.
//
// and forty-six lines later its table said:
//
//	| | how it differs from MRI 4.0.5 |
//
// 4.0.5 was the oracle of an EARLIER campaign. The section was re-measured
// against 4.0.7, the sentence was updated, the table header was not, and rbgo
// itself now reports 4.0.9 — so the header named a version that matched neither
// the measurement nor the runtime. Two rows added under it on 2026-11-15 (#948)
// inherited the wrong oracle, and its PR text repeated "MRI 4.0.5" from the
// header without anyone noticing.
//
// ⛔ WHY NO EXISTING GATE SAW IT. TestReadmeRubyVersionMatchesTheRuntime checks
// the version the runtime REPORTS, against kernel_introspect.go. This header is
// a different claim — the version rbgo was COMPARED WITH — and nothing read it.
// A document can hold two version claims that are each individually plausible
// and still contradict each other; the only thing that catches that is reading
// both and comparing them, which needs no runtime and no network because both
// facts are in the file.
//
// The exemptions in the version test do not apply here either: a table header is
// not prose and cannot be "historical", because the rows under it are claims
// about the code as it is now.
func TestTheDivergenceTableNamesTheSectionsOwnOracle(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	lines := strings.Split(string(readme), "\n")

	const heading = "### What does not work yet"
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("README.md has no %q section: this test measured nothing, which is not a pass. "+
			"If the section was renamed, rename it here too rather than deleting the gate.", heading)
	}
	// The section ends at the next heading of the same or higher level.
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") || strings.HasPrefix(lines[i], "### ") {
			end = i
			break
		}
	}
	section := lines[start:end]

	// Side one: what the section SAYS it was measured against.
	//
	// ⛔ MATCHED OVER THE JOINED TEXT, NOT LINE BY LINE. The sentence is wrapped
	// at 80 columns, so "re-checked" sits on one line and "against **MRI 4.0.7"
	// on the next. The first version of this test read line by line, found
	// nothing, and said so — only because "neither side found" is a FAILURE here.
	// Written the usual way, with an absence treated as nothing to report, it
	// would have gone green on the very defect it was written for.
	measured := regexp.MustCompile(`(?s)re-checked.{0,200}?against \*\*MRI (\d+\.\d+\.\d+)`)
	// Side two: what the table header CLAIMS to differ from. Table rows are never
	// wrapped, so this one is per line.
	header := regexp.MustCompile(`^\|.*differs from MRI (\d+\.\d+\.\d+)`)

	var measuredAt, measuredLine string
	var measuredAtLine int
	joined := strings.Join(section, "\n")
	if loc := measured.FindStringSubmatchIndex(joined); loc != nil {
		measuredAt = joined[loc[2]:loc[3]]
		// The line the VERSION sits on, which is the one a reader must edit.
		measuredAtLine = start + strings.Count(joined[:loc[2]], "\n") + 1
		measuredLine = strings.TrimSpace(lines[measuredAtLine-1])
	}

	var headers []string
	var headerLines []int
	for i, line := range section {
		if m := header.FindStringSubmatch(line); m != nil {
			headers = append(headers, m[1])
			headerLines = append(headerLines, start+i+1)
		}
	}

	// ⛔ Either side missing means this gate read nothing. A regexp that matches
	// nothing reports a clean document, which is the failure this whole file
	// exists to prevent — so each absence is named, not skipped.
	if measuredAt == "" {
		t.Fatalf("README.md:%d..%d: no %q sentence naming the MRI version the entries were measured against. "+
			"The section's oracle is the thing the table is compared to; without it this test cannot judge anything.",
			start+1, end, "re-checked … against **MRI x.y.z")
	}
	if len(headers) == 0 {
		t.Fatalf("README.md:%d..%d: no table header of the form %q. "+
			"The section says it was measured against MRI %s and no table claims to differ from anything.",
			start+1, end, "| | how it differs from MRI x.y.z |", measuredAt)
	}

	for i, h := range headers {
		if h != measuredAt {
			t.Errorf(`README.md:%d says the table differs from MRI %s, but README.md:%d says every entry in this section was measured against MRI %s:

    %s

A table header is not prose and cannot be historical: the rows under it are
claims about the code as it is NOW, so it must name the version they were
actually judged against. Change whichever of the two is wrong — and if the
section really was re-measured, the opening sentence is the one to update first,
because it carries the commit and the date.`,
				headerLines[i], h, measuredAtLine, measuredAt, measuredLine)
		}
	}
	t.Logf("section README.md:%d..%d measured against MRI %s; %d table header(s) checked", start+1, end, measuredAt, len(headers))
}
