// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build solaris

package vm

import "golang.org/x/sys/unix"

// rlimitHostResources are the optional RLIMIT_ resources (see rlimitResources).
// Solaris and illumos (GOOS=illumos also sets the solaris tag) have RLIMIT_AS
// but not MEMLOCK, NPROC or RSS.
var rlimitHostResources = map[string]int{
	"AS": unix.RLIMIT_AS,
}
