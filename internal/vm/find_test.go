// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFind exercises the Find module (require "find") backed by
// github.com/go-ruby-find/find against a real temporary directory tree, with
// every observable behaviour asserted to match MRI 4.0.5's stdlib Find: the
// top-down ascending-sorted depth-first walk, the no-block Enumerator, multiple
// start paths, Find.prune (the throw/catch control flow), the missing-start-path
// Errno::ENOENT and the ignore_error per-entry policy.
//
// WINDOWS-SAFETY: the Lister does real filesystem access, so the temp-dir path is
// always converted to forward slashes (filepath.ToSlash) before being embedded in
// Ruby source — never a backslash path — and the Ruby code uses '/'-separated
// paths throughout.
func TestFindModule(t *testing.T) {
	root := findTempTree(t)

	cases := []struct{ name, body, want string }{
		// Full top-down walk: each path yielded, directories before their (sorted)
		// children, depth-first.
		{"walk", `Find.find(ROOT) { |p| puts rel(p) }`,
			lines(".", "a", "a/1.txt", "a/2.txt", "b", "b/d", "b/d/k.txt", "z.txt")},
		// No block returns an Enumerator yielding the same paths.
		{"enum", `p Find.find(ROOT).map { |p| rel(p) }`,
			`[".", "a", "a/1.txt", "a/2.txt", "b", "b/d", "b/d/k.txt", "z.txt"]` + "\n"},
		// Find.prune on directory "b" skips it: prune runs before the puts, so b is
		// neither printed nor descended into (matching MRI's throw/catch).
		{"prune", `Find.find(ROOT) { |p| Find.prune if File.basename(p) == "b"; puts rel(p) }`,
			lines(".", "a", "a/1.txt", "a/2.txt", "z.txt")},
		// Two start paths are processed in turn, each fully walked.
		{"two_roots", `Find.find(ROOT + "/a", ROOT + "/b") { |p| puts rel(p) }`,
			lines("a", "a/1.txt", "a/2.txt", "b", "b/d", "b/d/k.txt")},
		// A start path that is a plain file is yielded once (and not descended).
		{"file_root", `Find.find(ROOT + "/z.txt") { |p| puts rel(p) }`, lines("z.txt")},
		// The module mixes in: an includer calls find / prune as private methods.
		// prune runs before the puts, so "a" is skipped (not printed, not descended).
		{"mixin", `class W; include Find; def go; find(ROOT) { |p| prune if File.basename(p) == "a"; puts rel(p) }; end; end; W.new.go`,
			lines(".", "b", "b/d", "b/d/k.txt", "z.txt")},
		// VERSION constant matches MRI's lib/find.rb.
		{"version", `puts Find::VERSION`, "0.2.0\n"},
		// The prelude already did `require "find"` (true, once), so a further require
		// returns false; defined?(Find) is "constant" after the load.
		{"require_again", `p require("find"); p defined?(Find)`,
			"false\n\"constant\"\n"},
	}

	prelude := `require "find"
ROOT = ` + rubyString(root) + "\n" +
		`def rel(p); r = p[ROOT.length..]; r = r.sub(%r{\A/}, ""); r.empty? ? "." : r; end` + "\n"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eval(t, prelude+tc.body)
			if got != tc.want {
				t.Errorf("body=%q\n got=%q\nwant=%q", tc.body, got, tc.want)
			}
		})
	}
}

// TestFindLazyLoad asserts Find is undefined until `require "find"`, mirroring
// MRI's lazy lib/find.rb load.
func TestFindLazyLoad(t *testing.T) {
	if got := eval(t, `p defined?(Find)`); got != "nil\n" {
		t.Errorf("Find should be undefined before require, got %q", got)
	}
	// require returns true the first time and false afterwards, installing Find.
	if got := eval(t, `p require("find"); p require("find"); p defined?(Find)`); got != "true\nfalse\n\"constant\"\n" {
		t.Errorf("require transition, got %q", got)
	}
}

// TestFindMissingPath covers the missing-start-path branch: Walk returns a
// *find.MissingPathError, which the binding maps to Errno::ENOENT with MRI's
// message, regardless of the ignore_error flag.
func TestFindMissingPath(t *testing.T) {
	for _, src := range []string{
		`require "find"; Find.find("/no/such/find/path") { |p| }`,
		`require "find"; Find.find("/no/such/find/path", ignore_error: false) { |p| }`,
	} {
		class, msg := evalErr(t, src)
		if class != "Errno::ENOENT" || msg != "No such file or directory - /no/such/find/path" {
			t.Errorf("src=%q got=%s:%q", src, class, msg)
		}
	}
}

