// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build aix

package vm

import "golang.org/x/sys/unix"

// rlimitHostResources are the optional RLIMIT_ resources (see rlimitResources).
// AIX has no RLIMIT_MEMLOCK.
var rlimitHostResources = map[string]int{
	"AS":    unix.RLIMIT_AS,
	"NPROC": unix.RLIMIT_NPROC,
	"RSS":   unix.RLIMIT_RSS,
}
