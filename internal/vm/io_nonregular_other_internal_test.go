// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !unix

package vm

import "errors"

func mkfifoForTest(string) error { return errors.New("fifos are a unix thing") }
