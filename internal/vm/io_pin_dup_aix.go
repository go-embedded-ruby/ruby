// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build aix

package vm

import "syscall"

// dupCloexec duplicates fd with close-on-exec set. AIX has no F_DUPFD_CLOEXEC
// in golang.org/x/sys, so it is dup(2) then FD_CLOEXEC, both under
// syscall.ForkLock, as the os package does: no child forked between the two
// calls inherits the descriptor.
func dupCloexec(fd int) (int, error) {
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	nfd, err := syscall.Dup(fd)
	if err != nil {
		return -1, err
	}
	syscall.CloseOnExec(nfd)
	return nfd, nil
}
