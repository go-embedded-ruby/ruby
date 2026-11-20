// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package ruby_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The conformance shim must have ONE source for what platform this is (#837).
//
// It had two. `PLATFORMS = [:darwin, :bsd, :unix]` was a literal, and
// `PlatformGuard::PLATFORM = RUBY_PLATFORM` beside it was derived, under a
// comment asserting the literal was the single source of truth. On darwin the
// two agree, so the split is invisible from the machine the work is done on;
// on the ubuntu lane that enforces the gate, `platform_is :linux` was FALSE.
//
// ⛔ EVERY ASSERTION BELOW IS PLATFORM-INDEPENDENT, and it has to be: this
// test runs on ubuntu, macos and windows, and a test that spelled out an
// expected answer would be asserting which runner it is on. What it asserts
// instead is that the two sources AGREE, which is the actual invariant, plus
// three facts true of every machine there is.
//
// Each of them is red under the old shim on EVERY platform, which is what makes
// this a gate rather than a note.

// platformProbe asks the shim and the guard class the same questions, then
// prints both answers. `platform_is` with no block returns its verdict
// (spec_helper.rb's `_guard_run` yields only when a block is given), so one run
// answers all of them.
const platformProbe = `require_relative '../spec_helper'
def say(label, v); puts "GUARD #{label}=#{!!v}"; end

# The four names the old literal held, each against the class beside it.
say "is_linux",   platform_is(:linux)
say "os_linux",   PlatformGuard.os?(:linux)
say "is_darwin",  platform_is(:darwin)
say "os_darwin",  PlatformGuard.os?(:darwin)
say "is_bsd",     platform_is(:bsd)
say "os_bsd",     PlatformGuard.os?(:bsd)
say "is_unix",    platform_is(:unix)
say "os_unix",    PlatformGuard.os?(:unix)

# The bits/bytes defect: the corpus writes 64, the shim compared it to
# WORDSIZE / 8.
say "ptr64",      platform_is(pointer_size: 64)
say "os_ptr64",   PlatformGuard.pointer_size?(64)
say "ptr32",      platform_is(pointer_size: 32)
say "os_ptr32",   PlatformGuard.pointer_size?(32)

# The separate negation path: mspec is run_unless(match?) over ONE predicate.
say "not_linux_cl32", platform_is_not(:linux, c_long_size: 32)
say "match_linux_cl32", (PlatformGuard.os?(:linux) && PlatformGuard.c_long_size?(32))

# A full triple is a substring of itself; a list of symbols can never hold one.
say "triple",     platform_is(RUBY_PLATFORM)
# ...and a name no machine carries is simply false.
say "unknown",    platform_is(:definitely_not_a_platform)

puts "PLATFORM=#{RUBY_PLATFORM}"

describe "a platform probe" do
  it "runs at all" do
    1.should == 1
  end
end
`

func platformCorpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	shim, err := os.ReadFile(shimRelPath)
	if err != nil {
		t.Fatalf("read the shim under test: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "spec_helper.rb"), shim, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "probe_spec.rb"), []byte(platformProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// answers reads the probe's output back. A label the probe never printed is
// reported as missing rather than as false: "it did not answer" and "it
// answered no" need opposite responses, and this file's whole subject is a
// guard that silently answered no.
func answers(t *testing.T, out string) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "GUARD ")
		if !ok {
			continue
		}
		label, value, ok := strings.Cut(rest, "=")
		if !ok {
			continue
		}
		got[label] = value == "true"
	}
	return got
}

func mustHave(t *testing.T, got map[string]bool, label, out string) bool {
	t.Helper()
	v, ok := got[label]
	if !ok {
		t.Fatalf("the probe never answered %q, so this run proves nothing:\n%s", label, out)
	}
	return v
}

