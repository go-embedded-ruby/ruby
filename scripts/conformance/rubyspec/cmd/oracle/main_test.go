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

// The tool's whole promise is that nothing is lost between the interpreter and
// the reader, so the tests are about what a redirection or a pipe would have
// dropped: the exit status, the stream a pattern did not match, and the
// difference between "could not ask" and "answered differently".
//
// The engines here are /bin/sh scripts rather than Ruby, deliberately: the tool
// knows nothing about Ruby, and a test that needed an interpreter installed
// would be measuring the machine instead of the tool.
func TestOracleKeepsWhatARedirectionWouldLose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engines are POSIX shell scripts")
	}
	dir := t.TempDir()
	// An "engine" that writes to BOTH streams and exits non-zero — the shape of
	// a Ruby run that raised. Each ignores the script it is handed.
	engine := fakeEngine(t, dir, "both", "echo out-line\necho err-line >&2\nexit 3\n")
	script := filepath.Join(dir, "probe.rb")
	if err := os.WriteFile(script, []byte("# ignored by the fake engine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := main1([]string{"-script", script, engine}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, want 0; stderr %q", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		"exit status: 3", // ⛔ what `cmd > file 2>&1` throws away
		"out-line",
		"err-line",
		"--- stdout (9 bytes) ---",
		"--- stderr (9 bytes) ---",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report is missing %q:\n%s", want, got)
		}
	}
	// The streams must not be swapped, which a single buffer could not tell you.
	iOut := strings.Index(got, "--- stdout")
	iErr := strings.Index(got, "--- stderr")
	if !(iOut < strings.Index(got, "out-line") && strings.Index(got, "out-line") < iErr) {
		t.Errorf("out-line is not under the stdout heading:\n%s", got)
	}
}

// Two engines that answer differently must exit 1 and NAME the channel that
// diverged; two that answer alike must exit 0. The second half is the control:
// a comparator stuck on "disagreed" would pass the first half alone.
func TestOracleComparesEngines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engines are POSIX shell scripts")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "probe.rb")
	if err := os.WriteFile(script, []byte("#\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	quiet := fakeEngine(t, dir, "quiet", "echo same\n")
	alsoQuiet := fakeEngine(t, dir, "also", "echo same\n")
	noisy := fakeEngine(t, dir, "noisy", "echo same\necho warning: something >&2\n")
	failing := fakeEngine(t, dir, "failing", "echo same\nexit 1\n")

	for _, tc := range []struct {
		name    string
		engines []string
		code    int
		wantIn  string
	}{
		{"agreeing", []string{quiet, alsoQuiet}, 0, "AGREED, byte for byte"},
		{"stderr diverges", []string{quiet, noisy}, 1, "stderr 0 vs 19 bytes"},
		{"status diverges", []string{quiet, failing}, 1, "exit status 0 vs 1"},
		{"one engine only", []string{quiet}, 0, "exit status: 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			args := append([]string{"-script", script}, tc.engines...)
			if code := main1(args, &out, &errOut); code != tc.code {
				t.Errorf("exit = %d, want %d; out %q err %q", code, tc.code, out.String(), errOut.String())
			}
			if !strings.Contains(out.String(), tc.wantIn) {
				t.Errorf("report is missing %q:\n%s", tc.wantIn, out.String())
			}
		})
	}
}

// ⛔ "Could not ask" must never read as "answered differently": the two call for
// opposite responses — fix the invocation, or go read the divergence — and a
// tool that returned 1 for a missing interpreter would send the reader to the
// wrong one. Every refusal is exit 2, as cmd/ratchet's is.
func TestOracleRefusalsAreNotDisagreements(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "probe.rb")
	if err := os.WriteFile(script, []byte("#\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no script", []string{"/bin/echo"}},
		{"unreadable script", []string{"-script", filepath.Join(dir, "absent.rb"), "/bin/echo"}},
		{"no engine", []string{"-script", script}},
		{"an engine that will not start", []string{"-script", script, filepath.Join(dir, "not-a-program")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := main1(tc.args, &out, &errOut); code != 2 {
				t.Errorf("exit = %d, want 2; out %q err %q", code, out.String(), errOut.String())
			}
		})
	}
}

// An engine that never returns is killed and REPORTED as killed, rather than
// hanging the measurement. One of the defects found with this tool was an
// endless range that never came back, so this case is not hypothetical.
func TestOracleKillsAnEngineThatNeverReturns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engines are POSIX shell scripts")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "probe.rb")
	if err := os.WriteFile(script, []byte("#\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hang := fakeEngine(t, dir, "hang", "sleep 60\n")
	var out, errOut bytes.Buffer
	code := main1([]string{"-script", script, "-timeout", "300ms", hang}, &out, &errOut)
	if code != 0 {
		t.Errorf("exit = %d, want 0 (one engine, killed is an answer); err %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "KILLED") {
		t.Errorf("report does not say it was killed:\n%s", out.String())
	}
}

// The in-binary control must pass, and it must be able to fail: the last case
// inside it is two messages of the SAME LENGTH differing by one byte, which is
// the commonest shape here (`$x` vs `$y`) and the one a length comparison would
// call an agreement.
func TestSelftestPasses(t *testing.T) {
	var out bytes.Buffer
	if code := runSelftest(&out); code != 0 {
		t.Fatalf("selftest exit = %d, want 0: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "right on all 5 cases") {
		t.Errorf("selftest did not report its case count: %q", out.String())
	}
	if agreed([]run{
		{stderr: []byte("global variable '$x' not initialized")},
		{stderr: []byte("global variable '$y' not initialized")},
	}) {
		t.Error("two same-length, different-byte messages compared equal")
	}
}

// fakeEngine writes a shell script that behaves like an interpreter: it is
// handed a script path, ignores it, and produces fixed output.
func fakeEngine(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}
