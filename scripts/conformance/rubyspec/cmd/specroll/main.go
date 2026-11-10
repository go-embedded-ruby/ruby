// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Command specroll rolls one or more specdelta reports up to the MODULE.
//
// Why it exists. "What is left to reach parity with MRI?" is a question about
// modules -- Date, coverage, socket -- and specdelta answers it per FILE. One
// run of `library/` produced 497 rows, 189 of them moved; the shape of the debt
// (two Date files carry 53 of the 634 missing examples, five coverage files
// carry 53 more and do not load at all) is not visible by reading 497 lines, and
// a human summarising them by eye is a count done by eye.
//
// ⛔ IT CHECKS ITS OWN ARITHMETIC AGAINST THE REPORT'S. specdelta prints its own
// footer:
//
//	497 file(s): control 1370 passing, treatment 736 passing, -634
//
// specroll recomputes those three numbers from the rows it parsed and REFUSES
// when they disagree. A rollup is exactly where a dropped row is invisible: the
// table still looks tidy, the modules are still sorted, and the only symptom is
// a total nobody re-derives. The judge and the subject must count alike.
//
// ⛔ IT SEPARATES "BEHIND" FROM "ABSENT". A file the treatment cannot load at
// all is reported by specdelta as DID NOT RUN, and its whole count is missing
// for a reason that is not a failing example -- a missing require, a missing
// class. Those are counted in their own column, because "implement Coverage"
// and "fix 29 Date examples" are different work and summing them hides which.
//
// ⛔ IT IS NOT A SECOND MEASUREMENT. It reads what specdelta measured and says
// nothing specdelta did not; a module missing from the reports is missing here.
// The population is the reports named on the command line, and the footer says
// which files it read, so a rollup of half a corpus cannot be read as a whole.
//
// Style note: stdlib `flag`, like its four neighbours in this directory
// (specdelta, ratchet, phantom, oracle). These are repository scripts, not
// released CLIs, and a cobra dependency for one of five would be the odd one
// out in the directory that matters most for reading them side by side.
//
// Usage:
//
//	specroll [-depth N] [-top N] <report>...
//
// Exit status: 0 the rollup printed and reconciled, 1 a report's own totals
// disagree with the rows it contains, 2 nothing could be read.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// A specdelta per-file row:
//
//	library/date/parse_spec.rb      54      22    -32  ⛔ fail+error 0 → 32
//
// Anchored at column zero so the indented "⛔ WENT RED:" lines beneath a row --
// which also contain digits and arrows -- can never be read as rows.
var rowRe = regexp.MustCompile(`^(\S+\.rb)\s+(\d+)\s+(\d+)\s+([+-]\d+)\s*(.*)$`)

// specdelta's footer, the control this tool judges itself against.
var footerRe = regexp.MustCompile(`^(\d+) file\(s\): control (\d+) passing, treatment (\d+) passing, ([+-]?\d+)$`)

// didNotRun is the marker specdelta puts on a file the treatment could not load.
const didNotRun = "DID NOT RUN on the treatment"

// module is one rolled-up group: a path prefix and what it holds.
type module struct {
	name       string
	files      int
	control    int
	treat      int
	unloadable int // files the treatment could not load at all
	unloadLost int // examples the control passes in those files
}

// report is what one specdelta file said about itself, so its claim can be
// compared with the rows beneath it.
type report struct {
	path                   string
	rows                   int
	control, treat         int
	saidFiles              int
	saidControl, saidTreat int
	saidDelta              int
	hasFooter              bool
}

// quiet reports whether this report was produced with specdelta -quiet, which
// prints only the files that MOVED while its footer counts every file walked.
// The two are distinguishable without a flag: a full report has one row per file
// the footer counted.
func (r *report) quiet() bool { return r.hasFooter && r.rows < r.saidFiles }

