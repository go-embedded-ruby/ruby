// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io/fs"
	"os"
)

// chmodBits are the fs.FileMode bits os.Chmod sets: the nine permission bits
// and the three special ones, which Go keeps far above them (fs.ModeSetuid is
// not 0o4000).
const chmodBits = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// chmodMode turns a POSIX mode, as Ruby passes it to File.chmod (0o4755,
// 0o1777…), into the fs.FileMode os.Chmod reads (#972). Passing the integer
// straight through, as `os.FileMode(m & 0o7777)` did, keeps the nine
// permission bits and silently drops setuid, setgid and sticky: Go reads
// 0o1000 as nothing at all.
func chmodMode(m int64) fs.FileMode {
	fm := fs.FileMode(m & 0o777)
	if m&0o4000 != 0 {
		fm |= fs.ModeSetuid
	}
	if m&0o2000 != 0 {
		fm |= fs.ModeSetgid
	}
	if m&0o1000 != 0 {
		fm |= fs.ModeSticky
	}
	return fm
}

// withOwnerWrite runs write with the owner-write bit of path put back for its
// length, for the reason ioFlush gives: a stream that already holds the
// descriptor's grant must not lose it because this model reopens by path. It
// returns refused when the mode cannot be read, and otherwise what write
// returns; when the mode cannot be changed (a file this process does not own),
// write is refused for permission again, so no separate branch is needed.
//
// Afterwards only the owner-write bit goes back to what it was (#972). The
// rest is read from the file as the write left it: MRI 4.0.7 measured on
// darwin keeps sticky through a write and loses setuid and setgid, which is
// the kernel clearing them on a write, and restoring the mode read BEFORE the
// write would put a setuid bit back on content that was just changed.
func withOwnerWrite(path string, refused error, write func() error) error {
	st, err := os.Stat(path)
	if err != nil {
		return refused
	}
	ownerWrite := st.Mode() & 0o200
	_ = os.Chmod(path, st.Mode()&chmodBits|0o200)
	werr := write()
	if after, err := os.Stat(path); err == nil {
		_ = os.Chmod(path, after.Mode()&chmodBits&^0o200|ownerWrite)
	}
	return werr
}
