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
	if code := main1([]string{"-root", root, "-dir", "core/empty", "/bin/echo", "/bin/echo"}, &out, &errOut); code != 2 {
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
		{"one engine", []string{"-root", root, "-dir", "x", "/bin/echo"}},
		{"three engines", []string{"-root", root, "-dir", "x", "/bin/echo", "/bin/echo", "/bin/echo"}},
		{"no dir", []string{"-root", root, "/bin/echo", "/bin/echo"}},
		{"absent dir", []string{"-root", root, "-dir", "nope", "/bin/echo", "/bin/echo"}},
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
	if code := main1([]string{"-root", root, "-dir", "core/x", two, three}, &out, &errOut); code != 0 {
		t.Errorf("improvement: exit = %d, want 0; err %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "+1") {
		t.Errorf("the improvement is not reported:\n%s", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := main1([]string{"-root", root, "-dir", "core/x", three, two}, &out, &errOut); code != 1 {
		t.Errorf("regression: exit = %d, want 1; err %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "-1") || !strings.Contains(out.String(), "1 regressed") {
		t.Errorf("the regression is not reported:\n%s", out.String())
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
	main1([]string{"-root", root, "-dir", "core/y", ok, mute}, &out, &errOut)
	if !strings.Contains(out.String(), "DID NOT RUN on the treatment") {
		t.Errorf("a silent engine is not named:\n%s", out.String())
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
