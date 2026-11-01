// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build plan9

package vm

import (
	"errors"
	"io/fs"
	"os"
	"sync"
	"syscall"
	"time"
)

// This is the GOOS=plan9 counterpart of filestat_unix.go. Plan 9 has no
// syscall.Stat_t: os.Stat fills os.FileInfo.Sys() with a *syscall.Dir
// (src/os/stat_plan9.go), which carries a server type and subtype, a Qid and the
// owner names as STRINGS.

// chrootFn is unsupported: Plan 9 has no chroot(2) (it has per-process namespaces,
// which Dir.chroot does not model), so the call reports the operation unsupported.
var chrootFn = func(string) error { return errors.ErrUnsupported }

// lutimesFn sets a file's access and modification times. Plan 9 has no symbolic
// links, so "without following a final link" is the same operation as
// utime: os.Chtimes (a wstat of the Dir's Atime and Mtime).
var lutimesFn = func(path string, atime, mtime int64) error {
	return os.Chtimes(path, time.Unix(atime, 0), time.Unix(mtime, 0))
}

// statSys extracts the stat fields from the *syscall.Dir os.FileInfo carries; see
// plan9StatFields for the mapping. A FileInfo whose Sys() is some other type gets
// the defaults.
func statSys(fi fs.FileInfo) statFields {
	d, ok := fi.Sys().(*syscall.Dir)
	if !ok {
		return statFields{nlink: 1}
	}
	return plan9StatFields(d.Qid.Path, d.Type, d.Dev)
}

// lockFdFn, flockFn and closeFdFn are the File#flock seam. Plan 9 has no flock(2)
// (its locking is the exclusive-use bit of a file's mode, a different model), so
// File#flock raises NotImplementedError, as MRI does where the call is missing
// (rb_f_notimplement). The lock descriptor is requested first, so the error is
// raised before anything is opened.
var (
	lockFdFn = func(string) (int, error) {
		notImplemented("flock")
		return 0, nil
	}
	flockFn   = func(int, int) error { return errors.ErrUnsupported }
	closeFdFn = func(int) error { return nil }
)

// flockAgainErrs is empty: with no flock(2) there is no "would block" to retry on.
var flockAgainErrs []error

// plan9User is the current user's name, read once from /dev/user (the file Go's own
// os/user reads on plan9, src/os/user/lookup_plan9.go). A read failure leaves it
// empty, which plan9Owner never matches.
var plan9User = sync.OnceValue(func() string {
	b, err := os.ReadFile("/dev/user")
	if err != nil {
		return ""
	}
	return string(b)
})

// statOwned / statGrpowned compare the Dir's owner and group NAMES with the
// current user: Plan 9 has no numeric ids to compare, and a user is also the group
// of the same name. statPerm is the mode's permission bits, unaltered.
func statOwned(s *FileStat) bool {
	d, ok := s.fi.Sys().(*syscall.Dir)
	return ok && plan9Owner(d.Uid, plan9User())
}

func statGrpowned(s *FileStat) bool {
	d, ok := s.fi.Sys().(*syscall.Dir)
	return ok && plan9Owner(d.Gid, plan9User())
}

func statPerm(fi fs.FileInfo) int64 { return int64(fi.Mode().Perm()) }