// TestFindBlockRaise covers the findRubyError re-raise branch driven by the block
// itself: an exception raised inside the Find.find block stops the walk and
// surfaces verbatim (not swallowed by ignore_error, which governs the Lister, not
// the block).
func TestFindBlockRaise(t *testing.T) {
	root := findTempTree(t)
	src := `require "find"
Find.find(` + rubyString(root) + `) { |p| raise "boom" if File.basename(p) == "1.txt" }`
	class, msg := evalErr(t, src)
	if class != "RuntimeError" || msg != "boom" {
		t.Errorf("got=%s:%q want RuntimeError:boom", class, msg)
	}
}

// TestFindPerEntryError covers the Lister-error policy: a Dir.children that fails
// mid-walk is swallowed under ignore_error: true (the default, the walk continues)
// and re-raised verbatim under ignore_error: false (the findRubyError branch). The
// override is installed in Ruby, proving the Lister honours rbgo's own Dir method.
func TestFindPerEntryError(t *testing.T) {
	root := findTempTree(t)
	rs := rubyString(root)
	prelude := `require "find"
ROOT = ` + rs + "\n" +
		`ORIG = Dir.method(:children)` + "\n" +
		`def Dir.children(d); raise Errno::EACCES.new(d) if d == ROOT + "/a"; ORIG.call(d); end` + "\n" +
		`def rel(p); r = p[ROOT.length..].sub(%r{\A/}, ""); r.empty? ? "." : r; end` + "\n"

	// ignore_error: true (default) — the failing "a" listing is skipped, the walk
	// continues to b and z.txt.
	got := eval(t, prelude+`Find.find(ROOT) { |p| puts rel(p) }`)
	want := lines(".", "a", "b", "b/d", "b/d/k.txt", "z.txt")
	if got != want {
		t.Errorf("swallow: got=%q want=%q", got, want)
	}

	// ignore_error: false — the SystemCallError propagates out of Find.find.
	class, _ := evalErr(t, prelude+`Find.find(ROOT, ignore_error: false) { |p| }`)
	if class != "Errno::EACCES" {
		t.Errorf("propagate: got class=%q want Errno::EACCES", class)
	}
}

// TestFindLstatError covers the IsDir error branch: a File.lstat that fails
// mid-walk (the directory test) is swallowed under ignore_error: true — the entry
// is treated as a leaf and the walk continues — and propagated under
// ignore_error: false. The Lister honours rbgo's own File.lstat override.
func TestFindLstatError(t *testing.T) {
	root := findTempTree(t)
	rs := rubyString(root)
	prelude := `require "find"
ROOT = ` + rs + "\n" +
		`ORIG = File.method(:lstat)` + "\n" +
		`def File.lstat(p); raise Errno::EACCES.new(p) if p == ROOT + "/b"; ORIG.call(p); end` + "\n" +
		`def rel(p); r = p[ROOT.length..].sub(%r{\A/}, ""); r.empty? ? "." : r; end` + "\n"

	// ignore_error: true — b cannot be lstat'd, so it is yielded as a leaf (not
	// descended into) and the walk continues to z.txt.
	got := eval(t, prelude+`Find.find(ROOT) { |p| puts rel(p) }`)
	want := lines(".", "a", "a/1.txt", "a/2.txt", "b", "z.txt")
	if got != want {
		t.Errorf("lstat swallow: got=%q want=%q", got, want)
	}

	// ignore_error: false — the lstat failure propagates.
	class, _ := evalErr(t, prelude+`Find.find(ROOT, ignore_error: false) { |p| }`)
	if class != "Errno::EACCES" {
		t.Errorf("lstat propagate: got class=%q want Errno::EACCES", class)
	}
}

// findTempTree builds a deterministic tree under t.TempDir() and returns its root
// path. Layout (depth-first, sorted): a/1.txt a/2.txt b/d/k.txt z.txt.
func findTempTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"a", "b/d"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"a/1.txt", "a/2.txt", "b/d/k.txt", "z.txt"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(f)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// rubyString renders s as a forward-slash Ruby string literal — the WINDOWS-SAFETY
// rule: never embed an OS path with backslashes into Ruby source.
func rubyString(s string) string {
	return `"` + strings.ReplaceAll(filepath.ToSlash(s), `"`, `\"`) + `"`
}

// lines joins parts into newline-terminated puts output.
func lines(parts ...string) string { return strings.Join(parts, "\n") + "\n" }
