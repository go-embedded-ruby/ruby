// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFileMetaOps covers File.chmod/chown/lchown/utime/umask and the access
// predicates against a real temp file. The mutating ops are exercised through
// their os seams so the error branches are reachable on every platform; the
// happy paths run against a real file (chmod/chown may be permission-limited, so
// only their return value — the affected-count — is asserted, not the on-disk
// effect, which differs across OSes).
func TestFileMetaOps(t *testing.T) {
	defer restoreFileSeams()
	dir := slash(t.TempDir())
	f := dir + "/m.txt"
	if err := os.WriteFile(filepath.FromSlash(f), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ src, want string }{
		// chmod returns the number of paths; multiple paths sum.
		{`p File.chmod(0o600, "` + f + `")`, "1\n"},
		// utime returns the count; accepts a Time and an Integer second count.
		{`p File.utime(Time.now, Time.now, "` + f + `")`, "1\n"},
		{`p File.utime(0, 0, "` + f + `")`, "1\n"},
		// chown with nil ids leaves them unchanged and still reports the count.
		{`p File.chown(nil, nil, "` + f + `")`, "1\n"},
		{`p File.lchown(nil, nil, "` + f + `")`, "1\n"},
		// umask with no arg reads without changing; with an arg returns the previous.
		{`m = File.umask; p m.is_a?(Integer)`, "true\n"},
		{`old = File.umask; n = File.umask(old); p(n == old)`, "true\n"},
		// access predicates over a readable file.
		{`p [File.readable?("` + f + `"), File.writable?("` + f + `")]`, "[true, true]\n"},
		// access predicate on a missing path is false (not a raise).
		{`p [File.readable?("/no/x"), File.executable?("/no/x"), File.executable_real?("/no/x")]`,
			"[false, false, false]\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFileMetaOpsErrors drives the chmod/chown/lchown/utime error branches via
// failing os seams, so each ENOENT mapping is exercised deterministically (a
// real chmod is a no-op on Windows, so the seam is the only portable way).
func TestFileMetaOpsErrors(t *testing.T) {
	defer restoreFileSeams()
	failMode := func(string, os.FileMode) error { return os.ErrNotExist }
	failOwn := func(string, int, int) error { return os.ErrNotExist }
	failTimes := func(string, int64, int64) error { return os.ErrNotExist }

	fileChmod = failMode
	fileChown = failOwn
	fileLchown = failOwn
	fileChtimes = failTimes

	for _, c := range []struct{ src string }{
		{`File.chmod(0o600, "x")`},
		{`File.chown(0, 0, "x")`},
		{`File.lchown(0, 0, "x")`},
		{`File.utime(0, 0, "x")`},
	} {
		if got := runFSErr(t, c.src); got != "Errno::ENOENT" {
			t.Errorf("%s: got %q want Errno::ENOENT", c.src, got)
		}
	}
}

// TestFileInstanceMetaErrors drives the File instance chmod/chown error branches
// (Puppet's replace_file calls tempfile.chmod/chown) via failing os seams, so the
// ENOENT mapping is exercised deterministically and portably (a real chmod is a
// no-op on Windows).
func TestFileInstanceMetaErrors(t *testing.T) {
	defer restoreFileSeams()
	dir := slash(t.TempDir())
	f := dir + "/m.txt"
	fileChmod = func(string, os.FileMode) error { return os.ErrNotExist }
	fileChown = func(string, int, int) error { return os.ErrNotExist }

	for _, src := range []string{
		`f = File.open("` + f + `", "w"); f.chmod(0o600)`,
		`f = File.open("` + f + `", "w"); f.chown(0, 0)`,
	} {
		if got := runFSErr(t, src); got != "Errno::ENOENT" {
			t.Errorf("%s: got %q want Errno::ENOENT", src, got)
		}
	}
}

// Captured at init so restoreFileSeams resets to the PLATFORM defaults rather
// than hardcoding os.Chown — on Windows fileChown/fileLchown default to no-ops
// (os.Chown always fails there), and hardcoding os.Chown would clobber that and
// break later real chown calls (e.g. TestFileRenameAndInstanceMeta).
var (
	origFileChmod   = fileChmod
	origFileChown   = fileChown
	origFileLchown  = fileLchown
	origFileChtimes = fileChtimes
	origSetUmask    = setUmask
)

// restoreFileSeams resets the os seams the File-ops tests override.
func restoreFileSeams() {
	fileChmod = origFileChmod
	fileChown = origFileChown
	fileLchown = origFileLchown
	fileChtimes = origFileChtimes
	setUmask = origSetUmask
}

// TestFileUtilsChmod covers FileUtils.chmod (success + the failing-seam ENOENT
// branch) — Puppet's file_impl#chmod drives it.
func TestFileUtilsChmod(t *testing.T) {
	defer restoreFileSeams()
	dir := t.TempDir()
	f := filepath.Join(dir, "c.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	vm := New(nil)
	if got := runFSWith(t, vm, `p FileUtils.chmod(0o600, "`+slash(f)+`")`); got != "\""+slash(f)+"\"\n" {
		t.Errorf("FileUtils.chmod: got %q", got)
	}
	// Failing seam → Errno::ENOENT.
	fileChmod = func(string, os.FileMode) error { return os.ErrNotExist }
	if got := runFSErr(t, `FileUtils.chmod(0o600, "missing")`); got != "Errno::ENOENT" {
		t.Errorf("FileUtils.chmod fail: got %q", got)
	}
}

// runFSWith runs src on the given VM, returning captured stdout. (Unlike runFS
// it does not allocate a fresh VM, so a test can pre-set seams that the VM's
// construction does not reset.)
func runFSWith(t *testing.T, _ *VM, src string) string {
	t.Helper()
	return runFS(t, src)
}

// TestFileOpenMaterialise covers that File.open in a write mode creates the
// (empty) file on disk immediately, so a subsequent File.stat sees it before any
// flush — the MRI behaviour Tempfile relies on.
func TestFileOpenMaterialise(t *testing.T) {
	dir := slash(t.TempDir())
	p := dir + "/w.txt"
	src := `f = File.open("` + p + `", "w"); ex = File.exist?("` + p + `"); f.write("hi"); f.close
            p [ex, File.read("` + p + `")]`
	if got := runFS(t, src); got != "[true, \"hi\"]\n" {
		t.Errorf("File.open w materialise: got %q", got)
	}
}

// TestChompSeparator covers String#chomp / chomp! with the separator argument:
// nil/absent (line ending), "" (paragraph: strip all trailing newlines), and an
// explicit suffix (removed once, or left when absent). Asserted vs MRI 4.0.5.
func TestChompSeparator(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p "hi\n".chomp`, "\"hi\"\n"},
		{`p "hi\r\n".chomp`, "\"hi\"\n"},
		{`p "a &&\nb &&\n".chomp(" &&\n")`, "\"a &&\\nb\"\n"},
		{`p "x,y,".chomp(",")`, "\"x,y\"\n"},
		{`p "hello".chomp("lo")`, "\"hel\"\n"},
		{`p "hellox".chomp("lo")`, "\"hellox\"\n"}, // no match: unchanged
		{`p "hi\n\n\n".chomp("")`, "\"hi\"\n"},     // paragraph mode
		{`p "hi\r\n\r\n".chomp("")`, "\"hi\"\n"},   // paragraph mode, CRLF
		{`p "hi".chomp("")`, "\"hi\"\n"},           // paragraph mode, nothing to strip
		{`p "hi".chomp(nil)`, "\"hi\"\n"},          // explicit nil == default
		// chomp! mutates and returns self, or nil when nothing changed.
		{`s="a &&\n".dup; r=s.chomp!(" &&\n"); p [s, r.equal?(s)]`, "[\"a\", true]\n"},
		{`s="abc".dup; p s.chomp!(",")`, "nil\n"},
		{`s="x\n\n".dup; s.chomp!(""); p s`, "\"x\"\n"},
	}
	for _, c := range cases {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
