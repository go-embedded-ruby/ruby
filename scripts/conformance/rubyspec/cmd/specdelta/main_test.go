// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// parse is where a silent wrong answer would live, so it is tested first and
// hardest: every field it cannot read must be an ERROR, never a zero.
func TestParseRefusesWhatItCannotRead(t *testing.T) {
	// fail=2 + error=1 is THREE detail lines; the shim emits one per failure and
	// one per error, so the consistent case has to add up.
	good := "RBGO_RESULT pass=12 fail=2 error=1 skip=0\n"
	r, err := parse(good + "RBGO_DETAIL\tfail\tX\ta\tb\nRBGO_DETAIL\tfail\tX\tc\td\nRBGO_DETAIL\terror\tY\te\tf\n")
	if err != nil {
		t.Fatalf("parse(good): %v", err)
	}
	if r.pass != 12 || r.fail != 2 || r.errs != 1 || r.skip != 0 || !r.ran {
		t.Errorf("parse(good) = %+v", r)
	}
	if r.details != 3 {
		t.Errorf("details = %d, want 3", r.details)
	}
	if r.tallyDisagrees() {
		t.Error("fail=2 error=1 with three detail lines must agree")
	}

	// ⛔ Each of these used to be a plausible "default it to zero" case. A zero
	// that means "unreadable" and a zero that means "nothing passed" lead to
	// opposite conclusions, so neither may be guessed.
	for _, bad := range []string{
		"RBGO_RESULT pass=12 fail=3 error=1\n",          // skip missing
		"RBGO_RESULT pass=12 fail=3 skip=0\n",           // error missing
		"RBGO_RESULT fail=3 error=1 skip=0\n",           // pass missing
		"RBGO_RESULT pass=twelve fail=3 error=1 skip=0", // not a number
		"RBGO_RESULT pass fail=3 error=1 skip=0",        // no '='
		"RBGO_RESULT pass=1 fail=1 error=1 skip=1 oops=1",
		good + good, // two runs in one output
	} {
		if _, err := parse(bad); err == nil {
			t.Errorf("parse(%q) returned no error", bad)
		}
	}

	// No RESULT line at all is not an error — it is a file that did not run,
	// which the caller reports differently from one that ran and scored zero.
	r, err = parse("some warning on stdout\n")
	if err != nil {
		t.Fatalf("parse(no result): %v", err)
	}
	if r.ran {
		t.Error("a run with no RESULT line must not be marked as having run")
	}
}

// ⛔ The tally comes out of the SUBJECT's stdout, so a subject that miscounts
// can claim anything. The detail lines are the independent channel — but the
// relationship is an INEQUALITY, not an equality, and getting that wrong is how
// the first version of this check fired on a perfectly well-formed file.
//
// The shim's shape, read from spec_helper.rb rather than assumed: `fail` and
// `error` each push exactly one detail line; a guard-driven `skip` bumps the
// counter with NO detail (:970) while a runtime skip pushes one (:990); and a
// `before/after(:all)` error pushes a detail counted in NO tally (:965, :1020).
func TestDetailShortfallIsDetected(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
		flag bool
		why  string
	}{{
		name: "a missing detail line",
		out:  "RBGO_RESULT pass=1 fail=2 error=0 skip=0\nRBGO_DETAIL\tfail\tX\ta\tb\n",
		flag: true,
		why:  "fail=2 with one detail line means a line went missing",
	}, {
		name: "exactly as many as claimed",
		out:  "RBGO_RESULT pass=1 fail=1 error=0 skip=0\nRBGO_DETAIL\tfail\tX\ta\tb\n",
		flag: false,
		why:  "fail=1 with one detail line is consistent",
	}, {
		// ⛔ The row the first version got wrong: core/set/set_spec.rb, skipped
		// by a guard, is pass=0 fail=0 error=0 skip=1 with one `skip` detail.
		name: "a skip with its detail line",
		out:  "RBGO_RESULT pass=0 fail=0 error=0 skip=1\nRBGO_DETAIL\tskip\tSpecSkip\tx\ty\n",
		flag: false,
		why:  "a skip carries a detail and no failure; that is well-formed",
	}, {
		name: "a guard skip with no detail line",
		out:  "RBGO_RESULT pass=0 fail=0 error=0 skip=3\n",
		flag: false,
		why:  "spec_helper.rb:970 bumps $RB_SKIP without pushing a detail",
	}, {
		name: "a hook error, counted nowhere",
		out:  "RBGO_RESULT pass=0 fail=0 error=0 skip=0\nRBGO_DETAIL\thookerror\tX\t(before :all)\ty\n",
		flag: false,
		why:  "a hookerror detail belongs to no counter; MORE details is legitimate",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := parse(tc.out)
			if err != nil {
				t.Fatal(err)
			}
			if got := r.tallyDisagrees(); got != tc.flag {
				t.Errorf("tallyDisagrees() = %v, want %v — %s", got, tc.flag, tc.why)
			}
		})
	}
}

