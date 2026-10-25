// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !wasm && !openbsd && !aix && !solaris

package vm

import "golang.org/x/sys/unix"

// rlimitHostResources are the optional RLIMIT_ resources (see rlimitResources).
// Linux, Darwin, FreeBSD, NetBSD and DragonFly have all four.
var rlimitHostResources = map[string]int{
	"AS":      unix.RLIMIT_AS,
	"MEMLOCK": unix.RLIMIT_MEMLOCK,
	"NPROC":   unix.RLIMIT_NPROC,
	"RSS":     unix.RLIMIT_RSS,
}