// reconcile compares what the rows add up to with what the report claimed, and
// returns the mismatch as a sentence, or "" when they agree.
//
// ⛔ THE INVARIANT IS DIFFERENT IN THE TWO MODES, and using the full-report one
// on a -quiet report would report every quiet report as corrupt. A full report
// must match on all three numbers. A quiet report lists only the files that
// moved -- every file it omits has control == treat and so contributes 0 to the
// delta -- so what must match is the DELTA, which is the number this tool exists
// to attribute. Checking nothing in quiet mode was the other option and it is
// the one that makes a dropped row invisible.
func (r *report) reconcile() string {
	if !r.hasFooter {
		return fmt.Sprintf("%d row(s) read, and the report has no \"N file(s): control …\" footer to check them against", r.rows)
	}
	if r.quiet() {
		// specdelta's delta is TREATMENT MINUS CONTROL -- negative when rbgo is
		// behind. The gap column below is the other way round on purpose (a
		// positive "how far behind"), and reconciling with that sign reported
		// every correct quiet report as off by twice the gap.
		if got := r.treat - r.control; got != r.saidDelta {
			return fmt.Sprintf("%d moved row(s) sum to a delta of %+d; the report's own footer says %+d",
				r.rows, got, r.saidDelta)
		}
		return ""
	}
	if r.rows != r.saidFiles || r.control != r.saidControl || r.treat != r.saidTreat {
		return fmt.Sprintf("rows say %d file(s), control %d, treatment %d; the report's own footer says %d, %d, %d",
			r.rows, r.control, r.treat, r.saidFiles, r.saidControl, r.saidTreat)
	}
	return ""
}

func main() {
	depth := flag.Int("depth", 2, "roll up to this many leading path segments (2 = library/date)")
	top := flag.Int("top", 0, "print only the N modules with the largest gap (0 = all)")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "specroll: name at least one specdelta report")
		flag.Usage()
		os.Exit(2)
	}
	if err := run(os.Stdout, os.Stderr, *depth, *top, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "specroll:", err)
		os.Exit(exitCode(err))
	}
}

// codedError carries the exit status the house style distinguishes: 1 is a
// verdict (something disagrees), 2 is a refusal (nothing could be read).
type codedError struct {
	code int
	msg  string
}

func (e *codedError) Error() string { return e.msg }

func exitCode(err error) int {
	if ce, ok := err.(*codedError); ok {
		return ce.code
	}
	return 2
}

