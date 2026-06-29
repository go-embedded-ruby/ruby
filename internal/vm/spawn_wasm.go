// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build js && wasm

package vm

// runCaptured has no meaning under js/wasm (no subprocesses), so spawning a
// command raises NotImplementedError there rather than silently succeeding.
var runCaptured = func(cmd []string) (string, int) {
	raise("NotImplementedError", "subprocess execution is not supported on js/wasm")
	return "", 127
}
