// Command covratchet judges one `go tool cover -func` profile against a
// per-lane record of the functions already known to be below 100%.
//
// It replaces a gate on the ROUNDED total, which could not be calibrated. On
// main both POSIX lanes report `coverage: 100.0%` and pass while printing 19
// (ubuntu) and 16 (macOS) functions below 100%, the lowest at 75.0%. The
// tolerance is whatever %.1f absorbs at the current repository size: nobody
// chose it, nothing states it, and it SHRINKS as the codebase grows. A function
// falling from 100% to 75% is free until it is suddenly fatal, and the commit
// that trips the gate is not the one that created the debt.
//
// It was also arbitrary which lane you believed. On PR #727 the same commit
// gave 99.9% on ubuntu (FAIL) and 100.0% on macOS (pass): ubuntu carried three
// filesystem-dependent functions macOS does not, so one further dead function
// tipped only one lane over the edge.
//
// So: the gate is the SET of below-100 functions, recorded per lane, and it may
// not grow. Percentages are reported but not gated -- coverage is measured under
// -race, where a percentage jitters while the set does not, and a gate that
// flakes is a gate people learn to rerun.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// fn identifies one function across runs. The LINE is deliberately not part of
// it: inserting a comment above a function shifts every line below it, and a
// line-keyed diff then reports hundreds of functions as removed and re-added at
// once. Measured: a 12-line comment faked ~600 of each.
type fn struct{ file, name string }

func (f fn) String() string { return f.file + " " + f.name }

type entry struct {
	fn
	pct  float64
	line int // for the report only, never for identity
}

// parse reads `go tool cover -func` output and returns every function below
// 100%, keyed by identity. The total line is recognised and skipped rather than
// silently mis-parsed: a parser that swallows what it does not understand turns
// a format change into a clean result.
func parse(r io.Reader, what, mod string) (map[fn]entry, error) {
	out := make(map[fn]entry)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	seenTotal := false
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 || !strings.HasSuffix(f[len(f)-1], "%") {
			return nil, fmt.Errorf("%s line %d: not a cover -func record: %q", what, n, line)
		}
		pct, err := strconv.ParseFloat(strings.TrimSuffix(f[len(f)-1], "%"), 64)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %q is not a percentage", what, n, f[len(f)-1])
		}
		if f[0] == "total:" {
			seenTotal = true
			continue
		}
		if len(f) < 3 {
			return nil, fmt.Errorf("%s line %d: expected file:line: name pct, got %q", what, n, line)
		}
		file, ln := splitPos(f[0], mod)
		if pct >= 100 {
			continue
		}
		k := fn{file: file, name: f[1]}
		if prev, dup := out[k]; dup {
			return nil, fmt.Errorf("%s line %d: %s appears twice (was %.1f%%)", what, n, k, prev.pct)
		}
		out[k] = entry{fn: k, pct: pct, line: ln}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", what, err)
	}
	if !seenTotal {
		return nil, fmt.Errorf("%s has no total: line -- a profile this tool could not read must not be reported as clean", what)
	}
	return out, nil
}

// splitPos splits "<module>/internal/vm/vm.go:1473:" into its repo-relative path
// and line. mod is stripped so a record reads as repository paths and does not
// carry the import path in every row; it is passed in rather than guessed,
// because a rule that infers it from the data would strip a whole package path
// on a run where every function happens to sit in one package.
func splitPos(s, mod string) (string, int) {
	s = strings.TrimSuffix(s, ":")
	file, lineStr := s, ""
	if i := strings.LastIndex(s, ":"); i >= 0 {
		file, lineStr = s[:i], s[i+1:]
	}
	file = strings.TrimPrefix(file, mod)
	n, _ := strconv.Atoi(lineStr)
	return file, n
}

// parseRecord reads a baseline, which is the same three columns the report
// prints: file, function, percentage.
func parseRecord(r io.Reader, what string) (map[fn]entry, error) {
	out := make(map[fn]entry)
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			return nil, fmt.Errorf("%s line %d: expected file<TAB>function<TAB>pct, got %q", what, n, line)
		}
		pct, err := strconv.ParseFloat(strings.TrimSuffix(f[2], "%"), 64)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %q is not a percentage", what, n, f[2])
		}
		k := fn{file: f[0], name: f[1]}
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("%s line %d: %s appears twice", what, n, k)
		}
		out[k] = entry{fn: k, pct: pct}
	}
	return out, sc.Err()
}