// An empty population is a REFUSAL. A mistyped -dir would otherwise print
// "0 files, no regressions" and exit 0, which is the most convincing way for
// this tool to tell someone their change is safe.
func TestEmptyPopulationIsRefused(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "core", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := main1([]string{"-root", root, "-move", "none", "-dir", "core/empty", "/bin/echo", "/bin/echo"}, &out, &errOut); code != 2 {
		t.Errorf("exit = %d, want 2; err %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "no _spec.rb files") {
		t.Errorf("the refusal does not say why: %q", errOut.String())
	}
}

// Usage refusals, each exit 2 — distinct from 1, which means a real regression.
func TestUsageRefusals(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"one engine", []string{"-root", root, "-move", "none", "-dir", "x", "/bin/echo"}},
		{"three engines", []string{"-root", root, "-move", "none", "-dir", "x", "/bin/echo", "/bin/echo", "/bin/echo"}},
		{"no dir", []string{"-root", root, "/bin/echo", "/bin/echo"}},
		{"absent dir", []string{"-root", root, "-move", "none", "-dir", "nope", "/bin/echo", "/bin/echo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := main1(tc.args, &out, &errOut); code != 2 {
				t.Errorf("exit = %d, want 2; out %q err %q", code, out.String(), errOut.String())
			}
		})
	}
}

