// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package guardcensus

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The platform half of the census (#837).
//
// `ruby_version_is` selects the population by VERSION; `platform_is` selects it
// by MACHINE. The two are read the same way and for the same reason: a guard
// decides whether examples run at all, so a shim that answers it differently
// from mspec is measuring a different corpus than the one it names.
//
// ⛔ WHAT THIS IS FOR. spec_helper.rb answered `platform_is` from a hardcoded
// list, `PLATFORMS = [:darwin, :bsd, :unix]`, while the class beside it derived
// `PLATFORM = RUBY_PLATFORM` — under a comment saying the list was the single
// source of truth. On darwin the two agree and the split is invisible; on the
// ubuntu lane that enforces the gate they do not. This reads the corpus and
// says, for a named machine, exactly which guards the two answers disagree
// about and how many examples sit behind them.
//
// The reference is ruby/mspec lib/mspec/guards/platform.rb, read at the tip on
// 2026-11-20:
//
//	:19-24   PLATFORM = RUBY_PLATFORM   (RbConfig's host triple under JRuby)
//	:26-38   os?(*oses) — `:windows` is the regexp /(mswin|mingw)/, `:wsl` is
//	         its own predicate, and EVERY OTHER NAME is a plain substring of
//	         PLATFORM. There is no list of known systems anywhere in it.
//	:61-66   POINTER_SIZE = RbConfig::SIZEOF["void*"] * 8 — in BITS
//	:69-73   C_LONG_SIZE  = RbConfig::SIZEOF["long"]  * 8 — in BITS
//	:97-113  match? — the platform names AND every option must all hold
//	:116-122 platform_is is run_if(match?), platform_is_not run_unless(match?)
//
// ⛔ Note :116-122: the two entry points share ONE predicate, so
// `platform_is_not` is exactly `!match?`. A shim that re-derives the negation
// from the parts answers differently as soon as a site carries both a name and
// an option.

// PlatformSite is one mention of platform_is or platform_is_not.
//
// A site whose argument list does not close on its own line is left
// Unclassified rather than guessed at: the corpus has at least one such call
// (core/kernel/system_spec.rb opens `platform_is(:windows, :darwin, :freebsd,
// :netbsd,` and continues on the next line), and a census that silently read
// the first line's names would report a smaller guard than the source holds.
// "Could not read it" and "read it and it names nothing" need opposite answers.
type PlatformSite struct {
	File, Text string
	Line       int

	Negated      bool     // the call was platform_is_not
	OSes         []string // bare arguments, as written, without the leading colon
	OptOSes      []string // the `os:` option's arguments
	PointerSize  int      // `pointer_size:` in BITS, 0 when absent
	CLongSize    int      // `c_long_size:` in BITS, 0 when absent
	Wordsize     int      // `wordsize:` in BITS, 0 when absent (deprecated upstream)
	Block        bool     // it opens a block, so it SELECTS examples
	Examples     int      // `it` blocks inside the block: a LOWER BOUND
	Unclassified bool     // the call could not be read; it is counted, never judged
}

// Machine is the platform a census is asked about. The sizes are in BITS,
// because that is the unit mspec's POINTER_SIZE and C_LONG_SIZE carry
// (platform.rb:61-73) and mixing the two units is the defect this file was
// written to measure.
type Machine struct {
	RubyPlatform string
	PointerSize  int
	CLongSize    int
}

// Linux64 and Darwin64 are the two machines this project actually runs the
// sweep on: the gate's ubuntu lane, and the developer's mac.
//
// ⛔ The triples are what rbgo REPORTS, read from it rather than written from
// memory. internal/vm/kernel_introspect.go builds RUBY_PLATFORM from the Go
// build target, so it is `arm64-darwin` with no version suffix — MRI on the
// same machine says `arm64-darwin24`. Every name the corpus guards on is a
// substring of both, so the census is the same either way; the point is that
// the constant records a measurement and not a guess.
var (
	Linux64  = Machine{RubyPlatform: "x86_64-linux", PointerSize: 64, CLongSize: 64}
	Darwin64 = Machine{RubyPlatform: "arm64-darwin", PointerSize: 64, CLongSize: 64}
)

// OS reports whether a name matches this machine, as mspec's
// PlatformGuard.os? does (platform.rb:26-38).
func (m Machine) OS(name string) bool {
	switch name {
	case "windows":
		return strings.Contains(m.RubyPlatform, "mswin") || strings.Contains(m.RubyPlatform, "mingw")
	case "wsl":
		// mspec shells out to `uname -r` for this one. The census answers for a
		// named machine rather than for the host, so it cannot run that command,
		// and a guess would be the fabricated positive this repository has
		// already paid for twice. Treated as false and reported as such by
		// cmd/platguard, which is what the sweep's own shim also answers.
		return false
	}
	return strings.Contains(m.RubyPlatform, name)
}

// Match is mspec's PlatformGuard#match? (platform.rb:97-113): the names, then
// every option, all of which must hold.
func (m Machine) Match(s PlatformSite) bool {
	match := true
	if len(s.OSes) > 0 {
		match = false
		for _, name := range s.OSes {
			if m.OS(name) {
				match = true
				break
			}
		}
	}
	if len(s.OptOSes) > 0 {
		any := false
		for _, name := range s.OptOSes {
			if m.OS(name) {
				any = true
				break
			}
		}
		match = match && any
	}
	if s.PointerSize != 0 {
		match = match && s.PointerSize == m.PointerSize
	}
	if s.CLongSize != 0 {
		match = match && s.CLongSize == m.CLongSize
	}
	if s.Wordsize != 0 {
		match = match && s.Wordsize == m.CLongSize
	}
	return match
}

// Runs reports whether the examples behind this site execute on m, which is
// match? for platform_is and !match? for platform_is_not (platform.rb:116-122).
func (m Machine) Runs(s PlatformSite) bool {
	if s.Negated {
		return !m.Match(s)
	}
	return m.Match(s)
}

// Literal is what spec_helper.rb answered BEFORE #837, kept so the cost of the
// defect can be measured rather than argued. It is a faithful transcription of
// the two guard bodies as they stood at spec_helper.rb:867-885:
//
//	platform_is:     a name matches when it is in the hardcoded PLATFORMS list;
//	                 :pointer_size is compared against WORDSIZE/8 — BYTES against
//	                 a value the corpus writes in BITS, so both 32 and 64 are
//	                 false on every machine;
//	                 the `os:` option is not read at all.
//	platform_is_not: the negation is re-derived from the parts instead of being
//	                 !match?, so a site carrying a name AND a size answers
//	                 differently from mspec even when every part agrees.
//
// ⛔ It takes the list and the word size as arguments rather than reading
// constants, so a test can drive it with the values the shim actually had.
func Literal(s PlatformSite, platforms []string, wordsize int) bool {
	inList := func(name string) bool {
		for _, p := range platforms {
			if p == name {
				return true
			}
		}
		return false
	}
	anyInList := func(names []string) bool {
		for _, name := range names {
			if inList(name) {
				return true
			}
		}
		return false
	}
	if s.Negated {
		match := anyInList(s.OSes)
		if s.Wordsize != 0 && s.Wordsize != wordsize {
			match = true
		}
		if s.CLongSize != 0 && s.CLongSize != wordsize {
			match = true
		}
		return !match
	}
	match := true
	if len(s.OSes) > 0 {
		match = anyInList(s.OSes)
	}
	if s.Wordsize != 0 {
		match = match && s.Wordsize == wordsize
	}
	if s.PointerSize != 0 {
		match = match && s.PointerSize == wordsize/8
	}
	if s.CLongSize != 0 {
		match = match && s.CLongSize == wordsize
	}
	return match
}

var (
	platCallRe = regexp.MustCompile(`\bplatform_is(_not)?\b`)
	platHeadRe = regexp.MustCompile(`\bplatform_is(_not)?\s*(\(|\s)`)
	symRe      = regexp.MustCompile(`^:([A-Za-z_][A-Za-z0-9_]*)$`)
	strArgRe   = regexp.MustCompile(`^["']([^"']*)["']$`)
	optRe      = regexp.MustCompile(`^([a-z_]+):\s*(.+)$`)
)

// ClassifyPlatform reads one line of Ruby. It returns false when the line does
// not mention a platform guard at all.
func ClassifyPlatform(line string) (PlatformSite, bool) {
	if !platCallRe.MatchString(line) {
		return PlatformSite{}, false
	}
	s := PlatformSite{Text: strings.TrimSpace(line)}
	loc := platHeadRe.FindStringSubmatchIndex(line)
	if loc == nil {
		s.Unclassified = true
		return s, true
	}
	s.Negated = loc[2] != -1
	rest := line[loc[1]-1:]
	args, body, ok := splitArgs(rest)
	if !ok {
		s.Unclassified = true
		return s, true
	}
	s.Block = opensBlock(body)
	for _, a := range args {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if m := symRe.FindStringSubmatch(a); m != nil {
			s.OSes = append(s.OSes, m[1])
			continue
		}
		if m := strArgRe.FindStringSubmatch(a); m != nil {
			s.OSes = append(s.OSes, m[1])
			continue
		}
		m := optRe.FindStringSubmatch(a)
		if m == nil {
			s.Unclassified = true
			return s, true
		}
		val := strings.TrimSpace(m[2])
		switch m[1] {
		case "os":
			if vm := symRe.FindStringSubmatch(val); vm != nil {
				s.OptOSes = append(s.OptOSes, vm[1])
			} else {
				s.Unclassified = true
				return s, true
			}
		case "pointer_size", "c_long_size", "wordsize":
			n, err := strconv.Atoi(val)
			if err != nil {
				s.Unclassified = true
				return s, true
			}
			switch m[1] {
			case "pointer_size":
				s.PointerSize = n
			case "c_long_size":
				s.CLongSize = n
			default:
				s.Wordsize = n
			}
		default:
			s.Unclassified = true
			return s, true
		}
	}
	return s, true
}

// splitArgs takes the text from the call's name onwards and returns its
// arguments and whatever follows them. It reports false when the call does not
// finish on this line, which is a REFUSAL and not an empty argument list.
func splitArgs(rest string) (args []string, body string, ok bool) {
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "(") {
		depth, end := 0, -1
		for i, r := range rest {
			switch r {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			return nil, "", false
		}
		return splitTop(rest[1:end]), rest[end+1:], true
	}
	// No parentheses: the arguments run to `do`, to `{`, to a `?`, to `and`/`or`,
	// or to the end of the line.
	cut := len(rest)
	for _, kw := range []string{" do", " {", " ?", "?", " and ", " or ", " #"} {
		if i := strings.Index(rest, kw); i >= 0 && i < cut {
			cut = i
		}
	}
	return splitTop(rest[:cut]), rest[cut:], true
}

// splitTop splits an argument list on the commas that are not inside brackets.
func splitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

var platBlockRe = regexp.MustCompile(`^\s*(do\b|\{)`)

func opensBlock(body string) bool {
	return platBlockRe.MatchString(body)
}

// CensusPlatform reads one spec file.
func CensusPlatform(path string, src []byte) []PlatformSite {
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	var out []PlatformSite
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		s, ok := ClassifyPlatform(line)
		if !ok {
			continue
		}
		s.File, s.Line = path, i+1
		if s.Block {
			s.Examples = ExamplesIn(lines, i)
		}
		out = append(out, s)
	}
	return out
}

