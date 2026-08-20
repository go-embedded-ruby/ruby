// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build unix

package vm

import "golang.org/x/sys/unix"

func mkfifoForTest(path string) error { return unix.Mkfifo(path, 0o600) }
