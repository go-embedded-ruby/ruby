// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The tests in this file exist because mutsweep v0.12.0 — the first version
// that sweeps an `if` with an init clause, and retries a deletion that does
// not compile as `false && (C)` — found 16 of specpin's 23 refusals held by
// nothing. v0.11.0 had reported 11 refusals and 0 survivors.
//
// Almost every one of them was DOUBLED: with the refusal gone, a later step
// still failed — git refusing to clone into a non-empty directory, a missing
// HEAD never equal to the SHA, a missing origin never equal to -url. So the
// existing tests, which asked only "did it fail?", passed either way. Each
// test here asserts what ITS refusal says, which no later step says.

func wantErr(t *testing.T, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error; want one saying %q", want)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("error %q does not say %q — another refusal answered, not this one", err, w)
		}
	}
}

// ⛔⛔ main.go:140, the line that replaced `rm -rf "$CACHE"`. With it gone,
// `git clone` refuses a non-empty destination by itself, so the directory
// survived and the old test passed: the guard was held by git. A git that
// cloned into a non-empty directory (or a future `--force`-like flag) would
// have had nothing in front of it.
func TestTheNonEmptyRefusalIsOursNotGits(t *testing.T) {
	needGit(t)
	url, older, _ := upstream(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "thesis.tex"), "three years of work\n")
	_, err := pin(dir, older, url)
	wantErr(t, err, "refusing to delete or reuse it")
}

// main.go:120. A parent that cannot be created is reported as the mkdir
// failure, not as the clone failure that would follow it.
//
// The first version of this test put a regular FILE in the path, and the
// sweep reported :120 still surviving: os.Stat answers ENOTDIR there, not
// ErrNotExist, so the switch's `case err != nil` returned before MkdirAll was
// ever reached. That case is kept as its own row below; :120 needs a parent
// that does not exist and cannot be made.
func TestAParentThatCannotBeCreatedIsNamed(t *testing.T) {
	needGit(t)
	url, older, _ := upstream(t)

	t.Run("a file in the path is the stat error", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "a-file")
		writeFile(t, file, "x")
		_, err := pin(filepath.Join(file, "sub", "rubyspec-corpus"), older, url)
		if err == nil || strings.Contains(err.Error(), "clone") {
			t.Fatalf("want the stat error, got %v", err)
		}
	})
	t.Run("an absent parent under a read-only directory is the mkdir error", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("mode 0500 does not stop root, and Windows has no such mode")
		}
		ro := t.TempDir()
		if err := os.Chmod(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(ro, 0o755) })
		_, err := pin(filepath.Join(ro, "sub", "rubyspec-corpus"), older, url)
		if !errors.Is(err, fs.ErrPermission) || strings.Contains(err.Error(), "clone") {
			t.Fatalf("want the mkdir permission error, got %v", err)
		}
	})
}

// main.go:123, :146 and git()'s own :273/:274. A clone that fails says so,
// and carries git's own words. With :123/:146 gone the next refusal is the
// origin check ("origin is \"\""); with :274 gone the error is a bare
// "exit status 128", and the reason — git's stderr — is lost.
func TestAFailedCloneSaysCloneAndCarriesGitsReason(t *testing.T) {
	needGit(t)
	_, older, _ := upstream(t)
	nowhere := filepath.Join(t.TempDir(), "no-such-remote")

	t.Run("into an absent directory", func(t *testing.T) {
		_, err := pin(filepath.Join(t.TempDir(), "c"), older, nowhere)
		wantErr(t, err, "clone ", "failed (offline?)", "fatal:")
	})
	t.Run("into an empty directory", func(t *testing.T) {
		_, err := pin(t.TempDir(), older, nowhere)
		wantErr(t, err, "clone ", "failed (offline?)", "fatal:")
	})
}

// main.go:137 and isEmpty's own ReadDir check. A directory that cannot be
// listed is a permission error, not "not empty" (what :140 would say with
// :137 gone) and not an empty directory to clone into (with isEmpty's check
// gone).
func TestAnUnreadableDirectoryIsAPermissionError(t *testing.T) {
	needGit(t)
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("mode 000 does not stop root, and Windows has no such mode; the other platforms hold this")
	}
	url, older, _ := upstream(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	_, err := pin(dir, older, url)
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("want a permission error, got %v", err)
	}
}

