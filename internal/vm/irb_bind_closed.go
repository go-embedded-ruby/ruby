//go:build rbgo_closed

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// irbEval is unavailable in a closed-world binary: the IRB REPL evaluates its
// input through the front-end (parser + compiler), which `rbgo build --closed`
// drops from the link. A closed binary is an AOT-compiled program, not a REPL,
// so nothing calls IRB.start there; should it be reached, each unit reports the
// dropped-front-end error and the loop moves on rather than crashing.
func (vm *VM) irbEval(_ *Binding, _ string) (result object.Value, errClass, errMsg string, raised bool) {
	return nil, "NotImplementedError",
		"IRB evaluation is unavailable in a closed-world binary (built with rbgo build --closed, without the front-end)",
		true
}
