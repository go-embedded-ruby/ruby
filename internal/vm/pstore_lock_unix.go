// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import "syscall"

// On Unix the PStore advisory lock is a real flock(2). flockSyscall is a var so
// tests can force the lock-failure branch of flockFile.
var flockSyscall = syscall.Flock

const (
	lockEx = syscall.LOCK_EX
	lockSh = syscall.LOCK_SH
	lockUn = syscall.LOCK_UN
)
