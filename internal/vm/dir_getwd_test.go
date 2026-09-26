// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// skipIfCwdCannotBeRemoved skips a test that has to construct a process whose
// working directory no longer exists. Windows REFUSES to unlink a directory that
// is any process's current directory, so the state under test cannot be built
// there at all — and MRI takes a different implementation on that platform
// (rb_w32_ugetcwd, dir.c line 140), so there is no witness here for what it would
// report. The 100% coverage gate runs on the POSIX lanes, so this costs no
// coverage.
func skipIfCwdCannotBeRemoved(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a process's current directory cannot be removed on Windows, and MRI uses rb_w32_ugetcwd there")
	}
}

// chdirTo changes the process working directory to dir for the duration of the
// test and restores the original afterwards. The original is captured BEFORE the
// change, because these tests go on to REMOVE the directory they changed into —
// leaving the process there would break every test that runs after them.
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatalf("restoring the working directory: %v", err)
		}
	})
}

// TestDirPwdWorkingDirectoryRemoved is the witness for #681: Dir.pwd used to
// return the path of a working directory that no longer existed, because the
// error from os.Getwd was discarded. MRI reaches util.c ruby_getcwd, which calls
// rb_syserr_fail(errno, "getcwd") instead of returning, so every one of these
// raises. Each expectation was run against MRI Ruby 4.0.5 on this platform
// first; the message text ("- getcwd", with no "@ func" part) is
// rb_syserr_fail's, not rb_sys_fail_path's.
func TestDirPwdWorkingDirectoryRemoved(t *testing.T) {
	skipIfCwdCannotBeRemoved(t)
	// The removed directory must NOT be the one t.TempDir will try to clean up,
	// so make a child of it and remove only that.
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	chdirTo(t, gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	// Errno::ENOENT with rb_syserr_fail's message, for every entry point that
	// reaches ruby_getcwd: Dir.pwd and its alias Dir.getwd (dir.c dir_s_getwd),
	// File.expand_path and File.absolute_path (file.c
	// rb_file_expand_path_internal -> append_fspath(..., ruby_getcwd(), ...)),
	// the same through an explicit relative base, and File.realdirpath (which is
	// rb_check_realpath_emulate, the branch that does call rb_dir_getwd_ospath).
	for _, src := range []string{
		`Dir.pwd`,
		`Dir.getwd`,
		`File.expand_path("x")`,
		`File.absolute_path("x")`,
		`File.expand_path("x", ".")`,
		`File.realdirpath("x")`,
	} {
		err := runErr(t, src)
		if err == nil {
			t.Errorf("%s: no error; MRI 4.0.5 raises Errno::ENOENT", src)
			continue
		}
		if !strings.Contains(err.Error(), "No such file or directory - getcwd") {
			t.Errorf("%s: got %v, want a message containing %q", src, err,
				"No such file or directory - getcwd")
		}
	}
	if got := eval(t, `begin; Dir.pwd; rescue Errno::ENOENT => e; p e.class; end`); got != "Errno::ENOENT\n" {
		t.Errorf("rescue Errno::ENOENT: got %q", got)
	}

	// dir.c chdir_path reads the directory it will restore ONLY under
	// chdir_alone_block_p(), so the BLOCK form raises and the plain form does not.
	// MRI 4.0.5 returns 0 for the plain form out of a removed directory.
	other := filepath.ToSlash(parent)
	if err := runErr(t, fmt.Sprintf(`Dir.chdir(%q) { 1 }`, other)); err == nil ||
		!strings.Contains(err.Error(), "No such file or directory - getcwd") {
		t.Errorf("Dir.chdir(block) from a removed directory: got %v, want ENOENT - getcwd", err)
	}
	// Back in the removed directory for the next two cases: the block form above
	// left the process wherever its failed restore put it.
	if err := os.Chdir(gone); err == nil {
		t.Fatal("the removed directory became enterable again")
	}
}

// TestDirChdirOutOfRemovedWorkingDirectory covers the two siblings of #681 that
// MRI 4.0.5 was measured NOT to fail, so the discarded os.Getwd error in each is
// deliberate. These are GUARDS, not witnesses — they pass against the code from
// before #681 too — and they are kept so a later "fix the other Getwd calls too"
// sweep has to justify itself against MRI rather than against a rule.
func TestDirChdirOutOfRemovedWorkingDirectory(t *testing.T) {
	skipIfCwdCannotBeRemoved(t)
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.ToSlash(parent)

	// Dir.chdir(d) with NO block: dir.c chdir_path goes straight to chdir(2) and
	// never consults getcwd, so it returns 0.
	chdirTo(t, gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, fmt.Sprintf(`p Dir.chdir(%q)`, other)); got != "0\n" {
		t.Errorf("Dir.chdir without a block from a removed directory: got %q, want 0", got)
	}

	// Dir#chdir with a block: dir.c dir_chdir delegates to dir_s_fchdir, which
	// saves the previous directory by OPENING "." rather than by calling getcwd,
	// so the block runs and its value comes back.
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(gone); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if got := eval(t, fmt.Sprintf(`p Dir.new(%q).chdir { 7 }`, other)); got != "7\n" {
		t.Errorf("Dir#chdir with a block from a removed directory: got %q, want 7", got)
	}
}

// TestDirChdirErrnoKeepsTheRealError covers dir.c chdir_path's
// rb_sys_fail_path(path): a failed chdir(2) reports the errno the call actually
// returned, under the C function's own name. Changing into a regular file is
// Errno::ENOTDIR in MRI 4.0.5, where rbgo used to assert Errno::ENOENT for every
// failure; the "@ chdir_path" label is MRI's too (it was "@ dir_chdir" here).
func TestDirChdirErrnoKeepsTheRealError(t *testing.T) {
	// Windows reports its own error for a chdir into a regular file and MRI maps
	// it through rb_w32_* rather than through this errno table, so there is no
	// witness here for what it should say on that platform.
	if runtime.GOOS == "windows" {
		t.Skip("the chdir errno mapping is asserted against MRI on POSIX")
	}
	dir := filepath.ToSlash(t.TempDir())
	reg := dir + "/regular.txt"
	if err := os.WriteFile(filepath.FromSlash(reg), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ src, wantClass, wantMsg string }{
		{fmt.Sprintf(`Dir.chdir(%q)`, reg), "Errno::ENOTDIR", "Not a directory @ chdir_path - " + reg},
		{fmt.Sprintf(`Dir.chdir(%q)`, dir+"/nope"), "Errno::ENOENT", "No such file or directory @ chdir_path - " + dir + "/nope"},
		{fmt.Sprintf(`Dir.new(%q).chdir`, dir), "", ""}, // a real directory succeeds
	}
	for _, c := range cases {
		if c.wantClass == "" {
			continue
		}
		err := runErr(t, c.src)
		if err == nil || !strings.Contains(err.Error(), c.wantMsg) {
			t.Errorf("%s: got %v, want a message containing %q", c.src, err, c.wantMsg)
		}
		got := eval(t, fmt.Sprintf(`begin; %s; rescue SystemCallError => e; p e.class; end`, c.src))
		if got != c.wantClass+"\n" {
			t.Errorf("%s: class %q, want %q", c.src, strings.TrimSpace(got), c.wantClass)
		}
	}
}

// TestDirPwdWorkingDirectoryRecreated is the case a bare error check would have
// missed entirely. On darwin os.Getwd is getattrlist(".", ATTR_CMN_FULLPATH) and
// keeps naming a path that has been removed AND recreated, with no error; the
// name then resolves to a DIFFERENT inode from ".". MRI 4.0.5 raises
// Errno::ENOENT there, because getcwd(3) cannot find "." under that name.
func TestDirPwdWorkingDirectoryRecreated(t *testing.T) {
	skipIfCwdCannotBeRemoved(t)
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	chdirTo(t, gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	err := runErr(t, `Dir.pwd`)
	if err == nil || !strings.Contains(err.Error(), "No such file or directory - getcwd") {
		t.Errorf("Dir.pwd with the working directory recreated: got %v, want ENOENT - getcwd", err)
	}
}

// TestDirPwdIntact is the other direction of the #681 check: an ordinary working
// directory, including one reached through a SYMLINK, still answers. os.Getwd
// names the physical path there, which is the same file as ".", so the
// confirmation cwdStillNamed performs must not reject it. It is a GUARD — it
// passes against the code from before #681 as well — and its job is to fail if
// that confirmation ever starts rejecting a live directory.
func TestDirPwdIntact(t *testing.T) {
	real := t.TempDir()
	// On darwin the per-test temp directory sits under /var, itself a symlink to
	// /private/var, so the PHYSICAL path is what Dir.pwd must report: MRI's
	// getcwd(3) resolves every component. Resolving it here rather than asking
	// os.Getwd keeps the expectation independent of the call under test.
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	chdirTo(t, real)
	want := filepath.ToSlash(resolved)
	if got := eval(t, `print Dir.pwd`); got != want {
		t.Errorf("Dir.pwd = %q, want %q", got, want)
	}
	if got := eval(t, `print File.expand_path("x")`); got != want+"/x" {
		t.Errorf("File.expand_path = %q, want %q", got, want+"/x")
	}

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	chdirTo(t, link)
	if got := eval(t, `print Dir.pwd`); got != want {
		t.Errorf("Dir.pwd through a symlink = %q, want the physical path %q", got, want)
	}
}
