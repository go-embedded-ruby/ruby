// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package ruby_test

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestReadmeListsTheNumericSurfaceThatExists.
//
// The README's Special and Stats entries list function and distribution names
// one by one, which is the kind of claim that goes stale silently: a name added
// to the binding and not to the list leaves a reader thinking it is absent, and
// a name REMOVED from the binding and left in the list is worse, because it
// reads as a promise.
//
// A guard for this can be built from files already here, with no VM: the README
// names them in backticks, and internal/vm/special.go and internal/vm/stats.go
// register them by name in a string literal. So the two sets must be equal, in
// both directions. That is what this checks.
//
// It is deliberately narrow. It does not validate the prose, the ufunc claim or
// the scipy correspondence -- those are tested in the vm package, against
// closed forms. It checks the one thing a list of names can get wrong.
func TestReadmeListsTheNumericSurfaceThatExists(t *testing.T) {
	readme := read(t, "README.md")

	for _, tc := range []struct {
		module string // the README section to read
		file   string // the binding that registers the names
		// registered matches the registration calls in that file, with the
		// Ruby name in group 1.
		registered *regexp.Regexp
		// prose lists the backticked words of that entry which are NOT method
		// names, with the reason beside them. Naming them per module is cheaper
		// than a rule that would also excuse a real omission.
		prose []string
	}{
		{
			module:     "Special",
			file:       "internal/vm/special.go",
			registered: regexp.MustCompile(`(?m)^\t(?:u1|u2|u3|uInt1|uInt2|vector|def)\("([a-z_0-9]+)"`),
		},
		{
			module: "Stats",
			file:   "internal/vm/stats.go",
			// Stats registers names three ways: cont/disc for a
			// distribution, c.define for a method, and a map literal for the
			// methods Continuous and Discrete share. All three count, or the
			// README's method list would be unchecked.
			registered: regexp.MustCompile(`(?m)^\t(?:cont|disc)\("([a-z_0-9]+)"|\.define\("([a-z_0-9]+)"|^\t\t"([a-z_0-9]+)": +func\(d distCommon\)`),
			// scipy's PARAMETER names, which the entry explains in prose.
			prose: []string{"loc", "scale"},
		},
		{
			module:     "Integrate",
			file:       "internal/vm/integrate.go",
			registered: regexp.MustCompile(`(?m)^\t(?:def|sampledRule)\("([a-z_0-9]+)"`),
			// the block-pass idiom, not a method.
			prose: []string{"&f"},
		},
		{
			module:     "Optimize",
			file:       "internal/vm/optimize.go",
			registered: regexp.MustCompile(`(?m)^\t(?:def|bracketing)\("([a-z_0-9]+)"`),
			// method: VALUES (scipy's own method strings), and the block-pass idiom.
			prose: []string{"brent", "golden", "bounded", "&f"},
		},
	} {
		t.Run(tc.module, func(t *testing.T) {
			code := names(tc.registered.FindAllStringSubmatch(read(t, tc.file), -1))
			if len(code) == 0 {
				t.Fatalf("read no registered names out of %s: the regexp no longer matches "+
					"how the module registers, so this test proves nothing", tc.file)
			}
			doc := backticked(section(t, readme, tc.module))
			if len(doc) == 0 {
				t.Fatalf("found no backticked names in the README's %s entry", tc.module)
			}

			// Every name the binding defines must be listed.
			for _, n := range code {
				if !contains(doc, n) {
					t.Errorf("%s defines %q and the README does not list it", tc.file, n)
				}
			}
			// And every name the README lists under a per-name list must exist.
			// The entry also backticks types and keywords (NDArray, seed:,
			// ArgumentError, Float); a name is only checked when it looks like
			// one of these functions, which is to say lower-case.
			prose := tc.prose
			for _, n := range doc {
				if n != strings.ToLower(n) || strings.ContainsAny(n, " :(.") || contains(prose, n) {
					continue
				}
				if !contains(code, n) {
					t.Errorf("the README's %s entry lists %q, which %s does not define",
						tc.module, n, tc.file)
				}
			}
		})
	}
}

// section returns the README's "- **<name>:**" bullet, up to the next bullet at
// the same level.
func section(t *testing.T, readme, name string) string {
	t.Helper()
	start := strings.Index(readme, "- **"+name+":**")
	if start < 0 {
		t.Fatalf("the README has no %q entry", name)
	}
	rest := readme[start+1:]
	if end := strings.Index(rest, "\n- **"); end >= 0 {
		return rest[:end]
	}
	return rest
}

var backtick = regexp.MustCompile("`([^`]+)`")

// backticked returns the distinct backticked tokens of a passage, splitting the
// README's "`a`/`b`" lists and dropping anything that is not a bare word.
func backticked(s string) []string {
	var out []string
	for _, m := range backtick.FindAllStringSubmatch(s, -1) {
		for _, part := range strings.Split(m[1], "/") {
			part = strings.TrimSpace(part)
			// A token written as a call names the method: `rvs(size…, seed:)`
			// is rvs. Keep the name and drop the argument list.
			if i := strings.IndexByte(part, '('); i > 0 {
				part = part[:i]
			}
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return dedup(out)
}

// names collects the first non-empty capture of each match, so one regexp may
// carry several alternatives.
func names(ms [][]string) []string {
	var out []string
	for _, m := range ms {
		for _, g := range m[1:] {
			if g != "" {
				out = append(out, g)
				break
			}
		}
	}
	return dedup(out)
}

func dedup(in []string) []string {
	sort.Strings(in)
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

func contains(hay []string, s string) bool {
	for _, h := range hay {
		if h == s {
			return true
		}
	}
	return false
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
