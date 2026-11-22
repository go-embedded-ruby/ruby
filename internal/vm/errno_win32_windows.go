// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// win32Errnos is MRI's errmap (win32/win32.c, v4.0.9), the Win32 rows: the
// errno class rb_w32_map_errno gives each Win32 error code. On Windows a
// syscall.Errno carries the Win32 code itself, where errnoNumbers holds the
// numbers Go invented for the POSIX names, so a code looked up there directly
// lands on an unrelated class (ERROR_PATH_NOT_FOUND, 3, read as ENOTDIR).
//
// ERROR_INVALID_HANDLE appears twice in errmap, as EBADF and then EINVAL; the
// first row is the one rb_w32_map_errno's loop returns, so it alone is here.
// The WSA rows are left out: their codes are at or above WSABASEERR, which
// mapHostErrno keeps as they are, as rb_w32_map_errno does for a WSA code
// absent from the table.
var win32Errnos = map[syscall.Errno]string{
	windows.ERROR_INVALID_FUNCTION:          "EINVAL",
	windows.ERROR_FILE_NOT_FOUND:            "ENOENT",
	windows.ERROR_PATH_NOT_FOUND:            "ENOENT",
	windows.ERROR_TOO_MANY_OPEN_FILES:       "EMFILE",
	windows.ERROR_ACCESS_DENIED:             "EACCES",
	windows.ERROR_INVALID_HANDLE:            "EBADF",
	windows.ERROR_ARENA_TRASHED:             "ENOMEM",
	windows.ERROR_NOT_ENOUGH_MEMORY:         "ENOMEM",
	windows.ERROR_INVALID_BLOCK:             "ENOMEM",
	windows.ERROR_BAD_ENVIRONMENT:           "E2BIG",
	windows.ERROR_BAD_FORMAT:                "ENOEXEC",
	windows.ERROR_INVALID_ACCESS:            "EINVAL",
	windows.ERROR_INVALID_DATA:              "EINVAL",
	windows.ERROR_INVALID_DRIVE:             "ENOENT",
	windows.ERROR_CURRENT_DIRECTORY:         "EACCES",
	windows.ERROR_NOT_SAME_DEVICE:           "EXDEV",
	windows.ERROR_NO_MORE_FILES:             "ENOENT",
	windows.ERROR_WRITE_PROTECT:             "EROFS",
	windows.ERROR_BAD_UNIT:                  "ENODEV",
	windows.ERROR_NOT_READY:                 "ENXIO",
	windows.ERROR_BAD_COMMAND:               "EACCES",
	windows.ERROR_CRC:                       "EACCES",
	windows.ERROR_BAD_LENGTH:                "EACCES",
	windows.ERROR_SEEK:                      "EIO",
	windows.ERROR_NOT_DOS_DISK:              "EACCES",
	windows.ERROR_SECTOR_NOT_FOUND:          "EACCES",
	windows.ERROR_OUT_OF_PAPER:              "EACCES",
	windows.ERROR_WRITE_FAULT:               "EIO",
	windows.ERROR_READ_FAULT:                "EIO",
	windows.ERROR_GEN_FAILURE:               "EACCES",
	windows.ERROR_LOCK_VIOLATION:            "EACCES",
	windows.ERROR_SHARING_VIOLATION:         "EACCES",
	windows.ERROR_WRONG_DISK:                "EACCES",
	windows.ERROR_SHARING_BUFFER_EXCEEDED:   "EACCES",
	windows.ERROR_BAD_NETPATH:               "ENOENT",
	windows.ERROR_NETWORK_ACCESS_DENIED:     "EACCES",
	windows.ERROR_BAD_NET_NAME:              "ENOENT",
	windows.ERROR_FILE_EXISTS:               "EEXIST",
	windows.ERROR_CANNOT_MAKE:               "EACCES",
	windows.ERROR_FAIL_I24:                  "EACCES",
	windows.ERROR_INVALID_PARAMETER:         "EINVAL",
	windows.ERROR_NO_PROC_SLOTS:             "EAGAIN",
	windows.ERROR_DRIVE_LOCKED:              "EACCES",
	windows.ERROR_BROKEN_PIPE:               "EPIPE",
	windows.ERROR_DISK_FULL:                 "ENOSPC",
	windows.ERROR_INVALID_TARGET_HANDLE:     "EBADF",
	windows.ERROR_WAIT_NO_CHILDREN:          "ECHILD",
	windows.ERROR_CHILD_NOT_COMPLETE:        "ECHILD",
	windows.ERROR_DIRECT_ACCESS_HANDLE:      "EBADF",
	windows.ERROR_NEGATIVE_SEEK:             "EINVAL",
	windows.ERROR_SEEK_ON_DEVICE:            "EACCES",
	windows.ERROR_DIR_NOT_EMPTY:             "ENOTEMPTY",
	windows.ERROR_DIRECTORY:                 "ENOTDIR",
	windows.ERROR_NOT_LOCKED:                "EACCES",
	windows.ERROR_BAD_PATHNAME:              "ENOENT",
	windows.ERROR_MAX_THRDS_REACHED:         "EAGAIN",
	windows.ERROR_LOCK_FAILED:               "EACCES",
	windows.ERROR_ALREADY_EXISTS:            "EEXIST",
	windows.ERROR_INVALID_STARTING_CODESEG:  "ENOEXEC",
	windows.ERROR_INVALID_STACKSEG:          "ENOEXEC",
	windows.ERROR_INVALID_MODULETYPE:        "ENOEXEC",
	windows.ERROR_INVALID_EXE_SIGNATURE:     "ENOEXEC",
	windows.ERROR_EXE_MARKED_INVALID:        "ENOEXEC",
	windows.ERROR_BAD_EXE_FORMAT:            "ENOEXEC",
	windows.ERROR_ITERATED_DATA_EXCEEDS_64k: "ENOEXEC",
	windows.ERROR_INVALID_MINALLOCSIZE:      "ENOEXEC",
	windows.ERROR_DYNLINK_FROM_INVALID_RING: "ENOEXEC",
	windows.ERROR_IOPL_NOT_ENABLED:          "ENOEXEC",
	windows.ERROR_INVALID_SEGDPL:            "ENOEXEC",
	windows.ERROR_AUTODATASEG_EXCEEDS_64k:   "ENOEXEC",
	windows.ERROR_RING2SEG_MUST_BE_MOVABLE:  "ENOEXEC",
	windows.ERROR_RELOC_CHAIN_XEEDS_SEGLIM:  "ENOEXEC",
	windows.ERROR_INFLOOP_IN_RELOC_CHAIN:    "ENOEXEC",
	windows.ERROR_FILENAME_EXCED_RANGE:      "ENOENT",
	windows.ERROR_NESTING_NOT_ALLOWED:       "EAGAIN",
	229:                                     "EPIPE", // ERROR_PIPE_LOCAL, which win32.c defines itself
	windows.ERROR_BAD_PIPE:                  "EPIPE",
	windows.ERROR_PIPE_BUSY:                 "EAGAIN",
	windows.ERROR_NO_DATA:                   "EPIPE",
	windows.ERROR_PIPE_NOT_CONNECTED:        "EPIPE",
	windows.ERROR_OPERATION_ABORTED:         "EINTR",
	windows.ERROR_NOT_ENOUGH_QUOTA:          "ENOMEM",
	windows.ERROR_MOD_NOT_FOUND:             "ENOENT",
	windows.ERROR_PRIVILEGE_NOT_HELD:        "EACCES",
	windows.ERROR_CANT_RESOLVE_FILENAME:     "ELOOP",
}

// wsaBaseErr is WSABASEERR: from there up a code is a socket error, or one of
// the numbers Go invented for the POSIX names, and is kept as it is.
const wsaBaseErr = 10000

// mapHostErrno is rb_w32_map_errno: the errno number of the class MRI raises
// for the Win32 code eno. A code in errmap gets its class; a code at or above
// WSABASEERR is kept; any other code is EINVAL.
func mapHostErrno(eno int64) int64 {
	if eno == 0 || eno >= wsaBaseErr {
		return eno
	}
	if name, ok := win32Errnos[syscall.Errno(eno)]; ok {
		return errnoNumbers[name]
	}
	return errnoNumbers["EINVAL"]
}
