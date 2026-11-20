// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Command shimdelta measures what a change to the SHIM does to the population,
// by running ONE interpreter over the same files under two spec_helper.rb.
//
// Why it exists. cmd/specdelta compares two ENGINES through one shim, which is
// the right axis for "is rbgo behind MRI". It is the wrong axis for a change
// like #837, where the engine is fixed and the shim's guards decide which
// examples exist at all. Nothing here measured that, and the standing rule for
// such a change — written on #799 and repeated on #837 — is that the move must
// be measured BEFORE `UPDATE_BASELINE=1` regenerates anything, because the
// regeneration records whatever it sees and would absorb a loss silently.
//
// ⛔ A MISSING TALLY IS NOT A TALLY OF ZERO. The shim writes its pass count to
// RBGO_RESULT_FILE (#843) and writes nothing when it never reaches at_exit — a
// crash, a timeout, a file that would not load. Those come back as DIDNOTRUN
// and are counted in their own column, never as 0. A run where the control
// could not be measured is not evidence that the treatment gained anything.
//
// ⛔ IT MEASURES PASSING EXAMPLES AND NOTHING ELSE, and that is a real blind
// spot, stated rather than discovered later. The shim's file channel carries
// the pass count alone, deliberately (#843): the four-field line lives on
// stdout, which is the stream the subject can write to and therefore the one
// the judge must not read. So a guard change that removes a FAILURE is
// invisible here. It happens: on darwin, `platform_is :bsd` in
// core/file/sticky_spec.rb held exactly one example, a BSD-only expectation
// that was being asserted on a mac because :bsd sat in the shim's hardcoded
// list. Fixing the guard took that file from `pass=4 fail=1` to `pass=4 fail=0`
// and this command reported no change, correctly — the gate judges passing
// counts, and the passing count did not move. Read the engine's own output for
// a file whose fail column is the question.
//
// ⛔ IT REPORTS THE TWO DIRECTIONS SEPARATELY and refuses to print their sum.
// A guard fix both adds examples that should always have run and removes
// examples that were running on the wrong machine; a single net figure lets the
// two cancel, which is how a sweep once reported "+217 in rbgo's favour" twenty
// minutes before the same corpus read -1566.
//
//	shimdelta -rbgo <binary> -corpus <ruby/spec checkout> \
//	          -control <spec_helper.rb> -treatment <spec_helper.rb> \
//	          [-file <path> ...] [-suites language,core]
//
// Paths given with -file are relative to the corpus. With no -file, every
// *_spec.rb under the named suites is run, which is a full sweep and takes as
// long as one.
//
// Exit status: 0 the two shims select the same population; 1 they differ;
// 2 the measurement could not be made — a refusal, not an empty delta.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type fileList []string

func (f *fileList) String() string { return strings.Join(*f, ",") }
func (f *fileList) Set(v string) error {
	*f = append(*f, v)
	return nil
}

// result is one file under one shim. Ran distinguishes the two states the
// whole command exists to keep apart.
type result struct {
	Ran  bool
	Pass int
}

type row struct {
	File               string
	Control, Treatment result
}

