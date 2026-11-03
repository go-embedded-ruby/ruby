package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const baseline3 = "OK\tcore/a_spec.rb\t10\nOK\tcore/b_spec.rb\t20\nFILEFAIL\tcore/c_spec.rb\n"

// judge runs the tool over a sweep against a fixed three-file baseline and
// returns its output and error, so every case below differs only in its input.
func judge(t *testing.T, sweep string, extra ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	bp := filepath.Join(dir, "BASELINE")
	if err := os.WriteFile(bp, []byte(baseline3), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err := run(append([]string{"-baseline", bp}, extra...), strings.NewReader(sweep), &out)
	return out.String(), err
}

// TestRatchetVerdicts is the whole point of the tool: the answer must depend on
// the input. Each case is one shape of move, and the pairs that differ by one
// example exist because a scalar floor with a margin wide enough for a whole
// file could not see them at all.
func TestRatchetVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sweep   string
		wantBad bool
		want    []string
		absent  []string
	}{
		{
			name:  "unchanged",
			sweep: baseline3,
			want:  []string{"no file went backwards", "passing examples: 30 (baseline 30, +0)"},
		},
		{
			name:    "one example lost in one file",
			sweep:   "OK\tcore/a_spec.rb\t9\nOK\tcore/b_spec.rb\t20\nFILEFAIL\tcore/c_spec.rb\n",
			wantBad: true,
			want:    []string{"REGRESSED (1)", "core/a_spec.rb: 9 passing, baseline 10 (-1)"},
		},
		{
			// The case the scalar floor was blind to: one file gains more than
			// another loses, so the total RISES while a file went backwards.
			name:    "a regression hidden under a larger gain",
			sweep:   "OK\tcore/a_spec.rb\t9\nOK\tcore/b_spec.rb\t40\nFILEFAIL\tcore/c_spec.rb\n",
			wantBad: true,
			want:    []string{"REGRESSED (1)", "core/a_spec.rb: 9 passing, baseline 10 (-1)", "passing examples: 49 (baseline 30, +19)"},
		},
		{
			name:    "a file stops loading",
			sweep:   "FILEFAIL\tcore/a_spec.rb\nOK\tcore/b_spec.rb\t20\nFILEFAIL\tcore/c_spec.rb\n",
			wantBad: true,
			want:    []string{"FAILED TO LOAD", "core/a_spec.rb (baseline: 10 passing)"},
			// A load failure must not be reported as a conformance regression:
			// the two need opposite responses.
			absent: []string{"REGRESSED"},
		},
		{
			name:    "a file vanishes from the sweep",
			sweep:   "OK\tcore/a_spec.rb\t10\nOK\tcore/b_spec.rb\t20\n",
			wantBad: true,
			want:    []string{"MISSING FROM THE SWEEP ENTIRELY", "core/c_spec.rb"},
		},
		{
			name:  "improvement is reported, not gated",
			sweep: "OK\tcore/a_spec.rb\t11\nOK\tcore/b_spec.rb\t20\nFILEFAIL\tcore/c_spec.rb\n",
			want:  []string{"improved (1)", "core/a_spec.rb: 11 passing, baseline 10 (+1)", "UPDATE_BASELINE=1"},
		},
		{
			// A file the baseline records as not loading, now loading, is an
			// improvement and not a "regressed to fewer than zero".
			name:  "a file starts loading",
			sweep: "OK\tcore/a_spec.rb\t10\nOK\tcore/b_spec.rb\t20\nOK\tcore/c_spec.rb\t5\n",
			want:  []string{"now loading (1)", "core/c_spec.rb: now loads, 5 passing"},
		},
		{
			name:  "a new file is reported and not gated",
			sweep: baseline3 + "OK\tcore/d_spec.rb\t7\n",
			want:  []string{"new files, not gated (1)", "core/d_spec.rb: 7 passing"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := judge(t, tc.sweep)
			if bad := errors.Is(err, errRegression); bad != tc.wantBad {
				t.Errorf("regression = %v, want %v (err=%v)\n%s", bad, tc.wantBad, err, out)
			}
			if err != nil && !errors.Is(err, errRegression) {
				t.Fatalf("unexpected tool error: %v\n%s", err, out)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("output does not contain %q\n%s", w, out)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(out, a) {
					t.Errorf("output should not contain %q\n%s", a, out)
				}
			}
		})
	}
}

