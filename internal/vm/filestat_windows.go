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
