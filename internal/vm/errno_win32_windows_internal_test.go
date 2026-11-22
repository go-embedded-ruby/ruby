// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

import (
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// TestMapHostErrnoIsRbW32MapErrno: each row is what MRI's rb_w32_map_errno
// (win32/win32.c, v4.0.9) gives for the code. ERROR_PATH_NOT_FOUND is the row
// the Windows runner showed: read as a POSIX number, 3 raised Errno::ENOTDIR
// where MRI raises Errno::ENOENT.
func TestMapHostErrnoIsRbW32MapErrno(t *testing.T) {
	cases := []struct {
		eno  syscall.Errno
		want string
	}{
		{windows.ERROR_PATH_NOT_FOUND, "ENOENT"},
		{windows.ERROR_FILE_NOT_FOUND, "ENOENT"},
		{windows.ERROR_ACCESS_DENIED, "EACCES"},
		// errmap's first row for it; the second says EINVAL.
		{windows.ERROR_INVALID_HANDLE, "EBADF"},
		{windows.ERROR_DIRECTORY, "ENOTDIR"},
		{windows.ERROR_DISK_FULL, "ENOSPC"},
		// ERROR_PIPE_LOCAL, which win32.c defines itself.
		{229, "EPIPE"},
		// Absent from errmap, and below WSABASEERR.
		{windows.ERROR_INVALID_NAME, "EINVAL"},
		// A number Go invented for a POSIX name: kept.
		{syscall.ENOENT, "ENOENT"},
	}
	for _, c := range cases {
		if got := mapHostErrno(int64(c.eno)); got != errnoNumbers[c.want] {
			t.Errorf("mapHostErrno(%d) = %d (%s), want %s", c.eno, got, errnoClasses[got], c.want)
		}
	}
	if got := mapHostErrno(0); got != 0 {
		t.Errorf("mapHostErrno(0) = %d, want 0", got)
	}
	if got := mapHostErrno(int64(windows.WSAECONNRESET)); got != int64(windows.WSAECONNRESET) {
		t.Errorf("a WSA code was changed to %d", got)
	}
}
