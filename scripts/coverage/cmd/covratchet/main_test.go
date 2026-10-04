package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A record of two partially-covered functions, and a profile that reproduces it
// exactly. Every case below differs from this by one thing.
const record2 = "internal/vm/a.go\tfoo\t80.0%\ninternal/vm/b.go\tbar\t90.0%\n"
const profile2 = "github.com/go-embedded-ruby/ruby/internal/vm/a.go:10:\tfoo\t80.0%\n" +
	"github.com/go-embedded-ruby/ruby/internal/vm/b.go:20:\tbar\t90.0%\n" +
	"github.com/go-embedded-ruby/ruby/internal/vm/c.go:30:\tbaz\t100.0%\n" +
	"total:\t(statements)\t100.0%\n"

func judge(t *testing.T, profile string, extra ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	rp := filepath.Join(dir, "BELOW100.test")
	if err := os.WriteFile(rp, []byte(record2), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err := run(append([]string{"-record", rp}, extra...), strings.NewReader(profile), &out)
	return out.String(), err
}

// TestCovRatchetVerdicts: the answer must depend on the input, and in
// particular a function newly below 100% must fail EVEN WHEN the rounded total
// still reads 100.0% -- which is the whole defect this replaces. On main both
// POSIX lanes pass at "100.0%" while listing 19 and 16 such functions.
func TestCovRatchetVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile string
		wantBad bool
		want    []string
		absent  []string
	}{
		{
			name:    "unchanged",
			profile: profile2,
			want:    []string{"no function newly below 100%", "2 measured, 2 recorded"},
		},
		{
			// The case the rounded gate cannot see: one function drops to 0%
			// and the total still prints 100.0%.
			name: "a function newly below 100%, total still rounds to 100.0%",
			profile: strings.Replace(profile2,
				"internal/vm/c.go:30:\tbaz\t100.0%", "internal/vm/c.go:30:\tbaz\t0.0%", 1),
			wantBad: true,
			want:    []string{"NEWLY BELOW 100% (1)", "internal/vm/c.go baz  0.0% (was fully covered)"},
		},
		{
			name:    "a recorded function falls further",
			profile: strings.Replace(profile2, "foo\t80.0%", "foo\t50.0%", 1),
			want:    []string{"further below than recorded (1)", "internal/vm/a.go foo  50.0%, recorded 80.0%"},
			// Falling further is REPORTED, not gated: coverage is measured under
			// -race, where a percentage jitters. Only the set is the gate.
			absent: []string{"NEWLY BELOW"},
		},
		{
			name:    "a recorded function reaches 100%",
			profile: strings.Replace(profile2, "foo\t80.0%", "foo\t100.0%", 1),
			want:    []string{"now fully covered (1)", "internal/vm/a.go foo  now 100% (was 80.0%)", "UPDATE_COVERAGE=1"},
		},
		{
			// A line shift must NOT read as a removal plus an addition. A
			// 12-line comment above a function moves every function below it;
			// keying on the line faked ~600 of each.
			name:    "the same functions at different lines",
			profile: strings.ReplaceAll(strings.ReplaceAll(profile2, "a.go:10:", "a.go:999:"), "b.go:20:", "b.go:888:"),
			want:    []string{"no function newly below 100%"},
			absent:  []string{"NEWLY BELOW", "now fully covered"},
		},
		{
			// Jitter under the gate's own threshold must not be reported as a
			// fall, or the gate teaches people to rerun it.
			name:    "jitter of 0.05 is not a fall",
			profile: strings.Replace(profile2, "foo\t80.0%", "foo\t79.99%", 1),
			absent:  []string{"further below"},
			want:    []string{"no function newly below 100%"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := judge(t, tc.profile)
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

// TestCovRatchetRefusesWhatItCannotRead: a profile this tool cannot read must
// not pass as a clean lane. Each of these is exit 2, distinct from exit 1 for a
// real regression, so a broken ratchet is not mistaken for a failing one.
func TestCovRatchetRefusesWhatItCannotRead(t *testing.T) {
	for _, tc := range []struct{ name, profile, want string }{
		{"empty", "", "no total: line"},
		{"truncated, no total", "github.com/go-embedded-ruby/ruby/a.go:1:\tf\t50.0%\n", "no total: line"},
		{"not a cover record", "hello world\n", "not a cover -func record"},
		{"percentage is not a number", "github.com/go-embedded-ruby/ruby/a.go:1:\tf\tmany%\ntotal:\t(statements)\t100.0%\n", "is not a percentage"},
		{"same function twice", "github.com/go-embedded-ruby/ruby/a.go:1:\tf\t50.0%\ngithub.com/go-embedded-ruby/ruby/a.go:9:\tf\t60.0%\ntotal:\t(statements)\t100.0%\n", "appears twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := judge(t, tc.profile)
			if err == nil || errors.Is(err, errRegression) {
				t.Fatalf("want a tool error, got %v\n%s", err, out)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
	// A record that is not there must fail loudly: treating an absent record as
	// "nothing to beat" would pass forever.
	var out strings.Builder
	if err := run([]string{"-record", filepath.Join(t.TempDir(), "nope")}, strings.NewReader(profile2), &out); err == nil || errors.Is(err, errRegression) {
		t.Errorf("want a tool error for a missing record, got %v", err)
	}
	if err := run(nil, strings.NewReader(profile2), &out); err == nil {
		t.Error("want an error when -record is omitted entirely")
	}
}

// TestCovRatchetUpdateRoundTrips: -update writes a record this tool reads back
// and accepts, sorted, so a regenerated record diffs cleanly.
func TestCovRatchetUpdateRoundTrips(t *testing.T) {
	dir := t.TempDir()
	rp := filepath.Join(dir, "BELOW100.test")
	if err := os.WriteFile(rp, []byte(record2), 0o644); err != nil {
		t.Fatal(err)
	}
	shuffled := "github.com/go-embedded-ruby/ruby/internal/vm/b.go:20:\tbar\t90.0%\n" +
		"github.com/go-embedded-ruby/ruby/internal/vm/a.go:10:\tfoo\t80.0%\n" +
		"total:\t(statements)\t100.0%\n"
	var out strings.Builder
	if err := run([]string{"-record", rp, "-lane", "test", "-update"}, strings.NewReader(shuffled), &out); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := os.ReadFile(rp)
	if err != nil {
		t.Fatal(err)
	}
	var body []string
	for _, l := range strings.Split(strings.TrimRight(string(got), "\n"), "\n") {
		if !strings.HasPrefix(l, "#") {
			body = append(body, l)
		}
	}
	want := []string{"internal/vm/a.go\tfoo\t80.0%", "internal/vm/b.go\tbar\t90.0%"}
	if strings.Join(body, "|") != strings.Join(want, "|") {
		t.Errorf("record written as %q, want %q (sorted, module prefix stripped)", body, want)
	}
	var out2 strings.Builder
	if err := run([]string{"-record", rp}, strings.NewReader(shuffled), &out2); err != nil {
		t.Errorf("the regenerated record rejects its own profile: %v\n%s", err, out2.String())
	}
}

// TestRecordedLanesParse is the check that the records checked in beside this
// tool are readable by it, and that the two POSIX lanes differ only where a
// platform difference explains it. ubuntu carries three filesystem-dependent
// functions macOS does not, which is exactly why one number could not serve
// both lanes: on PR #727 the same commit read 99.9% on ubuntu and 100.0% on
// macOS.
func TestRecordedLanesParse(t *testing.T) {
	sets := map[string]map[fn]entry{}
	for _, lane := range []string{"ubuntu-latest", "macos-latest"} {
		f, err := os.Open(filepath.Join("..", "..", "BELOW100."+lane))
		if err != nil {
			t.Fatalf("the record for %s must exist and be readable: %v", lane, err)
		}
		m, err := parseRecord(f, lane)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", lane, err)
		}
		if len(m) == 0 {
			t.Fatalf("%s: empty record -- an empty gate passes forever", lane)
		}
		sets[lane] = m
	}
	for k := range sets["macos-latest"] {
		if _, ok := sets["ubuntu-latest"][k]; !ok {
			t.Errorf("%s is below 100%% on macOS but not recorded on ubuntu; the macOS set was measured as a strict subset", k)
		}
	}
	if len(sets["ubuntu-latest"]) <= len(sets["macos-latest"]) {
		t.Errorf("ubuntu (%d) should carry more than macOS (%d): the POSIX-only filesystem paths",
			len(sets["ubuntu-latest"]), len(sets["macos-latest"]))
	}
}

// TestSameNamedFunctionsInOneFile: Go methods carry a receiver the coverage
// profile does not print, so (file, name) is not an identity --
// internal/vm/csv.go has three `ToS()`, on *CSVRow, *CSVTable and *csvSink.
// Keying on the pair made the second one read as a duplicate record and this
// tool refused the whole profile as unreadable:
//
//	covratchet: profile line 1871: internal/vm/csv.go ToS appears twice (was 0.0%)
//	exit status 2
//
// It needed TWO of them below 100% at once, because functions at 100% are
// skipped -- so it lay dormant until a change pushed a second one under, and
// then stopped the lane rather than answering wrongly.
func TestSameNamedFunctionsInOneFile(t *testing.T) {
	const prof = `github.com/x/y/internal/vm/csv.go:37:	ToS	0.0%
github.com/x/y/internal/vm/csv.go:54:	ToS	50.0%
github.com/x/y/internal/vm/csv.go:280:	ToS	100.0%
github.com/x/y/internal/vm/io.go:10:	displayStr	90.0%
total:	(statements)	99.9%
`
	got, err := parse(strings.NewReader(prof), "profile", "github.com/x/y")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Two of the three ToS are below 100 and must be two distinct records; the
	// third is skipped but still CONSUMES its ordinal, so the survivors keep
	// their numbers when a sibling crosses the line.
	want := map[string]float64{
		"/internal/vm/csv.go ToS":       0.0,
		"/internal/vm/csv.go ToS#2":     50.0,
		"/internal/vm/io.go displayStr": 90.0,
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d records, want %d: %v", len(got), len(want), got)
	}
	for k, e := range got {
		w, ok := want[k.String()]
		if !ok {
			t.Errorf("unexpected record %q", k.String())
			continue
		}
		if e.pct != w {
			t.Errorf("%s: %.1f%%, want %.1f%%", k.String(), e.pct, w)
		}
	}
}

// TestNameColumnRoundTrips: the ordinal travels in the function column, so a
// record written by one run is read back identically by the next.
func TestNameColumnRoundTrips(t *testing.T) {
	for _, k := range []fn{
		{file: "a.go", name: "ToS", ord: 0},
		{file: "a.go", name: "ToS", ord: 1},
		{file: "a.go", name: "ToS", ord: 11},
	} {
		col := k.nameCol()
		name, ord, err := parseNameCol(col)
		if err != nil {
			t.Fatalf("%q: %v", col, err)
		}
		if name != k.name || ord != k.ord {
			t.Errorf("%q round-tripped to (%q, %d), want (%q, %d)", col, name, ord, k.name, k.ord)
		}
	}
	// A malformed column is refused rather than read as a name containing '#'.
	for _, bad := range []string{"ToS#", "ToS#x", "ToS#0", "ToS#1"} {
		if _, _, err := parseNameCol(bad); err == nil {
			t.Errorf("parseNameCol(%q) was accepted; N must be an integer >= 2", bad)
		}
	}
}

// TestOrdinalSurvivesALineShift is the property the line was rejected for: the
// identity must not move when a comment is inserted above a function.
func TestOrdinalSurvivesALineShift(t *testing.T) {
	before := `github.com/x/y/a.go:10:	ToS	0.0%
github.com/x/y/a.go:20:	ToS	50.0%
total:	(statements)	99.9%
`
	after := `github.com/x/y/a.go:110:	ToS	0.0%
github.com/x/y/a.go:120:	ToS	50.0%
total:	(statements)	99.9%
`
	b, err := parse(strings.NewReader(before), "before", "github.com/x/y")
	if err != nil {
		t.Fatalf("before: %v", err)
	}
	a, err := parse(strings.NewReader(after), "after", "github.com/x/y")
	if err != nil {
		t.Fatalf("after: %v", err)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			t.Errorf("%s vanished when its line moved; the identity still depends on the line", k)
		}
	}
	if len(a) != len(b) {
		t.Errorf("record count changed across a pure line shift: %d -> %d", len(b), len(a))
	}
}
