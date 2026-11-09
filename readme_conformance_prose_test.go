// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package ruby_test

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Why this file exists.
//
// TestReadmeConformanceCountMatchesTheBaseline already ties the ruby/spec
// example COUNT to the gate that measures it, because "a number a human
// retypes is a number that rots". Everything else the README says about that
// measurement was still retyped, and had rotted in both directions at once:
//
//   - the prose divided by a ratio of 93.3 % while the table it interprets says
//     95.0 %, and the headline warned against reading it as "93 % of Ruby";
//   - the prose counted 477 skips and 10 unreadable files where the table says
//     491 and 4;
//   - the list of stubbed shim helpers named `be_computed_by`, which is
//     implemented (#911 corrected that in the conformance README and nowhere
//     else), named "some argf/IO helpers", which do not exist, and omitted
//     `ruby_cmd`, which sits one line below the `ruby_exe` it does name.
//
// Every one of those is derivable from a file in this repository, so none of
// them needs to be trusted. These two tests derive them.
//
// ⛔ They deliberately do NOT re-measure conformance: the table is one dated
// sweep and is allowed to be older than the baseline (see the note beside it).
// What is checked is that the README is consistent with ITSELF and with the
// shim — a dated measurement going stale is not an error, a derived number
// contradicting the measurement it is derived from is.

const (
	rootReadmePath = "README.md"
	shimReadmePath = "scripts/conformance/rubyspec/README.md"
	shimHelperPath = "scripts/conformance/rubyspec/spec_helper.rb"
)

// ---------------------------------------------------------------------------
// The stubbed-helper census
// ---------------------------------------------------------------------------

// stubbedHelper is one shim helper that exists only to raise SpecSkip, with the
// line the READMEs are required to cite.
type stubbedHelper struct {
	name string
	line int
}

// specSkipRaise matches a line that RAISES SpecSkip, in either spelling the
// shim uses (`raise SpecSkip, …` and `::Kernel.raise(SpecSkip, …)`).
var specSkipRaise = regexp.MustCompile(`\braise[\s(]+SpecSkip\b`)

// oneLineStubDef matches the whole shape the shim's stubs take: a one-line
// `def name(args); raise SpecSkip…; end`.
var oneLineStubDef = regexp.MustCompile(`^\s*def\s+([A-Za-z_][A-Za-z0-9_]*[?!]?)\b`)

