// guards censuses ruby/spec's `ruby_version_is` by SHAPE, because the two
// shapes move the population in different directions and only one of them has
// ever been counted.
//
//	BLOCK     ruby_version_is "4.0" do … end
//	          The examples inside are SKIPPED while CUR_VERSION is below the
//	          threshold. Raising it ADDS them to the population.
//
//	INLINE    msg_class = ruby_version_is("4.0") ? "Module" : "module"
//	          The example RUNS either way; the guard only picks a value. Below
//	          the threshold it picks the PRE-4.0 branch, so the example holds
//	          rbgo to a string MRI no longer produces. Raising CUR_VERSION
//	          changes what these ASSERT without changing how many there are.
//
// Why it exists: #799 asks for a decision about CUR_VERSION and says both
// directions must be measured before BASELINE is regenerated, because
// `UPDATE_BASELINE=1` records whatever it sees and would absorb a loss
// silently. This is the static half of that measurement — the half that needs
// no sweep, and so can be run on a loaded machine, where a sweep cannot.
//
// ⛔ THE SHAPE IS DECIDED BY WHAT OPENS A BLOCK, NOT BY THE PARENTHESES. A
// first draft keyed on "no parentheses, and the line ends in do". It was wrong
// twice, and both were found by PRINTING THE SITES instead of only the totals:
//
//	ruby_version_is "4.0" do # https://bugs.ruby-lang.org/issues/21844
//	    a block guard whose trailing comment defeats a `do\s*$` anchor
//	ruby_version_is("4.0") do
//	    a block guard that IS parenthesised
//
// Five guards at "4.0" hide in those two shapes, so a census anchored that way
// reports 56 where there are 61.
//
// ⛔ THE EXAMPLE COUNT IS A LOWER BOUND AND SAYS SO. Counting `it` blocks
// inside a guard needs Ruby `end`-matching, which is a heuristic without a
// parser — and a static count cannot see shared examples (`it_behaves_like`)
// at all. The control below restricts the count to exactly the guards a
// `do\s*$` anchor finds, where #799 measured 56 guards and 167 examples: the
// guard number reproduces and the example number does not, which places the
// difference in the counting rather than in the census. Tuning the heuristic
// until it reached 167 would be fitting the instrument to the expected answer.
//
// Exit status: 0 the census ran; 2 the corpus could not be read — a refusal,
// not an empty census.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	callRe    = regexp.MustCompile(`ruby_version_is`)
	argRe     = regexp.MustCompile(`ruby_version_is\s*\(?\s*["']([^"']*)["']\s*(?:(\.\.\.?)\s*["']([^"']*)["']\s*)?\)?`)
	opensRe   = regexp.MustCompile(`(?:\bdo\b|\{)\s*(?:\|[^|]*\|)?\s*$`)
	bracesRe  = regexp.MustCompile(`ruby_version_is\s*\(?[^)]*\)?\s*\{`)
	commentRe = regexp.MustCompile(`\s+#(?:[^{].*)?$`)
	// oldAnchorRe is the earlier census's shape, kept ONLY so its number can
	// be reproduced as a control. Nothing else should use it.
	oldAnchorRe = regexp.MustCompile(`ruby_version_is\s+["'][^"']*["']\s*(\.\.\.?\s*["'][^"']*["']\s*)?do\s*$`)
	itRe        = regexp.MustCompile(`^\s*it[\s(]`)
	openKw      = regexp.MustCompile(`(^|\s)(def|class|module|case|begin|while|until|unless|if)\s`)
	modifIf     = regexp.MustCompile(`\S\s+(if|unless|while|until)\s`)
	endKw       = regexp.MustCompile(`^\s*end\b`)
	doKw        = regexp.MustCompile(`\bdo\b\s*(\|[^|]*\|)?\s*(#.*)?$`)
	braceEnd    = regexp.MustCompile(`^\s*\}`)
)

