// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !wasm && !plan9 && !aix

package vm

import "golang.org/x/sys/unix"

// dupCloexec duplicates fd with close-on-exec set in the same call, so that no
// child forked meanwhile inherits it.
func dupCloexec(fd int) (int, error) {
	return unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
}
