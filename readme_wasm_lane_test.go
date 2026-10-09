// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package ruby_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestReadmeDoesNotContradictTheWasmLane.
//
// For weeks the README said, in two places, that `GOOS=js GOARCH=wasm` does not
// build and has no CI lane. #684 fixed the build AND added the lane, which has
// been running green on every pull request since — so the repository was
// shipping a gate and a document that disagreed, and the document was the one a
// reader believes.
//
// A dated limitation going stale is not an error; the section says as much, and
// says when it was last checked. What IS an error is a limitation that a gate in
// THIS repository already disproves on every push: the answer was in the tree
// the whole time and nothing looked at it.
//
// This test is deliberately narrow. It does not try to validate the whole
// "What does not work yet" section against reality, which would need to run the
// world. It checks the one class of claim that can be settled from files
// already here: the README says a target does not build, while ci.yml builds it.
func TestReadmeDoesNotContradictTheWasmLane(t *testing.T) {
	readme := readFile(t, "README.md")
	ci := readFile(t, ".github/workflows/ci.yml")

	// Is the browser target gated? Match the build command rather than the lane
	// name, because a name can be renamed and the question is what is RUN.
	gated := regexp.MustCompile(`GOOS=js\s+GOARCH=wasm\s+go\s+build`).MatchString(ci)
	if !gated {
		// If the lane is ever removed, the README's old wording becomes true
		// again and this test must stop demanding its absence -- but the removal
		// should be deliberate, so say so rather than passing in silence.
		t.Skip("no GOOS=js GOARCH=wasm build step in ci.yml; the README is free to say the browser target is ungated")
	}

	// The claims that are now false. Each is matched on a distinctive phrase
	// rather than a loose keyword: "browser" and "wasm" appear all over this
	// README for things that are perfectly true.
	for _, claim := range []struct{ phrase, why string }{
		{"has no CI lane and does not currently",
			"ci.yml builds GOOS=js GOARCH=wasm on every pull request"},
		{"The browser WebAssembly target does not currently build",
			"ci.yml builds GOOS=js GOARCH=wasm on every pull request"},
		{"no CI lane catches it, because only the",
			"the js/wasm lane catches exactly this"},
	} {
		if strings.Contains(readme, claim.phrase) {
			t.Errorf("README.md claims %q, but %s.\n"+
				"Either the claim is stale and should go, or the lane should, "+
				"but the repository must not ship both.", claim.phrase, claim.why)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
