// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

// osUmask is the Windows counterpart of the Unix syscall.Umask: Windows has no
// umask, so File.umask is a no-op that reports a conventional 0o022 mask without
// changing any process state. Code that brackets work in File.umask (Puppet's
// withumask) still runs; the mask simply has no effect on file creation.
func osUmask(mask int) int { _ = mask; return 0o022 }