// TestRatchetRejectsUnreadableInput: a sweep this tool cannot read must not be
// reported as a clean machine. Silence is the failure mode the whole tool exists
// to prevent, so every one of these is exit 2, distinct from a regression.
func TestRatchetRejectsUnreadableInput(t *testing.T) {
	for _, tc := range []struct{ name, sweep, want string }{
		{"empty", "", "measured nothing"},
		{"unknown verb", "MAYBE\tcore/a_spec.rb\t1\n", "unrecognised record"},
		{"OK without a count", "OK\tcore/a_spec.rb\n", "unrecognised record"},
		{"count is not a number", "OK\tcore/a_spec.rb\tmany\n", "is not a number"},
		{"the same file twice", "OK\tcore/a_spec.rb\t1\nOK\tcore/a_spec.rb\t2\n", "appears twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := judge(t, tc.sweep)
			if err == nil || errors.Is(err, errRegression) {
				t.Fatalf("want a tool error, got %v\n%s", err, out)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestRatchetUpdateRoundTrips: -update must write a baseline this tool reads
// back identically, and sorted, so a regenerated baseline diffs cleanly.
func TestRatchetUpdateRoundTrips(t *testing.T) {
	dir := t.TempDir()
	bp := filepath.Join(dir, "BASELINE")
	if err := os.WriteFile(bp, []byte(baseline3), 0o644); err != nil {
		t.Fatal(err)
	}
	sweep := "OK\tcore/b_spec.rb\t20\nFILEFAIL\tcore/c_spec.rb\nOK\tcore/a_spec.rb\t11\n"
	var out strings.Builder
	if err := run([]string{"-baseline", bp, "-update"}, strings.NewReader(sweep), &out); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := os.ReadFile(bp)
	if err != nil {
		t.Fatal(err)
	}
	body := got
	for len(body) > 0 && body[0] == '#' {
		i := strings.IndexByte(string(body), '\n')
		body = body[i+1:]
	}
	const want = "OK\tcore/a_spec.rb\t11\nOK\tcore/b_spec.rb\t20\nFILEFAIL\tcore/c_spec.rb\n"
	if string(body) != want {
		t.Errorf("baseline written as %q, want %q (sorted, same shape it reads)", body, want)
	}
	// And the written baseline now accepts the sweep it was written from.
	var out2 strings.Builder
	if err := run([]string{"-baseline", bp}, strings.NewReader(sweep), &out2); err != nil {
		t.Errorf("the regenerated baseline rejects its own sweep: %v\n%s", err, out2.String())
	}
}

// ⛔ -update used to write straight over the baseline WITHOUT READING IT, so the
// one thing it could never report was whether the sweep replacing the record was
// better or worse than it. The hazard was written down twice in this repository
// — cmd/guards' header, and the ratchet lane's comment about an Ubuntu image
// roll ("UPDATE_BASELINE=1 would bank it as progress") — and gated in neither.
//
// recordOver re-records `sweep` over the three-file baseline and returns the
// tool's output, its error, and what the baseline file holds afterwards.
func recordOver(t *testing.T, sweep string, extra ...string) (string, error, string) {
	t.Helper()
	dir := t.TempDir()
	bp := filepath.Join(dir, "BASELINE")
	if err := os.WriteFile(bp, []byte(baseline3), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err := run(append([]string{"-baseline", bp, "-update"}, extra...), strings.NewReader(sweep), &out)
	after, rerr := os.ReadFile(bp)
	if rerr != nil {
		t.Fatal(rerr)
	}
	return out.String(), err, string(after)
}

func TestARerecordWillNotSilentlyBankALoss(t *testing.T) {
	// a_spec goes 10 → 9 while b_spec gains: a NET of zero, which is exactly the
	// shape no total can see.
	lost := "OK\tcore/a_spec.rb\t9\nOK\tcore/b_spec.rb\t21\nFILEFAIL\tcore/c_spec.rb\n"

	out, err, after := recordOver(t, lost)
	if !errors.Is(err, errWouldAbsorb) {
		t.Fatalf("err = %v, want errWouldAbsorb; out:\n%s", err, out)
	}
	if !errors.Is(err, errWouldAbsorb) || errors.Is(err, errRegression) {
		t.Errorf("the refusal must be distinct from a plain regression verdict")
	}
	if !strings.Contains(out, "core/a_spec.rb: 9 passing, baseline 10 (-1)") {
		t.Errorf("the refusal does not name the file that went backwards:\n%s", out)
	}
	// ⛔ And it must not have half-written. A refusal that truncated the file
	// would destroy the very record it declined to replace.
	if after != baseline3 {
		t.Errorf("the refused re-record changed the baseline:\n%q", after)
	}

	// -absorb is the named derogation, and it STAMPS THE ARTEFACT: the console
	// scrolls away, the baseline outlives the run by months.
	out, err, after = recordOver(t, lost, "-absorb")
	if err != nil {
		t.Fatalf("-absorb: %v\n%s", err, out)
	}
	if !strings.Contains(after, "ABSORBED 1 file(s)") || !strings.Contains(after, "core/a_spec.rb: 9 passing, baseline 10 (-1)") {
		t.Errorf("-absorb did not stamp the baseline with what it absorbed:\n%s", after)
	}
	if !strings.Contains(after, "OK\tcore/a_spec.rb\t9\n") {
		t.Errorf("-absorb did not actually record the sweep:\n%s", after)
	}

	// The CONTROL, without which the above only proves the gate always fires:
	// a re-record that merely locks in a win is accepted, and is NOT stamped.
	won := "OK\tcore/a_spec.rb\t12\nOK\tcore/b_spec.rb\t20\nFILEFAIL\tcore/c_spec.rb\n"
	out, err, after = recordOver(t, won)
	if err != nil {
		t.Fatalf("a winning re-record was refused: %v\n%s", err, out)
	}
	if strings.Contains(after, "ABSORBED") {
		t.Errorf("a clean re-record was stamped:\n%s", after)
	}
	if !strings.Contains(out, "improved (1)") {
		t.Errorf("the re-record does not report what it locked in:\n%s", out)
	}
}

// The other two ways a file goes backwards are the same refusal, and a first
// recording -- with no baseline to be worse than -- is not one at all.
func TestTheRerecordRefusalCoversEveryWayBackwards(t *testing.T) {
	for _, tc := range []struct {
		name, sweep, want string
	}{
		{
			"a file stops loading",
			"OK\tcore/a_spec.rb\t10\nFILEFAIL\tcore/b_spec.rb\nFILEFAIL\tcore/c_spec.rb\n",
			"core/b_spec.rb",
		},
		{
			"a file vanishes from the sweep",
			"OK\tcore/a_spec.rb\t10\nFILEFAIL\tcore/c_spec.rb\n",
			"core/b_spec.rb",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err, after := recordOver(t, tc.sweep)
			if !errors.Is(err, errWouldAbsorb) {
				t.Fatalf("err = %v, want errWouldAbsorb; out:\n%s", err, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the refusal does not name %s:\n%s", tc.want, out)
			}
			if after != baseline3 {
				t.Errorf("the refused re-record changed the baseline")
			}
		})
	}

	t.Run("the first recording has nothing to go backwards from", func(t *testing.T) {
		bp := filepath.Join(t.TempDir(), "BASELINE")
		var out strings.Builder
		if err := run([]string{"-baseline", bp, "-update"}, strings.NewReader(baseline3), &out); err != nil {
			t.Fatalf("a first recording was refused: %v\n%s", err, out.String())
		}
		got, err := os.ReadFile(bp)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "OK\tcore/a_spec.rb\t10\n") {
			t.Errorf("the first recording did not write the sweep:\n%s", got)
		}
	})
}

// TestRatchetMissingBaselineIsAnError: pointing at a baseline that is not there
// must fail loudly. A tool that treats an absent baseline as "nothing to beat"
// would pass forever.
func TestRatchetMissingBaselineIsAnError(t *testing.T) {
	var out strings.Builder
	err := run([]string{"-baseline", filepath.Join(t.TempDir(), "nope")}, strings.NewReader(baseline3), &out)
	if err == nil || errors.Is(err, errRegression) {
		t.Fatalf("want a tool error for a missing baseline, got %v", err)
	}
	if err := run(nil, strings.NewReader(baseline3), &out); err == nil {
		t.Error("want an error when -baseline is omitted entirely")
	}
}