func run(stdout, stderr io.Writer, depth, top int, paths []string) error {
	if depth < 1 {
		return &codedError{2, "-depth must be at least 1"}
	}
	byName := map[string]*module{}
	var reports []*report

	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return &codedError{2, fmt.Sprintf("open %s: %v", p, err)}
		}
		rep, err := scan(f, p, depth, byName)
		f.Close()
		if err != nil {
			return err
		}
		reports = append(reports, rep)
	}

	mods := make([]*module, 0, len(byName))
	total := module{name: "TOTAL"}
	for _, m := range byName {
		mods = append(mods, m)
		total.files += m.files
		total.control += m.control
		total.treat += m.treat
		total.unloadable += m.unloadable
		total.unloadLost += m.unloadLost
	}
	if len(mods) == 0 {
		// ⛔ Not "no gap": a report this tool cannot parse yields no rows and no
		// disagreement either, and an empty table reads exactly like parity.
		return &codedError{2, fmt.Sprintf("no specdelta rows in %s; the report format and this parser have drifted apart",
			strings.Join(paths, ", "))}
	}

	// Largest gap first -- that is the question this answers -- then by name so
	// two modules with the same gap keep a stable order between runs.
	sort.Slice(mods, func(i, j int) bool {
		gi, gj := mods[i].control-mods[i].treat, mods[j].control-mods[j].treat
		if gi != gj {
			return gi > gj
		}
		return mods[i].name < mods[j].name
	})

	shown := mods
	if top > 0 && top < len(shown) {
		shown = shown[:top]
	}

	fmt.Fprintf(stdout, "%-34s %6s %8s %7s %7s  %s\n", "module", "files", "MRI", "rbgo", "gap", "unloadable (examples)")
	for _, m := range shown {
		un := ""
		if m.unloadable > 0 {
			un = fmt.Sprintf("%d (%d)", m.unloadable, m.unloadLost)
		}
		fmt.Fprintf(stdout, "%-34s %6d %8d %7d %7d  %s\n",
			m.name, m.files, m.control, m.treat, m.control-m.treat, un)
	}
	if len(shown) < len(mods) {
		fmt.Fprintf(stdout, "… %d more module(s) not shown (-top %d)\n", len(mods)-len(shown), top)
	}
	fmt.Fprintf(stdout, "%-34s %6d %8d %7d %7d  %d (%d)\n",
		total.name, total.files, total.control, total.treat,
		total.control-total.treat, total.unloadable, total.unloadLost)

	// ⛔ SAY WHAT THE COLUMNS COVER WHEN A QUIET REPORT IS IN THE MIX. A -quiet
	// report omits every file where the two engines agreed, so its MRI and rbgo
	// columns count the MOVED files only and are not that module's totals. The
	// gap column is exact either way -- an omitted file contributes 0 to it --
	// which is why the gap is what this table sorts by and the two count columns
	// carry this sentence instead of a footnote nobody reads.
	for _, r := range reports {
		if r.quiet() {
			fmt.Fprintf(stdout, "⛔ some input is a -quiet report: for those modules the MRI and rbgo columns count\n"+
				"   only the files that MOVED, not the module's totals. The gap column is exact.\n")
			break
		}
	}

	// The reconciliation, printed whether it passes or not: a control reported
	// only on failure is a control nobody can see working.
	bad := 0
	for _, r := range reports {
		if msg := r.reconcile(); msg != "" {
			fmt.Fprintf(stderr, "%s: %s\n", r.path, msg)
			bad++
			continue
		}
		if r.quiet() {
			fmt.Fprintf(stdout, "%s: QUIET report — %d moved file(s) of %d walked, delta %+d reconciles with its own footer\n",
				r.path, r.rows, r.saidFiles, r.treat-r.control)
			continue
		}
		fmt.Fprintf(stdout, "%s: %d file(s), control %d, treatment %d — rows reconcile with the report's own footer\n",
			r.path, r.rows, r.control, r.treat)
	}
	if bad > 0 {
		return &codedError{1, fmt.Sprintf("%d report(s) do not reconcile; the table above is not trustworthy", bad)}
	}
	return nil
}

// scan reads one report, adding its rows into byName and returning what the
// report claimed about itself.
//
// ⛔ A -quiet report lists only the files that MOVED, so its rows cannot sum to
// its footer, which counts every file walked. That is not a parse failure and
// must not be reported as one: the footer line is recognised either way, and a
// report whose rows are fewer than its footer's file count is reconciled
// against the moved files alone. See reconcile below.
func scan(r io.Reader, path string, depth int, byName map[string]*module) (*report, error) {
	rep := &report{path: path}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if m := footerRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			rep.hasFooter = true
			rep.saidFiles = atoi(m[1])
			rep.saidControl = atoi(m[2])
			rep.saidTreat = atoi(m[3])
			rep.saidDelta = atoi(m[4])
			continue
		}
		m := rowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := moduleOf(m[1], depth)
		mod := byName[name]
		if mod == nil {
			mod = &module{name: name}
			byName[name] = mod
		}
		control, treat := atoi(m[2]), atoi(m[3])
		mod.files++
		mod.control += control
		mod.treat += treat
		if strings.Contains(m[5], didNotRun) {
			mod.unloadable++
			mod.unloadLost += control
		}
		rep.rows++
		rep.control += control
		rep.treat += treat
	}
	if err := sc.Err(); err != nil {
		return nil, &codedError{2, fmt.Sprintf("read %s: %v", path, err)}
	}
	return rep, nil
}

// moduleOf takes the first depth segments of a spec path, or the whole
// directory when the path is shallower.
func moduleOf(p string, depth int) string {
	dir := path.Dir(p)
	parts := strings.Split(dir, "/")
	if len(parts) > depth {
		parts = parts[:depth]
	}
	return strings.Join(parts, "/")
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(s, "+"))
	if err != nil {
		return 0
	}
	return n
}