// WalkPlatform censuses every *_spec.rb under root/suite for each named suite,
// with File relative to root. The suites are NAMED, and the file count comes
// back, for the same reasons Walk gives.
//
// ⛔ THE RELATIVE PATH IS BUILT, NOT ASKED FOR, and that is a deliberate
// divergence from Walk above. Walk ends its callback with
//
//	rel, rerr := filepath.Rel(root, p)
//	if rerr != nil { return rerr }
//
// and that refusal CANNOT FIRE: WalkDir is rooted at filepath.Join(root, suite),
// so every p it yields already starts with root, which is the one condition Rel
// needs. A mutation sweep deleted the arm and nothing failed, because nothing
// could. Keeping a refusal no input can reach means keeping a line no test can
// ever hold — and this file argues, two functions down, that such a guard is
// one nobody can keep honest. The suite and the walked remainder give the same
// answer with no error to invent.
func WalkPlatform(root string, suites []string) ([]PlatformSite, int, error) {
	var sites []PlatformSite
	files := 0
	for _, suite := range suites {
		dir := filepath.Join(root, suite)
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
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
			rel := filepath.ToSlash(filepath.Join(suite, strings.TrimPrefix(p, dir)))
			sites = append(sites, CensusPlatform(rel, b)...)
			return nil
		})
		if err != nil {
			return nil, files, err
		}
	}
	return sites, files, nil
}

