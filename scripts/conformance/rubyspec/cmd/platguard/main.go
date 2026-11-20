// platguard censuses ruby/spec's `platform_is` / `platform_is_not` guards and
// says which ones the conformance shim answered differently from mspec, on a
// named machine (#837).
//
// It is the static half of that measurement — the half that needs no sweep, and
// so can be run on a loaded machine, where a sweep cannot. The same reason
// cmd/guards exists for `ruby_version_is`.
//
// ⛔ WHY A NAMED MACHINE AND NOT THIS ONE. The gate runs on ubuntu and the
// developer runs on darwin, and the defect is INVISIBLE on darwin: the shim's
// hardcoded list happens to contain :darwin, so there the list and
// RUBY_PLATFORM agree. A tool that could only answer about its own host would
// have reported a clean corpus from the machine where the work is done.
//
// ⛔ A FILE THIS CENSUS DOES NOT NAME CAN STILL MOVE, and the reason is not a
// bug here: ruby/spec is built on `it_behaves_like`, so a guard lives in a
// shared-examples file and its examples are counted against whichever spec
// files include it. Three files with NO guard of their own moved on the ubuntu
// sweep for #837 — core/string/{byteslice,element_reference}_spec.rb through
// `platform_is pointer_size: 64` at core/string/shared/slice.rb:122, and
// core/kernel/require_spec.rb through `platform_is :darwin` at
// core/kernel/shared/require.rb:230. They were nearly written off as flakes on
// the evidence that their own source contains no guard, which is evidence
// about the wrong file.
//
// So this census bounds the guards; it does not bound the FILES. Read the
// shared directory a spec includes before concluding a move is noise.
//
//	platguard -root <ruby/spec checkout> [-platform x86_64-linux] [-list]
//
// Exit status: 0 the census ran and the two readings agree; 1 they disagree;
// 2 the corpus could not be read — a refusal, not an empty census.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	gc "github.com/go-embedded-ruby/ruby/scripts/conformance/rubyspec/internal/guardcensus"
)

// The shim's constants as they stood when #837 was filed, so the report says
// what the defect cost rather than what some other list would have cost.
var (
	oldPlatforms = []string{"darwin", "bsd", "unix"}
	oldWordsize  = 64
)

func main() {
	root := flag.String("root", ".", "the ruby/spec checkout")
	platform := flag.String("platform", "", "the RUBY_PLATFORM to answer about; empty means both machines the sweep runs on")
	list := flag.Bool("list", false, "print every disagreeing site, not just the totals")
	flag.Parse()

	// ⛔ language/ and core/ ONLY, named rather than filtered: the gate sweeps
	// exactly two suites, so this walks exactly two.
	sites, files, err := gc.WalkPlatform(*root, []string{"language", "core"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "platguard: walking %s: %v\n", *root, err)
		os.Exit(2)
	}
	if files == 0 {
		fmt.Fprintf(os.Stderr, "platguard: no *_spec.rb under %s/{language,core} -- not a ruby/spec checkout\n", *root)
		os.Exit(2)
	}

	unread := 0
	for _, s := range sites {
		if s.Unclassified {
			unread++
		}
	}

	machines := []gc.Machine{gc.Linux64, gc.Darwin64}
	if *platform != "" {
		machines = []gc.Machine{{RubyPlatform: *platform, PointerSize: 64, CLongSize: 64}}
	}

	out := os.Stdout
	fmt.Fprintf(out, "corpus: %s — %d spec files, %d platform guard site(s)\n", *root, files, len(sites))
	if unread > 0 {
		// Named, never folded into the totals: a site the reader could not
		// parse is not a site the two readings agree about.
		fmt.Fprintf(out, "⚠ %d site(s) could NOT be read and are judged by neither side\n", unread)
		if *list {
			for _, s := range sites {
				if s.Unclassified {
					fmt.Fprintf(out, "    %s:%d  %s\n", s.File, s.Line, s.Text)
				}
			}
		}
	}

	disagrees := false
	for _, m := range machines {
		ds := gc.Disagreements(sites, m, oldPlatforms, oldWordsize)
		lostSites, gainedSites := 0, 0
		lostEx, gainedEx := 0, 0
		lostFiles, gainedFiles := map[string]bool{}, map[string]bool{}
		for _, d := range ds {
			if !d.Site.Block {
				// A site used as a VALUE (`cmd = platform_is(:windows) ? …`)
				// changes what an example asserts, not how many run. Counted
				// apart for the same reason cmd/guards separates inline sites.
				continue
			}
			if d.Lost() {
				lostSites++
				lostEx += d.Site.Examples
				lostFiles[d.Site.File] = true
			}
			if d.Gained() {
				gainedSites++
				gainedEx += d.Site.Examples
				gainedFiles[d.Site.File] = true
			}
		}
		values := 0
		for _, d := range ds {
			if !d.Site.Block {
				values++
			}
		}
		if len(ds) > 0 {
			disagrees = true
		}

		fmt.Fprintf(out, "\n%s (pointer %d bits, c_long %d bits)\n", m.RubyPlatform, m.PointerSize, m.CLongSize)
		fmt.Fprintf(out, "  SKIPPED but should run : %4d guard(s)  ≥%4d example(s)  %3d file(s)\n", lostSites, lostEx, len(lostFiles))
		fmt.Fprintf(out, "  RAN but should not     : %4d guard(s)  ≥%4d example(s)  %3d file(s)\n", gainedSites, gainedEx, len(gainedFiles))
		fmt.Fprintf(out, "  used as a VALUE        : %4d site(s) — these change what an example asserts, not how many run\n", values)
		// ⛔ The two directions are deliberately NOT summed. One says the
		// measurement is too small; the other says it is of the wrong machine.
		// A single number would let them cancel.
		fmt.Fprintf(out, "  ⛔ example counts are a LOWER BOUND: it_behaves_like expands at run time and a static reader cannot see it\n")

		if *list {
			sorted := append([]gc.Disagreement{}, ds...)
			sort.Slice(sorted, func(i, j int) bool {
				if sorted[i].Site.File != sorted[j].Site.File {
					return sorted[i].Site.File < sorted[j].Site.File
				}
				return sorted[i].Site.Line < sorted[j].Site.Line
			})
			for _, d := range sorted {
				verdict := "value"
				if d.Site.Block {
					if d.Lost() {
						verdict = "SKIPPED→run"
					} else {
						verdict = "ran→SKIP"
					}
				}
				fmt.Fprintf(out, "    %-11s %s:%d  (≥%d ex)  %s\n",
					verdict, d.Site.File, d.Site.Line, d.Site.Examples, strings.TrimSpace(d.Site.Text))
			}
		}
	}

	if disagrees {
		os.Exit(1)
	}
}