// End to end against fake engines, because the tool must not know anything
// about Ruby. The control passes 2 of 3 in one file; the treatment passes 3 —
// an improvement, exit 0 — and then the reverse, which must be exit 1.
func TestReportsMovementAndRegression(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engines are POSIX shell scripts")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "core", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "x", "a_spec.rb"), []byte("#\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	two := fakeEngine(t, root, "two", "echo 'RBGO_RESULT pass=2 fail=1 error=0 skip=0'\nprintf 'RBGO_DETAIL\\tfail\\tX\\ta\\tb\\n'\n")
	three := fakeEngine(t, root, "three", "echo 'RBGO_RESULT pass=3 fail=0 error=0 skip=0'\n")

	var out, errOut bytes.Buffer
	if code := main1([]string{"-root", root, "-move", "none", "-dir", "core/x", two, three}, &out, &errOut); code != 0 {
		t.Errorf("improvement: exit = %d, want 0; err %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "+1") {
		t.Errorf("the improvement is not reported:\n%s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := main1([]string{"-root", root, "-move", "none", "-dir", "core/x", three, two}, &out, &errOut); code != 1 {
		t.Errorf("regression: exit = %d, want 1; err %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "-1") || !strings.Contains(out.String(), "1 regressed") {
		t.Errorf("the regression is not reported:\n%s", out.String())
	}
}

// The loudest movement prints first, whichever direction it points, because a
// reader wants the biggest change and not the alphabetically first one. With a
// single file the comparator is never called, so this needs two — and it is the
// documented ordering, which is reason enough to pin it.
func TestTheBiggestMovementIsReportedFirst(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engines are POSIX shell scripts")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "core", "w"), 0o755); err != nil {
		t.Fatal(err)
	}
	// `zz` moves by one and `aa` by five, so alphabetical order and movement
	// order disagree — which is what makes the assertion mean anything.
	for name, body := range map[string]string{
		"aa_spec.rb": "#\n",
		"zz_spec.rb": "#\n",
	} {
		if err := os.WriteFile(filepath.Join(root, "core", "w", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// One engine scores by file name; the other adds 5 to aa and 1 to zz.
	low := fakeEngine(t, root, "low", `case "$1" in
  *aa_spec.rb) echo 'RBGO_RESULT pass=10 fail=0 error=0 skip=0' ;;
  *)           echo 'RBGO_RESULT pass=10 fail=0 error=0 skip=0' ;;
esac
`)
	high := fakeEngine(t, root, "high", `case "$1" in
  *aa_spec.rb) echo 'RBGO_RESULT pass=15 fail=0 error=0 skip=0' ;;
  *)           echo 'RBGO_RESULT pass=11 fail=0 error=0 skip=0' ;;
esac
`)
	var out, errOut bytes.Buffer
	if code := main1([]string{"-root", root, "-move", "none", "-dir", "core/w", low, high}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, want 0; err %q", code, errOut.String())
	}
	iAA := strings.Index(out.String(), "aa_spec.rb")
	iZZ := strings.Index(out.String(), "zz_spec.rb")
	if iAA < 0 || iZZ < 0 {
		t.Fatalf("both files must appear:\n%s", out.String())
	}
	if iAA > iZZ {
		t.Errorf("+5 must print before +1:\n%s", out.String())
	}
}

// A file that produces no RESULT line is named as such rather than counted as
// zero — the distinction the ratchet's "FAILED TO LOAD" verdict exists for, and
// the one that made an endless range look like a load failure in #853.
func TestAFileThatDidNotRunIsNamed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engines are POSIX shell scripts")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "core", "y"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "y", "b_spec.rb"), []byte("#\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok := fakeEngine(t, root, "ok", "echo 'RBGO_RESULT pass=1 fail=0 error=0 skip=0'\n")
	mute := fakeEngine(t, root, "mute", "exit 1\n")

	var out, errOut bytes.Buffer
	main1([]string{"-root", root, "-move", "none", "-dir", "core/y", ok, mute}, &out, &errOut)
	if !strings.Contains(out.String(), "DID NOT RUN on the treatment") {
		t.Errorf("a silent engine is not named:\n%s", out.String())
	}
}

// ⛔ A single run per engine cannot tell a FLAKE from a difference. The first
// real use of this tool reported `core/random/bytes_spec.rb` as -1 between two
// binaries differing only in a version string; three runs of each gave control
// 9, 9, 8 and treatment 8, 9, 9 — a coin flip, attributed to the change.
//
// So above `-repeat 1` an engine that disagrees with ITSELF marks the file
// UNSTABLE, its delta is excluded from the verdict, and the exit status stops
// depending on which way the coin landed.
func TestAnUnstableFileIsExcludedFromTheVerdict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engines are POSIX shell scripts")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "core", "z"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "z", "c_spec.rb"), []byte("#\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stable := fakeEngine(t, root, "stable", "echo 'RBGO_RESULT pass=9 fail=0 error=0 skip=0'\n")
	// Scores 8 the first time it is asked and 9 after: a flake, in the direction
	// that looks like a regression on the first run.
	flaky := fakeEngine(t, root, "flaky", `if [ -f "`+root+`/seen" ]; then
  echo 'RBGO_RESULT pass=9 fail=0 error=0 skip=0'
else
  : > "`+root+`/seen"
  echo 'RBGO_RESULT pass=8 fail=1 error=0 skip=0'
  printf 'RBGO_DETAIL\tfail\tX\ta\tb\n'
fi
`)

	// With one run the flake reads as a regression — which is the defect this
	// flag exists for, asserted so the difference between the two modes is
	// visible rather than claimed.
	var out, errOut bytes.Buffer
	if code := main1([]string{"-root", root, "-move", "none", "-dir", "core/z", stable, flaky}, &out, &errOut); code != 1 {
		t.Errorf("-repeat 1: exit = %d, want 1 (the flake reads as a regression); err %q", code, errOut.String())
	}

	// With three, the engine contradicts itself and the file is set aside.
	if err := os.Remove(filepath.Join(root, "seen")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := main1([]string{"-root", root, "-move", "none", "-dir", "core/z", "-repeat", "3", stable, flaky}, &out, &errOut); code != 0 {
		t.Errorf("-repeat 3: exit = %d, want 0 (unstable, excluded); out %q", code, out.String())
	}
	if !strings.Contains(out.String(), "UNSTABLE") {
		t.Errorf("the unstable file is not named:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "0 regressed") {
		t.Errorf("an excluded delta still counted as a regression:\n%s", out.String())
	}

	// A stable engine must NOT be called unstable, or every file would be.
	out.Reset()
	errOut.Reset()
	if code := main1([]string{"-root", root, "-move", "none", "-dir", "core/z", "-repeat", "3", stable, stable}, &out, &errOut); code != 0 {
		t.Errorf("stable pair: exit = %d, want 0; out %q", code, out.String())
	}
	if strings.Contains(out.String(), "UNSTABLE") {
		t.Errorf("a stable engine was called unstable:\n%s", out.String())
	}

	// And -repeat below 1 is a usage refusal, not a silent no-op.
	out.Reset()
	errOut.Reset()
	if code := main1([]string{"-root", root, "-move", "none", "-dir", "core/z", "-repeat", "0", stable, stable}, &out, &errOut); code != 2 {
		t.Errorf("-repeat 0: exit = %d, want 2; err %q", code, errOut.String())
	}
}

// ⛔ A file can get WORSE without its pass count moving, and the first version
// of this tool could not see it.
//
// Three `core/binding/implicit_parameter*_spec.rb` files report
// `pass=0 fail=0 error=0` at 3.4.1 — every example behind a `ruby_version_is
// "4.0"` guard — and **37 failures** at 4.0.9, where the guard opens and the
// three new Binding methods do not exist. Delta 0, absent from the report, and
// the measurement that missed them was used to size a plan: "ten examples of
// work" was short by those 37.
func TestAFileThatGainsFailuresWithoutLosingPassesIsNamed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engines are POSIX shell scripts")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "core", "v"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "v", "d_spec.rb"), []byte("#\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Guarded out on the control: no examples at all, nothing to pass or fail.
	guarded := fakeEngine(t, root, "guarded", "echo 'RBGO_RESULT pass=0 fail=0 error=0 skip=0'\n")
	// The guard opens on the treatment and every example fails.
	opened := fakeEngine(t, root, "opened", "echo 'RBGO_RESULT pass=0 fail=3 error=2 skip=0'\n"+
		"printf 'RBGO_DETAIL\\tfail\\tX\\ta\\tb\\n%.0s' 1 2 3\n"+
		"printf 'RBGO_DETAIL\\terror\\tY\\tc\\td\\n%.0s' 1 2\n")

	var out, errOut bytes.Buffer
	main1([]string{"-root", root, "-move", "none", "-dir", "core/v", "-quiet", guarded, opened}, &out, &errOut)
	if !strings.Contains(out.String(), "fail+error 0 → 5") {
		t.Errorf("the new failures are not named:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "1 gained failures without losing passes") {
		t.Errorf("the summary does not count it:\n%s", out.String())
	}
	// ⛔ And -quiet must NOT hide it: a delta of 0 was exactly how it hid before.
	if !strings.Contains(out.String(), "d_spec.rb") {
		t.Errorf("-quiet suppressed a file that got worse:\n%s", out.String())
	}

	// The control: a file that is quiet on both engines stays out of a -quiet
	// report, or every unchanged file would be printed.
	out.Reset()
	main1([]string{"-root", root, "-move", "none", "-dir", "core/v", "-quiet", guarded, guarded}, &out, &errOut)
	if strings.Contains(out.String(), "d_spec.rb") {
		t.Errorf("an unchanged file was printed under -quiet:\n%s", out.String())
	}
}

// ⛔ THE GATE. -move has no default, because the results cannot answer it: a
// run across a version move and a run within one produce the same table, and
// only one of them is a reading of the engines.
//
// Without it the tool refuses, and the refusal has to come BEFORE the engines
// run — a sweep of core/ is an hour, and an hour spent on a figure already
// known to be incomplete is the cost this gate exists to avoid. The fake engine
// here records every call, so "did not run" is asserted rather than assumed.
func TestAMoveMustBeDeclaredAndTheRefusalPrecedesTheRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engines are POSIX shell scripts")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "core", "q"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "q", "e_spec.rb"), []byte("#\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ran := filepath.Join(root, "ran")
	engine := fakeEngine(t, root, "counting", "echo x >> '"+ran+"'\necho 'RBGO_RESULT pass=1 fail=0 error=0 skip=0'\n")

	var out, errOut bytes.Buffer
	if code := main1([]string{"-root", root, "-dir", "core/q", engine, engine}, &out, &errOut); code != 2 {
		t.Errorf("a missing -move: exit = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "-move is required") {
		t.Errorf("the refusal does not say what is missing:\n%s", errOut.String())
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("an engine was started before the declaration was checked")
	}

	// And the malformed spellings, each exit 2 rather than a silent "no move".
	for _, bad := range []string{"3.4.1", "3.4.1:", ":4.0.9", "4.0.9:4.0.9", "4.0:4.0.0"} {
		out.Reset()
		errOut.Reset()
		if code := main1([]string{"-root", root, "-move", bad, "-dir", "core/q", engine, engine}, &out, &errOut); code != 2 {
			t.Errorf("-move %q: exit = %d, want 2; err %q", bad, code, errOut.String())
		}
	}
}

// ⛔ THE REFUSAL THAT WOULD HAVE CAUGHT THE PUBLISHED FIGURE. The command is
// well-formed and the table it produces is readable; it simply is not a table
// about the move, because the move's heaviest files are under a directory -dir
// never named. There is nothing in the RESULTS that could say so.
func TestAMoveReachingOutsideTheMeasuredSetIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engines are POSIX shell scripts")
	}
	root := t.TempDir()
	for _, d := range []string{"core/set", "core/binding"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "core", "set", "divide_spec.rb"), []byte("#\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The file the move opens, and the one -dir core/set cannot see.
	if err := os.WriteFile(filepath.Join(root, "core", "binding", "implicit_parameters_spec.rb"), []byte(
		"describe \"Binding#implicit_parameters\" do\n"+
			"  ruby_version_is \"4.0\" do\n"+
			"    it \"exists\" do\n"+
			"    end\n"+
			"  end\n"+
			"end\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := fakeEngine(t, root, "e", "echo 'RBGO_RESULT pass=1 fail=0 error=0 skip=0'\n")

	var out, errOut bytes.Buffer
	if code := main1([]string{"-root", root, "-move", "3.4.1:4.0.9", "-dir", "core/set", engine, engine}, &out, &errOut); code != 2 {
		t.Fatalf("exit = %d, want 2; err %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "core/binding/implicit_parameters_spec.rb") {
		t.Errorf("the refusal does not name the file it cannot see:\n%s", errOut.String())
	}

	// -partial is the named derogation, and it STAMPS the report rather than
	// silencing the gate: an opt-in that only removes a message would leave the
	// same number published with nothing attached to it.
	out.Reset()
	errOut.Reset()
	if code := main1([]string{"-root", root, "-move", "3.4.1:4.0.9", "-partial", "-dir", "core/set", engine, engine}, &out, &errOut); code != 0 {
		t.Fatalf("-partial: exit = %d, want 0; err %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "PARTIAL") {
		t.Errorf("-partial did not stamp the report:\n%s", out.String())
	}

	// And widening -dir to cover the surface is accepted, which is the control:
	// without it the refusal above would only prove the gate always fires.
	out.Reset()
	errOut.Reset()
	if code := main1([]string{"-root", root, "-move", "3.4.1:4.0.9", "-dir", "core", engine, engine}, &out, &errOut); code != 0 {
		t.Fatalf("widened -dir: exit = %d, want 0; err %q", code, errOut.String())
	}
	if strings.Contains(out.String(), "PARTIAL") {
		t.Errorf("a complete run was stamped PARTIAL:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "THE MOVE OPENS") {
		t.Errorf("the opened file is not marked in the table:\n%s", out.String())
	}
}

// A file the move opens is marked and kept even when nothing moved, because a
// delta of zero on such a file is the signature of the defect, not of calm.
func TestAFileTheMoveOpensSurvivesQuiet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engines are POSIX shell scripts")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "core", "binding"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "binding", "implicit_parameters_spec.rb"), []byte(
		"ruby_version_is \"4.0\" do\n  it \"a\" do\n  end\n  it \"b\" do\n  end\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Both engines score the same: delta zero, and under -quiet the row would
	// have been dropped before this gate existed.
	engine := fakeEngine(t, root, "e", "echo 'RBGO_RESULT pass=0 fail=0 error=0 skip=0'\n")

	var out, errOut bytes.Buffer
	if code := main1([]string{"-root", root, "-move", "3.4.1:4.0.9", "-quiet", "-dir", "core/binding", engine, engine}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, want 0; err %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "implicit_parameters_spec.rb") {
		t.Errorf("-quiet dropped a file the move opens:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "≥2 examples") {
		t.Errorf("the opened example count is not reported:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "the declared move 3.4.1 → 4.0.9 accounts for 1 file(s)") {
		t.Errorf("the summary does not separate the move's share:\n%s", out.String())
	}

	// The control: declared as no move, the same run under -quiet prints
	// nothing about the file — which is what makes the line above a reading of
	// the declaration rather than of the file name.
	out.Reset()
	if code := main1([]string{"-root", root, "-move", "none", "-quiet", "-dir", "core/binding", engine, engine}, &out, &errOut); code != 0 {
		t.Fatalf("control: exit = %d, want 0; err %q", code, errOut.String())
	}
	if strings.Contains(out.String(), "implicit_parameters_spec.rb") {
		t.Errorf("a quiet file was printed with no move declared:\n%s", out.String())
	}
}

func fakeEngine(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}
