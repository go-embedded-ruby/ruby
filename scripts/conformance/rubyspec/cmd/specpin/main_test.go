// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// needGit skips when the host has no git at all. That is not a build failure
// hidden as a skip (#687): this command's whole job is to drive git, and a host
// without it cannot arrange any of these conditions.
func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on this host; specpin drives git and cannot be exercised without it")
	}
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// upstream builds a repository shaped like ruby/spec with two commits and
// returns its path and both SHAs. The OLDER one is the pin target in most
// rows, so a tool that merely cloned and stayed on the default branch would be
// caught: the tip is not what was asked for.
func upstream(t *testing.T) (url, older, tip string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "spec")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "init", "--quiet", "--initial-branch=master")
	writeFile(t, filepath.Join(dir, "language", "a_spec.rb"), "# one\n")
	writeFile(t, filepath.Join(dir, "core", "b_spec.rb"), "# one\n")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "--quiet", "-m", "one")
	older = gitT(t, dir, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(dir, "core", "c_spec.rb"), "# two\n")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "--quiet", "-m", "two")
	tip = gitT(t, dir, "rev-parse", "HEAD")
	return dir, older, tip
}

func TestAnAbsentCacheIsClonedAtTheAskedCommitNotTheTip(t *testing.T) {
	needGit(t)
	url, older, _ := upstream(t)
	dir := filepath.Join(t.TempDir(), "cache", "rubyspec-corpus")

	got, err := pin(dir, older, url)
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	if got != older {
		t.Fatalf("pin reported %s, want %s", got, older)
	}
	if head := gitT(t, dir, "rev-parse", "HEAD"); head != older {
		t.Fatalf("the checkout is at %s, want %s", head, older)
	}
	if _, err := os.Stat(filepath.Join(dir, "core", "c_spec.rb")); err == nil {
		t.Fatal("a file from the TIP is present, so the checkout is not at the asked commit")
	}
}

// ⭐ #857's own case: a cache that exists, at the wrong commit. The old run.sh
// reused it and printed the pinned SHA beside it.
func TestACacheAtAnotherCommitIsMovedNotReused(t *testing.T) {
	needGit(t)
	url, older, tip := upstream(t)
	dir := filepath.Join(t.TempDir(), "rubyspec-corpus")
	gitT(t, filepath.Dir(dir), "clone", "--quiet", url, dir)
	if head := gitT(t, dir, "rev-parse", "HEAD"); head != tip {
		t.Fatalf("setup: the cache should start at the tip %s, is at %s", tip, head)
	}

	got, err := pin(dir, older, url)
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	if got != older {
		t.Fatalf("pin reported %s, want %s", got, older)
	}
	if head := gitT(t, dir, "rev-parse", "HEAD"); head != older {
		t.Fatalf("the cache stayed at %s; it had to move to %s", head, older)
	}
}

// The accepting side: a cache already at the commit is used as is, with no
// network. The URL here does not exist, so any fetch would fail the test.
func TestACacheAlreadyAtTheCommitNeedsNoNetwork(t *testing.T) {
	needGit(t)
	url, _, tip := upstream(t)
	dir := filepath.Join(t.TempDir(), "rubyspec-corpus")
	gitT(t, filepath.Dir(dir), "clone", "--quiet", url, dir)

	got, err := pin(dir, tip, filepath.Join(t.TempDir(), "no-such-remote"))
	if err != nil {
		t.Fatalf("pin of a cache already at the commit tried the network: %v", err)
	}
	if got != tip {
		t.Fatalf("pin reported %s, want %s", got, tip)
	}
}

// ⛔⛔ The line that replaced `rm -rf "$CACHE"`. A mistyped CACHE pointing at a
// directory with anything in it must be refused, and what was in it must still
// be there afterwards.
func TestADirectoryThatIsNotACheckoutIsNeverDeleted(t *testing.T) {
	needGit(t)
	url, older, _ := upstream(t)
	dir := t.TempDir() // stands for $HOME
	precious := filepath.Join(dir, "thesis.tex")
	writeFile(t, precious, "three years of work\n")

	if _, err := pin(dir, older, url); err == nil {
		t.Fatal("pin accepted a non-empty directory that is not a checkout")
	}
	b, err := os.ReadFile(precious)
	if err != nil || string(b) != "three years of work\n" {
		t.Fatalf("the directory's contents were touched: %v %q", err, b)
	}
}

