// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package ruby_test

import (
	"bufio"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// baselinePassingTotal sums the per-file baseline the ruby/spec ratchet judges
// against -- the repository's own record of how many examples pass.
func baselinePassingTotal(t *testing.T) int {
	t.Helper()
	const path = "scripts/conformance/rubyspec/BASELINE"
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	total, rows := 0, 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 || parts[0] != "OK" {
			continue // a FILEFAIL row carries no count
		}
		n, err := strconv.Atoi(parts[2])
		if err != nil {
			t.Fatalf("%s: unparsable count %q", path, parts[2])
		}
		total += n
		rows++
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if rows == 0 {
		t.Fatalf("%s has no OK rows; the file cannot be the source of truth it is used as", path)
	}
	return total
}

// TestReadmeConformanceCountMatchesTheBaseline: the README states how many
// ruby/spec examples pass, in a badge and in prose, and nothing kept those
// numbers in step with the gate that measures them. They had drifted apart from
// each other as well as from the truth -- the badge said 22,488, the prose
// 23,471 in three places, and BASELINE in the same commit summed to 23,481.
//
// A number a human retypes is a number that rots. This reads the gate's own
// file and fails with the figure to write, so the next person who moves the
// baseline is told rather than trusted.
func TestReadmeConformanceCountMatchesTheBaseline(t *testing.T) {
	want := baselinePassingTotal(t)

	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	text := string(readme)

	// The prose form: **23,481**
	prose := regexp.MustCompile(`\*\*(\d{1,3}(?:,\d{3})+)\*\*()`)
	// The badge form, URL-encoded: ruby%2Fspec-23%2C481%20examples
	badge := regexp.MustCompile(`ruby%2Fspec-(\d{1,3}(?:%2C\d{3})+)%20examples`)

	m := badge.FindStringSubmatch(text)
	if m == nil {
		t.Errorf("README.md has no ruby/spec example-count badge; expected one claiming %s", withCommas(want))
	} else if got := strings.ReplaceAll(m[1], "%2C", ""); got != strconv.Itoa(want) {
		t.Errorf("README badge claims %s ruby/spec examples, BASELINE sums to %d\n"+
			"update the badge to %s", strings.ReplaceAll(m[1], "%2C", ","), want, withCommas(want))
	}

	// Every bold thousands-separated number OUTSIDE a table row must agree.
	//
	// The table row is excluded on purpose, and the rule is the table's own: it
	// is one DATED full sweep ("Measured <date> on <commit>"), while the badge
	// and the prose count BASELINE, which moves with every merge. A dated
	// measurement going stale is not an error; an undated claim going stale is.
	// The README says so where the table sits, so a reader is not left with two
	// numbers and no explanation.
	//
	// A future bold thousands-separated number about something ELSE would fail
	// this and should be made unambiguous rather than have the rule widened --
	// there are exactly three today, and all three are this count.
	lines := strings.Split(text, "\n")
	checked := 0
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		for _, sm := range prose.FindAllStringSubmatch(line, -1) {
			num := sm[1]
			if num == "" {
				num = sm[2]
			}
			if num == "" {
				continue
			}
			checked++
			if strings.ReplaceAll(num, ",", "") != strconv.Itoa(want) {
				t.Errorf("README.md:%d claims %s examples, BASELINE sums to %d\n  %s",
					i+1, num, want, strings.TrimSpace(line))
			}
		}
	}
	if checked == 0 {
		t.Errorf("found no ruby/spec example count in README.md prose; expected at least one claiming %s", withCommas(want))
	}
}

// withCommas renders n with thousands separators, the form the README uses.
func withCommas(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
		if len(s) > lead {
			b.WriteByte(',')
		}
	}
	for i := lead; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}