func main() {
	rbgo := flag.String("rbgo", "", "the interpreter binary to run under both shims (required)")
	corpus := flag.String("corpus", "", "the ruby/spec checkout (required)")
	control := flag.String("control", "", "the spec_helper.rb to measure FROM (required)")
	treatment := flag.String("treatment", "", "the spec_helper.rb to measure TO (required)")
	suites := flag.String("suites", "language,core", "comma-separated suites to walk when no -file is given")
	timeout := flag.Duration("timeout", 60*time.Second, "per-file wall clock")
	var files fileList
	flag.Var(&files, "file", "a spec file relative to the corpus; repeatable")
	flag.Parse()

	for name, v := range map[string]string{"-rbgo": *rbgo, "-corpus": *corpus, "-control": *control, "-treatment": *treatment} {
		if v == "" {
			fmt.Fprintf(os.Stderr, "shimdelta: %s is required\n", name)
			os.Exit(2)
		}
	}

	controlSrc, err := os.ReadFile(*control)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shimdelta: reading the control shim: %v\n", err)
		os.Exit(2)
	}
	treatmentSrc, err := os.ReadFile(*treatment)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shimdelta: reading the treatment shim: %v\n", err)
		os.Exit(2)
	}
	// ⛔ A control and a treatment that are the same bytes cannot measure
	// anything, and would print a tidy "no change" that looks like a verdict.
	if string(controlSrc) == string(treatmentSrc) {
		fmt.Fprintf(os.Stderr, "shimdelta: the two shims are byte-identical, so this run could not measure a difference\n")
		os.Exit(2)
	}

	targets := []string(files)
	if len(targets) == 0 {
		targets, err = walk(*corpus, strings.Split(*suites, ","))
		if err != nil {
			fmt.Fprintf(os.Stderr, "shimdelta: walking the corpus: %v\n", err)
			os.Exit(2)
		}
	}
	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, "shimdelta: no spec files to run under %s -- not a ruby/spec checkout\n", *corpus)
		os.Exit(2)
	}

	// ⛔ The shim is installed ONCE per arm and every file is run under it,
	// rather than swapping the file between each pair. Swapping per file would
	// make the two arms differ in when they ran as well as in which shim they
	// used, and this machine's load is not constant.
	shim := filepath.Join(*corpus, "spec_helper.rb")
	saved, readErr := os.ReadFile(shim)
	if readErr != nil {
		saved = nil
	}
	defer func() {
		if saved != nil {
			_ = os.WriteFile(shim, saved, 0o644)
		}
	}()

	rows := make([]row, len(targets))
	for i, f := range targets {
		rows[i].File = f
	}
	// ⛔ A SLICE, not a map: ranging a map randomises which arm runs first, and
	// the arms would then differ in when they ran as well as in which shim they
	// used. The control goes first, always, so the order is a stated constant
	// rather than something the runtime picks.
	for _, arm := range []struct {
		name string
		src  []byte
	}{{"control", controlSrc}, {"treatment", treatmentSrc}} {
		if err := os.WriteFile(shim, arm.src, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "shimdelta: installing the %s shim: %v\n", arm.name, err)
			os.Exit(2)
		}
		for i, f := range targets {
			r := runOne(*rbgo, *corpus, f, *timeout)
			if arm.name == "control" {
				rows[i].Control = r
			} else {
				rows[i].Treatment = r
			}
		}
	}

	report(rows, *control, *treatment, len(targets))
}

// runOne runs one spec file and reads the tally from the shim's own channel.
//
// ⛔ The EXIT STATUS is not consulted and a failure to START is fatal to the
// measurement, because they are not the same thing: a spec with a failing
// example exits non-zero and has still been measured, while a binary that never
// ran has not. The tally file decides, and its absence is DIDNOTRUN.
func runOne(rbgo, corpus, file string, timeout time.Duration) result {
	tally, err := os.CreateTemp("", "shimdelta-tally-")
	if err != nil {
		return result{}
	}
	path := tally.Name()
	_ = tally.Close()
	defer os.Remove(path)

	cmd := exec.Command(rbgo, file)
	cmd.Dir = corpus
	cmd.Env = append(os.Environ(), "RBGO_RESULT_FILE="+path)
	cmd.Stdout, cmd.Stderr = nil, nil

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "shimdelta: could not run %s on %s: %v\n", rbgo, file, err)
		os.Exit(2)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exited *exec.ExitError
		if err != nil && !errors.As(err, &exited) {
			fmt.Fprintf(os.Stderr, "shimdelta: waiting on %s for %s: %v\n", rbgo, file, err)
			os.Exit(2)
		}
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return result{}
	}

	b, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return result{}
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return result{}
	}
	return result{Ran: true, Pass: n}
}