// Disagreement is one site the shim's old answer and mspec's answer differ
// about, on a named machine.
type Disagreement struct {
	Site      PlatformSite
	Machine   Machine
	WasRun    bool // the literal ran it
	ShouldRun bool // mspec runs it
}

// Lost reports whether examples that should run are being skipped. The other
// direction — examples running on a machine they were not written for — is
// Gained, and the two must never be summed: one is a measurement that is too
// small and the other is a measurement of the wrong thing.
func (d Disagreement) Lost() bool { return d.ShouldRun && !d.WasRun }

// Gained reports the opposite direction.
func (d Disagreement) Gained() bool { return d.WasRun && !d.ShouldRun }

// Disagreements compares the two readings over a census.
//
// ⛔ Unclassified sites are SKIPPED and counted separately by the caller. A
// site the reader could not parse is not a site the two readings agree about.
func Disagreements(sites []PlatformSite, m Machine, platforms []string, wordsize int) []Disagreement {
	var out []Disagreement
	for _, s := range sites {
		if s.Unclassified {
			continue
		}
		// Literal already answers for both entry points: it carries the shim's
		// own, separate negation path rather than negating a shared predicate,
		// which is one of the things being measured here.
		was := Literal(s, platforms, wordsize)
		should := m.Runs(s)
		if was == should {
			continue
		}
		out = append(out, Disagreement{Site: s, Machine: m, WasRun: was, ShouldRun: should})
	}
	return out
}
