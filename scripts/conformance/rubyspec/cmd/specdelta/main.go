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
// ⛔ -move IS REQUIRED, and that is the point of it. On 2026-11-01 this tool
// measured #799's version move and the figure was published as "ten examples of
// work". The real figure was about seventy-five. Three
// `core/binding/implicit_parameter*_spec.rb` files are `pass=0 fail=0 error=0`
// below 4.0 — every example behind a `ruby_version_is "4.0"` guard — and 37
// failures above it, and `-dir` had never been pointed at `core/binding` at
// all. The corpus itself changes between the two engines, and a tool that
// compares RESULTS cannot see that from the results.
//
// A note saying "ask what the target version ADDS before measuring what moves"
// had been written down beforehand and was not consulted. So the question is no
// longer something to remember: the tool will not produce a table until the
// caller has declared which comparison this is, and when a move is declared it
// censuses the corpus (internal/guardcensus) and REFUSES, before running
// anything, if the move touches files outside the measured set.
//
// Exit status:
//
//	0  every file ran and no file passed fewer examples on the second engine
//	1  at least one file regressed, or an EXAMPLE went red inside a file that
//	   improved on balance
//	2  refused: no files, an engine that would not start, output that could not
//	   be read, no -move declared, a declared move whose surface this run does
//	   not cover, or a pair of binaries that cannot be tied to two distinct
//	   commits — never reported as a regression, because "could not measure"
//	   and "measured worse" need opposite responses
package main

import (
	"bytes"
	"debug/buildinfo"
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

	gc "github.com/go-embedded-ruby/ruby/scripts/conformance/rubyspec/internal/guardcensus"
)

