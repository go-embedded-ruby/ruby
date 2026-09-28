// Command ratchet judges one ruby/spec sweep against a per-file baseline.
//
// It replaces a single frozen scalar. The scalar could not work: a spec file
// that fails to load carries tens of examples -- one flip was measured moving
// the total by 43 on a documentation-only commit -- so the floor needed a margin
// wider than the largest file, and a gate with a 43-example margin is blind to
// every regression smaller than 43. Worse, the number it reported was
// unattributable: a drop of 43 is one file not loading, or 43 specs regressing,
// and those need opposite responses.
//
// A per-file baseline needs no margin at all. Each file is judged against its
// own recorded count, so a real regression of one example in one file is visible
// while a file that failed to load is reported as a load failure and not summed
// into the same number. The total is still printed, but as a derived summary
// rather than as the thing being gated.
//
// It reads the sweep's TSV on stdin or from -results:
//
//	OK\t<path>\t<pass-count>
//	FILEFAIL\t<path>
//
// and the baseline from -baseline, in the same shape minus the counts it does
// not need. Exit 0 means no file went backwards.
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

// state is what the baseline records for one file, and what a sweep measures.
// A file that did not load is not the same as a file that loaded and passed
// zero examples: the first is an infrastructure fact, the second a conformance
// one, and collapsing them is how the scalar floor became unattributable.
type state struct {
	loaded bool
	pass   int
}

func (s state) String() string {
	if !s.loaded {
		return "did not load"
	}
	return fmt.Sprintf("%d passing", s.pass)
}

// parse reads the sweep/baseline TSV. An unrecognised verb is an error rather
// than a skipped line: a sweep whose format drifted must not be read as a clean
// result, which is the failure mode this whole tool exists to prevent.
func parse(r io.Reader, what string) (map[string]state, error) {
	out := make(map[string]state)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		switch {
		case f[0] == "OK" && len(f) == 3:
			p, err := strconv.Atoi(f[2])
			if err != nil {
				return nil, fmt.Errorf("%s line %d: pass count %q is not a number", what, n, f[2])
			}
			if prev, dup := out[f[1]]; dup {
				return nil, fmt.Errorf("%s line %d: %s appears twice (was %s)", what, n, f[1], prev)
			}
			out[f[1]] = state{loaded: true, pass: p}
		case f[0] == "FILEFAIL" && len(f) == 2:
			if prev, dup := out[f[1]]; dup {
				return nil, fmt.Errorf("%s line %d: %s appears twice (was %s)", what, n, f[1], prev)
			}
			out[f[1]] = state{}
		default:
			return nil, fmt.Errorf("%s line %d: unrecognised record %q", what, n, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", what, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s is empty -- a sweep that measured nothing must not read as a clean result", what)
	}
	return out, nil
}

// write emits a baseline in the same format it reads, sorted, so a regenerated
// baseline diffs cleanly against its predecessor.
func write(w io.Writer, m map[string]state, header string) error {
	paths := make([]string, 0, len(m))
	for p := range m {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	b := bufio.NewWriter(w)
	fmt.Fprint(b, header)
	for _, p := range paths {
		if s := m[p]; s.loaded {
			fmt.Fprintf(b, "OK\t%s\t%d\n", p, s.pass)
		} else {
			fmt.Fprintf(b, "FILEFAIL\t%s\n", p)
		}
	}
	return b.Flush()
}

// verdict is the whole comparison, computed before anything is printed so the
// summary cannot disagree with the detail.
type verdict struct {
	regressed  []string // loaded, but fewer passing than the baseline
	unloaded   []string // baseline says it loaded; this sweep says it did not
	missing    []string // in the baseline, absent from the sweep entirely
	improved   []string // more passing than the baseline
	nowLoading []string // baseline says it did not load; this sweep says it does
	added      []string // in the sweep, absent from the baseline (corpus bump)
	base, got  int      // derived totals, reported but not gated
}

func (v *verdict) bad() bool {
	return len(v.regressed) > 0 || len(v.unloaded) > 0 || len(v.missing) > 0
}

func compare(base, got map[string]state) *verdict {
	v := &verdict{}
	for p, b := range base {
		v.base += b.pass
		g, present := got[p]
		switch {
		case !present:
			v.missing = append(v.missing, p)
		case b.loaded && !g.loaded:
			v.unloaded = append(v.unloaded, fmt.Sprintf("%s (baseline: %s)", p, b))
		case g.loaded && g.pass < b.pass:
			v.regressed = append(v.regressed, fmt.Sprintf("%s: %d passing, baseline %d (-%d)", p, g.pass, b.pass, b.pass-g.pass))
		case g.loaded && g.pass > b.pass:
			if !b.loaded {
				v.nowLoading = append(v.nowLoading, fmt.Sprintf("%s: now loads, %d passing", p, g.pass))
			} else {
				v.improved = append(v.improved, fmt.Sprintf("%s: %d passing, baseline %d (+%d)", p, g.pass, b.pass, g.pass-b.pass))
			}
		}
	}
	for p, g := range got {
		v.got += g.pass
		if _, present := base[p]; !present {
			v.added = append(v.added, fmt.Sprintf("%s: %s", p, g))
		}
	}
	for _, s := range [][]string{v.regressed, v.unloaded, v.missing, v.improved, v.nowLoading, v.added} {
		sort.Strings(s)
	}
	return v
}

// section prints a named list, or nothing at all when it is empty. The count is
// always in the heading: a heading without one invites the reader to guess.
func section(w io.Writer, title string, items []string, limit int) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(w, "\n-- %s (%d) --\n", title, len(items))
	for i, s := range items {
		if limit > 0 && i == limit {
			fmt.Fprintf(w, "   ... and %d more\n", len(items)-limit)
			break
		}
		fmt.Fprintf(w, "   %s\n", s)
	}
}

