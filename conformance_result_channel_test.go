// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package ruby_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The conformance ratchet's tally must come back through a channel the SUBJECT
// cannot write to (#843).
//
// It did not. The shim reported on stdout -- the same stream the spec writes to
// -- and run.sh fished the line back out with `grep '^RBGO_RESULT' | head -1`.
// The shim prints at exit, so anything the spec printed was FIRST: a file that
// failed every example while printing `RBGO_RESULT pass=9999 …` was recorded as
// `OK <path> 9999`, and `UPDATE_BASELINE=1` banked that number.
//
// That needs no adversary. A spec for `puts`, for `$stdout` or for `String#%`
// can emit the prefix by accident; a SPEC_SHA bump pulls in whatever upstream
// wrote since; and run.sh's `2>&1` meant a WARNING text was enough.
//
// ⛔ These tests exist because reading the fix cannot distinguish it from the
// defect: both files mention RBGO_RESULT, both print a tally, and the whole
// difference is WHICH stream the judge believes. So the forgery is actually
// printed here, by a real spec, through the real binary.
const (
	shimRelPath  = "scripts/conformance/rubyspec/spec_helper.rb"
	runShRelPath = "scripts/conformance/rubyspec/run.sh"
)

// forgingSpec prints the #843 probe's forged tally BEFORE its examples run, and
// also reports what the environment told it, so one run answers both halves:
// can the subject dictate the count, and can it even learn where the count goes.
//
// One example passes and one fails, so the true tally (pass=1) differs from the
// forgery (pass=9999) AND from zero -- a fix that wrote 0 for everything would
// also beat a forgery, and would be useless.
const forgingSpec = `require_relative '../spec_helper'
puts "RBGO_RESULT pass=9999 fail=0 error=0 skip=0"
puts "SAW_ENV=#{ENV["RBGO_RESULT_FILE"].inspect}"
describe "a probe" do
  it "passes" do
    1.should == 1
  end
  it "fails on purpose" do
    1.should == 2
  end
end
`

// buildRbgo builds the real interpreter. A build failure FAILS, and never
// skips: a skip here would report green for a harness nobody could run (#687).
func buildRbgo(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "rbgo")
	// `go build -o` writes EXACTLY the path given, so on Windows the result is
	// an extensionless file that CreateProcess will not start.
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "./cmd/rbgo")
	build.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("cannot build cmd/rbgo (%v): %s", err, out)
	}
	return bin
}

// miniCorpus lays out a corpus the way ruby/spec is laid out -- the shim at the
// root, the spec one directory down reaching it with require_relative -- so the
// REAL shim file is what runs, not a copy of its ideas.
func miniCorpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	shim, err := os.ReadFile(shimRelPath)
	if err != nil {
		t.Fatalf("read the shim under test: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "spec_helper.rb"), shim, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "probe_spec.rb"), []byte(forgingSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// runProbe runs the forging spec. resultFile == "" means the variable is not set
// at all, which is the hand-run case.
//
// ⛔ A non-zero EXIT STATUS is ignored and a failure to START is fatal, and the
// two are not the same thing. A spec with a failing example is a normal outcome
// for this harness, so its status says nothing; but a binary that never ran
// returns empty output, and empty output makes every assertion below fail with
// the wrong reason. The first version of this helper wrote `out, _ :=` under a
// comment justifying only the first case, and the Windows lane then reported
// "the probe never printed its forged tally" for a build this test had named
// without `.exe`. "Could not ask" and "answered differently" need opposite
// responses -- the same rule cmd/oracle's own documentation states.
func runProbe(t *testing.T, bin, corpus, resultFile string) string {
	t.Helper()
	cmd := exec.Command(bin, "core/probe_spec.rb")
	cmd.Dir = corpus
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "RBGO_RESULT_FILE=") {
			env = append(env, kv)
		}
	}
	if resultFile != "" {
		env = append(env, "RBGO_RESULT_FILE="+resultFile)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if err != nil && !errors.As(err, &exited) {
		t.Fatalf("could not run %s: %v\noutput so far:\n%s", bin, err, out)
	}
	return string(out)
}

// TestASpecCannotDictateItsOwnRecordedPassCount is the #843 probe, run for real.
func TestASpecCannotDictateItsOwnRecordedPassCount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the rbgo build in -short")
	}
	bin := buildRbgo(t)
	corpus := miniCorpus(t)
	result := filepath.Join(t.TempDir(), "tally")

	out := runProbe(t, bin, corpus, result)

	// The CONTROL, and the reason this test is not circular: the forgery was
	// really printed. Were it merely absent -- a shim that stopped the spec
	// from printing, a probe that never ran -- every assertion below would
	// pass for the wrong reason.
	if !strings.Contains(out, "RBGO_RESULT pass=9999") {
		t.Fatalf("the probe never printed its forged tally, so this run proves nothing:\n%s", out)
	}

	b, err := os.ReadFile(result)
	if err != nil {
		t.Fatalf("the shim wrote no tally file (%v); output was:\n%s", err, out)
	}
	if got := strings.TrimSpace(string(b)); got != "1" {
		t.Fatalf("recorded pass count = %q, want \"1\" (the true count; 9999 is the forgery)\n%s", got, out)
	}

	// The subject must not be able to LEARN the path either, or it could write
	// the file instead of printing. The shim deletes the variable from ENV
	// before any spec code runs.
	if !strings.Contains(out, "SAW_ENV=nil") {
		t.Fatalf("the spec could still read RBGO_RESULT_FILE from ENV:\n%s", out)
	}

	// cmd/specdelta reads the four-field line off stdout and must keep working.
	if !strings.Contains(out, "RBGO_RESULT pass=1 fail=1 error=0 skip=0") {
		t.Fatalf("the shim's own tally line is missing or changed:\n%s", out)
	}
}