// result is one spec file's outcome under one engine.
type result struct {
	pass, fail, errs, skip int
	details                int  // RBGO_DETAIL lines seen
	ran                    bool // a RESULT line was found at all
	killed                 bool

	// labels counts the DESCRIPTION of each detail line — the example's own
	// name, without the message, which carries object addresses that differ
	// between runs. A multiset and not a set: a shared example block included
	// twice reports the same description twice, and a file that loses one of
	// the two has lost an example.
	labels map[string]int
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
// outcomeNote is the note printed beside a row when one of the two engines did
// not produce an ordinary result: it was stopped by the clock, or it produced
// nothing at all.
//
// ⛔ THE KILL IS TESTED FIRST, AND THE ORDER IS THE WHOLE POINT. A killed engine
// almost never prints a RESULT line, so `ran` is false and the DID-NOT-RUN arms
// would claim every killed row -- reporting a file the CLOCK stopped as a file
// the engine could not LOAD. Those are opposite findings: one says the timeout
// was too short or the engine hangs, the other says the feature is
// unimplemented, and they call for opposite work.
//
// It was worse than an ordering bug before this: run() returned `parse(...)`,
// a fresh result, so `killed` was false on every row ever printed and this
// branch was unreachable. The documented control for a whole report -- "grep
// for KILLED; if it is 0 the table is sound" -- could only ever answer 0, and
// every sweep looked clean by construction.
func outcomeNote(a, b result) string {
	switch {
	case a.killed && b.killed:
		return "  KILLED on timeout, both engines"
	case a.killed:
		return "  KILLED on timeout, the control"
	case b.killed:
		return "  KILLED on timeout, the treatment"
	case !a.ran && !b.ran:
		return "  DID NOT RUN on either"
	case !a.ran:
		return "  DID NOT RUN on the control"
	case !b.ran:
		return "  DID NOT RUN on the treatment"
	}
	return ""
}

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
	// ⛔ A SET, comma-separated. The move gate below refuses a surface -dir does
	// not cover and advises widening it; with one directory that advice was not
	// followable, because the 3.4.1→4.0.9 surface spans core/ AND language/ and
	// the only widening from `core` was the whole corpus — library/, optional/,
	// command_line/ and hours of them. The only reachable exit was -partial, so
	// the derogation was the normal road and the gate stamped every real answer.
	dir := fs.String("dir", "", "directories under -root to walk (recursively), comma-separated, e.g. core/set or core,language")
	timeout := fs.Duration("timeout", 60*time.Second, "per-file limit for one engine")
	quiet := fs.Bool("quiet", false, "print only the files that moved")
	// ⛔ One run per engine cannot tell a FLAKE from a difference, and the first
	// real use of this tool reported `core/random/bytes_spec.rb` as -1 between two
	// binaries that differ only in a version string. Three runs of each: control
	// 9, 9, 8 and treatment 8, 9, 9 — a coin flip, attributed to the change.
	// With -repeat above 1 a file whose own engine disagrees with itself is named
	// UNSTABLE and kept out of the regression verdict.
	repeat := fs.Int("repeat", 1, "run each file this many times per engine; above 1, detect flaky files")
	// ⛔ No default. A default here would be a guess about the one thing the
	// results cannot reveal, and the wrong guess is silent.
	move := fs.String("move", "", `required: "none" if both engines read the corpus at the same RUBY_VERSION, else FROM:TO (e.g. 3.4.1:4.0.9)`)
	partial := fs.Bool("partial", false, "run a declared move anyway when its surface reaches outside -dir; the report is stamped PARTIAL")
	unprovenanced := fs.Bool("unprovenanced", false, "compare engines that cannot be tied to two distinct commits; the report is stamped UNPROVENANCED")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	engines := fs.Args()
	if len(engines) != 2 {
		fmt.Fprintln(errOut, "specdelta: name exactly two interpreters — the control first, the treatment second")
		return 2
	}
	dirs := splitDirs(*dir)
	if len(dirs) == 0 {
		fmt.Fprintln(errOut, "specdelta: -dir is required")
		return 2
	}
	files, err := specFiles(*root, dirs)
	if err != nil {
		fmt.Fprintf(errOut, "specdelta: %v\n", err)
		return 2
	}
	if len(files) == 0 {
		// ⛔ An empty population is a refusal, not a clean result: a mistyped
		// -dir would otherwise print "0 files, no regressions" and pass.
		fmt.Fprintf(errOut, "specdelta: no _spec.rb files under %s\n", strings.Join(dirs, ", "))
		return 2
	}

	if *repeat < 1 {
		fmt.Fprintln(errOut, "specdelta: -repeat must be at least 1")
		return 2
	}

	// THE MOVE GATE, before a single engine is started: a run whose surface is
	// already known to be incomplete must not cost an hour first.
	mv, err := declaredMove(*move, *root, dirs, files, errOut, *partial)
	if err != nil {
		return 2
	}
	pv, err := checkProvenance(engines, []buildFacts{readBuildFacts(engines[0]), readBuildFacts(engines[1])}, *unprovenanced, errOut)
	if err != nil {
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

	// The declaration prints ABOVE the table, so a pasted table carries the
	// claim it was made under. "ten examples" was quoted from a table that said
	// nothing about which comparison it was.
	fmt.Fprint(out, "PROVENANCE")
	fmt.Fprintln(out, pv.stamp)
	for _, l := range pv.lines {
		fmt.Fprintln(out, l)
	}
	fmt.Fprintln(out, mv.header())

	var totalA, totalB, moved, regressed, suspect, appeared, wentRed int
	var movePass, moveBad, moveFiles int
	fmt.Fprintf(out, "%-56s %7s %7s %6s\n", "file", "control", "treat", "delta")
	for _, r := range rows {
		d := r.b.pass - r.a.pass
		// ⛔ A file can get WORSE without its pass count moving. Three
		// `core/binding/implicit_parameter*_spec.rb` files report
		// `pass=0 fail=0 error=0` under one engine — every example behind a
		// version guard — and 37 failures under the other, where the guard opens
		// and the methods do not exist. Delta 0, invisible, and the measurement
		// that missed them was used to size a plan.
		//
		// So the row is printed whenever EITHER the passes moved or the
		// failures did.
		badA, badB := r.a.fail+r.a.errs, r.b.fail+r.b.errs
		worse := badB - badA
		if d != 0 {
			moved++
		}
		if d == 0 && worse > 0 {
			appeared++
		}
		// ⛔ An unstable file is kept OUT of the verdict: its delta is noise, and
		// counting it would make the exit status depend on a coin flip.
		if d < 0 && !r.unstable {
			regressed++
		}
		totalA += r.a.pass
		totalB += r.b.pass
		note := outcomeNote(r.a, r.b)
		if r.unstable {
			note += "  ⛔ UNSTABLE: an engine disagreed with itself, delta excluded"
		}
		if r.a.tallyDisagrees() || r.b.tallyDisagrees() {
			note += "  ⛔ fewer detail lines than its own fail+error tally"
			suspect++
		}
		if worse > 0 {
			note += fmt.Sprintf("  ⛔ fail+error %d → %d", badA, badB)
		}
		// ⛔ A NET GAIN HIDES AN EXAMPLE THAT WENT RED. On 2026-11-02
		// core/thread/raise_spec.rb read `+17, 0 regressed` — true, and
		// incomplete: nineteen examples turned green and TWO turned red, and no
		// count of any kind can show a swap. The example that broke had been
		// passing BECAUSE of a different defect.
		//
		// So the failure DESCRIPTIONS are compared, not their number, and every
		// one that appears more often on the treatment is named.
		if newlyRed := gainedLabels(r.a.labels, r.b.labels); len(newlyRed) > 0 && !r.unstable {
			for _, l := range newlyRed {
				// The COUNT, not just the name: a shared example block included
				// twice in one file reports the same description twice, and
				// losing both is two examples rather than one.
				wentRed += l.gained
				times := ""
				if l.gained > 1 {
					times = fmt.Sprintf(" (×%d)", l.gained)
				}
				note += "\n      ⛔ WENT RED: " + strings.ReplaceAll(l.label, "\t", "  ") + times
			}
		}
		// A file the move itself changes is marked, because its delta is not a
		// reading of the engines at all: the corpus handed the two runs
		// different work. Reading `+3` on such a row as an improvement is
		// exactly the mistake this flag exists to stop.
		if n, ok := mv.opens[r.file]; ok {
			note += fmt.Sprintf("  ⛔ THE MOVE OPENS ≥%d examples here", n)
			movePass += d
			moveBad += worse
			moveFiles++
			// The note is non-empty from here, so -quiet cannot drop the row: a
			// delta of zero on a file the move opens is precisely how 37
			// failures stayed invisible.
		}
		if *quiet && d == 0 && note == "" {
			continue
		}
		fmt.Fprintf(out, "%-56s %7d %7d %+6d%s\n", r.file, r.a.pass, r.b.pass, d, note)
	}
	fmt.Fprintf(out, "\n%d file(s): control %d passing, treatment %d passing, %+d\n",
		len(rows), totalA, totalB, totalB-totalA)
	fmt.Fprintf(out, "%d file(s) moved, %d regressed", moved, regressed)
	if wentRed > 0 {
		// Counted apart from `regressed`, which is a per-FILE verdict: a file
		// can improve on balance and still contain an example that broke.
		fmt.Fprintf(out, ", %d example(s) WENT RED", wentRed)
	}
	if appeared > 0 {
		// Named separately because it is not a regression in the passing total
		// and would otherwise read as nothing at all.
		fmt.Fprintf(out, ", %d gained failures without losing passes", appeared)
	}
	fmt.Fprintln(out)
	// ⛔ The sentence a reader will quote. It must not be a passing total alone:
	// the published one was, and it read as a plan.
	if mv.on {
		fmt.Fprintf(out, "of which the declared move %s → %s accounts for %d file(s): %+d passing, %+d failing\n",
			mv.from, mv.to, moveFiles, movePass, moveBad)
		fmt.Fprintf(out, "the move also opens ≥%d examples in %d file(s) of the corpus in total%s\n",
			mv.move.ExamplesOpened(), len(gcFiles(mv.move.Opened)), mv.partialNote())
	}
	if suspect > 0 {
		// A total built on a tally the subject contradicts is not a measurement.
		fmt.Fprintf(errOut, "specdelta: %d file(s) reported a tally their detail lines contradict\n", suspect)
		return 2
	}
	// ⛔ An example that went red IS a regression, whatever the file's total did.
	// Letting a net gain carry it out would be the exact reading this check
	// exists to stop.
	if regressed > 0 || wentRed > 0 {
		return 1
	}
	return 0
}

// gainedLabels lists the failure descriptions the TREATMENT reports more often
// than the control — examples that were passing (or failing less often) and are
// not any more. Sorted, so a report is stable between runs.
//
// It reads descriptions rather than messages because a message carries object
// addresses (`#<Class:0x0000000000000008>`) that differ between two runs of the
// SAME binary, which would make every file look changed.
func gainedLabels(a, b map[string]int) []gainedLabel {
	var out []gainedLabel
	for label, n := range b {
		if n > a[label] {
			out = append(out, gainedLabel{label: label, gained: n - a[label]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].label < out[j].label })
	return out
}

// gainedLabel is one failure description and HOW MANY more times the treatment
// reports it than the control.
type gainedLabel struct {
	label  string
	gained int
}

// declared is what -move resolved to: either "the corpus is the same on both
// sides" or a census of what the version move does to it.
type declared struct {
	on       bool
	from, to string
	move     gc.Move
	opens    map[string]int // measured file -> lower-bound examples the move opens there
	partial  bool
}

func gcFiles(sites []gc.Site) []string { return gc.Files(sites) }

func (d declared) header() string {
	if !d.on {
		return "MOVE none declared: both engines read the same corpus, so every delta below is the ENGINE\n"
	}
	return fmt.Sprintf(
		"MOVE %s → %s declared: the CORPUS differs between the two runs — %d guards open, %d close, %d inline sites flip%s\n",
		d.from, d.to, len(d.move.Opened), len(d.move.Closed), len(d.move.Flipped), d.partialNote())
}

func (d declared) partialNote() string {
	if d.partial {
		return "  ⛔ PARTIAL: part of the move's surface was not measured"
	}
	return ""
}

// declaredMove resolves -move and, for a real move, REFUSES a run whose
// measured set does not cover the surface the move touches.
//
// ⛔ The refusal is the whole feature. `specdelta -dir core/set` across a
// 3.4→4.0 move is a perfectly well-formed command that produces a perfectly
// readable table saying nothing about the move, because the three files the
// move hits hardest are under core/binding. There is no way to see that in the
// results; it is only visible in the corpus, before anything runs.
func declaredMove(spec, root string, dirs []string, files []string, errOut io.Writer, allowPartial bool) (declared, error) {
	if spec == "" {
		fmt.Fprint(errOut, `specdelta: -move is required.

  -move none         both engines read the corpus at the same RUBY_VERSION, so
                     every difference below belongs to the engine
  -move 3.4.1:4.0.9  the engines read the corpus at DIFFERENT versions: the
                     guards move too, and the census says which files that hits

There is no default, because the results cannot answer it. A version move was
measured here on 2026-11-01 and published as "ten examples of work"; the real
figure was about seventy-five, and the missing part was three spec files whose
every example is guarded away below 4.0 — zero passes and zero failures on one
side, 37 failures on the other, and a pass delta of zero either way.
`)
		return declared{}, errRefused
	}
	if spec == "none" {
		return declared{on: false}, nil
	}
	from, to, ok := strings.Cut(spec, ":")
	if !ok || from == "" || to == "" {
		fmt.Fprintf(errOut, "specdelta: -move wants \"none\" or FROM:TO, e.g. 3.4.1:4.0.9; got %q\n", spec)
		return declared{}, errRefused
	}
	if gc.CompareVersions(from, to) == 0 {
		fmt.Fprintf(errOut, "specdelta: -move %s:%s names the same version twice — that is -move none\n", from, to)
		return declared{}, errRefused
	}

	sites, read, err := gc.Walk(root, []string{"language", "core"})
	if err != nil {
		fmt.Fprintf(errOut, "specdelta: censusing %s: %v\n", root, err)
		return declared{}, errRefused
	}
	if read == 0 {
		// ⛔ An unreadable corpus must not pass as a move that touches nothing:
		// "found no guards" and "could not look" are the same number.
		fmt.Fprintf(errOut, "specdelta: no *_spec.rb under %s/{language,core} — the move cannot be censused\n", root)
		return declared{}, errRefused
	}

	d := declared{on: true, from: from, to: to, move: gc.ClassifyMove(sites, from, to), opens: map[string]int{}}

	measured := make(map[string]bool, len(files))
	for _, f := range files {
		measured[filepath.ToSlash(f)] = true
	}
	for _, s := range d.move.Opened {
		if measured[s.File] {
			d.opens[s.File] += s.Examples
		}
	}
	var outside []string
	for _, f := range d.move.Touched() {
		if !measured[f] {
			outside = append(outside, f)
		}
	}
	if len(outside) == 0 {
		return d, nil
	}
	sort.Strings(outside)
	fmt.Fprintf(errOut, "specdelta: the move %s → %s touches %d file(s) that -dir %s does not measure:\n", from, to, len(outside), strings.Join(dirs, ","))
	for i, f := range outside {
		if i == 12 {
			fmt.Fprintf(errOut, "    … and %d more\n", len(outside)-i)
			break
		}
		fmt.Fprintf(errOut, "    %s\n", f)
	}
	if !allowPartial {
		// ⛔ Name the -dir that WOULD cover it. "Widen -dir" was advice the
		// reader could not act on while -dir held one directory: widening
		// `core` meant the whole corpus. An exit nobody can reach makes the
		// derogation the normal road, which is the opposite of a gate.
		fmt.Fprintln(errOut, "  A number from this run would not be a number about the move. Either")
		fmt.Fprintf(errOut, "    -dir %s\n", strings.Join(coveringDirs(dirs, d.move.Touched()), ","))
		fmt.Fprintln(errOut, "  which measures the whole surface, or -partial to say so on purpose —")
		fmt.Fprintln(errOut, "  the report is then stamped PARTIAL.")
		return declared{}, errRefused
	}
	d.partial = true
	return d, nil
}

// errRefused carries nothing: the refusing function has already said why, in
// the words the reader needs rather than a wrapped error string.
var errRefused = errors.New("refused")

// buildFacts is what can be learned about an engine binary WITHOUT running it.
type buildFacts struct {
	known   bool   // Go build information was readable at all
	rev     string // vcs.revision, as `go build` stamps it
	dirty   bool   // vcs.modified: the tree had uncommitted changes
	treeRev string // HEAD of the git work tree the binary sits in, "" if none
}

// prov is the provenance verdict: what to print, and whether it is attributable.
type prov struct {
	lines []string
	stamp string
}

// ⛔ THE SECOND RULE THIS TOOL NOW ENFORCES, and it is a repeat offence.
//
// On 2026-11-01 a figure was published for PR #858 — `pass=118 fail=1` — from a
// binary built BEFORE that branch existed. It carried the previous branch's
// code. The true figure was 117/2, and the correction had to go on the record.
// The same shape is in the notes from an earlier arc: a control built from the
// WORKING TREE rather than from the commit, which attributed +3 to the wrong
// fix. Writing it down twice did not stop it a third time.
//
// `go build` already stamps `vcs.revision` and `vcs.modified` into every binary
// it produces from a repository, so the binary can be asked what it is. The
// three refusals below are the three ways a comparison is void before it starts:
//
//	same revision      nothing to compare: one of them was not rebuilt
//	dirty tree         the revision names code the binary does not contain
//	stale against HEAD the tree moved on after the binary was built — #858
//
// An engine with no Go build information at all (MRI, a shell fake) is NOT
// refused: it is legitimately not a Go binary. It is STAMPED, because a table
// nobody can attribute must say so rather than look like the others.
func checkProvenance(engines []string, facts []buildFacts, allow bool, errOut io.Writer) (prov, error) {
	var p prov
	unattributable := false
	for i, f := range facts {
		switch {
		case !f.known:
			p.lines = append(p.lines, fmt.Sprintf("  %-9s %s — no Go build information", role(i), engines[i]))
			unattributable = true
		case f.dirty:
			p.lines = append(p.lines, fmt.Sprintf("  %-9s %s — built from %s with UNCOMMITTED changes", role(i), engines[i], short(f.rev)))
			unattributable = true
		default:
			p.lines = append(p.lines, fmt.Sprintf("  %-9s %s — built from %s", role(i), engines[i], short(f.rev)))
		}
	}

	var why []string
	if facts[0].known && facts[1].known && !facts[0].dirty && !facts[1].dirty && facts[0].rev == facts[1].rev {
		why = append(why, fmt.Sprintf("both engines were built from %s — one of them was not rebuilt, and there is nothing to compare", short(facts[0].rev)))
	}
	for i, f := range facts {
		if f.dirty {
			why = append(why, fmt.Sprintf("the %s was built from a tree with uncommitted changes, so %s does not name the code it contains", role(i), short(f.rev)))
		}
		// ⛔ #858 exactly: the binary is older than the branch it is being
		// quoted for. Nothing in the results can show this.
		if f.known && !f.dirty && f.treeRev != "" && f.treeRev != f.rev {
			why = append(why, fmt.Sprintf("the %s binary was built from %s but its work tree is now at %s — it is not the code you are measuring",
				role(i), short(f.rev), short(f.treeRev)))
		}
	}
	if len(why) == 0 {
		if unattributable {
			p.stamp = "  ⛔ UNPROVENANCED"
		}
		return p, nil
	}
	if allow {
		p.stamp = "  ⛔ UNPROVENANCED"
		return p, nil
	}
	fmt.Fprintln(errOut, "specdelta: this comparison cannot be attributed to any pair of commits:")
	for _, w := range why {
		fmt.Fprintf(errOut, "    %s\n", w)
	}
	fmt.Fprintln(errOut, "  Rebuild, or pass -unprovenanced to say so on purpose — the report is then stamped.")
	return prov{}, errRefused
}

func role(i int) string {
	if i == 0 {
		return "control"
	}
	return "treatment"
}

func short(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	if rev == "" {
		return "(no revision)"
	}
	return rev
}

// readBuildFacts asks a binary what it is. Replaceable so the policy above can
// be driven by a table rather than by building Go binaries in a test.
var readBuildFacts = func(path string) buildFacts {
	var f buildFacts
	bi, err := buildinfo.ReadFile(path)
	if err != nil {
		return f
	}
	f.known = true
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			f.rev = s.Value
		case "vcs.modified":
			f.dirty = s.Value == "true"
		}
	}
	if f.rev == "" {
		// Stamped with no revision is the same as not stamped: there is
		// nothing to attribute it to.
		f.known = false
		return f
	}
	f.treeRev = headOf(filepath.Dir(path))
	return f
}

