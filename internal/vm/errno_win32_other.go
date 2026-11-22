// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

// mapHostErrno is the identity outside Windows: a syscall.Errno already holds
// the POSIX number its Errno class is registered under. See
// errno_win32_windows.go.
func mapHostErrno(eno int64) int64 { return eno }