// ⛔ A cache directory INSIDE another repository answers `rev-parse HEAD` with
// that repository's commit. It must not be mistaken for a checkout, and the
// outer repository must not be fetched into or moved.
func TestADirectoryInsideAnotherRepositoryIsNotTreatedAsTheCorpus(t *testing.T) {
	needGit(t)
	url, older, _ := upstream(t)
	outer := t.TempDir()
	gitT(t, outer, "init", "--quiet", "--initial-branch=main")
	writeFile(t, filepath.Join(outer, "README"), "the interpreter\n")
	writeFile(t, filepath.Join(outer, "cache", "keep"), "x\n")
	gitT(t, outer, "add", "-A")
	gitT(t, outer, "commit", "--quiet", "-m", "outer")
	before := gitT(t, outer, "rev-parse", "HEAD")

	if _, err := pin(filepath.Join(outer, "cache"), older, url); err == nil {
		t.Fatal("pin accepted a non-empty subdirectory of another repository as the corpus")
	}
	if after := gitT(t, outer, "rev-parse", "HEAD"); after != before {
		t.Fatalf("the OUTER repository moved from %s to %s", before, after)
	}
}

// ⛔⛔ Found by the first end-to-end run of run.sh: SPECDIR named a scratch
// directory that happened to be a git repository of its own, and specpin went
// straight to `fetch`. It failed only because that repository had no origin.
// Here the foreign repository is the worst case — it even HOLDS the asked
// commit, so fetch and `checkout --force` would both have succeeded and thrown
// its uncommitted work away. It must be refused, and left exactly as it was.
func TestACheckoutOfAnotherRemoteIsNeitherFetchedIntoNorMoved(t *testing.T) {
	needGit(t)
	url, older, tip := upstream(t)
	dir := filepath.Join(t.TempDir(), "someone-elses-clone")
	gitT(t, filepath.Dir(dir), "clone", "--quiet", url, dir)
	// The fork EXISTS and holds the commit. A first version of this test named
	// a path with nothing at it, and passed with the guard deleted: the fetch
	// failed on its own, and its message quoted the path the assertion looked
	// for. Only a remote that would SERVE the commit shows what the guard stops.
	fork := filepath.Join(t.TempDir(), "their-fork")
	gitT(t, filepath.Dir(fork), "clone", "--quiet", "--bare", url, fork)
	gitT(t, dir, "remote", "set-url", "origin", fork)
	wip := filepath.Join(dir, "core", "c_spec.rb")
	writeFile(t, wip, "# uncommitted work\n")

	_, err := pin(dir, older, url)
	if err == nil {
		t.Fatal("pin moved a checkout whose origin is not -url")
	}
	if !strings.Contains(err.Error(), "their-fork") {
		t.Fatalf("the refusal does not name the origin it found: %v", err)
	}
	if head := gitT(t, dir, "rev-parse", "HEAD"); head != tip {
		t.Fatalf("HEAD moved from %s to %s", tip, head)
	}
	if b, err := os.ReadFile(wip); err != nil || string(b) != "# uncommitted work\n" {
		t.Fatalf("the uncommitted work was discarded: %v %q", err, b)
	}

	// The other side: the same clone with its origin put back IS moved, so
	// what refused above was the origin and nothing else.
	gitT(t, dir, "remote", "set-url", "origin", url)
	gitT(t, dir, "checkout", "--quiet", "--", ".")
	if got, err := pin(dir, older, url); err != nil || got != older {
		t.Fatalf("pin of a clone of -url = %q, %v; want %s", got, err, older)
	}
}

func TestAnEmptyDirectoryIsClonedInto(t *testing.T) {
	needGit(t)
	url, older, _ := upstream(t)
	dir := t.TempDir()
	if got, err := pin(dir, older, url); err != nil || got != older {
		t.Fatalf("pin into an empty directory = %q, %v; want %s", got, err, older)
	}
}

func TestRefusals(t *testing.T) {
	needGit(t)
	url, older, _ := upstream(t)

	t.Run("a short SHA is refused before the disk is touched", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "never-created")
		if _, err := pin(dir, older[:7], url); err == nil {
			t.Fatal("pin accepted a 7-character prefix")
		}
		if _, err := os.Stat(dir); err == nil {
			t.Fatal("the directory was created for a SHA that was refused")
		}
	})
	t.Run("a commit the remote does not have", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "c")
		if _, err := pin(dir, strings.Repeat("ab", 20), url); err == nil {
			t.Fatal("pin reported success for a commit that does not exist")
		}
	})
	t.Run("a repository that is not shaped like ruby/spec", func(t *testing.T) {
		other := filepath.Join(t.TempDir(), "other")
		if err := os.MkdirAll(other, 0o755); err != nil {
			t.Fatal(err)
		}
		gitT(t, other, "init", "--quiet", "--initial-branch=master")
		writeFile(t, filepath.Join(other, "README"), "not ruby/spec\n")
		gitT(t, other, "add", "-A")
		gitT(t, other, "commit", "--quiet", "-m", "x")
		sha := gitT(t, other, "rev-parse", "HEAD")
		if _, err := pin(filepath.Join(t.TempDir(), "c"), sha, other); err == nil {
			t.Fatal("pin accepted a checkout with no language/ or core/")
		}
	})
	t.Run("a path that is a file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "file")
		writeFile(t, f, "x")
		if _, err := pin(f, older, url); err == nil {
			t.Fatal("pin accepted a regular file as the corpus directory")
		}
	})
}

