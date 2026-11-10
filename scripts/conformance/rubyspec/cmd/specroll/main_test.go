// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fullReport is a specdelta report in its DEFAULT shape: one row per file
// walked, footer counting the same files. Every line below is the real format,
// including the indented "WENT RED" lines that sit under a moved row and which
// a column-zero anchor must never read as rows of their own.
const fullReport = `PROVENANCE  ⛔ UNPROVENANCED
  control   /opt/homebrew/opt/ruby@4.0/bin/ruby — no Go build information
  treatment /engines/rbgo-parity — built from c604cce1ba0b
MOVE none declared: both engines read the same corpus, so every delta below is the ENGINE

file                                                     control   treat  delta
library/date/parse_spec.rb                                    54      22    -32  ⛔ fail+error 0 → 32
      ⛔ WENT RED: error  Date#parse | coerces using to_str
      ⛔ WENT RED: error  Date#parse | parses DD as month day number
library/coverage/result_spec.rb                               29       0    -29  DID NOT RUN on the treatment
library/coverage/start_spec.rb                                14       0    -14  DID NOT RUN on the treatment
library/date/today_spec.rb                                     3       3     +0
library/cgi/escape_spec.rb                                     4       4     +0

5 file(s): control 104 passing, treatment 29 passing, -75
`

// quietReport is the same measurement with -quiet: ONLY the moved files are
// printed, so its rows cannot sum to its footer's file count or its passing
// counts -- but the DELTA still must.
const quietReport = `file                                                     control   treat  delta
library/date/parse_spec.rb                                    54      22    -32  ⛔ fail+error 0 → 32
library/coverage/result_spec.rb                               29       0    -29  DID NOT RUN on the treatment
library/coverage/start_spec.rb                                14       0    -14  DID NOT RUN on the treatment

497 file(s): control 1370 passing, treatment 1295 passing, -75
`

func write(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runRoll(t *testing.T, depth, top int, paths ...string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	err := run(&out, &errb, depth, top, paths)
	return out.String(), errb.String(), err
}

// A full report rolls up, reconciles, and separates "behind" from "absent".
func TestAFullReportRollsUpAndReconciles(t *testing.T) {
	out, errb, err := runRoll(t, 2, 0, write(t, "full.txt", fullReport))
	if err != nil {
		t.Fatalf("run: %v\nstdout: %s\nstderr: %s", err, out, errb)
	}
	for _, want := range []string{
		// date: 54+3 control, 22+3 treatment, gap 32, nothing unloadable.
		"library/date                            2       57      25      32",
		// coverage: both files unloadable, and the examples they cost named.
		"library/coverage                        2       43       0      43  2 (43)",
		"library/cgi                             1        4       4       0",
		"TOTAL                                   5      104      29      75  2 (43)",
		"rows reconcile with the report's own footer",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not contain %q\n--- got ---\n%s", want, out)
		}
	}
	// ⛔ The indented WENT RED lines must not have become rows: six rows would
	// make every column wrong and the footer check is what says so.
	if strings.Contains(out, "TOTAL") && strings.Contains(errb, "do not reconcile") {
		t.Errorf("the report reconciled in the tool but stderr complains:\n%s", errb)
	}
}

// ⛔ THE CASE THAT MADE THIS TOOL LIE BEFORE IT WAS WRITTEN CORRECTLY. Judged by
// the full-report invariant, every -quiet report is "corrupt": its 3 rows do not
// match a footer counting 497 files. Every report this tool was built to read is
// a quiet one.
func TestAQuietReportIsReconciledByItsDelta(t *testing.T) {
	out, errb, err := runRoll(t, 2, 0, write(t, "quiet.txt", quietReport))
	if err != nil {
		t.Fatalf("a -quiet report was rejected: %v\nstderr: %s", err, errb)
	}
	if !strings.Contains(out, "QUIET report — 3 moved file(s) of 497 walked, delta -75 reconciles") {
		t.Errorf("stdout does not say the quiet report reconciled:\n%s", out)
	}
	// And it must say what its columns then cover, or 43 will be read as
	// "coverage has 43 examples" rather than "43 of them moved".
	if !strings.Contains(out, "count\n   only the files that MOVED") {
		t.Errorf("a quiet rollup does not state what its count columns cover:\n%s", out)
	}
}

// The same sentence must NOT appear for a full report, where the columns are
// the modules' totals: a caveat printed always is a caveat nobody reads.
func TestAFullRollupDoesNotCarryTheQuietCaveat(t *testing.T) {
	out, _, err := runRoll(t, 2, 0, write(t, "full.txt", fullReport))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "only the files that MOVED") {
		t.Errorf("a full rollup carries the -quiet caveat:\n%s", out)
	}
}

// A row dropped by a parser that stopped matching must be a REFUSAL, not a
// tidier table. Here the footer claims a delta the rows cannot produce.
func TestRowsThatContradictTheFooterAreRefused(t *testing.T) {
	bad := strings.Replace(quietReport, "-75", "-999", 1)
	out, errb, err := runRoll(t, 2, 0, write(t, "bad.txt", bad))
	if err == nil {
		t.Fatalf("a report whose rows contradict its footer was accepted\nstdout: %s", out)
	}
	if got := exitCode(err); got != 1 {
		t.Errorf("exit status %d, want 1", got)
	}
	if !strings.Contains(errb, "the report's own footer says -999") {
		t.Errorf("stderr does not name the disagreement:\n%s", errb)
	}
}

// A report this parser cannot read yields no rows, and an empty table reads
// exactly like parity. It must refuse.
func TestAnUnparsableReportRefuses(t *testing.T) {
	_, _, err := runRoll(t, 2, 0, write(t, "empty.txt", "nothing here that looks like a sweep\n"))
	if err == nil {
		t.Fatal("an unparsable report was accepted as a clean rollup")
	}
	if got := exitCode(err); got != 2 {
		t.Errorf("exit status %d, want 2 (a refusal, not a verdict)", got)
	}
}

// Several reports roll up together, which is how a corpus split into chunks is
// read back as one answer.
func TestSeveralReportsRollUpTogether(t *testing.T) {
	a := write(t, "a.txt", fullReport)
	b := write(t, "b.txt", strings.ReplaceAll(fullReport, "library/", "core/"))
	out, errb, err := runRoll(t, 1, 0, a, b)
	if err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, errb)
	}
	// -depth 1 collapses each tree to its top-level name.
	for _, want := range []string{"library", "core", "TOTAL                                  10      208      58     150"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not contain %q\n--- got ---\n%s", want, out)
		}
	}
}

// -top keeps the table short and says how many it hid: a truncated list that
// does not say it is truncated is the same failure as an unstated population.
func TestTopSaysWhatItHid(t *testing.T) {
	out, _, err := runRoll(t, 2, 1, write(t, "full.txt", fullReport))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "more module(s) not shown") {
		t.Errorf("a truncated table does not say so:\n%s", out)
	}
}