// errRegression is the one failure that is not an error in the tool: the sweep
// was read correctly and the answer is no. main maps it to exit 1, distinct from
// exit 2 for a tool or input failure, so a broken ratchet cannot be mistaken for
// a failing one. It is a sentinel rather than an os.Exit inside run so that the
// verdict is reachable from a test.
var errRegression = errors.New("regression")

func main() {
	err := run(os.Args[1:], os.Stdin, os.Stdout)
	switch {
	case err == nil:
		return
	case errors.Is(err, errRegression):
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "ratchet: %v\n", err)
		os.Exit(2)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("ratchet", flag.ContinueOnError)
	fs.SetOutput(stdout)
	baseline := fs.String("baseline", "", "per-file baseline TSV (required)")
	results := fs.String("results", "", "sweep results TSV (default: stdin)")
	update := fs.Bool("update", false, "rewrite the baseline from this sweep and exit 0")
	limit := fs.Int("limit", 40, "cap each printed section at N entries (0 = no cap)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *baseline == "" {
		return fmt.Errorf("-baseline is required")
	}

	var src io.Reader = stdin
	if *results != "" {
		f, err := os.Open(*results)
		if err != nil {
			return err
		}
		defer f.Close()
		src = f
	}
	got, err := parse(src, "sweep results")
	if err != nil {
		return err
	}

	if *update {
		const header = "# ruby/spec per-file baseline. Regenerate with UPDATE_BASELINE=1 scripts/conformance/rubyspec/run.sh.\n" +
			"# One record per spec file: OK<TAB>path<TAB>passing, or FILEFAIL<TAB>path.\n" +
			"# Judged per file, so no margin is needed and every move is attributable.\n"
		f, err := os.Create(*baseline)
		if err != nil {
			return err
		}
		if err := write(f, got, header); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "baseline rewritten: %d files\n", len(got))
		return nil
	}

	bf, err := os.Open(*baseline)
	if err != nil {
		return err
	}
	defer bf.Close()
	base, err := parse(bf, "baseline")
	if err != nil {
		return err
	}

	v := compare(base, got)
	fmt.Fprintf(stdout, "files: %d measured, %d in baseline\n", len(got), len(base))
	fmt.Fprintf(stdout, "passing examples: %d (baseline %d, %+d)\n", v.got, v.base, v.got-v.base)

	section(stdout, "REGRESSED", v.regressed, *limit)
	section(stdout, "FAILED TO LOAD (the baseline says they load)", v.unloaded, *limit)
	section(stdout, "MISSING FROM THE SWEEP ENTIRELY", v.missing, *limit)
	section(stdout, "improved", v.improved, *limit)
	section(stdout, "now loading", v.nowLoading, *limit)
	section(stdout, "new files, not gated", v.added, *limit)

	if v.bad() {
		fmt.Fprintf(stdout, "\n::error::ruby/spec ratchet REGRESSION in %d file(s) -- named above\n",
			len(v.regressed)+len(v.unloaded)+len(v.missing))
		return errRegression
	}
	if n := len(v.improved) + len(v.nowLoading); n > 0 {
		fmt.Fprintf(stdout, "\n%d file(s) improved -- rerun with UPDATE_BASELINE=1 to lock it in.\n", n)
	}
	fmt.Fprintln(stdout, "\nruby/spec ratchet OK -- no file went backwards")
	return nil
}
