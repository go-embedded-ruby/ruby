// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package guardcensus reads ruby/spec's `ruby_version_is` guards out of the
// corpus and says which ones a given version move OPENS, CLOSES or FLIPS.
//
// It exists as a package rather than as one command's internals because two
// commands need the same answer and must not drift: `cmd/guards` censuses the
// corpus on its own, and `cmd/specdelta` is now REFUSED a version-move verdict
// unless the same census says the run covered the surface the move touches.
//
// ⛔ WHY THE REFUSAL. On 2026-11-01 a version move was measured by comparing
// PASSING counts between two engines, and the figure was published. Three
// `core/binding/implicit_parameter*_spec.rb` files are `pass=0 fail=0 error=0`
// below 4.0 — every example behind a guard — and 37 failures above it. A
// pass-only delta showed nothing, and a note saying "ask what the target
// version ADDS before measuring what moves" had been written down months
// earlier and was not consulted. The note was not the missing part; a gate was.
//
// The version predicate here is spec_helper.rb's `_version_in_range`
// (scripts/conformance/rubyspec/spec_helper.rb:133), which is itself mspec's
// VersionGuard semantics: a bare string is a LOWER bound, a Range is bounded at
// both ends with `...` exclusive at the top, an empty endpoint is unbounded,
// and versions compare component by component with absent components read as
// zero (`_ver_cmp`, :119). It is reimplemented rather than called because the
// census must answer for versions the running interpreter is NOT, which is the
// whole question.
package guardcensus

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	callRe    = regexp.MustCompile(`ruby_version_is`)
	argRe     = regexp.MustCompile(`ruby_version_is\s*\(?\s*["']([^"']*)["']\s*(?:(\.\.\.?)\s*["']([^"']*)["']\s*)?\)?`)
	opensRe   = regexp.MustCompile(`(?:\bdo\b|\{)\s*(?:\|[^|]*\|)?\s*$`)
	bracesRe  = regexp.MustCompile(`ruby_version_is\s*\(?[^)]*\)?\s*\{`)
	commentRe = regexp.MustCompile(`\s+#(?:[^{].*)?$`)
	// OldAnchorRe is the earlier census's shape, kept ONLY so its number can
	// be reproduced as a control. Nothing else should use it.
	oldAnchorRe = regexp.MustCompile(`ruby_version_is\s+["'][^"']*["']\s*(\.\.\.?\s*["'][^"']*["']\s*)?do\s*$`)
	itRe        = regexp.MustCompile(`^\s*it[\s(]`)
	openKw      = regexp.MustCompile(`(^|\s)(def|class|module|case|begin|while|until|unless|if)\s`)
	modifIf     = regexp.MustCompile(`\S\s+(if|unless|while|until)\s`)
	endKw       = regexp.MustCompile(`^\s*end\b`)
	doKw        = regexp.MustCompile(`\bdo\b\s*(\|[^|]*\|)?\s*(#.*)?$`)
	braceEnd    = regexp.MustCompile(`^\s*\}`)
)

// Shape is what a mention of ruby_version_is does.
//
//	BLOCK     ruby_version_is "4.0" do … end
//	          The examples inside are SKIPPED while the version is below the
//	          threshold. Raising it ADDS them to the population.
//
//	INLINE    msg_class = ruby_version_is("4.0") ? "Module" : "module"
//	          The example RUNS either way; the guard only picks a value. Raising
//	          the version changes what these ASSERT without changing how many
//	          there are.
type Shape int

const (
	Block Shape = iota
	Inline
	Unknown
)

func (s Shape) String() string {
	switch s {
	case Block:
		return "block"
	case Inline:
		return "inline"
	}
	return "unclassified"
}

// Site is one mention of ruby_version_is.
type Site struct {
	File, Text string
	Line       int
	From, To   string
	Ranged     bool // the source wrote `..` or `...`, so To is meaningful
	Excl       bool // `...`: the upper bound is exclusive
	Shape      Shape
	InIt       bool // an inline inside an `it` moves exactly one example
	Examples   int  // `it` blocks inside a block guard: a LOWER BOUND
	OldAnchor  bool // a `do$` anchor would have found it
}

// Key is the version range a site guards, as it reads in the source.
func (s Site) Key() string {
	if s.Ranged {
		dots := ".."
		if s.Excl {
			dots = "..."
		}
		return s.From + dots + s.To
	}
	if s.From == "" {
		return `"" (open lower bound)`
	}
	return s.From
}

// OpenAt reports whether this guard's condition holds at the given version —
// spec_helper.rb's `_version_in_range` (:133), reimplemented so it can be asked
// about a version the running interpreter is not.
func (s Site) OpenAt(version string) bool {
	if !s.Ranged {
		// A bare string is a LOWER bound, and the empty string is always true.
		return s.From == "" || CompareVersions(version, s.From) >= 0
	}
	if s.From != "" && CompareVersions(version, s.From) < 0 {
		return false
	}
	if s.To == "" {
		return true
	}
	if s.Excl {
		return CompareVersions(version, s.To) < 0
	}
	return CompareVersions(version, s.To) <= 0
}