// main.go:177. Both fetches failing is reported as the fetch, not as the
// checkout that would fail after it.
func TestAFetchThatCannotReachTheRemoteSaysFetch(t *testing.T) {
	needGit(t)
	url, _, _ := upstream(t)
	dir := filepath.Join(t.TempDir(), "rubyspec-corpus")
	gitT(t, filepath.Dir(dir), "clone", "--quiet", url, dir)
	if err := os.RemoveAll(url); err != nil {
		t.Fatal(err)
	}
	_, err := pin(dir, strings.Repeat("ab", 20), url)
	wantErr(t, err, "fetch of ", "failed (offline?)")
}

// main.go:181. A SHA the remote does not have fails AT the checkout; with that
// refusal gone the read-back (:190) answers, saying a hook moved HEAD — a
// wrong diagnosis.
func TestACommitTheRemoteLacksFailsAtTheCheckout(t *testing.T) {
	needGit(t)
	url, _, _ := upstream(t)
	_, err := pin(filepath.Join(t.TempDir(), "c"), strings.Repeat("ab", 20), url)
	wantErr(t, err, "checkout of ", "is it a commit of")
}

// main.go:218 and :132. When git itself cannot be run, that is the error —
// not "clone failed", which is what follows if ownCheckout's failure is
// swallowed and the directory is taken for not-a-checkout.
func TestNoGitOnThePathIsSaidPlainly(t *testing.T) {
	needGit(t)
	url, older, _ := upstream(t)
	dir := t.TempDir()
	t.Setenv("PATH", "")
	_, err := pin(dir, older, url)
	wantErr(t, err, "could not run git")
}

// main.go:260, gitOut's own error. The read-back after a checkout must carry
// WHY it could not read HEAD; with the error swallowed the refusal still
// fires (an empty head is never the SHA) but says "read back: <nil>".
func TestAnUnreadableHeadCarriesTheReadError(t *testing.T) {
	needGit(t)
	url, older, _ := upstream(t)
	dir := filepath.Join(t.TempDir(), "rubyspec-corpus")
	gitT(t, filepath.Dir(dir), "clone", "--quiet", url, dir)
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "post-checkout"),
		[]byte("#!/bin/sh\nrm -f \"$(git rev-parse --git-dir)/HEAD\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", hooks)

	_, err := pin(dir, older, url)
	wantErr(t, err, "read back: git rev-parse HEAD")
}

// main.go:77, :87 and :94 — run's own refusals, each doubled one layer down.
func TestRunsOwnRefusals(t *testing.T) {
	needGit(t)

	// :77. With the parse error ignored, run goes on to the required-flags
	// check and exits 2 all the same — with a second, misleading message.
	t.Run("an unknown flag stops at the flag", func(t *testing.T) {
		var out, errb bytes.Buffer
		if code := run([]string{"-bogus"}, &out, &errb); code != 2 {
			t.Fatalf("exit %d, want 2", code)
		}
		if strings.Contains(errb.String(), "are required") {
			t.Fatalf("an unknown flag went on to the required-flags check: %q", errb.String())
		}
	})

	// :87. ⛔ With no cache directory and the error ignored, the default
	// becomes the RELATIVE path rbgo/rubyspec-corpus, and specpin clones into
	// whatever directory it was started from.
	t.Run("no user cache directory is refused, not made relative", func(t *testing.T) {
		if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
			t.Skip("the cache directory comes from other variables there")
		}
		url, older, _ := upstream(t)
		t.Setenv("HOME", "")
		t.Setenv("XDG_CACHE_HOME", "")
		var out, errb bytes.Buffer
		if code := run([]string{"-sha", older, "-url", url}, &out, &errb); code != 2 {
			t.Fatalf("exit %d, want 2; stderr %q", code, errb.String())
		}
		if !strings.Contains(errb.String(), "no user cache directory") {
			t.Fatalf("stderr = %q, want it to say there is no cache directory", errb.String())
		}
	})

	// :94. A pin that fails must not print a directory: run.sh would use it.
	t.Run("a failed pin prints no directory and exits 2", func(t *testing.T) {
		url, _, _ := upstream(t)
		var out, errb bytes.Buffer
		code := run([]string{"-sha", strings.Repeat("ab", 20), "-url", url, "-cache", filepath.Join(t.TempDir(), "c")}, &out, &errb)
		if code != 2 || out.Len() != 0 {
			t.Fatalf("exit %d, stdout %q; want 2 and nothing", code, out.String())
		}
	})
}
