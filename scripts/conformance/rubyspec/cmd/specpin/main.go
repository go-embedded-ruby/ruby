// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Command specpin puts the ruby/spec corpus at exactly one commit, in a
// directory that survives a reboot, and says which commit it actually holds
// (#857).
//
//	specpin -sha <40-hex> -url <repo> [-cache <dir>]
//
// It prints the corpus directory on stdout, for run.sh to use, and
// `corpus: <dir> @ <sha read back from git>` on stderr.
//
// Why it is a program and not four lines of run.sh. The four lines it replaces
// had three defects, and the third is the reason this is written in a language
// that can be tested:
//
//  1. The cache defaulted to /tmp/rubyspec-corpus, which macOS purges and most
//     Linux distributions empty at boot. The corpus a baseline was measured
//     against was not a durable artefact.
//
//  2. ⛔ A cache that existed was reused WITHOUT checking its commit, and the
//     log then printed `corpus: <dir> @ $SPEC_SHA` — the CONSTANT, not what was
//     checked out. A cache from another SPEC_SHA reads as a sweep with files
//     added or missing, and cmd/ratchet's MISSING FROM THE SWEEP is the very
//     verdict a regression produces. The line meant to say which corpus was
//     measured could only ever say the one that was asked for.
//
//  3. ⛔⛔ `rm -rf "$CACHE"` ran whenever "$SPECDIR/language" was absent. That
//     condition is true of almost every directory on a machine. `CACHE=$HOME`,
//     one slip on a command line, met it and deleted the home directory.
//
// So: the commit is READ BACK from git after every path that can change it, a
// directory that is not already a ruby/spec checkout of its own is never
// deleted, and only an empty or absent one is cloned into.
//
// The default directory is os.UserCacheDir()/rbgo/rubyspec-corpus —
// ~/Library/Caches on macOS, $XDG_CACHE_HOME or ~/.cache elsewhere — which is
// what #857 suggested, from the one place that already knows each platform's
// answer.
//
// It uses `flag`, as the eight commands beside it do, rather than cobra: this
// module is the interpreter that other programs embed, and a dependency added
// for a script would be added to every one of them.
//
// Exit status: 0 the corpus is at -sha; 2 it is not, or could not be made so,
// or the directory was refused. There is no 1: there is no verdict here to
// disagree with, only a precondition that holds or does not.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var osExit = os.Exit

func main() {
	osExit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("specpin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sha := fs.String("sha", "", "the full 40-hex commit the corpus must be at (required)")
	url := fs.String("url", "", "where to clone or fetch it from (required)")
	cache := fs.String("cache", "", "the corpus directory (default: <user cache dir>/rbgo/rubyspec-corpus)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *sha == "" || *url == "" {
		fmt.Fprintln(stderr, "specpin: -sha and -url are required")
		return 2
	}
	dir := *cache
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			fmt.Fprintf(stderr, "specpin: no user cache directory on this machine (%v); name one with -cache\n", err)
			return 2
		}
		dir = filepath.Join(base, "rbgo", "rubyspec-corpus")
	}
	got, err := pin(dir, *sha, *url)
	if err != nil {
		fmt.Fprintf(stderr, "specpin: %v\n", err)
		return 2
	}
	fmt.Fprintf(stderr, "corpus: %s @ %s\n", dir, got)
	fmt.Fprintln(stdout, dir)
	return 0
}

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// pin makes dir a ruby/spec checkout at sha and returns the commit it READ
// BACK, which is the only value the caller may report.
//
// ⛔ A short or mistyped SHA is refused before anything touches the disk. A
// prefix would be resolved by git against whatever the clone happens to hold,
// and an identifier reconstructed from a prefix has already cost this fleet
// four wrong measurements.
func pin(dir, sha, url string) (string, error) {
	if !fullSHA.MatchString(sha) {
		return "", fmt.Errorf("-sha must be a full 40-character lowercase hex commit, got %q", sha)
	}

	fi, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", err
		}
		if err := git("", "clone", "--quiet", "--filter=blob:none", url, dir); err != nil {
			return "", fmt.Errorf("clone %s into %s failed (offline?): %w", url, dir, err)
		}
	case err != nil:
		return "", err
	case !fi.IsDir():
		return "", fmt.Errorf("%s exists and is not a directory; refusing to touch it", dir)
	default:
		own, err := ownCheckout(dir)
		if err != nil {
			return "", err
		}
		if !own {
			empty, err := isEmpty(dir)
			if err != nil {
				return "", err
			}
			if !empty {
				// ⛔ The line that replaced `rm -rf "$CACHE"`. A directory that is
				// not a checkout of its own and holds anything at all is someone's
				// directory, and the right answer to a mistyped CACHE is to say so.
				return "", fmt.Errorf("%s is not empty and is not a git checkout of its own; refusing to delete or reuse it — point -cache at an empty directory or at an existing ruby/spec clone", dir)
			}
			if err := git("", "clone", "--quiet", "--filter=blob:none", url, dir); err != nil {
				return "", fmt.Errorf("clone %s into %s failed (offline?): %w", url, dir, err)
			}
		}
	}

	if head, err := revParse(dir); err == nil && head == sha {
		// Already there. Nothing is fetched, so a machine with the right corpus
		// and no network still measures.
		return checkTree(dir, head)
	}

	// ⛔ Everything below MOVES the checkout — a fetch writes its refs, and
	// `checkout --force` discards its work tree's changes. Being a checkout of
	// its own is not enough for that: any clone is, and SPECDIR pointed at the
	// wrong one (the first run of this very wiring was pointed at a scratch
	// repository) would have it fetched into and overwritten. So it must be a
	// clone OF -url. Read raw from the config, not through `remote get-url`,
	// which applies the host's url.insteadOf rewrites and would refuse a clone
	// this command made itself.
	//
	// The early return above is NOT held to this: a tree already at sha and
	// shaped like ruby/spec is the corpus by content, whatever it was cloned
	// from, and nothing there is moved.
	if origin, _ := gitOut(dir, "config", "--get", "remote.origin.url"); origin != url {
		return "", fmt.Errorf("%s is at another commit and its origin is %q, not %s; refusing to fetch into it or check it out — point -cache at an empty directory or at a clone of %s", dir, origin, url, url)
	}

	// Fetching by SHA is the cheap path and GitHub serves it; a server that
	// refuses a SHA want gets an ordinary fetch of every ref instead.
	if err := git(dir, "fetch", "--quiet", "--filter=blob:none", "origin", sha); err != nil {
		if err2 := git(dir, "fetch", "--quiet", "--filter=blob:none", "origin"); err2 != nil {
			return "", fmt.Errorf("fetch of %s into %s failed (offline?): %v; %w", sha, dir, err, err2)
		}
	}
	if err := git(dir, "checkout", "--quiet", "--force", sha); err != nil {
		return "", fmt.Errorf("checkout of %s in %s failed — is it a commit of %s? %w", sha, dir, url, err)
	}
	// ONE refusal for "HEAD is not where it was asked to be", whether git could
	// not say where HEAD is or said somewhere else. They were two arms, and a
	// mutation sweep showed the first held nothing: a failed read leaves head
	// empty, which is never sha, so the second refused anyway — with a worse
	// message. The read error now travels in the one that remains.
	head, err := revParse(dir)
	if err != nil || head != sha {
		return "", fmt.Errorf("%s is at %q after checking out %s (read back: %v) — a post-checkout hook, or something else, moved it", dir, head, sha, err)
	}
	return checkTree(dir, head)
}

