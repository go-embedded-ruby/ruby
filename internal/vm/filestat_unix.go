// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !wasm

package vm

import (
	"io/fs"
	"syscall"

	"golang.org/x/sys/unix"
)

// chrootFn and lutimesFn are the POSIX seams behind Dir.chroot and File.lutime.
// On Unix they map onto the real system calls; the windows/wasm builds supply
// unsupported stubs (both methods are guarded `platform_is_not :windows` in the
// specs). Each is a function variable so a whitebox test can substitute a stub
// and drive the Errno mapping without the real syscall — chroot in particular
// must never be allowed to re-root the test process, so its error branches are
// exercised only through a swapped seam.
var (
	// Dir.chroot(path) -> 0 (dir.c dir_s_chroot, rb_sys_fail_path on -1); a
	// non-root process fails with EPERM.
	chrootFn = syscall.Chroot
	// lutimesFn sets a path's access/modification time WITHOUT following a final
	// symbolic link — utimensat(AT_FDCWD, path, ts, AT_SYMLINK_NOFOLLOW), the
	// no-follow that distinguishes File.lutime from File.utime (file.c
	// utime_internal passes follow=TRUE, which maps to AT_SYMLINK_NOFOLLOW).
	lutimesFn = func(path string, atime, mtime int64) error {
		ts := []unix.Timespec{
			unix.NsecToTimespec(atime * int64(1e9)),
			unix.NsecToTimespec(mtime * int64(1e9)),
		}
		return unix.UtimesNanoAt(unix.AT_FDCWD, path, ts, unix.AT_SYMLINK_NOFOLLOW)
	}
)

// statSys extracts the POSIX stat fields (uid/gid/ino/dev/nlink/blksize) from an
// os.FileInfo's underlying *syscall.Stat_t. These fields are Unix-only: the
// Stat_t struct does not exist on Windows, so the windows build supplies a stub
// that returns zeros instead. The cast is the only platform-specific line — it
// runs identically on Linux and macOS (both populate Sys() with a *Stat_t), and
// the not-ok fallback covers a FileInfo whose Sys() is some other type.
//
// It is held in the sysExtract function variable below so a test can substitute
// a stub and exercise the not-ok branch on any platform.
func statSys(fi fs.FileInfo) statFields {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return statFields{nlink: 1}
	}
	return statFields{
		uid:     int64(st.Uid),
		gid:     int64(st.Gid),
		ino:     int64(st.Ino),
		dev:     int64(st.Dev),
		nlink:   int64(st.Nlink),
		blksize: int64(st.Blksize),
		rdev:    int64(st.Rdev),
		blocks:  int64(st.Blocks),
		hasSys:  true,
	}
}

// lockFdFn, flockFn and closeFdFn are the POSIX seam behind File#flock. A
// buffer-backed stream has no descriptor of its own, but flock(2) is a property
// of an open file description, so one is opened on the file's path and held for
// as long as the lock is. They are function variables so a whitebox test can
// drive the retry and Errno mapping without taking a real lock.
//
// File::LOCK_SH/EX/NB/UN carry the same values on Linux and BSD as the constants
// rbgo defines, so the operation is passed through unchanged.
var (
	lockFdFn  = func(path string) (int, error) { return unix.Open(path, unix.O_RDONLY, 0) }
	flockFn   = func(fd, op int) error { return unix.Flock(fd, op) }
	closeFdFn = func(fd int) error { return unix.Close(fd) }
)

// flockAgainErrs are the errno values flock(2) reports for "another open file
// description holds this lock", which rb_file_flock retries on rather than
// raising. EACCES is in the list because some systems report it in place of
// EWOULDBLOCK.
var flockAgainErrs = []error{syscall.EWOULDBLOCK, syscall.EAGAIN, syscall.EACCES}
