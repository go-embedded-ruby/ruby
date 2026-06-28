// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

// Windows has no POSIX ownership model and os.Chown always fails there, so —
// like MRI, where File.chown/File.lchown are accepted no-ops on Windows — these
// report success without changing anything. A test may still swap the seam to a
// failing stub to exercise the Errno mapping.
var (
	fileChown  = func(string, int, int) error { return nil }
	fileLchown = func(string, int, int) error { return nil }
)
