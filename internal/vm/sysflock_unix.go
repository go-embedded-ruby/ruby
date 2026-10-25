// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !wasm && !aix

package vm

import "golang.org/x/sys/unix"

// sysFlock is flock(2), the lock behind File#flock and PStore's transaction
// lock. AIX has no flock(2); sysflock_aix.go emulates it with fcntl(2) locks,
// as MRI's missing/flock.c does there.
var sysFlock = unix.Flock
