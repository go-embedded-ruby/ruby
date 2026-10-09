// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// posixErrno maps a Winsock error to the POSIX errno Ruby reports for it.
//
// Windows socket calls fail with WSAE* codes in the 10000 range, which
// errnoClasses does not name -- so a refused connection arrived as a bare
// SystemCallError where MRI raises Errno::ECONNREFUSED, and `rescue
// Errno::ECONNREFUSED`, the retry idiom this change is about, matched nothing
// on this platform. The windows CI lane found it; darwin and linux were green,
// which is the shape of a divergence that only one lane can see.
//
// MRI does the same translation in rb_w32_map_errno (win32/win32.c), which is
// why a Ruby program written against POSIX errno classes works unchanged there.
//
// The target is syscall's own constant, not a number: on Windows Go defines the
// POSIX errnos above APPLICATION_ERROR rather than at their Unix values, and
// errnoClasses is built from those same constants -- so mapping to the constant
// lands in the table, and errnoStrerror then reports Go's text for it
// ("connection refused", capitalised). Hardcoding 61 would miss on both counts.
//
// The names come from golang.org/x/sys/windows because syscall defines only
// four of them (WSAEACCES, WSAECONNABORTED, WSAECONNRESET, WSAENOPROTOOPT) --
// found by cross-building, not by CI. x/sys is already a direct dependency of
// this package (filestat_unix.go, spawn_native.go use x/sys/unix).
//
// The table is the socket path only: each entry is a WSAE* code whose POSIX twin
// Ruby code actually rescues. A code not listed keeps its own number and reaches
// the SystemCallError arm, which is the honest answer for one with no POSIX
// meaning.
//
// ⚠ The CLASS is what this decides, and the windows lane asserts it. The MESSAGE
// is not measured against a Windows MRI anywhere here -- there is no Windows
// host on this machine -- so no test claims its exact bytes on this platform.
func posixErrno(e syscall.Errno) syscall.Errno {
	if p, ok := winsockToPosix[e]; ok {
		return p
	}
	return e
}

var winsockToPosix = map[syscall.Errno]syscall.Errno{
	windows.WSAECONNREFUSED:  syscall.ECONNREFUSED,
	windows.WSAETIMEDOUT:     syscall.ETIMEDOUT,
	windows.WSAEHOSTUNREACH:  syscall.EHOSTUNREACH,
	windows.WSAENETUNREACH:   syscall.ENETUNREACH,
	windows.WSAENETDOWN:      syscall.ENETDOWN,
	windows.WSAEADDRINUSE:    syscall.EADDRINUSE,
	windows.WSAEADDRNOTAVAIL: syscall.EADDRNOTAVAIL,
	windows.WSAENOTCONN:      syscall.ENOTCONN,
	windows.WSAEISCONN:       syscall.EISCONN,
	windows.WSAEINVAL:        syscall.EINVAL,
	windows.WSAEMFILE:        syscall.EMFILE,
	windows.WSAEINTR:         syscall.EINTR,
	syscall.WSAECONNRESET:    syscall.ECONNRESET,
	syscall.WSAECONNABORTED:  syscall.ECONNABORTED,
	syscall.WSAEACCES:        syscall.EACCES,
}