// CompareVersions is spec_helper.rb's `_ver_cmp` (:119): component by
// component, with a component the other side does not have read as zero, so
// "4.0" and "4.0.0" are equal and "3.4" < "3.4.1".
//
// ⛔ A component that is not a number reads as zero, exactly as Ruby's `to_i`
// does there. Diverging would make this census answer a question about a
// different corpus than the one the shim ran.
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		x, y := component(pa, i), component(pb, i)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func component(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	// Ruby's String#to_i takes the leading digits and yields 0 for none.
	end := 0
	for end < len(parts[i]) && parts[i][end] >= '0' && parts[i][end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(parts[i][:end])
	return n
}

// Classify decides what a single line of Ruby does with ruby_version_is.
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
func Classify(line string) (Site, bool) {
	if !callRe.MatchString(line) {
		return Site{}, false
	}
	s := Site{Text: strings.TrimSpace(line)}
	m := argRe.FindStringSubmatch(line)
	if m == nil {
		s.Shape = Unknown
		return s, true
	}
	s.From = m[1]
	if m[2] != "" {
		s.Ranged = true
		s.Excl = m[2] == "..."
		s.To = m[3]
	}
	s.OldAnchor = oldAnchorRe.MatchString(line)
	bare := commentRe.ReplaceAllString(line, "")
	if opensRe.MatchString(bare) || bracesRe.MatchString(bare) {
		s.Shape = Block
	} else {
		s.Shape = Inline
	}
	return s, true
}

// ExamplesIn counts the `it` blocks between a guard and its matching `end`.
//
// ⛔ A LOWER BOUND, and it says so: shared examples (`it_behaves_like`) expand
// at run time and are invisible to a static count. cmd/guards keeps a control
// that reproduces the guard number while the example number does not, which
// places the difference in the counting rather than in the census. Tuning the
// heuristic until it reached the expected answer would be fitting the
// instrument to the answer.
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

// Census reads one spec file. `path` is recorded on every Site as given.
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
		if s.Shape == Block {
			s.Examples = ExamplesIn(lines, i)
		}
		out = append(out, s)
	}
	return out
}

// Walk censuses every *_spec.rb under root/suite for each named suite, with
// Site.File relative to root.
//
// ⛔ The suites are NAMED by the caller rather than discovered. A draft walked
// the checkout minus a denylist of directory names and counted 2207 files where
// the gate counts 2206: a denylist inherits whatever it has not heard of.
//
// It returns the number of files read, so a caller can refuse an empty corpus
// rather than report an empty census — the two need opposite responses. A
// suite that is not present at all is skipped rather than refused, because a
// caller measuring one suite should not have to own the other; `files == 0`
// still distinguishes "nothing to read" from "read nothing".
func Walk(root string, suites []string) ([]Site, int, error) {
	var sites []Site
	files := 0
	for _, suite := range suites {
		if fi, err := os.Stat(filepath.Join(root, suite)); err != nil || !fi.IsDir() {
			continue
		}
		err := filepath.WalkDir(filepath.Join(root, suite), func(p string, d fs.DirEntry, err error) error {
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
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return rerr
			}
			sites = append(sites, Census(filepath.ToSlash(rel), b)...)
			return nil
		})
		if err != nil {
			return nil, files, err
		}
	}
	return sites, files, nil
}

// Move is what a version move does to the corpus's own population, before any
// engine is run. Every field is a reading of the SOURCE, not of a result.
type Move struct {
	From, To string
	Opened   []Site // block guards shut at From and open at To: examples APPEAR
	Closed   []Site // block guards open at From and shut at To: examples VANISH
	Flipped  []Site // inline sites whose value changes: the SAME example asserts something else
}

// ClassifyMove sorts a census into what the move opens, closes and flips.
func ClassifyMove(sites []Site, from, to string) Move {
	m := Move{From: from, To: to}
	for _, s := range sites {
		was, now := s.OpenAt(from), s.OpenAt(to)
		if was == now {
			continue
		}
		switch s.Shape {
		case Block:
			if now {
				m.Opened = append(m.Opened, s)
			} else {
				m.Closed = append(m.Closed, s)
			}
		case Inline:
			m.Flipped = append(m.Flipped, s)
		}
	}
	return m
}

// Files lists, in corpus order and without repeats, the files holding the given
// sites. This is the unit the gate works in: a measurement covers FILES, and a
// file the run never touched cannot have its guards attributed to anything.
func Files(sites []Site) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range sites {
		if !seen[s.File] {
			seen[s.File] = true
			out = append(out, s.File)
		}
	}
	return out
}

// Touched is every file the move opens, closes or flips.
func (m Move) Touched() []string {
	all := append(append(append([]Site{}, m.Opened...), m.Closed...), m.Flipped...)
	return Files(all)
}

// ExamplesOpened sums the lower-bound example counts of the opened guards.
func (m Move) ExamplesOpened() int {
	n := 0
	for _, s := range m.Opened {
		n += s.Examples
	}
	return n
}
