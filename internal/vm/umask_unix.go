// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import "syscall"

// osUmask sets the process file-mode creation mask to mask and returns the
// previous one — the syscall.Umask primitive File.umask exposes. syscall.Umask
// is Unix-only (Windows has no umask), so the windows build supplies a stub.
// It is held in the setUmask seam below so a test can drive File.umask without
// perturbing the real process mask (which would affect concurrent tests).
func osUmask(mask int) int { return syscall.Umask(mask) }
