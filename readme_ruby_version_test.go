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

// runtimeRubyVersion reads the single constant the interpreter answers
// RUBY_VERSION with. It is the same file the documentation site's figure gate
// reads, and it is the only authority here: a README that names a version the
// runtime does not report is wrong whichever of the two moved.
func runtimeRubyVersion(t *testing.T) string {
	t.Helper()
	const path = "internal/vm/kernel_introspect.go"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := regexp.MustCompile(`(?m)^\trubyVersion\s*=\s*"(\d+\.\d+\.\d+)"`).FindSubmatch(src)
	if m == nil {
		t.Fatalf("%s has no `rubyVersion = \"x.y.z\"`; this test cannot judge the README without it", path)
	}
	return string(m[1])
}

// readmeRubyVersionClaims are the shapes in which the README states the version
// the interpreter reports NOW. Each is a sentence that exists in the file
// today; nothing speculative is listed, because a pattern written for a
// sentence nobody writes is a false positive waiting for the sentence that
// does.
//
// ⛔ The count is asserted below. A vocabulary list is a filter, and a filter
// that matches nothing looks exactly like a file with nothing wrong -- which is
// how go-embedded-ruby/docs#44 shipped two stale figures past a gate that
// reported itself clean. If a future rewording drops out of this list the
// denominator falls and the test fails, instead of quietly checking less.
var readmeRubyVersionClaims = []*regexp.Regexp{
	// | `RUBY_VERSION` | rbgo reports `"4.0.9"`, the latest MRI release …
	regexp.MustCompile("rbgo reports `\"(\\d+\\.\\d+\\.\\d+)\"`"),
	// … and the summary above count the current `BASELINE` and so already
	// reflect 4.0.9.
	regexp.MustCompile(`already reflect (\d+\.\d+\.\d+)`),
}

// historicalMarkers are the phrases that let a line name a version OTHER than
// the one the runtime reports. Two lines qualify today, and they are different
// kinds of past tense:
//
//	"a sweep taken at `RUBY_VERSION` 3.4.1"   -- a dated measurement
//	"The move from `3.4.1` was measured …"    -- the version left behind
//
// Naming the exemptions rather than forbidding every deviation is deliberate. A
// check loosened to "any mismatch is fine" has stopped checking; this one still
// fails on an undeclared mismatch, and logs the lines it let through so the
// exemption stays visible instead of becoming invisible slack.
var historicalMarkers = []string{"sweep", "the move from"}

// historicalMarkerOf reports which marker, if any, lets this line name an older
// version.
func historicalMarkerOf(line string) (string, bool) {
	low := strings.ToLower(line)
	for _, m := range historicalMarkers {
		if strings.Contains(low, m) {
			return m, true
		}
	}
	return "", false
}

// TestReadmeRubyVersionMatchesTheRuntime: the README's conformance TOTAL is
// gated by TestReadmeConformanceCountMatchesTheBaseline, which reads BASELINE.
// The version that total is counted AT was gated by nothing, and it selects the
// population counted: the move from 3.4.1 to 4.0.9 changed which
// `ruby_version_is` guards admit examples (+144 passing, 27 gone red), so the
// same number means a different thing at a different version.
func TestReadmeRubyVersionMatchesTheRuntime(t *testing.T) {
	want := runtimeRubyVersion(t)

	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	lines := strings.Split(string(readme), "\n")

	checked := 0
	for i, line := range lines {
		for _, re := range readmeRubyVersionClaims {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				checked++
				if m[1] != want {
					t.Errorf("README.md:%d claims the runtime reports %s, kernel_introspect.go says %s\n  %s",
						i+1, m[1], want, strings.TrimSpace(line))
				}
			}
		}
	}
	if checked < len(readmeRubyVersionClaims) {
		t.Errorf("only %d present-tense version claim(s) matched in README.md, one per shape was expected at least; "+
			"a rewording has taken a claim out of this test's reach rather than out of the README", checked)
	}

	// The inverse: a line that talks about RUBY_VERSION and names a DIFFERENT
	// version must say it is historical.
	//
	// ⛔ The two halves cover each other's blind spot, which is why both are
	// here. Measured by setting the table row to 4.0.8: the forward check named
	// it, while this one EXEMPTED the same 4.0.8 — the line also says "the move
	// from", so a marker meant for the version left behind covered a wrong
	// present-tense claim sitting beside it. A line-granular exemption cannot
	// tell the two apart; the forward check, which reads the claim itself, can.
	version := regexp.MustCompile(`\b(\d+\.\d+\.\d+)\b`)
	exempt := 0
	for i, line := range lines {
		if !strings.Contains(line, "RUBY_VERSION") {
			continue
		}
		for _, m := range version.FindAllStringSubmatch(line, -1) {
			if m[1] == want {
				continue
			}
			if marker, ok := historicalMarkerOf(line); ok {
				exempt++
				t.Logf("README.md:%d names %s and is exempt: the line says %q", i+1, m[1], marker)
				continue
			}
			t.Errorf("README.md:%d names RUBY_VERSION %s, the runtime reports %s, and the line says none of %v\n  %s",
				i+1, m[1], want, historicalMarkers, strings.TrimSpace(line))
		}
	}
	t.Logf("%d present-tense version claim(s) checked against %s, %d historical line(s) exempt", checked, want, exempt)
}
