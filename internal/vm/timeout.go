// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerTimeout installs the Timeout module (require "timeout"). Timeout
// enforcement requires pre-emptively interrupting a running block, which needs
// the scheduler work planned for a later round; for now Timeout.timeout runs the
// block to completion and returns its value (the common case during local
// `puppet apply`, where the guarded blocks complete quickly). Timeout::Error is
// defined so `rescue Timeout::Error` resolves.
func (vm *VM) registerTimeout() {
	mod := newClass("Timeout", nil)
	mod.isModule = true
	vm.consts["Timeout"] = mod

	// Timeout::Error < RuntimeError, matching MRI's hierarchy so a bare rescue and
	// `rescue Timeout::Error` both catch it.
	mod.consts["Error"] = newClass("Timeout::Error", vm.consts["RuntimeError"].(*RClass))

	mod.smethods["timeout"] = &Method{name: "timeout", owner: mod,
		native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
			if blk == nil {
				return raise("LocalJumpError", "no block given (yield)")
			}
			// The block receives the limit (sec) as its single argument in MRI; pass
			// nil since no deadline is enforced yet.
			return vm.callBlock(blk, []object.Value{object.NilV})
		}}
}