func writeRecord(w io.Writer, m map[fn]entry, lane string) error {
	ks := make([]fn, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool {
		if ks[i].file != ks[j].file {
			return ks[i].file < ks[j].file
		}
		return ks[i].name < ks[j].name
	})
	b := bufio.NewWriter(w)
	fmt.Fprintf(b, "# Functions below 100%% coverage on the %s lane, recorded.\n", lane)
	fmt.Fprintf(b, "# The SET is the gate and may not grow; the percentages are reported, not gated\n")
	fmt.Fprintf(b, "# (coverage is measured under -race, where a percentage jitters and a set does not).\n")
	fmt.Fprintf(b, "# Regenerate with UPDATE_COVERAGE=1 on a green run of this lane.\n")
	for _, k := range ks {
		fmt.Fprintf(b, "%s\t%s\t%.1f%%\n", k.file, k.name, m[k].pct)
	}
	return b.Flush()
}

var errRegression = errors.New("coverage regression")

func main() {
	err := run(os.Args[1:], os.Stdin, os.Stdout)
	switch {
	case err == nil:
	case errors.Is(err, errRegression):
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "covratchet: %v\n", err)
		os.Exit(2)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("covratchet", flag.ContinueOnError)
	fs.SetOutput(stdout)
	record := fs.String("record", "", "per-lane record of functions below 100% (required)")
	profile := fs.String("profile", "", "`go tool cover -func` output (default: stdin)")
	lane := fs.String("lane", "", "lane name, for the record's header")
	mod := fs.String("module", "github.com/go-embedded-ruby/ruby/", "module path prefix to strip from each file path")
	update := fs.Bool("update", false, "rewrite the record from this run and exit 0")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *record == "" {
		return fmt.Errorf("-record is required")
	}

	var src io.Reader = stdin
	if *profile != "" {
		f, err := os.Open(*profile)
		if err != nil {
			return err
		}
		defer f.Close()
		src = f
	}
	got, err := parse(src, "profile", *mod)
	if err != nil {
		return err
	}

	if *update {
		f, err := os.Create(*record)
		if err != nil {
			return err
		}
		if err := writeRecord(f, got, *lane); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "recorded %d function(s) below 100%% on %s\n", len(got), *lane)
		return nil
	}

	rf, err := os.Open(*record)
	if err != nil {
		return err
	}
	defer rf.Close()
	base, err := parseRecord(rf, "record")
	if err != nil {
		return err
	}

	var added, fixed, worse []string
	for k, e := range got {
		b, known := base[k]
		if !known {
			added = append(added, fmt.Sprintf("%s  %.1f%% (was fully covered)", k, e.pct))
			continue
		}
		if e.pct < b.pct-0.05 {
			worse = append(worse, fmt.Sprintf("%s  %.1f%%, recorded %.1f%%", k, e.pct, b.pct))
		}
	}
	for k, b := range base {
		if _, still := got[k]; !still {
			fixed = append(fixed, fmt.Sprintf("%s  now 100%% (was %.1f%%)", k, b.pct))
		}
	}
	sort.Strings(added)
	sort.Strings(fixed)
	sort.Strings(worse)

	fmt.Fprintf(stdout, "functions below 100%%: %d measured, %d recorded\n", len(got), len(base))
	section(stdout, "NEWLY BELOW 100%", added)
	section(stdout, "further below than recorded", worse)
	section(stdout, "now fully covered", fixed)

	if len(added) > 0 {
		fmt.Fprintf(stdout, "\n::error::%d function(s) dropped below 100%% -- named above\n", len(added))
		return errRegression
	}
	if len(fixed) > 0 {
		fmt.Fprintf(stdout, "\n%d function(s) reached 100%% -- rerun with UPDATE_COVERAGE=1 to lock it in.\n", len(fixed))
	}
	fmt.Fprintln(stdout, "\ncoverage ratchet OK -- no function newly below 100%")
	return nil
}

func section(w io.Writer, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(w, "\n-- %s (%d) --\n", title, len(items))
	for _, s := range items {
		fmt.Fprintf(w, "   %s\n", s)
	}
}