// checkTree refuses a commit that is not shaped like ruby/spec, so a -url that
// points at the wrong repository fails here and not as a sweep of zero files.
func checkTree(dir, head string) (string, error) {
	for _, suite := range []string{"language", "core"} {
		fi, err := os.Stat(filepath.Join(dir, suite))
		if err != nil || !fi.IsDir() {
			return "", fmt.Errorf("%s @ %s has no %s/ directory; this is not a ruby/spec checkout", dir, head, suite)
		}
	}
	return head, nil
}

// ownCheckout reports whether dir is the TOP of a git work tree.
//
// ⛔ Not merely "inside one". A cache directory created under some other
// repository — the interpreter's own clone, say — answers `git rev-parse HEAD`
// with THAT repository's commit, and a fetch there would fetch into it. Only a
// directory that is its own toplevel is ours to move.
func ownCheckout(dir string) (bool, error) {
	var out bytes.Buffer
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return false, nil // git ran and said: not a work tree
		}
		return false, fmt.Errorf("could not run git: %w", err)
	}
	// Compared as FILES, not as paths. A path comparison needs both sides made
	// absolute and symlink-free first — macOS's /var is /private/var, and git
	// answers with the resolved one — and every step of that normalisation is
	// an error arm that cannot fire on a directory already stat'ed by the
	// caller. A mutation sweep deleted all three and nothing failed, because
	// nothing could. os.SameFile asks the filesystem the question directly.
	//
	// ⛔ The two Stat errors are discarded ON PURPOSE, and this is why it is
	// safe: os.SameFile answers false for a nil FileInfo (it type-asserts, it
	// does not dereference), and false is "not ours" — the refusing direction,
	// in which the caller will not fetch into dir or clone over it. A sweep
	// showed both `if err != nil` arms changing no answer at all; keeping them
	// would keep two lines nothing can ever hold.
	top, _ := os.Stat(strings.TrimSpace(out.String()))
	here, _ := os.Stat(dir)
	return os.SameFile(top, here), nil
}

func isEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func revParse(dir string) (string, error) {
	return gitOut(dir, "rev-parse", "HEAD")
}

// gitOut runs git in dir and returns its trimmed stdout.
func gitOut(dir string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s in %s: %w", strings.Join(args, " "), dir, err)
	}
	return strings.TrimSpace(out.String()), nil
}

func git(dir string, args ...string) error {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	var errb bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
