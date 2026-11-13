// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gemRoot builds a gem tree: each name becomes <root>/<name>/lib, except names
// ending in "!" which get the gem directory and NO lib (a gem with nothing to
// add to the load path, which must be skipped rather than counted).
func gemRoot(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		if strings.HasSuffix(n, "!") {
			if err := os.MkdirAll(filepath.Join(root, strings.TrimSuffix(n, "!")), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Join(root, n, "lib"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// ⛔ THE REFUSAL IS THE POINT OF THIS COMMAND. An interpreter that cannot load
// its bundled gems does not fail loudly -- it scores ZERO on every spec file,
// which a sweep reports as a clean result for the control and reads as the
// implementation under test beating the reference. So a gem root that yields
// no lib directory must stop the run, not start it.
func TestBuildArgsRefusesAGemRootThatYieldsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		root string
	}{
		{"an empty directory", gemRoot(t)},
		{"only gems with no lib", gemRoot(t, "a!", "b!")},
		{"a path that does not exist", filepath.Join(t.TempDir(), "absent")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildArgs("/bin/ruby", tc.root, []string{"x.rb"}); err == nil {
				t.Fatal("it must refuse, not run with an empty -I list")
			}
		})
	}
	// The empty-but-readable case names its own reason, so the message says what
	// to fix rather than only that something failed.
	_, err := buildArgs("/bin/ruby", gemRoot(t), []string{"x.rb"})
	if !errors.Is(err, errNoGems) {
		t.Errorf("err = %v, want it to wrap errNoGems", err)
	}
}

// Both environment variables are required: a half-configured oracle would run
// the wrong interpreter, or the right one with no gems, and both score zero.
func TestBuildArgsRequiresBothSettings(t *testing.T) {
	root := gemRoot(t, "net-ftp-0.3.9")
	if _, err := buildArgs("", root, nil); err == nil {
		t.Error("an unset MRIREF_RUBY must be refused")
	}
	if _, err := buildArgs("/bin/ruby", "", nil); err == nil {
		t.Error("an unset MRIREF_GEMS must be refused")
	}
}

// The -I list carries every gem that has a lib, in a stable order, and the
// caller's own arguments come last so the script is still the script.
func TestBuildArgsPutsEveryGemLibBeforeTheScript(t *testing.T) {
	root := gemRoot(t, "timeout-0.6.0", "net-ftp-0.3.9", "nolib!", "net-protocol-0.2.2")
	got, err := buildArgs("/bin/ruby", root, []string{"library/net-ftp/acct_spec.rb", "--extra"})
	if err != nil {
		t.Fatal(err)
	}
	// 3 gems with a lib -> 6 elements, then the 2 caller arguments.
	if len(got) != 8 {
		t.Fatalf("got %d argument(s) %v, want 8 (3 × -I plus 2 of the caller's)", len(got), got)
	}
	for i := 0; i < 6; i += 2 {
		if got[i] != "-I" {
			t.Errorf("argument %d = %q, want -I", i, got[i])
		}
	}
	// ⛔ Sorted, because a load path that changes order between runs can change
	// which gem wins a name collision, and an oracle has to be reproducible.
	//
	// Compared by the GEM DIRECTORY'S NAME, not by a "<gem>/lib" suffix: the
	// first version of this test used the suffix and passed on macOS and Linux
	// while failing on Windows, where the separator is `\`. The order was right
	// all along; the assertion was not portable. Naming the directory is also
	// what the check actually means.
	want := []string{"net-ftp-0.3.9", "net-protocol-0.2.2", "timeout-0.6.0"}
	for i, w := range want {
		if gem := filepath.Base(filepath.Dir(got[2*i+1])); gem != w {
			t.Errorf("-I entry %d is %q, want the gem %q (list: %v)", i, gem, w, got[:6])
		}
	}
	if got[6] != "library/net-ftp/acct_spec.rb" || got[7] != "--extra" {
		t.Errorf("the caller's arguments must come last, got %v", got[6:])
	}
}