// TestTheShimHasOneSourceForWhatPlatformThisIs is #837's gate.
func TestTheShimHasOneSourceForWhatPlatformThisIs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the rbgo build in -short")
	}
	bin := buildRbgo(t)
	corpus := platformCorpus(t)

	out := runProbe(t, bin, corpus, "")

	// The CONTROL, first: the probe really ran and really printed. Every
	// assertion below reads a map, and an empty map agrees with anything.
	if !strings.Contains(out, "PLATFORM=") {
		t.Fatalf("the probe never reached its last line, so this run proves nothing:\n%s", out)
	}
	got := answers(t, out)
	if len(got) == 0 {
		t.Fatalf("the probe printed no GUARD lines at all:\n%s", out)
	}

	// ⛔ THE INVARIANT. For each name, the guard helper and the guard class must
	// answer alike. This does not say what the answer is, so it holds on every
	// runner — and it is false on every runner under the old shim for :bsd and
	// :unix, which no RUBY_PLATFORM this project builds for contains.
	for _, name := range []string{"linux", "darwin", "bsd", "unix"} {
		helper := mustHave(t, got, "is_"+name, out)
		class := mustHave(t, got, "os_"+name, out)
		if helper != class {
			t.Errorf("platform_is(:%s) = %v but PlatformGuard.os?(:%s) = %v — two sources again (#837)\n%s",
				name, helper, name, class, out)
		}
	}

	// The same invariant for the sizes, which is where the units diverged.
	for _, size := range []string{"ptr64", "ptr32"} {
		helper := mustHave(t, got, size, out)
		class := mustHave(t, got, "os_"+size, out)
		if helper != class {
			t.Errorf("platform_is(pointer_size:) and PlatformGuard.pointer_size? disagree on %s: %v vs %v\n%s",
				size, helper, class, out)
		}
	}

	// ⭐ And an absolute, because the two could in principle agree on being
	// wrong: every machine has SOME pointer size. Under the old shim both of
	// these were false, on every platform, so core/array/pack/j_spec.rb and
	// core/string/unpack/j_spec.rb contributed nothing at all — measured at 0
	// in BASELINE, and 188 and 216 once the units matched.
	if !mustHave(t, got, "ptr64", out) && !mustHave(t, got, "ptr32", out) {
		t.Errorf("neither pointer_size: 64 nor pointer_size: 32 matches, so every such guard is shut:\n%s", out)
	}

	// ⛔ ONE PREDICATE, NEGATED. mspec's platform_is_not is run_unless over the
	// same match? platform_is runs on (guards/platform.rb:116-122). The old shim
	// rebuilt the negation by OR-ing the parts, which is not its dual and
	// differs exactly when a site carries a name AND an option — which this one
	// does, on every platform.
	notLinux := mustHave(t, got, "not_linux_cl32", out)
	matchLinux := mustHave(t, got, "match_linux_cl32", out)
	if notLinux != !matchLinux {
		t.Errorf("platform_is_not(:linux, c_long_size: 32) = %v, want !%v — the negation is not the dual of the match\n%s",
			notLinux, matchLinux, out)
	}

	// A machine always matches its own triple, which mspec gets from substring
	// matching and a list of symbols can never hold. True everywhere.
	if !mustHave(t, got, "triple", out) {
		t.Errorf("platform_is(RUBY_PLATFORM) is false, so the shim cannot match a full triple (core/process/clock_gettime_spec.rb:140 writes one)\n%s", out)
	}

	// The accepting side of the same boundary: a guard that matched everything
	// would satisfy every assertion above.
	if mustHave(t, got, "unknown", out) {
		t.Errorf("platform_is(:definitely_not_a_platform) is TRUE, so the guard matches anything\n%s", out)
	}
}

// TestTheShimDoesNotHoldAListOfPlatformNames keeps the fix from being undone by
// reintroducing the literal next to the derived source. It reads the file
// rather than running it, because a list that agrees with RUBY_PLATFORM on the
// machine the test runs on would satisfy every assertion above while putting
// the second source back for every other machine.
func TestTheShimDoesNotHoldAListOfPlatformNames(t *testing.T) {
	b, err := os.ReadFile(shimRelPath)
	if err != nil {
		t.Fatalf("read %s: %v", shimRelPath, err)
	}
	src := string(b)

	// Read-the-right-file control: a wrong path would make the assertion below
	// pass by vacuum.
	if !strings.Contains(src, "class PlatformGuard") {
		t.Fatalf("%s does not define PlatformGuard; this test is reading the wrong file (%d bytes)", shimRelPath, len(src))
	}
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "PLATFORMS") {
			t.Fatalf("%s names PLATFORMS in code: %q — the platform comes from RUBY_PLATFORM alone (#837)", shimRelPath, strings.TrimSpace(line))
		}
	}
}
