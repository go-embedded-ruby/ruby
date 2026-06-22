package aot

import (
	"sort"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
)

// frontendFeatures are the methods whose normal behaviour needs the embedded
// front-end (a runtime lexer/parser/compiler): they compile Ruby source at run
// time. A closed-world binary drops the front-end, so any of these — if actually
// reached — raises NotImplementedError instead.
// Note: bare `binding` is not here — it compiles to the OpBinding intrinsic and
// captures fine in a closed binary; only Binding#eval needs the front-end, and
// that surfaces as the `eval` selector below.
var frontendFeatures = map[string]bool{
	"eval":             true,
	"require":          true,
	"require_relative": true,
	"instance_eval":    true,
	"class_eval":       true,
	"module_eval":      true,
}

// FrontendUses scans a compiled program (the top-level ISeq and every nested
// method/class/block body) for calls to front-end-dependent methods, returning
// their names sorted and de-duplicated. `rbgo build --closed` reports these so
// the user knows which calls would raise in the closed binary.
//
// It is a name-level scan (the selectors a send could target), deliberately
// conservative: it cannot tell `obj.eval` from `Kernel#eval`, so it may
// over-report, but it never misses a literal use.
func FrontendUses(top *bytecode.ISeq) []string {
	seen := map[string]bool{}
	var walk func(s *bytecode.ISeq)
	walk = func(s *bytecode.ISeq) {
		if s == nil {
			return
		}
		for _, n := range s.Names {
			if frontendFeatures[n] {
				seen[n] = true
			}
		}
		for _, c := range s.Children {
			walk(c)
		}
	}
	walk(top)

	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
