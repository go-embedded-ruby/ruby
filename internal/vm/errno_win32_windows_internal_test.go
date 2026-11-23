// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

import (
	"os"
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
		// The Winsock rows (go-embedded-ruby/ruby#980).
		{windows.WSAECONNRESET, "ECONNRESET"},
		{windows.WSAECONNREFUSED, "ECONNREFUSED"},
		{windows.WSAENOTSOCK, "ENOTSOCK"},
		{windows.WSAEMSGSIZE, "EMSGSIZE"},
		{windows.WSAENOTEMPTY, "ENOTEMPTY"},
		// EWOULDBLOCK is a second name for EAGAIN's class in rbgo.
		{windows.WSAEWOULDBLOCK, "EAGAIN"},
	}
	for _, c := range cases {
		if got := mapHostErrno(int64(c.eno)); got != errnoNumbers[c.want] {
			t.Errorf("mapHostErrno(%d) = %d (%s), want %s", c.eno, got, errnoClasses[got], c.want)
		}
	}
	if got := mapHostErrno(0); got != 0 {
		t.Errorf("mapHostErrno(0) = %d, want 0", got)
	}
	// A WSA code absent from errmap is kept, as rb_w32_map_errno keeps it.
	if got := mapHostErrno(int64(windows.WSAEDISCON)); got != int64(windows.WSAEDISCON) {
		t.Errorf("WSAEDISCON was changed to %d", got)
	}
}

// TestWSARowsWithoutAClass pins the errmap rows whose class rbgo has no number
// for: mapHostErrno keeps their WSA code. Defining one of those names on
// Windows moves it out of this list, and the test says which.
func TestWSARowsWithoutAClass(t *testing.T) {
	want := map[string]bool{
		"EHOSTDOWN": true, "EPFNOSUPPORT": true, "EPROCLIM": true, "EREMOTE": true,
		"ESHUTDOWN": true, "ESOCKTNOSUPPORT": true, "ETOOMANYREFS": true, "EUSERS": true,
	}
	if len(wsaErrnos) != 43 {
		t.Errorf("wsaErrnos has %d rows; errmap has 43 Winsock rows", len(wsaErrnos))
	}
	for code, name := range wsaErrnos {
		got := mapHostErrno(int64(code))
		_, named := errnoNumberOf(name)
		switch {
		case want[name] && named:
			t.Errorf("%s now has a number; drop it from this list", name)
		case want[name] && got != int64(code):
			t.Errorf("%d (%s, no class) was changed to %d", code, name, got)
		case !want[name] && !named:
			t.Errorf("%s has no number on Windows; WSA code %d keeps its own", name, code)
		case !want[name] && got == int64(code):
			t.Errorf("%d (%s) was not mapped", code, name)
		}
	}
}

// TestErrnoOfErrorMapsTheWin32Code: the mapping is applied where the number
// is dug out, so every raiser sees MRI's class, not the raw code.
func TestErrnoOfErrorMapsTheWin32Code(t *testing.T) {
	err := &os.PathError{Op: "open", Path: `C:\nope\x`, Err: windows.ERROR_PATH_NOT_FOUND}
	if got, ok := errnoOfError(err); !ok || got != errnoNumbers["ENOENT"] {
		t.Errorf("errnoOfError = %d, %v; want ENOENT (%d)", got, ok, errnoNumbers["ENOENT"])
	}
	dir := &os.PathError{Op: "remove", Path: `C:\d`, Err: windows.ERROR_DIR_NOT_EMPTY}
	if !errIsErrno(dir, "ENOTEMPTY") {
		t.Errorf("ERROR_DIR_NOT_EMPTY is not ENOTEMPTY")
	}
	if errIsErrno(dir, "ENOENT") {
		t.Errorf("ERROR_DIR_NOT_EMPTY is ENOENT")
	}
}
