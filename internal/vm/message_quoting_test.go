// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// oldQuoting is MRI's pre-3.4 form: a backtick, then at least one character
// that is neither a quote nor another backtick, then an apostrophe.
var oldQuoting = regexp.MustCompile("`[^`\"'\n]+'")

// doubleQuotedSegments returns the contents of each "..." string on a line.
//
// ⛔ It exists because the first version of this gate matched the whole line and
// reported string_undump.go twice: that message is a Go RAW string, where the
// backtick is the delimiter and not part of the message at all. A detector that
// cannot tell a delimiter from content reports defects that are not there.
func doubleQuotedSegments(line string) []string {
	var out []string
	var cur strings.Builder
	inside, escaped := false, false
	for _, r := range line {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case inside && r == '\\':
			escaped = true
		case r == '"':
			if inside {
				out = append(out, cur.String())
				cur.Reset()
			}
			inside = !inside
		case inside:
			cur.WriteRune(r)
		}
	}
	return out
}

// ⛔ MRI stopped writing `name' and writes 'name' since 3.4; v4.0.9 is
// straight-quoted throughout (numeric.c:3207, file.c:3908). rbgo migrated most
// of its messages and missed thirteen, which was only visible because the SAME
// message existed in both spellings in this one package:
//
//	builtins.go:3321   "bignum too big to convert into 'long'"
//	arith.go:675       "bignum too big to convert into `long'"
//
// A note saying "finish the migration" would be forgotten exactly the way the
// first half was, so this is a gate instead. It reports the number of files it
// read: a scan that cannot say what it looked at cannot be told from a scan
// that could not look.
func TestErrorMessagesUseStraightQuotesNotTheOldBacktickForm(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	read := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		read++
		for i, line := range strings.Split(string(b), "\n") {
			if !strings.Contains(line, "raise(") {
				continue
			}
			for _, seg := range doubleQuotedSegments(line) {
				if oldQuoting.MatchString(seg) {
					offenders = append(offenders,
						filepath.Join("internal/vm", name)+":"+strconv.Itoa(i+1)+"  "+strings.TrimSpace(line))
					break
				}
			}
		}
	}
	// ⛔ The denominator. If a refactor moves these messages out of this package
	// the scan would find nothing and quietly pass, so assert it read something.
	if read < 50 {
		t.Fatalf("only %d source files scanned — the gate is looking in the wrong place", read)
	}
	t.Logf("scanned %d non-test source files in internal/vm", read)
	if len(offenders) > 0 {
		t.Errorf("%d message(s) still use MRI's pre-3.4 `x' quoting; v4.0.9 writes 'x':\n%s",
			len(offenders), strings.Join(offenders, "\n"))
	}
}

// The judge and its subject must count alike, so the detector is tested on the
// shapes it has to separate rather than only on the tree as it stands today.
func TestTheQuotingGateSeparatesAMessageFromADelimiter(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want bool
	}{
		{"the old form inside a double-quoted message",
			"\t\traise(\"NameError\", \"`%s' is not allowed as a class variable name\", name)", true},
		{"the new form", "\t\traise(\"NameError\", \"'%s' is not allowed as a class variable name\", name)", false},
		{"a backtick that delimits a Go RAW string is not content",
			"\t\traise(\"RuntimeError\", `invalid dumped string; not wrapped with '\"' nor '\"...\"' form`)", false},
		{"prose that pairs its backticks",
			"\traise(\"NotImplementedError\", \"`%%x`/backtick command execution is not supported on wasm\")", false},
		{"a line with no raise at all", "\t// see `foo' in the notes", false},
		{"an escaped quote does not end the segment",
			"\t\traise(\"ArgumentError\", \"say \\\"`x'\\\" please\")", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := false
			if strings.Contains(tc.line, "raise(") {
				for _, seg := range doubleQuotedSegments(tc.line) {
					if oldQuoting.MatchString(seg) {
						got = true
						break
					}
				}
			}
			if got != tc.want {
				t.Errorf("flagged=%v want=%v for:\n%s\nsegments: %q",
					got, tc.want, tc.line, doubleQuotedSegments(tc.line))
			}
		})
	}
}
