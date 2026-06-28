// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import (
	"io/fs"
	"syscall"
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
		hasSys:  true,
	}
}