// shape is what a mention of ruby_version_is does.
type shape int

const (
	block shape = iota
	inline
	unknown
)

func (s shape) String() string {
	switch s {
	case block:
		return "block"
	case inline:
		return "inline"
	}
	return "unclassified"
}

// Site is one mention.
type Site struct {
	File, Text string
	Line       int
	From, To   string
	Shape      shape
	InIt       bool // an inline inside an `it` moves exactly one example
	Examples   int  // `it` blocks inside a block guard: a LOWER BOUND
	OldAnchor  bool // a `do$` anchor would have found it
}

// Key is the version range a site guards, as it reads in the source.
func (s Site) Key() string {
	if s.To != "" {
		return s.From + "..." + s.To
	}
	if s.From == "" {
		return `"" (open lower bound)`
	}
	return s.From
}

// Classify decides what a single line of Ruby does with ruby_version_is.
// Split out because it is the part that was wrong twice, so it is the part a
// test drives directly.
func Classify(line string) (Site, bool) {
	if !callRe.MatchString(line) {
		return Site{}, false
	}
	s := Site{Text: strings.TrimSpace(line)}
	m := argRe.FindStringSubmatch(line)
	if m == nil {
		s.Shape = unknown
		return s, true
	}
	s.From = m[1]
	if m[2] != "" {
		s.To = m[3]
	}
	s.OldAnchor = oldAnchorRe.MatchString(line)
	bare := commentRe.ReplaceAllString(line, "")
	if opensRe.MatchString(bare) || bracesRe.MatchString(bare) {
		s.Shape = block
	} else {
		s.Shape = inline
	}
	return s, true
}

// ExamplesIn counts the `it` blocks between a guard and its matching `end`.
// A LOWER BOUND: shared examples expand at run time and are invisible here.
func ExamplesIn(lines []string, start int) int {
	depth, n := 1, 0
	for i := start + 1; i < len(lines) && depth > 0; i++ {
		l := lines[i]
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		if endKw.MatchString(l) || braceEnd.MatchString(l) {
			depth--
			continue
		}
		if doKw.MatchString(l) || (openKw.MatchString(l) && !modifIf.MatchString(l)) {
			depth++
		}
		if itRe.MatchString(l) {
			n++
		}
	}
	return n
}

// Census reads one spec file.
func Census(path string, src []byte) []Site {
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	var out []Site
	itDepth := 0
	for i, line := range lines {
		if itRe.MatchString(line) {
			itDepth++
		} else if strings.TrimSpace(line) == "end" && itDepth > 0 {
			itDepth--
		}
		s, ok := Classify(line)
		if !ok {
			continue
		}
		s.File, s.Line, s.InIt = path, i+1, itDepth > 0
		if s.Shape == block {
			s.Examples = ExamplesIn(lines, i)
		}
		out = append(out, s)
	}
	return out
}

