// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestReadTallyTellsAnAbsentAnswerFromZero is the I/O half of the same rule
// TestAMissingTallyIsNeverReadAsZero holds in the arithmetic. Both are needed:
// the classifier can be right about `Ran: false` while the reader never
// produces it.
func TestReadTallyTellsAnAbsentAnswerFromZero(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	for _, tc := range []struct {
		name string
		path string
		want result
	}{
		// ⭐ The pair that matters. Both produce the number 0 if you read the
		// file carelessly; they mean opposite things.
		{"a file that is not there at all", filepath.Join(dir, "no-such-file"), result{}},
		{"a spec that really passed nothing", write("zero", "0\n"), result{Ran: true, Pass: 0}},

		{"an empty file — killed between create and write", write("empty", ""), result{}},
		{"whitespace only", write("blank", " \n\t\n"), result{}},
		{"something that is not a number", write("junk", "RBGO_RESULT pass=9999\n"), result{}},
		{"a real tally", write("real", "188\n"), result{Ran: true, Pass: 188}},
		{"a tally with no trailing newline", write("bare", "216"), result{Ran: true, Pass: 216}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := readTally(tc.path)
			if got != tc.want {
				t.Fatalf("readTally = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestWalkTakesSpecFilesAndNotDirectoriesNamedLikeThem gives the corpus walk a
// witness on each side of its filter. A directory called `x_spec.rb` is a real
// shape — ruby/spec has `core/io/buffer/`, and nothing stops a fixture
// directory being named that way — and handing one to the runner as if it were
// a file would produce a DIDNOTRUN that means nothing.
func TestWalkTakesSpecFilesAndNotDirectoriesNamedLikeThem(t *testing.T) {
	root := t.TempDir()
	mkdir := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	touch := func(p string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, p), []byte("# spec\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mkdir("core/io")
	mkdir("core/trap_spec.rb") // a DIRECTORY whose name ends in _spec.rb
	mkdir("language")
	mkdir("library") // a suite that is not asked for
	touch("core/io/seek_spec.rb")
	touch("core/io/fixtures.rb") // not a spec
	touch("language/block_spec.rb")
	touch("library/socket_spec.rb") // outside the named suites

	got, err := walk(root, []string{"language", "core", "", "  "})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	want := []string{"core/io/seek_spec.rb", "language/block_spec.rb"}
	if len(got) != len(want) {
		t.Fatalf("walk found %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("walk found %v, want %v", got, want)
		}
	}
}

// TestWalkReportsADirectoryItCannotRead holds the WalkDir callback's error
// arm. Without it the walk would swallow an unreadable subtree and hand back a
// SHORT file list — and a shorter list is not an error anywhere downstream: it
// is simply a smaller measurement, reported with the same confidence as a
// whole one.
//
// ⛔ The skips are about what the host can express, not about what the code
// does. Windows does not honour a 0-mode directory, and root ignores the
// permission entirely, so on those the test cannot ARRANGE the condition. It
// says so and skips rather than asserting something weaker, because a test
// that quietly passes where it could not set up is the thing it is guarding
// against. The ubuntu and macos lanes both run it.
func TestWalkReportsADirectoryItCannotRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a 0-mode directory is not a readable-permission denial on this platform")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, which ignores the directory mode this test sets")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "core", "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "a_spec.rb"), []byte("# spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	// The control: the arrangement really did make it unreadable. A chmod that
	// silently did nothing would leave this test asserting on a walk that
	// simply succeeded.
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("this filesystem let the directory be read at mode 0; the condition could not be arranged")
	}

	got, err := walk(root, []string{"core"})
	if err == nil {
		t.Fatalf("walk returned %v and no error for a subtree it could not read", got)
	}
}

// TestWalkRefusesNothingWhenASuiteIsAbsent is the other side: a suite that is
// not present is SKIPPED, not an error, because a caller measuring one suite
// should not have to own the other. The empty result is then the caller's to
// refuse — main does, with exit 2.
func TestWalkRefusesNothingWhenASuiteIsAbsent(t *testing.T) {
	got, err := walk(t.TempDir(), []string{"language", "core"})
	if err != nil {
		t.Fatalf("walk on an empty tree: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("walk found %v in an empty tree", got)
	}
}

// TestAMissingTallyIsNeverReadAsZero is the whole reason this command is not a
// shell loop around two sweeps.
//
// A file that crashed, timed out or would not load writes no tally, and the
// zero value of an unwritten count is 0 — the same byte a file that genuinely
// passed nothing produces. Letting those meet in the arithmetic turns "I could
// not measure the control" into "the control passed nothing, so the treatment
// gained everything", which is a plausible, coherent and false story about a
// run that measured nothing.
func TestAMissingTallyIsNeverReadAsZero(t *testing.T) {
	for _, tc := range []struct {
		name       string
		row        row
		wantBucket string
		wantN      int
	}{
		{
			"the control could not be measured, so nothing is attributed to the treatment",
			row{File: "crashed.rb", Control: result{Ran: false}, Treatment: result{Ran: true, Pass: 188}},
			"appeared", 0,
		},
		{
			"a file that really passes nothing under both is NOT unmeasured",
			row{File: "empty.rb", Control: result{Ran: true, Pass: 0}, Treatment: result{Ran: true, Pass: 0}},
			"none", 0,
		},
		{
			"the treatment stopped producing a tally — a loss of measurement, not of examples",
			row{File: "broke.rb", Control: result{Ran: true, Pass: 12}, Treatment: result{Ran: false}},
			"unmeasured", 0,
		},
		{
			"neither arm measured it",
			row{File: "dead.rb", Control: result{Ran: false}, Treatment: result{Ran: false}},
			"unmeasured", 0,
		},
		{
			"a real gain",
			row{File: "j_spec.rb", Control: result{Ran: true, Pass: 0}, Treatment: result{Ran: true, Pass: 188}},
			"gained", 188,
		},
		{
			"a real loss",
			row{File: "sticky.rb", Control: result{Ran: true, Pass: 4}, Treatment: result{Ran: true, Pass: 3}},
			"lost", 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := classify([]row{tc.row})
			got := map[string]int{
				"gained":     len(s.Gained),
				"lost":       len(s.Lost),
				"appeared":   len(s.Appeared),
				"unmeasured": len(s.Unmeasured),
			}
			for bucket, n := range got {
				want := 0
				if bucket == tc.wantBucket {
					want = 1
				}
				if n != want {
					t.Errorf("%s holds %d row(s), want %d", bucket, n, want)
				}
			}
			if tc.wantBucket == "gained" && s.GainedN != tc.wantN {
				t.Errorf("gained %d example(s), want %d", s.GainedN, tc.wantN)
			}
			if tc.wantBucket == "lost" && s.LostN != tc.wantN {
				t.Errorf("lost %d example(s), want %d", s.LostN, tc.wantN)
			}
			// ⛔ The decisive one: an unmeasured row must contribute to NEITHER
			// total. A row that landed in the right bucket and still moved a
			// count would be the defect wearing the fix's label.
			if tc.wantBucket == "unmeasured" || tc.wantBucket == "appeared" {
				if s.GainedN != 0 || s.LostN != 0 {
					t.Errorf("an unmeasured row moved the totals: gained=%d lost=%d", s.GainedN, s.LostN)
				}
			}
		})
	}
}

// TestTheTwoDirectionsAreCountedApart holds the other rule: a run that gains in
// one file and loses in another must report both, not their difference. A
// sweep of this corpus once read "+217 in rbgo's favour" twenty minutes before
// the same files read -1566.
func TestTheTwoDirectionsAreCountedApart(t *testing.T) {
	s := classify([]row{
		{File: "up.rb", Control: result{Ran: true, Pass: 0}, Treatment: result{Ran: true, Pass: 188}},
		{File: "down.rb", Control: result{Ran: true, Pass: 190}, Treatment: result{Ran: true, Pass: 0}},
	})
	if s.GainedN != 188 {
		t.Errorf("gained %d, want 188", s.GainedN)
	}
	if s.LostN != 190 {
		t.Errorf("lost %d, want 190", s.LostN)
	}
	// Their difference is -2, which would read as a quiet, almost-clean run.
	if len(s.Gained) != 1 || len(s.Lost) != 1 {
		t.Fatalf("the two directions collapsed: %d gained, %d lost", len(s.Gained), len(s.Lost))
	}
}
