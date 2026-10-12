// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package ruby_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestNoTestSkipsOnAFailureToBuild (#687).
//
// Two tests used to turn a build failure into a PASS:
//
//	build := exec.Command("go", "build", "-o", bin, "./cmd/rbgo")
//	if out, err := build.CombinedOutput(); err != nil {
//	    t.Skipf("cannot build cmd/rbgo (%v): %s", err, out)
//	}
//
// If `cmd/rbgo` stopped compiling, both reported **skip**, the lane stayed
// green, and the binary-level HTTP and socket coverage silently stopped being
// exercised. `t.Skipf` there says "this machine cannot build it"; what it
// reports is "the product is broken". Those are opposite meanings and only one
// of them may be green.
//
// Both sites are `t.Fatalf` now. This exists so they cannot come back, and so
// the next test that reaches for the same shape fails instead of going quiet --
// a fix to two lines closes two lines; a guard closes the class.
//
// WHAT IT JUDGES, and what it deliberately does not. A skip is legitimate when
// the thing it skips over is genuinely absent from the MACHINE: no symlink
// support, no /dev/zero, not root. It is never legitimate when the thing absent
// is THIS REPOSITORY'S OWN BUILD, because every lane builds it and a developer
// who cannot wants to be told. So the test reads the skip's REASON and refuses a
// vocabulary that can only describe the second case. It does not try to infer
// intent from the surrounding code, which would be a judgement call dressed up
// as a check.
func TestNoTestSkipsOnAFailureToBuild(t *testing.T) {
	// Each alternative is a phrase that can only mean "the code did not
	// compile". "build" alone is not among them: "no build tag", "build
	// directory" and "builder" are ordinary and innocent, and a guard that fires
	// on those gets edited away rather than obeyed.
	//
	// The match is on WORD BOUNDARIES, and that is not fussiness. The first
	// version used strings.Contains and reported
	// cmd/rbgo/build_test.go's "set RBGO_BUILD_IT=1 to run the rbgo build
	// integration test" as an offence -- because "go build" is a substring of
	// "rb|go build". One false positive on a guard like this is enough to get it
	// deleted, so the two cases below are pinned as a self-test.
	forbidden := regexp.MustCompile(`(?i)\b(` + strings.Join([]string{
		`cannot build`,
		`could not build`,
		`failed to build`,
		`build failed`,
		`go build`,
		`cannot compile`,
		`could not compile`,
		`does not compile`,
		`compile failed`,
		`failed to compile`,
	}, `|`) + `)\b`)

	// The guard's own control, run before it judges anything: a known positive
	// it must catch and a known negative it must not. Without these the
	// vocabulary is a guess, and a guess that matches nothing looks exactly like
	// a clean tree.
	for _, c := range []struct {
		reason string
		want   bool
	}{
		{"cannot build cmd/rbgo (exit status 1): ...", true},
		{"could not compile the fixture", true},
		{"go build failed", true},
		{"set RBGO_BUILD_IT=1 to run the rbgo build integration test (needs the Go toolchain)", false},
		{"no build tag for this platform", false},
		{"builder not available", false},
		{"requires root", false},
	} {
		if got := forbidden.MatchString(c.reason); got != c.want {
			t.Fatalf("the vocabulary itself is wrong: MatchString(%q) = %v, want %v", c.reason, got, c.want)
		}
	}

	var offences []string
	skipsSeen := 0

	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// testdata holds fixtures, not tests; .git and the corpus caches are
			// not ours to judge.
			switch d.Name() {
			case ".git", "testdata", "vendor", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			// A test file that does not parse is a failure of this guard's input,
			// not a pass. Say so rather than skipping the file.
			offences = append(offences, path+": does not parse: "+perr.Error())
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Skip", "Skipf", "SkipNow":
			default:
				return true
			}
			skipsSeen++
			// The reason is the first argument when there is one, and it is a
			// literal in every site in this repository. A computed reason cannot
			// be judged here and is left alone rather than guessed at.
			if len(call.Args) == 0 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			reason, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			if forbidden.MatchString(reason) {
				pos := fset.Position(call.Pos())
				offences = append(offences, pos.String()+": skips on a build failure: "+strconv.Quote(reason))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	// "N of M, never N alone": a walk that parsed nothing finds no offence and
	// is indistinguishable from a clean tree. This repository has dozens of
	// legitimate skips, so zero means the walk is broken.
	if skipsSeen == 0 {
		t.Fatalf("the walk found no t.Skip call anywhere, which cannot be right; "+
			"it has not read the tree (offences collected so far: %v)", offences)
	}
	for _, o := range offences {
		t.Errorf("%s\n  a build failure is the product being broken, not this machine "+
			"lacking something: use t.Fatalf. See #687.", o)
	}
	t.Logf("%d t.Skip call(s) examined, %d offence(s)", skipsSeen, len(offences))
}
