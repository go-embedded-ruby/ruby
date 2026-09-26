// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_closed && !wasm

package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// The exit status is the one thing no in-process test can observe: os.Exit ends
// the process, and being killed by a signal ends it harder. These tests therefore
// re-exec THIS TEST BINARY with WT37_EXIT_SRC set, which makes the child run one
// Ruby program through the same run()+finish() pair `rbgo run` uses and then exit.
//
// The precondition is established by the test itself — it sets the variable on
// the child it spawns — so unlike a test gated on an externally-supplied
// RBGO_* variable this one cannot silently not run. os.Args[0] is the compiled
// test binary, so no Go toolchain is needed and it runs in every CI lane.
const exitSrcEnv = "WT37_EXIT_SRC"

// childMain is the re-exec entry point. It runs before any assertion in the
// child, so the child never reaches the testing framework's own output.
func childMain() {
	if src, ok := os.LookupEnv(exitSrcEnv); ok {
		finish(run(src, "-e")) // never returns
	}
}

func TestMain(m *testing.M) {
	childMain()
	os.Exit(m.Run())
}

// runChild runs one program in a child process and reports its stdout, stderr,
// exit status, and whether the child was KILLED BY a signal rather than exiting.
func runChild(t *testing.T, src string) (stdout, stderr string, status int, signaled bool, signo int) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestExitStatusReachesTheProcess$")
	c.Env = append(os.Environ(), exitSrcEnv+"="+src)
	var so, se strings.Builder
	c.Stdout, c.Stderr = &so, &se
	err := c.Run()
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("re-exec %s: %v", os.Args[0], err)
		}
		if ws, okWS := ee.Sys().(syscall.WaitStatus); okWS && ws.Signaled() {
			return so.String(), se.String(), 128 + int(ws.Signal()), true, int(ws.Signal())
		}
		status = ee.ExitCode()
	}
	return so.String(), se.String(), status, false, 0
}

