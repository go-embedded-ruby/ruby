// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Command specdelta runs a set of ruby/spec files through TWO interpreters and
// reports, file by file, how many examples each one passed.
//
// It answers a question `cmd/ratchet` cannot. The ratchet judges one sweep
// against `BASELINE`, which is the right gate for "may this land" but the wrong
// instrument for "what does THIS change do" — a baseline comparison folds in
// every difference between the recording machine and this one, and #799's
// version move needed the other reading: hold the corpus and the machine fixed,
// vary only the binary, and name every file that moved.
//
//	specdelta -dir core/set -root ~/ruby-spec ./rbgo-main ./rbgo-branch
//
// ⛔ Two channels, not one. The `RBGO_RESULT pass=…` line comes out of the
// SUBJECT's own stdout (#843), so a subject that miscounts can report any total
// it likes. specdelta also counts the `RBGO_DETAIL` lines, which the shim emits
// one per failure and error, and flags any file where the two disagree —
// `fail+error` claimed against detail lines seen. That is not a theoretical
// concern: reading the detail lines rather than the tally is what caught a
// figure misattributed between two branches on 2026-11-01.
//
// Exit status:
//
//	0  every file ran and no file passed fewer examples on the second engine
//	1  at least one file regressed
//	2  refused: no files, an engine that would not start, or output that could
//	   not be read — never reported as a regression, because "could not measure"
//	   and "measured worse" need opposite responses
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// result is one spec file's outcome under one engine.
type result struct {
	pass, fail, errs, skip int
	details                int  // RBGO_DETAIL lines seen
	ran                    bool // a RESULT line was found at all
	killed                 bool
}

// tallyDisagrees reports whether the subject printed FEWER detail lines than
// its own tally of failures and errors. That is a one-sided check, and the
// asymmetry is the shim's, not a hedge:
//
//	kind        counter      detail line
//	fail        $RB_FAIL     yes          (spec_helper.rb:983, :987)
//	error       $RB_ERROR    yes          (:995, :1029, :1102)
//	skip        $RB_SKIP     yes          (:990, :993)
//	skip        $RB_SKIP     NO           (:970 — a version/platform guard)
//	hookerror   NONE         yes          (:965, :1020 — before/after :all)
//
// So `details == fail+error` is wrong in both directions: a guard-skipped file
// has a detail with no failure, and a `before(:all)` that raises has a detail
// counted in no tally at all. `details >= fail+error` is what holds, because
// those two kinds each push exactly one line — and a shortfall means detail
// lines went missing, which makes the whole report unreadable.
//
// ⛔ The first version of this asserted equality and fired on the first real
// run, against `core/set/set_spec.rb` — which is `pass=0 fail=0 error=0 skip=1`
// with one `skip` detail, entirely well-formed. Reading the shim rather than
// assuming its shape is what produced the table above; the line
// `hookerror` has no counter is a defect in the harness, filed separately.
func (r result) tallyDisagrees() bool {
	return r.ran && r.details < r.fail+r.errs
}

func main() {
	os.Exit(main1(os.Args[1:], os.Stdout, os.Stderr))
}

