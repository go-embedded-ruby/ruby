// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os/user"
	"strings"
	"testing"
)

// TestFilePathComponents drives File.basename/dirname/extname/split through rbgo,
// asserting Ruby 3.4 semantics: internal repeated separators are preserved, a
// leading run of separators collapses to the root, trailing separators are
// stripped, and the empty string / all-separator edge cases match MRI.
func TestFilePathComponents(t *testing.T) {
	cases := []struct{ src, want string }{
		// basename: last component, trailing separators stripped; "" and "//" edges.
		{`p File.basename("/Some/path/to/test.txt")`, "\"test.txt\"\n"},
		{`p File.basename("dir///base///")`, "\"base\"\n"},
		{`p File.basename("//foo//")`, "\"foo\"\n"},
		{`p File.basename("")`, "\"\"\n"},
		{`p File.basename("/")`, "\"/\"\n"},
		{`p File.basename("//")`, "\"/\"\n"},
		{`p File.basename(".")`, "\".\"\n"},
		{`p File.basename("foo")`, "\"foo\"\n"},
		// basename suffix: ".*" strips the extension, a literal suffix is exact.
		{`p File.basename("bar.txt.exe", ".*")`, "\"bar.txt\"\n"},
		{`p File.basename("bar.txt.exe", ".txt.exe")`, "\"bar\"\n"},
		{`p File.basename("/tmp.c", ".?")`, "\"tmp.c\"\n"},
		{`p File.basename("tmp", ".*")`, "\"tmp\"\n"},
		{`p File.basename("baz.rb", "z.rb")`, "\"ba\"\n"},
		// dirname: internal separators preserved, root collapsed, trailing stripped.
		{`p File.dirname("/holy///schnikies//w00t.bin")`, "\"/holy///schnikies\"\n"},
		{`p File.dirname("/home/jason")`, "\"/home\"\n"},
		{`p File.dirname("poot.txt")`, "\".\"\n"},
		{`p File.dirname("")`, "\".\"\n"},
		{`p File.dirname("/")`, "\"/\"\n"},
		{`p File.dirname("/foo")`, "\"/\"\n"},
		{`p File.dirname("/foo/")`, "\"/\"\n"},
		{`p File.dirname("//foo//")`, "\"/\"\n"},
		{`p File.dirname("/////foo/bar/")`, "\"/foo\"\n"},
		{`p File.dirname("foo/../")`, "\"foo\"\n"},
		{`p File.dirname("./b/./")`, "\"./b\"\n"},
		// dirname level: repeated stripping, level 0 identity, > depth converges.
		{`p File.dirname("/home/jason/poot.txt", 2)`, "\"/home\"\n"},
		{`p File.dirname("poot.txt", 0)`, "\"poot.txt\"\n"},
		{`p File.dirname("/home/jason", 100)`, "\"/\"\n"},
		// dirname level via #to_int coercion.
		{"o = Object.new; def o.to_int; 2; end; p File.dirname(\"/a/b/c/d\", o)", "\"/a/b\"\n"},
		// extname edge cases.
		{`p File.extname("foo.rb")`, "\".rb\"\n"},
		{`p File.extname(".bashrc")`, "\"\"\n"},
		{`p File.extname("foo.")`, "\".\"\n"},
		{`p File.extname("..")`, "\"\"\n"},
		{`p File.extname("a.b.c.d.e")`, "\".e\"\n"},
		{`p File.extname("/foo.bar/baz")`, "\"\"\n"},
		// split == [dirname, basename].
		{`p File.split("/foo///")`, "[\"/\", \"foo\"]\n"},
		{`p File.split("")`, "[\".\", \"\"]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFilePathArityErrors covers the ArgumentError/TypeError branches: a wrong
// argument count on basename/dirname/extname/split, a negative dirname level, and
// a non-coercible level argument.
func TestFilePathArityErrors(t *testing.T) {
	for _, src := range []string{
		`File.basename("a", "b", "c")`,
		`File.dirname("a", "b", "c")`,
		`File.extname`,
		`File.extname("a", "b")`,
		`File.split("a", "b")`,
	} {
		if got := runFSErr(t, src); got != "ArgumentError" {
			t.Errorf("%s: got %q want ArgumentError", src, got)
		}
	}
	if got := runFSErr(t, `File.dirname("/home/jason", -1)`); got != "ArgumentError" {
		t.Errorf("negative level: got %q want ArgumentError", got)
	}
	// A level that is neither an Integer nor #to_int-coercible is a TypeError.
	if got := runFSErr(t, `File.dirname("/a/b", "x")`); got != "TypeError" {
		t.Errorf("non-int level: got %q want TypeError", got)
	}
	// extname of a non-String, non-path-like value is a TypeError.
	if got := runFSErr(t, `File.extname(0)`); got != "TypeError" {
		t.Errorf("extname(0): got %q want TypeError", got)
	}
}

// TestFileJoin covers File.join: the boundary-separator rules, empty-string and
// empty-array insertion, nested-array recursion into a single string, the single
// argument duplicate, and the ArgumentError raised for a recursive array or a
// null byte.
func TestFileJoin(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p File.join("usr", "bin")`, "\"usr/bin\"\n"},
		{`p File.join("usr/", "//bin")`, "\"usr//bin\"\n"},
		{`p File.join("usr//", "/bin")`, "\"usr/bin\"\n"},
		{`p File.join("usr/", "bin")`, "\"usr/bin\"\n"},
		{`p File.join("usr", "/bin")`, "\"usr/bin\"\n"},
		{`p File.join("a", "", "c")`, "\"a/c\"\n"},
		{`p File.join`, "\"\"\n"},
		{`p File.join("usr")`, "\"usr\"\n"},
		{`p File.join("", "")`, "\"/\"\n"},
		{`p File.join([])`, "\"\"\n"},
		{`p File.join([], [])`, "\"/\"\n"},
		{`p File.join([[], []])`, "\"/\"\n"},
		{`p File.join(["a", ["b", ["c"]]])`, "\"a/b/c\"\n"},
		{`p File.join("a", [])`, "\"a/\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if got := runFSErr(t, "a = [\"a\"]; a << a; File.join(a)"); got != "ArgumentError" {
		t.Errorf("recursive array: got %q want ArgumentError", got)
	}
	if got := runFSErr(t, `File.join("\x00x", "y")`); got != "ArgumentError" {
		t.Errorf("null byte: got %q want ArgumentError", got)
	}
	if got := runFSErr(t, `File.join(nil)`); got != "TypeError" {
		t.Errorf("nil arg: got %q want TypeError", got)
	}
}

// TestFileExpandPath covers File.expand_path and File.absolute_path: ~ / ~user
// expansion, the leading-multiple-separator preservation, an explicit base
// directory, and the ArgumentError guards for an empty or non-absolute HOME and
// an unknown ~user. absolute_path must not expand ~.
func TestFileExpandPath(t *testing.T) {
	t.Setenv("HOME", "/rubyspec_home")
	cases := []struct{ src, want string }{
		{`p File.expand_path("~")`, "\"/rubyspec_home\"\n"},
		{`p File.expand_path("~/a")`, "\"/rubyspec_home/a\"\n"},
		{`p File.expand_path("////some/path")`, "\"////some/path\"\n"},
		{`p File.expand_path("/some////path")`, "\"/some/path\"\n"},
		{`p File.expand_path("a", "/base")`, "\"/base/a\"\n"},
		{`p File.expand_path("a", nil).start_with?("/")`, "true\n"},
		// absolute_path leaves ~ literal (no HOME expansion).
		{`p File.absolute_path("~", "/base")`, "\"/base/~\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// ~user: the current user resolves, an unknown user raises ArgumentError.
	if u, err := user.Current(); err == nil {
		src := `p File.expand_path("~` + u.Username + `").is_a?(String)`
		if got := runFS(t, src); got != "true\n" {
			t.Errorf("~user current: got %q", got)
		}
	}
	if got := runFSErr(t, `File.expand_path("~no_such_user_rbgo_xyz_123")`); got != "ArgumentError" {
		t.Errorf("~unknown user: got %q want ArgumentError", got)
	}
}

// TestFileExpandPathHomeGuards covers the empty-HOME and non-absolute-HOME
// ArgumentError branches of ~ expansion.
func TestFileExpandPathHomeGuards(t *testing.T) {
	t.Setenv("HOME", "")
	if got := runFSErr(t, `File.expand_path("~")`); got != "ArgumentError" {
		t.Errorf("empty HOME: got %q want ArgumentError", got)
	}
	t.Setenv("HOME", "relative/home")
	if got := runFSErr(t, `File.expand_path("~/x")`); got != "ArgumentError" {
		t.Errorf("non-absolute HOME: got %q want ArgumentError", got)
	}
}

// TestUserHomeDir covers the userHomeDir seam directly: a real user resolves and
// a bogus name returns an error (the branch File.expand_path maps to ArgumentError).
func TestUserHomeDir(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skipf("no current user: %v", err)
	}
	h, err := userHomeDir(u.Username)
	if err != nil || h == "" {
		t.Errorf("userHomeDir(%q) = %q, %v; want a home dir", u.Username, h, err)
	}
	if _, err := userHomeDir("no_such_user_rbgo_xyz_123"); err == nil {
		t.Error("userHomeDir(bogus): want error, got nil")
	}
}

// TestFilePathHelpers unit-tests the pure helpers directly for the no-separator
// and non-tilde fast paths not otherwise reached through the surface.
func TestFilePathHelpers(t *testing.T) {
	if got := expandTildePath("relative/path"); got != "relative/path" {
		t.Errorf("expandTildePath(non-~) = %q", got)
	}
	if got := rubyExtname("noext"); got != "" {
		t.Errorf("rubyExtname(noext) = %q", got)
	}
	if got := stripBaseSuffix("bar.txt", ".exe"); got != "bar.txt" {
		t.Errorf("stripBaseSuffix no-match = %q", got)
	}
	if got := fileJoin(nil); got != "" {
		t.Errorf("fileJoin(nil) = %q", got)
	}
	if !strings.HasPrefix(rubyDirname("a/b"), "a") {
		t.Errorf("rubyDirname(a/b) unexpected")
	}
}
