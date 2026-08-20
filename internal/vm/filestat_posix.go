// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import "io/fs"

// statOwned / statGrpowned / statPerm are the platform seam behind the
// ownership and world-permission File predicates. On POSIX (Unix and wasip1)
// they read the real stat identity and permission bits; the windows build
// supplies MSVCRT-faithful counterparts (see filestat_windows.go), because
// Windows has no uid/gid model and its stat reports a fixed 0644/0444 mode.
//
// owned? is true when the file's uid is the process's effective uid;
// grpowned? when its gid is the effective gid or a supplementary group. Both
// need real POSIX ids (hasSys), so a FileInfo without a *Stat_t reports false.
func statOwned(s *FileStat) bool    { return s.sys.hasSys && int64(statEuid()) == s.sys.uid }
func statGrpowned(s *FileStat) bool { return s.sys.hasSys && inGroupFor(s.sys.gid, statEgid()) }

// statPerm returns the permission bits world_readable?/world_writable? test.
// On POSIX these are the file's real mode bits, unaltered.
func statPerm(fi fs.FileInfo) int64 { return int64(fi.Mode().Perm()) }
