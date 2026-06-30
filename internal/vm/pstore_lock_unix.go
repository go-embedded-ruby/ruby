// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import (
	"os"
	"syscall"
)

// flockSyscall is syscall.Flock as a var so a test can force the lock-failure
// branch of flockFile (a valid fd never fails it in practice).
var flockSyscall = syscall.Flock

// flockFile opens the store file O_CREAT|O_RDWR and takes a real advisory flock(2)
// on it (LOCK_SH for a read-only transaction, LOCK_EX otherwise), returning a
// closure that releases the lock and closes the fd. The lock is held for the whole
// transaction so concurrent transactions serialise, as MRI's does. The Backend
// reads / writes the path on its own fd; this one only carries the lock.
func flockFile(path string, readOnly bool) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	how := syscall.LOCK_EX
	if readOnly {
		how = syscall.LOCK_SH
	}
	if err := flockSyscall(int(f.Fd()), how); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		flockSyscall(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
