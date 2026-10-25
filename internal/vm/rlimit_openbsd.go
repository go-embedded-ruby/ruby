// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build openbsd

package vm

import "golang.org/x/sys/unix"

// rlimitHostResources are the optional RLIMIT_ resources (see rlimitResources).
// OpenBSD has no RLIMIT_AS.
var rlimitHostResources = map[string]int{
	"MEMLOCK": unix.RLIMIT_MEMLOCK,
	"NPROC":   unix.RLIMIT_NPROC,
	"RSS":     unix.RLIMIT_RSS,
}
