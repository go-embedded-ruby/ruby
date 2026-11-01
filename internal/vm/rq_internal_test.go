// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "strconv"

// rq renders an OS path as a Ruby double-quoted string literal (paths here are
// plain ASCII temp dirs, so Go's quoting matches Ruby's).
func rq(p string) string { return strconv.Quote(slash(p)) }