func main() {
	root := flag.String("root", ".", "the ruby/spec checkout")
	list := flag.Bool("list", false, "print every inline site and every guard the old anchor missed")
	flag.Parse()

	var sites []Site
	files := 0
	walk := func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, "_spec.rb") {
			return nil
		}
		files++
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		sites = append(sites, Census(strings.TrimPrefix(strings.TrimPrefix(p, *root), "/"), b)...)
		return nil
	}

	// ⛔ language/ and core/ ONLY, named rather than filtered. A draft walked
	// the checkout minus a denylist of directory names and counted 2207 files
	// where the gate counts 2206: a denylist inherits whatever it has not
	// heard of. The gate sweeps exactly two suites, so this walks exactly two.
	var err error
	for _, suite := range []string{"language", "core"} {
		if err = filepath.WalkDir(filepath.Join(*root, suite), walk); err != nil {
			break
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "guards: walking %s: %v\n", *root, err)
		os.Exit(2)
	}
	if files == 0 {
		fmt.Fprintf(os.Stderr, "guards: no *_spec.rb under %s/{language,core} -- not a ruby/spec checkout\n", *root)
		os.Exit(2)
	}

	var blocks, inlines, unk []Site
	for _, s := range sites {
		switch s.Shape {
		case block:
			blocks = append(blocks, s)
		case inline:
			inlines = append(inlines, s)
		default:
			unk = append(unk, s)
		}
	}

	fmt.Printf("%d spec files; %d mentions: %d block, %d inline, %d unclassified\n\n",
		files, len(sites), len(blocks), len(inlines), len(unk))

	// THE CONTROL FIRST. If the subset the earlier census could see does not
	// reproduce, nothing below it is worth reading.
	ctlG, ctlE := 0, 0
	for _, s := range blocks {
		if s.OldAnchor && s.Key() == "4.0" {
			ctlG++
			ctlE += s.Examples
		}
	}
	fmt.Printf("CONTROL -- the subset a `do$` anchor finds at \"4.0\": %d guards, %d examples\n", ctlG, ctlE)
	fmt.Printf("           #799 reported 56 guards, 167 examples\n")
	fmt.Printf("           guards   %s\n", verdict(ctlG == 56))
	fmt.Printf("           examples %s\n", verdict(ctlE == 167))
	if ctlG == 56 && ctlE != 167 {
		fmt.Print(`           The GUARD identification reproduces and the EXAMPLE count does
           not, which places the difference in the counting rather than in
           the census: a static ` + "`it`" + ` count cannot see shared examples. Read
           the example columns as a LOWER BOUND and the guard columns as the
           measurement.
`)
	}
	fmt.Println()

	fmt.Println("BLOCK guards -- raising CUR_VERSION ADDS these examples to the population")
	tally(blocks, true)
	fmt.Println("INLINE guards -- the example runs either way; the threshold flips what it asserts")
	tally(inlines, false)

	inIt := 0
	for _, s := range inlines {
		if s.InIt {
			inIt++
		}
	}
	fmt.Printf("  %d inline sites sit inside an `it` (one example each);\n", inIt)
	fmt.Printf("  %d sit at describe level, where one site can steer SEVERAL examples\n\n", len(inlines)-inIt)

	if *list {
		fmt.Println(`BLOCK guards at "4.0" a ` + "`do$`" + ` anchor does NOT find:`)
		for _, s := range blocks {
			if !s.OldAnchor && s.Key() == "4.0" {
				fmt.Printf("  %s:%d  %s\n", s.File, s.Line, s.Text)
			}
		}
		fmt.Println("\nevery inline site:")
		sort.Slice(inlines, func(i, j int) bool {
			if inlines[i].File != inlines[j].File {
				return inlines[i].File < inlines[j].File
			}
			return inlines[i].Line < inlines[j].Line
		})
		for _, s := range inlines {
			where := "describe"
			if s.InIt {
				where = "it"
			}
			fmt.Printf("  %s:%d  [%s]  %s\n", s.File, s.Line, where, s.Text)
		}
	}
	for _, s := range unk {
		fmt.Printf("UNCLASSIFIED %s:%d  %s\n", s.File, s.Line, s.Text)
	}
}

func tally(sites []Site, withExamples bool) {
	type agg struct{ guards, examples int }
	by := map[string]*agg{}
	for _, s := range sites {
		k := s.Key()
		if by[k] == nil {
			by[k] = &agg{}
		}
		by[k].guards++
		by[k].examples += s.Examples
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return by[keys[i]].guards > by[keys[j]].guards })
	for _, k := range keys {
		if withExamples {
			fmt.Printf("  %-26s %4d guards  %5d examples (lower bound)\n", k, by[k].guards, by[k].examples)
		} else {
			fmt.Printf("  %-26s %4d sites\n", k, by[k].guards)
		}
	}
	fmt.Println()
}

func verdict(ok bool) string {
	if ok {
		return "REPRODUCED"
	}
	return "DOES NOT REPRODUCE"
}
