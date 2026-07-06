// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// classOfPlatform is the tail of classOf on Windows, where no platform-gated
// value types exist (the AF_UNIX sockets are not compiled), so it is a plain
// nil return — reached for any value not matched by the shared classOf switch.
func (vm *VM) classOfPlatform(object.Value) *RClass {
	return nil // unreachable for the closed set of value types
}