// shimStubbedHelpers reads spec_helper.rb and returns every helper that is
// stubbed, i.e. whose entire body raises SpecSkip.
//
// ⛔ POPULATION, stated rather than left to the reader. `skip` is NOT in it:
// `skip` raises SkippedSpecError (a SpecSkip subclass) at :318 because the spec
// ASKED to be skipped, which is mspec behaviour rbgo implements, not a hole in
// the shim. The census is "helpers that skip because the shim cannot do the
// thing", and that is what the READMEs' table claims to list.
//
// It also refuses rather than under-reporting: a SpecSkip raise that is not a
// one-line `def` is a stub this parser cannot name, and it fails the test
// asking for the parser to be widened instead of silently dropping it.
func shimStubbedHelpers(t *testing.T) []stubbedHelper {
	t.Helper()
	f, err := os.Open(shimHelperPath)
	if err != nil {
		t.Fatalf("open %s: %v", shimHelperPath, err)
	}
	defer f.Close()

	var out []stubbedHelper
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if !specSkipRaise.MatchString(line) {
			continue
		}
		if strings.Contains(line, "SkippedSpecError") {
			continue // the spec asked to skip; see the population note above
		}
		m := oneLineStubDef.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("%s:%d raises SpecSkip but is not a one-line `def`, so this\n"+
				"census cannot name it. Widen shimStubbedHelpers rather than leaving\n"+
				"a stub out of the README table:\n  %s", shimHelperPath, n, strings.TrimSpace(line))
		}
		out = append(out, stubbedHelper{name: m[1], line: n})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", shimHelperPath, err)
	}
	if len(out) == 0 {
		t.Fatalf("%s raises SpecSkip nowhere; the shim cannot be the source of truth it is used as", shimHelperPath)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// shimReadmeStubTable returns the rows of the conformance README's
// "stubbed (raises `SpecSkip`)" table as name -> cited spec_helper.rb line, and
// the count of rows it found (which may exceed len(map) if a row names nothing).
func shimReadmeStubTable(t *testing.T) (map[string]int, int) {
	t.Helper()
	body, err := os.ReadFile(shimReadmePath)
	if err != nil {
		t.Fatalf("read %s: %v", shimReadmePath, err)
	}
	lines := strings.Split(string(body), "\n")

	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "| stubbed (raises `SpecSkip`)") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s has no \"| stubbed (raises `SpecSkip`) |\" table header; this test\n"+
			"cannot check a list it cannot find, and the list is the thing that rots", shimReadmePath)
	}

	name := regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_]*[?!]?)`")
	cite := regexp.MustCompile(`spec_helper\.rb:(\d+)`)

	got, rows := map[string]int{}, 0
	for i := start + 2; i < len(lines); i++ { // +2 skips the |---|---| separator
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "|") {
			break
		}
		rows++
		nm := name.FindStringSubmatch(line)
		if nm == nil {
			t.Errorf("%s:%d is a row of the stubbed-helper table that names no helper:\n  %s",
				shimReadmePath, i+1, line)
			continue
		}
		cm := cite.FindStringSubmatch(line)
		if cm == nil {
			t.Errorf("%s:%d names `%s` but cites no spec_helper.rb line. An empty cell is\n"+
				"how the phantom \"argf/IO helpers\" row survived; cite it or drop it:\n  %s",
				shimReadmePath, i+1, nm[1], line)
			continue
		}
		n, err := strconv.Atoi(cm[1])
		if err != nil {
			t.Fatalf("%s:%d: unparsable line citation %q", shimReadmePath, i+1, cm[1])
		}
		got[nm[1]] = n
	}
	return got, rows
}

// TestReadmeStubbedHelpersMatchTheShim: both READMEs claim to list the shim
// helpers that are stubbed, and that list is what tells a reader whether the
// conformance total is a lower bound. It is derived here from the shim itself,
// in BOTH directions -- a helper the shim stubs and the README omits hides a
// skip, and a helper the README calls stubbed when it is implemented sends the
// reader hunting among the skips for an example that actually FAILED, which is
// precisely how #909 was nearly missed.
func TestReadmeStubbedHelpersMatchTheShim(t *testing.T) {
	stubs := shimStubbedHelpers(t)

	// --- the conformance README's table: exactly this set, with right lines ---
	table, rows := shimReadmeStubTable(t)
	if rows != len(stubs) {
		t.Errorf("%s's stubbed-helper table has %d row(s), the shim stubs %d helper(s)",
			shimReadmePath, rows, len(stubs))
	}
	for _, s := range stubs {
		line, ok := table[s.name]
		if !ok {
			t.Errorf("%s stubs `%s` at %s:%d, and %s's table does not list it",
				shimHelperPath, s.name, shimHelperPath, s.line, shimReadmePath)
			continue
		}
		if line != s.line {
			t.Errorf("%s cites `%s` at %s:%d; it is at :%d",
				shimReadmePath, s.name, shimHelperPath, line, s.line)
		}
		delete(table, s.name)
	}
	for name := range table {
		t.Errorf("%s's table calls `%s` stubbed; %s does not raise SpecSkip for it,\n"+
			"so its examples RUN and a failure of theirs counts against the ratchet",
			shimReadmePath, name, shimHelperPath)
	}

	// --- the top-level README's bullet: names every stub, and no phantom ---
	root, err := os.ReadFile(rootReadmePath)
	if err != nil {
		t.Fatalf("read %s: %v", rootReadmePath, err)
	}
	bullet := stubBullet(t, string(root))
	for _, s := range stubs {
		if !strings.Contains(bullet, "`"+s.name+"`") {
			t.Errorf("%s's \"The shim is not MSpec\" bullet does not name `%s`, which the\n"+
				"shim stubs at %s:%d", rootReadmePath, s.name, shimHelperPath, s.line)
		}
	}
	// And the other direction, which the negative control for this test showed was
	// still open: the old bullet named `be_computed_by` AS STUBBED. Everything the
	// bullet names before it reaches "a conservative lower bound" is being called
	// stubbed; after that sentence it may discuss helpers that are not.
	claimed := bullet
	if i := strings.Index(claimed, "conservative lower bound"); i >= 0 {
		claimed = claimed[:i]
	}
	stubNames := map[string]bool{}
	for _, s := range stubs {
		stubNames[s.name] = true
	}
	// A bare helper name only: `spec_helper.rb:882-883` and the like do not match,
	// so a citation is never mistaken for a claim.
	helperName := regexp.MustCompile("`([a-z_][a-z0-9_]*[?!]?)`")
	for _, m := range helperName.FindAllStringSubmatch(claimed, -1) {
		if !stubNames[m[1]] {
			t.Errorf("%s's \"The shim is not MSpec\" bullet calls `%s` stubbed; %s does not\n"+
				"raise SpecSkip for it, so its examples RUN and a reader sent looking for them\n"+
				"among the skips will not find them", rootReadmePath, m[1], shimHelperPath)
		}
	}

	// The three that #911 established are IMPLEMENTED. Asserting them by name is
	// not redundant with the two-way check above: it is the regression test for
	// the specific claim that was wrong, and it fails loudly if one of them is
	// ever genuinely stubbed while the READMEs still say otherwise.
	for _, implemented := range []string{"complain", "output", "be_computed_by"} {
		for _, s := range stubs {
			if s.name == implemented {
				t.Errorf("`%s` now raises SpecSkip at %s:%d, and both READMEs still say it is\n"+
					"implemented. Correct them: its examples became skips, so the total dropped\n"+
					"for a reason that is not a regression", implemented, shimHelperPath, s.line)
			}
		}
	}
}

// stubBullet returns the top-level README's "The shim is not MSpec" bullet,
// from its marker to the next bullet.
func stubBullet(t *testing.T, text string) string {
	t.Helper()
	const marker = "- **The shim is not MSpec.**"
	i := strings.Index(text, marker)
	if i < 0 {
		t.Fatalf("%s has no %q bullet; this test cannot check a list it cannot find", rootReadmePath, marker)
	}
	rest := text[i+len(marker):]
	if j := strings.Index(rest, "\n- **"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// ---------------------------------------------------------------------------
// The derived numbers
// ---------------------------------------------------------------------------

// conformanceTable is the dated sweep the README prints, read back out of it.
type conformanceTable struct {
	pass, fail, erroring, skipped, ran int
	share                              string // as printed, e.g. "95.0 %"
	filesWithResult, filesTotal        int
	filesWithoutResult                 int
}

// atoiLoose parses a count written with thousands separators.
func atoiLoose(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(strings.ReplaceAll(s, ",", ""))
	if err != nil {
		t.Fatalf("%s: unparsable count %q", rootReadmePath, s)
	}
	return n
}

var (
	rowPass   = regexp.MustCompile(`\|\s*\*\*passing examples\*\*\s*\|\s*\*\*([\d,]+)\*\*\s*\|`)
	rowFail   = regexp.MustCompile(`\|\s*failing / erroring examples\s*\|\s*([\d,]+)\s*/\s*([\d,]+)\s*\|`)
	rowSkip   = regexp.MustCompile(`\|\s*skipped \([^)]*\)\s*\|\s*([\d,]+)\s*\|`)
	rowRan    = regexp.MustCompile(`\|\s*examples that actually ran[^|]*\|\s*([\d,]+)\s*\|`)
	rowShare  = regexp.MustCompile(`\|\s*share of those that passed\s*\|\s*\*\*([\d.]+ %)\*\*\s*\|`)
	rowFiles  = regexp.MustCompile(`\|\s*spec files\s*\|\s*([\d,]+) of ([\d,]+) produced a result; ([\d,]+) produced none\s*\|`)
	allGroups = []*regexp.Regexp{rowPass, rowFail, rowSkip, rowRan, rowShare, rowFiles}
)

func readConformanceTable(t *testing.T, text string) conformanceTable {
	t.Helper()
	for _, re := range allGroups {
		if !re.MatchString(text) {
			t.Fatalf("%s has no table row matching %s.\nThe table is this test's subject; if its\n"+
				"shape changed, teach the regexp rather than deleting the check", rootReadmePath, re)
		}
	}
	var c conformanceTable
	c.pass = atoiLoose(t, rowPass.FindStringSubmatch(text)[1])
	fe := rowFail.FindStringSubmatch(text)
	c.fail, c.erroring = atoiLoose(t, fe[1]), atoiLoose(t, fe[2])
	c.skipped = atoiLoose(t, rowSkip.FindStringSubmatch(text)[1])
	c.ran = atoiLoose(t, rowRan.FindStringSubmatch(text)[1])
	c.share = rowShare.FindStringSubmatch(text)[1]
	fm := rowFiles.FindStringSubmatch(text)
	c.filesWithResult = atoiLoose(t, fm[1])
	c.filesTotal = atoiLoose(t, fm[2])
	c.filesWithoutResult = atoiLoose(t, fm[3])
	return c
}

// TestReadmeConformanceProseMatchesItsOwnTable: the paragraph under the table
// re-states the table's numbers in words, and words do not move when a sweep is
// re-run. Each check below failed on the commit that introduced this test.
func TestReadmeConformanceProseMatchesItsOwnTable(t *testing.T) {
	body, err := os.ReadFile(rootReadmePath)
	if err != nil {
		t.Fatalf("read %s: %v", rootReadmePath, err)
	}
	text := string(body)
	c := readConformanceTable(t, text)

	// The table has to add up before anything is derived from it.
	if sum := c.pass + c.fail + c.erroring; sum != c.ran {
		t.Errorf("%s: pass+fail+error = %d+%d+%d = %d, the table says %d ran",
			rootReadmePath, c.pass, c.fail, c.erroring, sum, c.ran)
	}
	if sum := c.filesWithResult + c.filesWithoutResult; sum != c.filesTotal {
		t.Errorf("%s: %d files with a result + %d without = %d, the table says %d",
			rootReadmePath, c.filesWithResult, c.filesWithoutResult, sum, c.filesTotal)
	}

	// The printed share, to the tenth it is printed to.
	ratio := float64(c.pass) / float64(c.ran) * 100
	wantShare := fmt.Sprintf("%.1f %%", math.Round(ratio*10)/10)
	if c.share != wantShare {
		t.Errorf("%s: the table says %q passed; %d of %d is %s",
			rootReadmePath, c.share, c.pass, c.ran, wantShare)
	}

	// The prose, which is where every one of these drifted.
	for _, want := range []string{
		fmt.Sprintf("The %s divides passes by the examples that", wantShare),
		fmt.Sprintf("the %d skips", c.skipped),
		fmt.Sprintf("the %d files that produced no result", c.filesWithoutResult),
		fmt.Sprintf(`"rbgo implements %.0f %% of Ruby"`, math.Round(ratio)),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("%s does not contain %q, which its own conformance table requires",
				rootReadmePath, want)
		}
	}
}