func main1(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("specdelta", flag.ContinueOnError)
	fs.SetOutput(errOut)
	root := fs.String("root", ".", "the ruby/spec checkout")
	dir := fs.String("dir", "", "a directory under -root to walk (recursively), e.g. core/set")
	timeout := fs.Duration("timeout", 60*time.Second, "per-file limit for one engine")
	quiet := fs.Bool("quiet", false, "print only the files that moved")
	// ⛔ One run per engine cannot tell a FLAKE from a difference, and the first
	// real use of this tool reported `core/random/bytes_spec.rb` as -1 between two
	// binaries that differ only in a version string. Three runs of each: control
	// 9, 9, 8 and treatment 8, 9, 9 — a coin flip, attributed to the change.
	// With -repeat above 1 a file whose own engine disagrees with itself is named
	// UNSTABLE and kept out of the regression verdict.
	repeat := fs.Int("repeat", 1, "run each file this many times per engine; above 1, detect flaky files")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	engines := fs.Args()
	if len(engines) != 2 {
		fmt.Fprintln(errOut, "specdelta: name exactly two interpreters — the control first, the treatment second")
		return 2
	}
	if *dir == "" {
		fmt.Fprintln(errOut, "specdelta: -dir is required")
		return 2
	}
	files, err := specFiles(*root, *dir)
	if err != nil {
		fmt.Fprintf(errOut, "specdelta: %v\n", err)
		return 2
	}
	if len(files) == 0 {
		// ⛔ An empty population is a refusal, not a clean result: a mistyped
		// -dir would otherwise print "0 files, no regressions" and pass.
		fmt.Fprintf(errOut, "specdelta: no _spec.rb files under %s\n", filepath.Join(*root, *dir))
		return 2
	}

	if *repeat < 1 {
		fmt.Fprintln(errOut, "specdelta: -repeat must be at least 1")
		return 2
	}
	type row struct {
		file     string
		a, b     result // the LOWEST pass count seen, so a verdict is never built on a lucky run
		unstable bool   // an engine disagreed with itself across repeats
	}
	rows := make([]row, 0, len(files))
	refused := false
	for _, f := range files {
		r := row{file: f}
		for i, engine := range engines {
			lowest, varied, err := runRepeatedly(engine, *root, f, *timeout, *repeat)
			if err != nil {
				fmt.Fprintf(errOut, "specdelta: %s on %s: %v\n", engines[i], f, err)
				refused = true
				continue
			}
			if varied {
				r.unstable = true
			}
			if i == 0 {
				r.a = lowest
			} else {
				r.b = lowest
			}
		}
		rows = append(rows, r)
	}
	if refused {
		return 2
	}

	// Loudest movement first: a reader wants the biggest change, not the
	// alphabetically first one.
	sort.SliceStable(rows, func(i, j int) bool {
		di := rows[i].b.pass - rows[i].a.pass
		dj := rows[j].b.pass - rows[j].a.pass
		if di != dj {
			return abs(di) > abs(dj)
		}
		return rows[i].file < rows[j].file
	})

	var totalA, totalB, moved, regressed, suspect int
	fmt.Fprintf(out, "%-56s %7s %7s %6s\n", "file", "control", "treat", "delta")
	for _, r := range rows {
		d := r.b.pass - r.a.pass
		if d != 0 {
			moved++
		}
		// ⛔ An unstable file is kept OUT of the verdict: its delta is noise, and
		// counting it would make the exit status depend on a coin flip.
		if d < 0 && !r.unstable {
			regressed++
		}
		totalA += r.a.pass
		totalB += r.b.pass
		note := ""
		switch {
		case !r.a.ran && !r.b.ran:
			note = "  DID NOT RUN on either"
		case !r.a.ran:
			note = "  DID NOT RUN on the control"
		case !r.b.ran:
			note = "  DID NOT RUN on the treatment"
		case r.a.killed || r.b.killed:
			note = "  KILLED on timeout"
		}
		if r.unstable {
			note += "  ⛔ UNSTABLE: an engine disagreed with itself, delta excluded"
		}
		if r.a.tallyDisagrees() || r.b.tallyDisagrees() {
			note += "  ⛔ fewer detail lines than its own fail+error tally"
			suspect++
		}
		if *quiet && d == 0 && note == "" {
			continue
		}
		fmt.Fprintf(out, "%-56s %7d %7d %+6d%s\n", r.file, r.a.pass, r.b.pass, d, note)
	}
	fmt.Fprintf(out, "\n%d file(s): control %d passing, treatment %d passing, %+d\n",
		len(rows), totalA, totalB, totalB-totalA)
	fmt.Fprintf(out, "%d file(s) moved, %d regressed\n", moved, regressed)
	if suspect > 0 {
		// A total built on a tally the subject contradicts is not a measurement.
		fmt.Fprintf(errOut, "specdelta: %d file(s) reported a tally their detail lines contradict\n", suspect)
		return 2
	}
	if regressed > 0 {
		return 1
	}
	return 0
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// specFiles lists the *_spec.rb files under root/dir, as paths relative to root
// — which is how the shim expects to be handed them.
func specFiles(root, dir string) ([]string, error) {
	base := filepath.Join(root, dir)
	info, err := os.Stat(base)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", base)
	}
	var out []string
	err = filepath.Walk(base, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() || !strings.HasSuffix(p, "_spec.rb") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// runRepeatedly runs one file under one engine `times` over, and returns the
// LOWEST-scoring result plus whether the engine disagreed with itself.
//
// The lowest rather than the mean or the last, because the question a reader
// asks of this table is "did anything get worse", and the answer must not rest
// on a lucky run. With times == 1 `varied` is always false, which is why the
// regression verdict is only trustworthy above 1 — said in the flag's help.
func runRepeatedly(engine, root, file string, timeout time.Duration, times int) (result, bool, error) {
	var lowest result
	varied := false
	for i := 0; i < times; i++ {
		r, err := run(engine, root, file, timeout)
		if err != nil {
			return lowest, false, err
		}
		switch {
		case i == 0:
			lowest = r
		case r.pass != lowest.pass || r.ran != lowest.ran:
			varied = true
			if r.pass < lowest.pass {
				lowest = r
			}
		}
	}
	return lowest, varied, nil
}

// run executes one spec file under one engine, from the corpus root so the
// shim's relative requires resolve.
func run(engine, root, file string, timeout time.Duration) (result, error) {
	var r result
	var out bytes.Buffer
	cmd := exec.Command(engine, file)
	cmd.Dir = root
	cmd.Stdin = nil
	cmd.Stdout = &out
	cmd.Stderr = io.Discard // the shim reports on stdout; stderr is warnings
	cmd.WaitDelay = time.Second

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return r, err
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var ee *exec.ExitError
		if err != nil && !errors.As(err, &ee) {
			return r, err
		}
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		r.killed = true
	}
	return parse(out.String())
}

// parse reads the shim's report. It REFUSES a RESULT line it cannot read rather
// than defaulting a field to zero, because a zero that means "unreadable" and a
// zero that means "nothing passed" lead to opposite conclusions.
func parse(s string) (result, error) {
	var r result
	for _, line := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(line, "RBGO_DETAIL"):
			r.details++
		case strings.HasPrefix(line, "RBGO_RESULT"):
			if r.ran {
				return r, fmt.Errorf("two RBGO_RESULT lines in one run")
			}
			fields := strings.Fields(strings.TrimPrefix(line, "RBGO_RESULT"))
			seen := map[string]bool{}
			for _, f := range fields {
				k, v, ok := strings.Cut(f, "=")
				if !ok {
					return r, fmt.Errorf("unreadable field %q in %q", f, line)
				}
				n, err := strconv.Atoi(v)
				if err != nil {
					return r, fmt.Errorf("field %q is not a number in %q", f, line)
				}
				seen[k] = true
				switch k {
				case "pass":
					r.pass = n
				case "fail":
					r.fail = n
				case "error":
					r.errs = n
				case "skip":
					r.skip = n
				default:
					return r, fmt.Errorf("unknown field %q in %q", k, line)
				}
			}
			for _, k := range []string{"pass", "fail", "error", "skip"} {
				if !seen[k] {
					return r, fmt.Errorf("%q missing from %q", k, line)
				}
			}
			r.ran = true
		}
	}
	return r, nil
}
