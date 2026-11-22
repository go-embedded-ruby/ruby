// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !wasm && !plan9

package vm

import (
	"os"

	"golang.org/x/sys/unix"
)

// pinFile opens a descriptor on whatever path names, only to hold its inode:
// see io_identity.go, which keeps it for a regular file alone. Non-blocking,
// so that a fifo cannot block the open, and O_NOCTTY, so that a terminal never
// becomes the controlling one; read-only first, and write-only for a file whose
// mode refuses reading. nil when neither opens.
func pinFile(path string) *os.File {
	for _, flag := range []int{os.O_RDONLY, os.O_WRONLY} {
		if f, err := os.OpenFile(path, flag|unix.O_NONBLOCK|unix.O_NOCTTY, 0); err == nil {
			return f
		}
	}
	return nil
}

// dupFd is dup(2) with close-on-exec, the seam a test uses to make it fail.
var dupFd = func(fd int) (int, error) { return unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0) }

// dupPin gives a second stream its own descriptor on the pinned inode, so that
// closing one stream leaves the other's pin in place. nil when f is nil, closed,
// or the dup fails, which leaves that stream with an unpinned identity.
func dupPin(f *os.File) *os.File {
	if f == nil {
		return nil
	}
	rc, err := f.SyscallConn()
	if err != nil {
		return nil
	}
	nfd, derr := -1, error(nil)
	if err := rc.Control(func(fd uintptr) { nfd, derr = dupFd(int(fd)) }); err != nil || derr != nil {
		return nil
	}
	return os.NewFile(uintptr(nfd), f.Name())
}
