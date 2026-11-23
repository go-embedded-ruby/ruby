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
// The WSA rows are wsaErrnos.
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

// wsaErrnos is the rest of MRI's errmap (win32/win32.c:235-277, v4.0.9): the
// Winsock rows, all 43 of them, in errmap's order. A socket call fails with
// one of these, so without them a refused connection is a bare
// SystemCallError where MRI raises Errno::ECONNREFUSED.
//
// Eight targets (EHOSTDOWN, EPFNOSUPPORT, EPROCLIM, EREMOTE, ESHUTDOWN,
// ESOCKTNOSUPPORT, ETOOMANYREFS, EUSERS) have no number in rbgo on any
// target (errnoUndefinedEverywhere), so no class to land in: mapHostErrno
// keeps the WSA code for them, which raises SystemCallError, until the
// per-platform errno tables define them. TestWSARowsWithoutAClass pins that
// list, so defining one shows up there.
var wsaErrnos = map[syscall.Errno]string{
	windows.WSAEINTR:           "EINTR",
	windows.WSAEBADF:           "EBADF",
	windows.WSAEACCES:          "EACCES",
	windows.WSAEFAULT:          "EFAULT",
	windows.WSAEINVAL:          "EINVAL",
	windows.WSAEMFILE:          "EMFILE",
	windows.WSAEWOULDBLOCK:     "EWOULDBLOCK",
	windows.WSAEINPROGRESS:     "EINPROGRESS",
	windows.WSAEALREADY:        "EALREADY",
	windows.WSAENOTSOCK:        "ENOTSOCK",
	windows.WSAEDESTADDRREQ:    "EDESTADDRREQ",
	windows.WSAEMSGSIZE:        "EMSGSIZE",
	windows.WSAEPROTOTYPE:      "EPROTOTYPE",
	windows.WSAENOPROTOOPT:     "ENOPROTOOPT",
	windows.WSAEPROTONOSUPPORT: "EPROTONOSUPPORT",
	windows.WSAESOCKTNOSUPPORT: "ESOCKTNOSUPPORT",
	windows.WSAEOPNOTSUPP:      "EOPNOTSUPP",
	windows.WSAEPFNOSUPPORT:    "EPFNOSUPPORT",
	windows.WSAEAFNOSUPPORT:    "EAFNOSUPPORT",
	windows.WSAEADDRINUSE:      "EADDRINUSE",
	windows.WSAEADDRNOTAVAIL:   "EADDRNOTAVAIL",
	windows.WSAENETDOWN:        "ENETDOWN",
	windows.WSAENETUNREACH:     "ENETUNREACH",
	windows.WSAENETRESET:       "ENETRESET",
	windows.WSAECONNABORTED:    "ECONNABORTED",
	windows.WSAECONNRESET:      "ECONNRESET",
	windows.WSAENOBUFS:         "ENOBUFS",
	windows.WSAEISCONN:         "EISCONN",
	windows.WSAENOTCONN:        "ENOTCONN",
	windows.WSAESHUTDOWN:       "ESHUTDOWN",
	windows.WSAETOOMANYREFS:    "ETOOMANYREFS",
	windows.WSAETIMEDOUT:       "ETIMEDOUT",
	windows.WSAECONNREFUSED:    "ECONNREFUSED",
	windows.WSAELOOP:           "ELOOP",
	windows.WSAENAMETOOLONG:    "ENAMETOOLONG",
	windows.WSAEHOSTDOWN:       "EHOSTDOWN",
	windows.WSAEHOSTUNREACH:    "EHOSTUNREACH",
	windows.WSAEPROCLIM:        "EPROCLIM",
	windows.WSAENOTEMPTY:       "ENOTEMPTY",
	windows.WSAEUSERS:          "EUSERS",
	windows.WSAEDQUOT:          "EDQUOT",
	windows.WSAESTALE:          "ESTALE",
	windows.WSAEREMOTE:         "EREMOTE",
}

// wsaBaseErr is WSABASEERR: from there up a code absent from errmap is a
// socket error MRI has no class for, or one of the numbers Go invented for the
// POSIX names, and is kept as it is.
const wsaBaseErr = 10000

// mapHostErrno is rb_w32_map_errno: the errno number of the class MRI raises
// for the Win32 code eno. A code in errmap gets its class; a code at or above
// WSABASEERR is kept; any other code is EINVAL. A row whose class rbgo has no
// number for keeps the code too (see wsaErrnos).
func mapHostErrno(eno int64) int64 {
	if eno == 0 {
		return 0
	}
	name, ok := win32Errnos[syscall.Errno(eno)]
	if !ok {
		name, ok = wsaErrnos[syscall.Errno(eno)]
	}
	if ok {
		if n, ok := errnoNumberOf(name); ok {
			return n
		}
		return eno
	}
	if eno >= wsaBaseErr {
		return eno
	}
	return errnoNumbers["EINVAL"]
}

// errnoNumberOf is the number of the Errno class `name` names on this target:
// its own, or the one an alias (EWOULDBLOCK) shares. ok is false for a name
// with no number here.
func errnoNumberOf(name string) (int64, bool) {
	if n, ok := errnoNumbers[name]; ok {
		return n, true
	}
	if a, ok := errnoAliases[name]; ok {
		n, ok := errnoNumbers[a]
		return n, ok
	}
	return 0, false
}
