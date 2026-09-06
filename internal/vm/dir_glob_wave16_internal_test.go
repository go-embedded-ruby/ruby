// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"testing"
)

// globDotTree builds a fixture exercising the "." matching rule and '**' symlink
// handling: dot files at several depths, plus a symlink to a directory.
func globDotTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"deeply/nested/directory/structure", "sub"} {
		if err := os.MkdirAll(dir+"/"+d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		"deeply/.dotfile", "deeply/nondotfile", "deeply/nested/.dotfile.ext",
		"deeply/nested/directory/structure/.ext", "deeply/nested/directory/structure/bar",
		"sub/f",
	} {
		if err := os.WriteFile(dir+"/"+f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestDirGlobDotRule drives Dir.glob's synthetic-"." rule (litPath) exactly as
// MRI: "." matches a terminal segment only on an all-literal path, or at the top
// level of a '**' pattern under FNM_DOTMATCH. Every expected value was verified
// against MRI Ruby 4.0.5.
func TestDirGlobDotRule(t *testing.T) {
	dir := slash(globDotTree(t)) + "/deeply"
	cd := func(src string) string { return `Dir.chdir("` + dir + `") { ` + src + ` }` }
	cases := []struct{ src, want string }{
		// Literal path: "." (and "nested/.") match.
		{cd(`p Dir.glob(".*").sort`), "[\".\", \".dotfile\"]\n"},
		{cd(`p Dir.glob("nested/.*").sort`), "[\"nested/.\", \"nested/.dotfile.ext\"]\n"},
		// Wildcard-reached path: no "nested/.".
		{cd(`p Dir.glob("*/.*").sort`), "[\"nested/.dotfile.ext\"]\n"},
		// '**' without DOTMATCH: no "." anywhere.
		{cd(`p Dir.glob("**/.*").sort`), "[\".dotfile\", \"nested/.dotfile.ext\", \"nested/directory/structure/.ext\"]\n"},
		// '**' with DOTMATCH: "." at the top level only.
		{cd(`p Dir.glob("**/.*", File::FNM_DOTMATCH).sort`), "[\".\", \".dotfile\", \"nested/.dotfile.ext\", \"nested/directory/structure/.ext\"]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestDirGlobSymlink covers the '**' symlink rule: a symlink to a directory is
// matched as an entry but never traversed (isRealDirFS), matching MRI. Skipped
// where symlinks cannot be created.
func TestDirGlobSymlink(t *testing.T) {
	dir := slash(globDotTree(t))
	if err := os.Symlink(dir+"/sub", dir+"/link"); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	// '**/f' finds sub/f but NOT link/f (the symlink is not traversed).
	if got := runFS(t, `Dir.chdir("`+dir+`") { p Dir.glob("**/f").sort }`); got != "[\"sub/f\"]\n" {
		t.Errorf("glob **/f through symlink: got %q want [\"sub/f\"]", got)
	}
	// An explicit path segment DOES follow the symlink (isDirFS).
	if got := runFS(t, `Dir.chdir("`+dir+`") { p Dir.glob("link/f") }`); got != "[\"link/f\"]\n" {
		t.Errorf("glob link/f: got %q want [\"link/f\"]", got)
	}
}

// TestIsRealDirFS covers isRealDirFS directly: a real directory is true, a
// symlink to a directory is false (it is not followed), and a plain file / a
// missing path are false.
func TestIsRealDirFS(t *testing.T) {
	dir := t.TempDir()
	if !isRealDirFS(dir) {
		t.Errorf("isRealDirFS(real dir) = false")
	}
	f := dir + "/file"
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isRealDirFS(f) {
		t.Errorf("isRealDirFS(file) = true")
	}
	if isRealDirFS(dir + "/missing") {
		t.Errorf("isRealDirFS(missing) = true")
	}
	link := dir + "/link"
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if isRealDirFS(link) {
		t.Errorf("isRealDirFS(symlink to dir) = true; want false (not followed)")
	}
	if !isDirFS(link) {
		t.Errorf("isDirFS(symlink to dir) = false; want true (followed)")
	}
}

// TestDirGlobBraceAndDedup covers MRI's brace/duplicate semantics: brace
// alternatives expand left-most first and concatenate in that order, and
// duplicates across repeated patterns or brace alternatives are preserved (Dir
// does not de-duplicate across sub-globs).
func TestDirGlobBraceAndDedup(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"brace/x.js.rjs", "brace/x.html.erb", "one.ext", "two.ext"} {
		if err := os.MkdirAll(dir+"/"+f[:len(f)-len(baseName(f))], 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir+"/"+f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	d := slash(dir)
	cases := []struct{ src, want string }{
		// Brace order (sort default true): the sub-globs concatenate in expansion
		// order, not alphabetically.
		{`Dir.chdir("` + d + `") { p Dir.glob("brace/x{.js,.html}{.erb,.rjs}") }`,
			"[\"brace/x.js.rjs\", \"brace/x.html.erb\"]\n"},
		// A repeated pattern doubles its matches (no cross-pattern dedup).
		{`Dir.chdir("` + d + `") { p Dir["*.ext", "*.ext"] }`,
			"[\"one.ext\", \"two.ext\", \"one.ext\", \"two.ext\"]\n"},
		// A brace with a duplicate alternative repeats its match.
		{`Dir.chdir("` + d + `") { p Dir.glob("{one.ext,one.ext}") }`,
			"[\"one.ext\", \"one.ext\"]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// baseName returns the final path component of a slash path (test helper).
func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// TestDirGlobArgGuards covers the pattern/keyword coercion guards: an empty
// pattern is [], sort: accepts only true/false, a NUL in the pattern is an
// ArgumentError, an ASCII-incompatible pattern is an Encoding::CompatibilityError,
// and a #to_path pattern object is accepted.
func TestDirGlobArgGuards(t *testing.T) {
	if got := runFS(t, `p Dir.glob("")`); got != "[]\n" {
		t.Errorf("Dir.glob(\"\") = %q want []", got)
	}
	if got := runFS(t, `p Dir.glob("*.none", sort: false).class`); got != "Array\n" {
		t.Errorf("Dir.glob sort:false = %q", got)
	}
	errCases := []struct{ src, want string }{
		{`Dir.glob("*", sort: 1)`, "ArgumentError"},
		{`Dir.glob("*", sort: nil)`, "ArgumentError"},
		{"Dir.glob(\"a\\0b\")", "ArgumentError"},
		{`Dir.glob(["ok", "a\0b"])`, "ArgumentError"},
		{`Dir.glob("*".encode(Encoding::UTF_16LE))`, "Encoding::CompatibilityError"},
	}
	for _, c := range errCases {
		if got := runFSErr(t, c.src); got != c.want {
			t.Errorf("%s: got %q want %q", c.src, got, c.want)
		}
	}
	// A #to_path pattern object is accepted and globbed.
	dir := slash(t.TempDir())
	if err := os.WriteFile(dir+"/f.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `o = Object.new; def o.to_path; "` + dir + `/*.txt"; end; p Dir.glob(o).map { |p| File.basename(p) }`
	if got := runFS(t, src); got != "[\"f.txt\"]\n" {
		t.Errorf("Dir.glob(#to_path) = %q", got)
	}
}
