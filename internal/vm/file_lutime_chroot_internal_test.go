// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"testing"
)

// origChrootFn / origLutimesFn capture the platform seams so each test can
// restore them after swapping in a deterministic stub (the real chroot seam must
// never actually run in a test — it would re-root the test process).
var (
	origChrootFn  = chrootFn
	origLutimesFn = lutimesFn
)

func restoreFDSeams() {
	chrootFn = origChrootFn
	lutimesFn = origLutimesFn
}

// TestFileLutime covers File.lutime: the real utimensat(AT_SYMLINK_NOFOLLOW)
// seam on POSIX (setting a regular file's time and returning the path count),
// the nil-means-now branch, the arity guard, and the ENOENT error branch via a
// failing seam. Verified against MRI Ruby 4.0.5 (File.lutime returns the number
// of file names; file.c utime_internal with follow=TRUE -> AT_SYMLINK_NOFOLLOW).
func TestFileLutime(t *testing.T) {
	defer restoreFDSeams()

	if runtime.GOOS != "windows" {
		dir := slash(t.TempDir())
		f := dir + "/f"
		if err := os.WriteFile(dir+"/f", []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Real call exercises the lutimesFn body: returns the path count.
		if got := runFS(t, `p File.lutime(Time.at(1000000000), Time.at(1000000001), "`+f+`")`); got != "1\n" {
			t.Errorf("lutime real: got %q want 1", got)
		}
		// mtime was set to the second argument.
		if got := runFS(t, `p (File.mtime("`+f+`").to_i == 1000000001)`); got != "true\n" {
			t.Errorf("lutime mtime: got %q want true", got)
		}
		// nil atime/mtime means the current time (timeArgUnixOrNow nil branch).
		if got := runFS(t, `File.lutime(nil, nil, "`+f+`"); p ((Time.now.to_i - File.mtime("`+f+`").to_i).abs < 5)`); got != "true\n" {
			t.Errorf("lutime nil: got %q want true", got)
		}
	}

	// Arity guard: fewer than two arguments is an ArgumentError.
	if got := runFSErr(t, `File.lutime(0)`); got != "ArgumentError" {
		t.Errorf("lutime arity: got %q want ArgumentError", got)
	}

	// Error branch: a failing seam maps to Errno::ENOENT.
	lutimesFn = func(string, int64, int64) error { return os.ErrNotExist }
	if got := runFSErr(t, `File.lutime(0, 0, "x")`); got != "Errno::ENOENT" {
		t.Errorf("lutime err: got %q want Errno::ENOENT", got)
	}
}

// TestDirChroot covers Dir.chroot: the success return (0) and each
// raiseChrootErr errno branch, all through the chrootFn seam so the real chroot
// (which would re-root the test process) never runs. Verified against MRI Ruby
// 4.0.5 (Dir.chroot returns 0; a regular user gets Errno::EPERM; dir.c
// dir_s_chroot, rb_sys_fail_path).
func TestDirChroot(t *testing.T) {
	defer restoreFDSeams()

	// Success: seam returns nil, Dir.chroot returns 0.
	chrootFn = func(string) error { return nil }
	if got := runFS(t, `p Dir.chroot(".")`); got != "0\n" {
		t.Errorf("chroot success: got %q want 0", got)
	}

	// Arity guard.
	if got := runFSErr(t, `Dir.chroot(".", ".")`); got != "ArgumentError" {
		t.Errorf("chroot arity: got %q want ArgumentError", got)
	}

	// raiseChrootErr branches.
	for _, c := range []struct {
		err  error
		want string
	}{
		{syscall.EPERM, "Errno::EPERM"},
		{syscall.ENOENT, "Errno::ENOENT"},
		{syscall.ENOTDIR, "Errno::ENOTDIR"},
		{errors.New("other"), "Errno::EPERM"},
	} {
		chrootFn = func(string) error { return c.err }
		if got := runFSErr(t, `Dir.chroot("x")`); got != c.want {
			t.Errorf("chroot err %v: got %q want %q", c.err, got, c.want)
		}
	}
}
