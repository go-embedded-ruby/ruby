// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFileLinkOps covers File.link/symlink/readlink/truncate/path over a real
// temp tree, plus their MRI error classes (EEXIST/EINVAL/ENOENT) and arity/type
// errors — exercising every branch of the new link/readlink helpers.
func TestFileLinkOps(t *testing.T) {
	dir := slash(t.TempDir())
	f := dir + "/f.txt"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("1234567890"), 0o644); err != nil {
		t.Fatal(err)
	}
	hard := dir + "/hard.lnk"
	sym := dir + "/sym.lnk"

	cases := []struct{ src, want string }{
		// link returns 0 and makes an identical file.
		{`p [File.link("` + f + `", "` + hard + `"), File.identical?("` + f + `", "` + hard + `")]`, "[0, true]\n"},
		// symlink returns 0; readlink round-trips the target; File.symlink? true.
		{`p [File.symlink("` + f + `", "` + sym + `"), File.readlink("` + sym + `") == "` + f + `", File.symlink?("` + sym + `")]`,
			"[0, true, true]\n"},
		// truncate shrinks the file and returns 0.
		{`File.truncate("` + f + `", 5); p [File.size("` + f + `")]`, "[5]\n"},
		// path echoes its argument unchanged.
		{`p File.path("some/path")`, "\"some/path\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}

	// Error classes.
	errCases := []struct{ expr, want string }{
		{`File.link("` + f + `", "` + hard + `")`, "Errno::EEXIST"}, // hard exists now
		{`File.link("` + dir + `/missing/x", "` + dir + `/y")`, "Errno::ENOENT"},
		{`File.link("` + f + `")`, "ArgumentError"},
		{`File.link("` + f + `", 1)`, "TypeError"},
		{`File.symlink("` + f + `", "` + sym + `")`, "Errno::EEXIST"}, // sym exists now
		{`File.readlink("` + f + `")`, "Errno::EINVAL"},               // not a link
		{`File.readlink("` + dir + `/nope")`, "Errno::ENOENT"},
		{`File.truncate("` + dir + `/nope", 1)`, "Errno::ENOENT"},
	}
	for _, c := range errCases {
		if got := runFSErr(t, c.expr); got != c.want {
			t.Errorf("%s: got %q want %q", c.expr, got, c.want)
		}
	}
}

// TestFileRealdirpath covers realdirpath's three branches: a fully-resolvable
// path, a path whose leaf does not yet exist (parent resolved + leaf rejoined),
// and a missing intermediate directory (Errno::ENOENT).
func TestFileRealdirpath(t *testing.T) {
	real := t.TempDir()
	// Full resolution: an existing file resolves to its canonical path.
	f := filepath.Join(real, "file")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	canon, err := filepath.EvalSymlinks(f)
	if err != nil {
		t.Fatal(err)
	}
	if got := runFS(t, `p File.realdirpath("`+slash(f)+`")`); got != "\""+slash(canon)+"\"\n" {
		t.Errorf("realdirpath(existing): got %q want %q", got, slash(canon))
	}
	// Leaf missing: the parent resolves and the leaf is rejoined.
	leaf := slash(real) + "/newleaf"
	got := strings.TrimSpace(runFS(t, `p File.realdirpath("`+leaf+`")`))
	if !strings.HasSuffix(got, "/newleaf\"") {
		t.Errorf("realdirpath(missing leaf): got %s, want …/newleaf", got)
	}
	// Missing intermediate directory: Errno::ENOENT.
	if e := runFSErr(t, `File.realdirpath("`+slash(real)+`/no/such/leaf")`); e != "Errno::ENOENT" {
		t.Errorf("realdirpath(missing dir): got %q want Errno::ENOENT", e)
	}
}
