// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package main

import "testing"

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