// TestExitStatusReachesTheProcess is the end-to-end witness for issue #676 and
// for the terminal half of #691. Every "want" here was measured against MRI 4.0.5
// (ruby 4.0.5 (2026-05-20) +PRISM [arm64-darwin25]) with the same program.
//
// On origin/main this test's first row alone fails: `rbgo -e 'exit 3'` exited 0,
// because run() returned no error for a SystemExit and runCmd exited 0. Reverting
// runCmd to `if err := run(...); err != nil { reportError(err); os.Exit(1) }`
// still compiles and fails ten of these rows.
func TestExitStatusReachesTheProcess(t *testing.T) {
	if _, isChild := os.LookupEnv(exitSrcEnv); isChild {
		// The child never gets here: childMain ran finish() from TestMain. Guard
		// anyway so a mistake shows as a failure rather than a recursive spawn.
		t.Fatal("child reached the test body; childMain did not run")
	}
	for _, tc := range []struct {
		name, src        string
		wantStatus       int
		wantOut, wantErr string
		wantSignal       int // non-zero: the child must be KILLED BY this signal
	}{
		{name: "exit 3", src: `exit 3`, wantStatus: 3},
		{name: "exit false", src: `exit false`, wantStatus: 1},
		{name: "exit 0", src: `exit 0`, wantStatus: 0},
		{name: "exit true", src: `exit true`, wantStatus: 0},
		{name: "exit!(4)", src: `exit!(4)`, wantStatus: 4},
		{name: "abort", src: `abort "boom"`, wantStatus: 1, wantErr: "boom\n"},
		{name: "bare abort", src: `abort`, wantStatus: 1},
		{name: "raise", src: `raise "x"`, wantStatus: 1,
			wantErr: "-e:1:in '<main>': x (RuntimeError)\n"},
		{name: "Process.exit(5)", src: `Process.exit(5)`, wantStatus: 5},
		{name: "Process.exit!(6)", src: `Process.exit!(6)`, wantStatus: 6},
		{name: "Process.abort", src: `Process.abort "pboom"`, wantStatus: 1, wantErr: "pboom\n"},
		// A rescued SystemExit must NOT exit non-zero — the easiest way to "fix"
		// the status is to exit on every SystemExit ever raised, and this row is
		// what makes that wrong.
		{name: "a rescued SystemExit exits 0",
			src:     `begin; exit 3; rescue SystemExit => e; puts "status=#{e.status}"; end`,
			wantOut: "status=3\n"},
		{name: "at_exit runs and the status survives",
			src: `at_exit { puts "atexit" }; exit 3`, wantStatus: 3, wantOut: "atexit\n"},
		{name: "ensure runs and the status survives",
			src: `begin; exit 3; ensure; puts "ensure"; end`, wantStatus: 3, wantOut: "ensure\n"},
		{name: "an at_exit exit overrides the status",
			src: `at_exit { exit 7 }; exit 3`, wantStatus: 7},
		{name: "exit! runs no at_exit handler",
			src: `at_exit { puts "atexit" }; exit!(4)`, wantStatus: 4},
		// A rescued self-signal leaves the process healthy: this is the #691 row
		// that was 10-20% fatal on origin/main.
		{name: "a rescued SignalException exits 0",
			src: `begin
  Process.kill(:TERM, Process.pid)
rescue SignalException => e
  puts "#{e.class} #{e.message} #{e.signo}"
end
puts "alive"`, wantOut: "SignalException SIGTERM 15\nalive\n"},
		// An UNCAUGHT SignalException must leave the process dead BY the signal,
		// with nothing on stderr (exiting_split gives the base class no message).
		{name: "an uncaught SignalException dies by its signal",
			src:        `Process.kill(:TERM, Process.pid); puts "unreached"`,
			wantStatus: 128 + 15, wantSignal: 15},
		// An uncaught Interrupt is the same death, but it DOES print, because it is
		// a subclass. Its message is empty, so print_errinfo writes the class name
		// alone with no parenthesised suffix.
		{name: "an uncaught Interrupt prints its class and dies by SIGINT",
			src:        `Process.kill(:INT, Process.pid); puts "unreached"`,
			wantStatus: 128 + 2, wantSignal: 2, wantErr: "-e:1:in '<main>': Interrupt\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, status, signaled, signo := runChild(t, tc.src)
			if status != tc.wantStatus {
				t.Errorf("exit status = %d, want %d (stderr %q)", status, tc.wantStatus, errOut)
			}
			if out != tc.wantOut {
				t.Errorf("stdout = %q, want %q", out, tc.wantOut)
			}
			if errOut != tc.wantErr {
				t.Errorf("stderr = %q, want %q", errOut, tc.wantErr)
			}
			// "killed by signal 15" and "exited with 143" give a shell the same $?
			// but a different WIFSIGNALED, and MRI's ruby_default_signal produces
			// the former. Asserting only the number would pass on a plain exit.
			if tc.wantSignal != 0 {
				if !signaled {
					t.Errorf("child exited normally, want death by signal %d", tc.wantSignal)
				} else if signo != tc.wantSignal {
					t.Errorf("killed by signal %d, want %d", signo, tc.wantSignal)
				}
			} else if signaled {
				t.Errorf("child was killed by signal %d, want a normal exit", signo)
			}
		})
	}
}

// TestParseErrorStillExitsOne pins the path where finish has no machine to
// classify against: a program that never compiled. The status has always been 1;
// the risk the new code introduced is a nil-pointer dereference on the machine
// run() could not build, which this row is here to catch.
func TestParseErrorStillExitsOne(t *testing.T) {
	_, errOut, status, signaled, _ := runChild(t, `def (`)
	if status != 1 || signaled {
		t.Errorf("status = %d (signaled %v), want a plain exit 1", status, signaled)
	}
	if errOut == "" {
		t.Error("no diagnostic on stderr for a parse error")
	}
}
