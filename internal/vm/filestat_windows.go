// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

import "io/fs"

// statSys is the Windows counterpart of the Unix syscall.Stat_t extraction.
// Windows has no Stat_t (no uid/gid/inode model), so the POSIX-only fields take
// sensible defaults: uid/gid 0, a single link, and a 0 inode/device. The Ruby
// File::Stat object still answers uid/gid/ino/dev/nlink/blksize without raising,
// matching what MRI returns for these fields on Windows (uid/gid 0).
func statSys(fi fs.FileInfo) statFields {
	_ = fi
	return statFields{nlink: 1}
}

// statOwned / statGrpowned / statPerm are the Windows counterparts of the POSIX
// ownership/permission seam (see filestat_posix.go), matching what MRI 4.0.x
// returns on Windows (verified against ruby 4.0.6 aarch64-mingw-ucrt):
//
//   - owned?    => true. MRI's stat reports st_uid 0 and geteuid() is 0, so
//     `st_uid == geteuid()` holds for every file the process can stat.
//   - grpowned? => false. MRI reports st_gid 0 but its group check does not
//     match, so File.grpowned? is false on Windows.
//   - world permission bits come from the MSVCRT stat, which never sets the
//     group/other WRITE bits (a normal file is 0644, a read-only one 0444).
//     Masking off 0o022 reproduces that from Go's mode (Go reports 0666/0444),
//     so File.world_writable? is nil on Windows while world_readable? is the
//     0644/0444 integer — exactly MRI's behavior.
func statOwned(*FileStat) bool      { return true }
func statGrpowned(*FileStat) bool   { return false }
func statPerm(fi fs.FileInfo) int64 { return int64(fi.Mode().Perm()) &^ 0o022 }
