// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Command oracle runs one Ruby script through several interpreters and prints
// everything each of them produced — exit status, stdout and stderr, byte counts
// and all — then says whether they agreed.
//
// It exists because the question "what does MRI actually do here?" is the first
// step of every parity fix in this repository, and the obvious way to ask it
// loses the answer. `ruby probe.rb > out 2>&1` discards the exit status; a pipe
// into `grep` or `tail` discards whichever part of the answer did not match the
// pattern the asker already had in mind; and running the reference and rbgo in
// two separate commands invites comparing a fresh measurement against a
// remembered one.
//
// So this tool PARSES NOTHING. It copies both streams through verbatim and
// reports the status as a number. The only judgement it makes is byte equality
// between engines, which is also the only judgement it can get wrong, and
// -selftest proves that comparison can see a difference.
//
//	oracle -script probe.rb /usr/bin/ruby ./rbgo
//	oracle -script probe.rb /usr/bin/ruby ./rbgo-main ./rbgo-branch
//
// Three engines is the shape the measurement rules ask for: the reference, the
// control (main) and the treatment (the branch). A difference between the last
// two that the reference does not share is the defect or the fix.
//
// Exit status:
//
//	0  every engine ran, and they agreed (or only one was named)
//	1  the engines disagreed
//	2  refused: no script, a script that cannot be read, or an engine that
//	   would not start — never reported as a disagreement, because "could not
//	   ask" and "answered differently" need opposite responses
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// maxStream bounds what is held per stream. A probe that loops forever printing
// is a real possibility — one of the defects found with this tool was an endless
// range that never came back — and an unbounded buffer turns that into the
// machine swapping rather than a message.
const maxStream = 8 << 20

// run is one engine's whole answer. Nothing here is interpreted; the fields are
// the bytes as they arrived.
type run struct {
	engine string
	status int
	stdout []byte
	stderr []byte
	err    error // the engine would not start at all — not a verdict about Ruby
}

func main() {
	os.Exit(main1(os.Args[1:], os.Stdout, os.Stderr))
}

func main1(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("oracle", flag.ContinueOnError)
	fs.SetOutput(errOut)
	script := fs.String("script", "", "the Ruby script to run through every engine (required)")
	timeout := fs.Duration("timeout", time.Minute, "kill an engine that has not finished by then")
	selftest := fs.Bool("selftest", false, "prove the comparison can see a difference, and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *selftest {
		return runSelftest(out)
	}
	if *script == "" {
		fmt.Fprintln(errOut, "oracle: -script is required")
		return 2
	}
	if _, err := os.ReadFile(*script); err != nil {
		// Read it here rather than let each engine fail its own way: a missing
		// script is one refusal, not N disagreements.
		fmt.Fprintf(errOut, "oracle: cannot read %s: %v\n", *script, err)
		return 2
	}
	engines := fs.Args()
	if len(engines) == 0 {
		fmt.Fprintln(errOut, "oracle: name at least one interpreter")
		return 2
	}

	runs := make([]run, 0, len(engines))
	for _, e := range engines {
		runs = append(runs, ask(e, *script, *timeout))
	}
	for _, r := range runs {
		report(out, r)
	}
	refused := false
	for _, r := range runs {
		if r.err != nil {
			fmt.Fprintf(errOut, "oracle: %s would not run: %v\n", r.engine, r.err)
			refused = true
		}
	}
	if refused {
		return 2
	}
	if len(runs) == 1 {
		return 0
	}
	if agreed(runs) {
		fmt.Fprintf(out, "=== %d engines AGREED, byte for byte ===\n", len(runs))
		return 0
	}
	fmt.Fprintf(out, "=== the engines DISAGREED ===\n")
	for _, r := range runs[1:] {
		fmt.Fprintf(out, "  %s vs %s: %s\n", runs[0].engine, r.engine, differences(runs[0], r))
	}
	return 1
}

// ask runs one engine. A non-zero exit is an ANSWER, not an error: a SyntaxError
// is exactly what half these probes are looking for.
func ask(engine, script string, timeout time.Duration) run {
	r := run{engine: engine}
	var out, errBuf bytes.Buffer
	cmd := exec.Command(engine, script)
	cmd.Stdin = nil // a probe that reads stdin must block on nothing
	cmd.Stdout = &limitWriter{w: &out}
	cmd.Stderr = &limitWriter{w: &errBuf}
	cmd.WaitDelay = time.Second

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		r.err = err
		return r
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			r.status = ee.ExitCode()
		} else if err != nil {
			r.err = err
		}
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		r.status = -1 // distinct from any real exit status, and reported as such
	}
	r.stdout, r.stderr = out.Bytes(), errBuf.Bytes()
	return r
}

