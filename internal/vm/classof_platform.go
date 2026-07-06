// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// classOfPlatform is the tail of classOf for platform-gated value types. On
// non-Windows it resolves the AF_UNIX sockets (compiled only here) via the
// rubyClassCarrier interface, keeping the shared classOf switch platform-neutral.
func (vm *VM) classOfPlatform(v object.Value) *RClass {
	if cc, ok := v.(rubyClassCarrier); ok {
		return cc.rubyClass()
	}
	return nil // unreachable for the closed set of value types
}

// rubyClassCarrier is implemented by value types whose Ruby class cannot be a
// case in the classOf switch because they are compiled only on some platforms
// (currently the AF_UNIX UNIXSocket / UNIXServer).
type rubyClassCarrier interface{ rubyClass() *RClass }