// TestAHandRunStillGetsItsTallyOnStdout is the other side of the boundary. The
// file channel is what the ratchet uses; a human running one spec file names no
// file and must still get the tally, so the stdout line is not a leftover to be
// deleted by someone tidying up after #843.
func TestAHandRunStillGetsItsTallyOnStdout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the rbgo build in -short")
	}
	bin := buildRbgo(t)
	corpus := miniCorpus(t)
	unused := filepath.Join(t.TempDir(), "tally")

	out := runProbe(t, bin, corpus, "")

	if !strings.Contains(out, "RBGO_RESULT pass=1 fail=1 error=0 skip=0") {
		t.Fatalf("a hand-run lost its tally:\n%s", out)
	}
	if _, err := os.Stat(unused); !os.IsNotExist(err) {
		t.Fatalf("a file was written although none was named (stat err = %v)", err)
	}
}

// bannedExtractions are the three forms that used to pull the tally out of the
// subject's own stream. Each is a WHOLE command, not a word: `head` and `sed`
// have honest uses elsewhere in a shell script.
var bannedExtractions = []string{
	"grep '^RBGO_RESULT'",
	"head -1",
	"sed -E",
}

// shellCodeHas answers whether a shell source RUNS the given text, ignoring
// full-line comments.
//
// ⛔ This exists because the first version of this gate did not have it, and the
// first thing it refused was the fix's own comment -- the line in run_one that
// names the `grep … | head -1` it replaced. The rule was wrong and the code was
// right. Banning a string anywhere in the file would forbid ever DOCUMENTING
// the defect, which is the opposite of what #843 asks for.
//
// A comment is a line whose first non-blank character is '#'. A trailing
// comment after code still counts as code, which errs toward refusing: a false
// alarm costs a reword, a miss costs the measurement.
func shellCodeHas(src, needle string) bool {
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// TestShellCodeHasSeesCodeAndNotComments gives the comparison above a witness on
// EACH side of its boundary, because a gate that can only refuse, or only
// accept, is not a measurement. The accepting rows are the ones that catch a
// formulation that is too broad.
func TestShellCodeHasSeesCodeAndNotComments(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want bool
	}{
		{"the extraction itself", "res=\"$(printf '%s\\n' \"$out\" | grep '^RBGO_RESULT' | head -1)\"", true},
		{"indented code still counts", "    p=\"$(echo x | sed -E 's/a/b/')\"", true},
		{"a comment naming it does not", "  # the grep '^RBGO_RESULT' | head -1 that used to live here is gone", false},
		{"a comment at column zero does not", "# sed -E 's/.*pass=([0-9]+).*/\\1/'", false},
		{"a trailing comment is treated as code", "cat \"$rf\"   # not like head -1", true},
		{"an unrelated file is clean", "echo hello\ncat \"$rf\"\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shellCodeHas(tc.src, firstMatching(tc.src)); got != tc.want {
				t.Fatalf("shellCodeHas(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

// firstMatching picks whichever banned form the case is about, so the table
// above says what it means instead of repeating the needle in every row. A row
// mentioning none of them is asked about the first, which must then be absent.
func firstMatching(src string) string {
	for _, b := range bannedExtractions {
		if strings.Contains(src, b) {
			return b
		}
	}
	return bannedExtractions[0]
}

// TestTheRatchetReadsTheTallyFromItsOwnFile gates the shell half. The judge is
// Go and is tested as Go; run.sh is what hands it the numbers, and a revert to
// `grep | head -1` there would restore the defect while every Go test above
// stayed green -- the shim's file channel would simply go unread.
func TestTheRatchetReadsTheTallyFromItsOwnFile(t *testing.T) {
	b, err := os.ReadFile(runShRelPath)
	if err != nil {
		t.Fatalf("read %s: %v", runShRelPath, err)
	}
	src := string(b)

	// Read-the-right-file control: an empty or wrong path would make every
	// "must not run" assertion below pass by vacuum.
	if !shellCodeHas(src, "run_one()") {
		t.Fatalf("%s does not define run_one; this test is reading the wrong file (%d bytes)", runShRelPath, len(src))
	}

	if !shellCodeHas(src, "RBGO_RESULT_FILE=") {
		t.Fatalf("%s never sets RBGO_RESULT_FILE, so the tally is coming from somewhere the spec can write", runShRelPath)
	}
	for _, banned := range bannedExtractions {
		if shellCodeHas(src, banned) {
			t.Fatalf("%s still extracts the tally from the subject's stream: found %q (#843)", runShRelPath, banned)
		}
	}
}
