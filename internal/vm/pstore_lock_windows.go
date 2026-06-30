// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

// Windows has no flock(2); the PStore advisory lock is a no-op there (PStore
// still works single-process, and the lock is only advisory). flockSyscall is a
// var so tests can force the lock-failure branch of flockFile.
var flockSyscall = func(fd, how int) error { return nil }

const (
	lockEx = 1
	lockSh = 2
	lockUn = 3
)