func walk(corpus string, suites []string) ([]string, error) {
	var out []string
	for _, suite := range suites {
		suite = strings.TrimSpace(suite)
		if suite == "" {
			continue
		}
		dir := filepath.Join(corpus, suite)
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, "_spec.rb") {
				return nil
			}
			rel, rerr := filepath.Rel(corpus, p)
			if rerr != nil {
				return rerr
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

// summary is what a run found, with the four outcomes kept apart.
type summary struct {
	Gained, Lost   []row // both arms measured the file and the count moved
	Appeared       []row // the control could not measure it, the treatment could
	Unmeasured     []row // at least one arm produced no tally at all
	GainedN, LostN int
}

// classify sorts the rows.
//
// ⛔ THE FIRST THREE CASES COME BEFORE ANY ARITHMETIC. A row whose tally is
// missing has a Pass of 0, so letting it fall through to the comparison would
// read "the control passed 0" — the exact substitution of an absent answer for
// a negative one that this whole command exists to refuse. A file neither arm
// could measure is Unmeasured, not an agreement at zero.
func classify(rows []row) summary {
	var s summary
	for _, r := range rows {
		switch {
		case r.Control.Ran && !r.Treatment.Ran:
			s.Unmeasured = append(s.Unmeasured, r)
		case !r.Control.Ran && r.Treatment.Ran:
			s.Appeared = append(s.Appeared, r)
		case !r.Control.Ran && !r.Treatment.Ran:
			s.Unmeasured = append(s.Unmeasured, r)
		case r.Treatment.Pass > r.Control.Pass:
			s.Gained = append(s.Gained, r)
			s.GainedN += r.Treatment.Pass - r.Control.Pass
		case r.Treatment.Pass < r.Control.Pass:
			s.Lost = append(s.Lost, r)
			s.LostN += r.Control.Pass - r.Treatment.Pass
		}
	}
	return s
}

func report(rows []row, control, treatment string, files int) {
	s := classify(rows)
	gained, lost, appeared, vanished := s.Gained, s.Lost, s.Appeared, s.Unmeasured
	gainedN, lostN := s.GainedN, s.LostN

	out := os.Stdout
	fmt.Fprintf(out, "control  : %s\n", control)
	fmt.Fprintf(out, "treatment: %s\n", treatment)
	fmt.Fprintf(out, "%d file(s) run under both\n\n", files)

	for _, r := range gained {
		fmt.Fprintf(out, "  +%-5d %s  (%d -> %d)\n", r.Treatment.Pass-r.Control.Pass, r.File, r.Control.Pass, r.Treatment.Pass)
	}
	for _, r := range lost {
		fmt.Fprintf(out, "  -%-5d %s  (%d -> %d)\n", r.Control.Pass-r.Treatment.Pass, r.File, r.Control.Pass, r.Treatment.Pass)
	}
	for _, r := range appeared {
		fmt.Fprintf(out, "  NEW    %s  (DID NOT RUN under the control -> %d)\n", r.File, r.Treatment.Pass)
	}
	for _, r := range vanished {
		fmt.Fprintf(out, "  ⛔ %s  (control ran=%v, treatment ran=%v) -- NOT counted either way\n", r.File, r.Control.Ran, r.Treatment.Ran)
	}

	// ⛔ The two totals are printed on their own lines and never added. A guard
	// fix moves examples in both directions and the netting of the two is not a
	// quantity anyone can act on.
	fmt.Fprintf(out, "\n  gained : %d example(s) across %d file(s)\n", gainedN, len(gained))
	fmt.Fprintf(out, "  lost   : %d example(s) across %d file(s)\n", lostN, len(lost))
	fmt.Fprintf(out, "  unmeasured: %d file(s)\n", len(vanished))

	if gainedN != 0 || lostN != 0 || len(appeared) != 0 || len(vanished) != 0 {
		os.Exit(1)
	}
}