// TestTheReportedCommitIsTheOneReadBack holds defect 2 at the CLI boundary:
// stdout carries the directory alone (run.sh captures it), and the stderr line
// names the commit git reports, which here is asserted against git itself
// rather than against the flag that was passed.
func TestTheReportedCommitIsTheOneReadBack(t *testing.T) {
	needGit(t)
	url, older, _ := upstream(t)
	dir := filepath.Join(t.TempDir(), "rubyspec-corpus")
	var out, errb bytes.Buffer

	if code := run([]string{"-sha", older, "-url", url, "-cache", dir}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if out.String() != dir+"\n" {
		t.Fatalf("stdout = %q, want exactly the directory", out.String())
	}
	head := gitT(t, dir, "rev-parse", "HEAD")
	if !strings.Contains(errb.String(), "corpus: "+dir+" @ "+head) {
		t.Fatalf("stderr = %q, want the commit git reports (%s)", errb.String(), head)
	}

	// A missing flag is refused with a message that NAMES the flag. pin would
	// refuse an empty -sha too, one layer down, but as "-sha must be a full
	// 40-character … got \"\"" — true, and useless to someone who forgot it.
	// The required-flags check exists for its message, so its message is what
	// is held.
	for _, args := range [][]string{{"-url", url}, {"-sha", older}} {
		out.Reset()
		errb.Reset()
		if code := run(args, &out, &errb); code != 2 {
			t.Fatalf("run(%v) exited %d, want 2", args, code)
		}
		if !strings.Contains(errb.String(), "-sha and -url are required") {
			t.Fatalf("run(%v) stderr = %q, want it to name the missing flags", args, errb.String())
		}
		if out.Len() != 0 {
			t.Fatalf("a refusal printed a directory on stdout: %q — run.sh would use it", out.String())
		}
	}
}

// TestTheCommitIsReadBackAfterCheckoutNotAssumed holds defect 2 where it can
// actually happen. A checkout that exits 0 has not necessarily left HEAD where
// it was asked to: a post-checkout hook runs afterwards and can move it, and
// this machine, like many, has global hooks. The old run.sh printed the pinned
// constant whatever HEAD said; this asks git again and refuses a mismatch.
//
// The hook directory is forced through GIT_CONFIG_COUNT, which outranks every
// config file, so the test is hermetic against the host's own core.hooksPath.
// It is set AFTER the fixtures are built, so only pin's own git runs see it.
func TestTheCommitIsReadBackAfterCheckoutNotAssumed(t *testing.T) {
	needGit(t)

	withHook := func(t *testing.T, script string) {
		t.Helper()
		hooks := t.TempDir()
		p := filepath.Join(hooks, "post-checkout")
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_CONFIG_COUNT", "1")
		t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
		t.Setenv("GIT_CONFIG_VALUE_0", hooks)
	}

	t.Run("a hook that moves HEAD elsewhere is caught", func(t *testing.T) {
		url, older, tip := upstream(t)
		dir := filepath.Join(t.TempDir(), "rubyspec-corpus")
		gitT(t, filepath.Dir(dir), "clone", "--quiet", url, dir)
		// The guard variable stops the hook re-entering on its own checkout.
		withHook(t, `[ -n "$SPECPIN_HOOK_RAN" ] && exit 0
SPECPIN_HOOK_RAN=1 git checkout --quiet --force `+tip)

		_, err := pin(dir, older, url)
		if err == nil {
			t.Fatalf("pin reported %s although a hook left HEAD at %s", older, tip)
		}
		if !strings.Contains(err.Error(), tip) {
			t.Fatalf("the refusal does not name where HEAD actually is (%s): %v", tip, err)
		}
	})

	t.Run("a hook that leaves no readable HEAD is caught", func(t *testing.T) {
		url, older, _ := upstream(t)
		dir := filepath.Join(t.TempDir(), "rubyspec-corpus")
		gitT(t, filepath.Dir(dir), "clone", "--quiet", url, dir)
		withHook(t, `rm -f "$(git rev-parse --git-dir)/HEAD"`)

		if got, err := pin(dir, older, url); err == nil {
			t.Fatalf("pin reported %q with no HEAD left to read", got)
		}
	})
}
