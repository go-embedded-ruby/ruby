// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import "os"

// On POSIX systems File.chown / File.lchown map straight onto the os package;
// passing -1 for an id leaves it unchanged, matching Ruby.
var (
	fileChown  = os.Chown
	fileLchown = os.Lchown
)