// headOf asks git for a work tree's HEAD, and answers "" for anything it
// cannot establish — a binary outside a repository is not a defect.
func headOf(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// splitDirs reads the comma-separated -dir into a set, in the order given and
// without repeats. An empty element is dropped rather than read as the root:
// `-dir core,` must not quietly become the whole corpus.
func splitDirs(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		d := filepath.ToSlash(strings.TrimSpace(part))
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// specFiles lists the *_spec.rb files under each of root/dirs, as paths
// relative to root — which is how the shim expects to be handed them.
//
// ⛔ The result is DEDUPLICATED. `-dir core,core/set` names core/set twice, and
// a file run twice would be counted twice on both sides: a total that moves
// with how the caller spelled the set, not with the engines.
func specFiles(root string, dirs []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, dir := range dirs {
		base := filepath.Join(root, dir)
		info, err := os.Stat(base)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", base)
		}
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
			rel = filepath.ToSlash(rel)
			if seen[rel] {
				return nil
			}
			seen[rel] = true
			out = append(out, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

// coveringDirs is the -dir the refusal advises: the set already asked for, plus
// the top-level suite of every file the move touches that it does not reach.
//
// Top level and no deeper on purpose. A list of the 50 exact directories would
// be unreadable and would go stale the moment the corpus moves; `core,language`
// is both the honest surface and something a reader can retype.
func coveringDirs(dirs, touched []string) []string {
	out := append([]string(nil), dirs...)
	have := map[string]bool{}
	for _, d := range out {
		have[d] = true
	}
	for _, f := range touched {
		top := filepath.ToSlash(f)
		if i := strings.Index(top, "/"); i >= 0 {
			top = top[:i]
		}
		if covered(have, filepath.ToSlash(f)) || have[top] {
			continue
		}
		have[top] = true
		out = append(out, top)
	}
	sort.Strings(out)
	return out
}

// covered says whether one of the named directories already contains f.
func covered(dirs map[string]bool, f string) bool {
	for d := range dirs {
		if f == d || strings.HasPrefix(f, d+"/") {
			return true
		}
	}
	return false
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
	// ⛔ CARRY THE KILL ACROSS THE PARSE. This used to `return parse(...)`, which
	// builds a FRESH result and threw `killed` away on the floor -- so `killed`
	// was false on every row ever printed and the "KILLED on timeout" note was
	// dead code. A killed engine usually prints no RESULT line either, so it fell
	// through to `ran == false` and the report said "DID NOT RUN on the
	// treatment": a HANG was published as an ABSENCE, which is a statement about
	// conformance rather than about the clock, and the two call for opposite work.
	//
	// It also broke the control for the report as a whole. "grep KILLED; if 0 the
	// table is sound" is the documented way to tell whether the machine's load
	// decided a cell -- and that grep could only ever answer 0.
	parsed, err := parse(out.String())
	parsed.killed = r.killed
	return parsed, err
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
			// RBGO_DETAIL \t kind \t class \t description \t message
			if f := strings.Split(line, "\t"); len(f) >= 4 {
				if r.labels == nil {
					r.labels = map[string]int{}
				}
				r.labels[f[1]+"\t"+f[3]]++
			}
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
