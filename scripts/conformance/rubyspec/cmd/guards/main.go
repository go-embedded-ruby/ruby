// guards censuses ruby/spec's `ruby_version_is` by SHAPE, because the two
// shapes move the population in different directions and only one of them has
// ever been counted. The reading itself lives in internal/guardcensus, which
// cmd/specdelta's version-move gate uses too, so the two cannot drift.
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
// With -move FROM:TO it answers the question that half of #799 actually
// needed and nobody asked: what does the target version ADD? That reading is
// now also a precondition of cmd/specdelta's verdict, rather than a report
// someone has to remember to run.
//
// Exit status: 0 the census ran; 2 the corpus could not be read — a refusal,
// not an empty census.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	gc "github.com/go-embedded-ruby/ruby/scripts/conformance/rubyspec/internal/guardcensus"
)

func main() {
	root := flag.String("root", ".", "the ruby/spec checkout")
	list := flag.Bool("list", false, "print every inline site and every guard the old anchor missed")
	move := flag.String("move", "", "a version move FROM:TO, e.g. 3.4.1:4.0.9 — report what it opens, closes and flips")
	flag.Parse()

	// ⛔ language/ and core/ ONLY, named rather than filtered: the gate sweeps
	// exactly two suites, so this walks exactly two.
	sites, files, err := gc.Walk(*root, []string{"language", "core"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "guards: walking %s: %v\n", *root, err)
		os.Exit(2)
	}
	if files == 0 {
		fmt.Fprintf(os.Stderr, "guards: no *_spec.rb under %s/{language,core} -- not a ruby/spec checkout\n", *root)
		os.Exit(2)
	}

	var blocks, inlines, unk []gc.Site
	for _, s := range sites {
		switch s.Shape {
		case gc.Block:
			blocks = append(blocks, s)
		case gc.Inline:
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

	if *move != "" {
		from, to, ok := strings.Cut(*move, ":")
		if !ok || from == "" || to == "" {
			fmt.Fprintf(os.Stderr, "guards: -move wants FROM:TO, e.g. 3.4.1:4.0.9; got %q\n", *move)
			os.Exit(2)
		}
		reportMove(gc.ClassifyMove(sites, from, to), *list)
	}

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

// reportMove prints what the move does to the corpus's own population. The
// OPENED section comes first and names its files, because that is the half a
// pass-only delta cannot see: a file whose every example is guarded away on one
// side scores zero on both counters there and moves by nothing.
func reportMove(m gc.Move, list bool) {
	fmt.Printf("MOVE %s -> %s -- what the CORPUS does, before any engine runs\n", m.From, m.To)
	fmt.Printf("  OPENED   %4d block guards in %3d files  (>= %d examples appear; a lower bound)\n",
		len(m.Opened), len(gc.Files(m.Opened)), m.ExamplesOpened())
	fmt.Printf("  CLOSED   %4d block guards in %3d files  (examples vanish)\n",
		len(m.Closed), len(gc.Files(m.Closed)))
	fmt.Printf("  FLIPPED  %4d inline sites in %3d files  (the same example asserts something else)\n\n",
		len(m.Flipped), len(gc.Files(m.Flipped)))
	if !list {
		fmt.Println("  -list names the files.")
		fmt.Println()
		return
	}
	for _, s := range m.Opened {
		fmt.Printf("  OPENED  %s:%d  >=%d examples  %s\n", s.File, s.Line, s.Examples, s.Text)
	}
	for _, s := range m.Closed {
		fmt.Printf("  CLOSED  %s:%d  %s\n", s.File, s.Line, s.Text)
	}
	for _, s := range m.Flipped {
		fmt.Printf("  FLIPPED %s:%d  %s\n", s.File, s.Line, s.Text)
	}
	fmt.Println()
}

func tally(sites []gc.Site, withExamples bool) {
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
