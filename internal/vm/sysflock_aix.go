// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build aix

package vm

import (
	"io"

	"golang.org/x/sys/unix"
)

// sysFlock emulates flock(2), which AIX does not have, with a whole-file
// fcntl(2) record lock, exactly as MRI's missing/flock.c (ruby/ruby v3_4_0, the
// HAVE_FCNTL branch) does: LOCK_SH is F_RDLCK, LOCK_EX is F_WRLCK, LOCK_UN is
// F_UNLCK, over [0, EOF) from SEEK_SET; LOCK_NB selects F_SETLK instead of the
// blocking F_SETLKW; any other operation is EINVAL.
//
// The semantics differ from flock(2) the way MRI's do on this platform: a
// record lock belongs to the process, not to the open file description.
func sysFlock(fd, how int) error {
	lk := unix.Flock_t{Whence: io.SeekStart}
	switch how &^ unix.LOCK_NB {
	case unix.LOCK_SH:
		lk.Type = unix.F_RDLCK
	case unix.LOCK_EX:
		lk.Type = unix.F_WRLCK
	case unix.LOCK_UN:
		lk.Type = unix.F_UNLCK
	default:
		return unix.EINVAL
	}
	cmd := unix.F_SETLKW
	if how&unix.LOCK_NB != 0 {
		cmd = unix.F_SETLK
	}
	return unix.FcntlFlock(uintptr(fd), cmd, &lk)
}
