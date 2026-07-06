// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestUnixNarrowingGuards covers the type-narrowing guards on the AF_UNIX socket
// receivers directly: their raising arm is unreachable through normal dispatch
// (the classes only receive their own instances), so it is injected. Compiled
// only on non-Windows, where the AF_UNIX transport exists.
func TestUnixNarrowingGuards(t *testing.T) {
	mustRaiseClass(t, "TypeError", func() { asUnixSocket(object.NilV) })
	mustRaiseClass(t, "TypeError", func() { asUnixServer(object.NilV) })
}
