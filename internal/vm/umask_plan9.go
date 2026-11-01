// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build plan9

package vm

// osUmask is the Plan 9 counterpart of the Unix syscall.Umask: Plan 9 has no umask
// (syscall.Umask is undefined there), so File.umask is a no-op that reports a
// conventional 0o022 mask without changing any process state, exactly as on
// Windows (umask_windows.go).
func osUmask(mask int) int { _ = mask; return 0o022 }
