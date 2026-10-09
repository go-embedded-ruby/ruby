// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import "syscall"

// posixErrno is the identity everywhere but Windows: a POSIX platform's socket
// calls already fail with POSIX errno numbers, so there is nothing to
// translate. See errno_winsock_windows.go for what the Windows half does and
// why the split exists at all.
func posixErrno(e syscall.Errno) syscall.Errno { return e }