func report(w io.Writer, r run) {
	fmt.Fprintf(w, "=== %s ===\n", r.engine)
	if r.err != nil {
		fmt.Fprintf(w, "did not run: %v\n", r.err)
		return
	}
	if r.status < 0 {
		fmt.Fprintln(w, "exit status: KILLED, it had not finished")
	} else {
		fmt.Fprintf(w, "exit status: %d\n", r.status)
	}
	fmt.Fprintf(w, "--- stdout (%d bytes) ---\n%s", len(r.stdout), r.stdout)
	if n := len(r.stdout); n > 0 && r.stdout[n-1] != '\n' {
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "--- stderr (%d bytes) ---\n%s", len(r.stderr), r.stderr)
	if n := len(r.stderr); n > 0 && r.stderr[n-1] != '\n' {
		fmt.Fprintln(w)
	}
}

func agreed(runs []run) bool {
	for _, r := range runs[1:] {
		if r.status != runs[0].status ||
			!bytes.Equal(r.stdout, runs[0].stdout) ||
			!bytes.Equal(r.stderr, runs[0].stderr) {
			return false
		}
	}
	return true
}

// differences names WHICH of the three channels diverged, because the three call
// for different reading: a status difference is usually an exception, a stderr
// difference usually a warning or a message, and a stdout difference the
// program's own answer.
func differences(a, b run) string {
	var which []string
	if a.status != b.status {
		which = append(which, fmt.Sprintf("exit status %d vs %d", a.status, b.status))
	}
	if !bytes.Equal(a.stdout, b.stdout) {
		which = append(which, fmt.Sprintf("stdout %d vs %d bytes", len(a.stdout), len(b.stdout)))
	}
	if !bytes.Equal(a.stderr, b.stderr) {
		which = append(which, fmt.Sprintf("stderr %d vs %d bytes", len(a.stderr), len(b.stderr)))
	}
	if len(which) == 0 {
		return "nothing — which is a defect in this tool, not an agreement"
	}
	s := which[0]
	for _, w := range which[1:] {
		s += ", " + w
	}
	return s
}

// runSelftest proves the comparison can see a difference without needing an
// interpreter. A comparator that answered "agreed" for everything would make
// every parity measurement taken with this tool read as parity, which is the
// most reassuring possible way for it to be broken — so it is checked, in the
// binary, by anyone who runs it.
func runSelftest(w io.Writer) int {
	same := []run{{engine: "a", stdout: []byte("x"), stderr: []byte("y")}, {engine: "b", stdout: []byte("x"), stderr: []byte("y")}}
	cases := []struct {
		name  string
		runs  []run
		agree bool
	}{
		{"identical", same, true},
		{"stdout differs", []run{same[0], {engine: "b", stdout: []byte("X"), stderr: []byte("y")}}, false},
		{"stderr differs", []run{same[0], {engine: "b", stdout: []byte("x"), stderr: []byte("Y")}}, false},
		{"status differs", []run{same[0], {engine: "b", status: 1, stdout: []byte("x"), stderr: []byte("y")}}, false},
		// The one a length-only comparison would miss, and warnings differing by
		// one character is the commonest shape in this repository: `$x` vs `$y`.
		{"same length, different bytes", []run{
			{engine: "a", stderr: []byte("global variable '$x' not initialized")},
			{engine: "b", stderr: []byte("global variable '$y' not initialized")},
		}, false},
	}
	bad := 0
	for _, c := range cases {
		if got := agreed(c.runs); got != c.agree {
			fmt.Fprintf(w, "selftest FAILED: %s: agreed = %v, want %v\n", c.name, got, c.agree)
			bad++
		}
	}
	if bad > 0 {
		return 2
	}
	fmt.Fprintf(w, "selftest: the comparison is right on all %d cases\n", len(cases))
	return 0
}

// limitWriter stops after maxStream bytes and keeps accepting writes, so a
// runaway probe is truncated rather than allowed to exhaust memory — and the
// byte count printed afterwards is the truncated one, which is why the cap is
// generous enough that no real probe reaches it.
type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n >= maxStream {
		return len(p), nil
	}
	room := maxStream - l.n
	if len(p) > room {
		p = p[:room]
	}
	n, err := l.w.Write(p)
	l.n += n
	return len(p), err
}
