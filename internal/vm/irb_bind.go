//go:build !rbgo_closed

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// irbParse and irbCompileWithLocals are the front-end seams the IRB REPL
// evaluates through. Routing eval via these package vars (rather than the
// packages directly) both keeps IRB on the same front-end the rest of the VM
// uses and lets a fault-injection test drive the otherwise-unreachable
// parse/compile-error branches deterministically (a complete statement that has
// passed irb.CheckCode almost never fails to compile).
var (
	irbParse             = parser.Parse
	irbCompileWithLocals = compiler.CompileWithLocals
)

// irbEval evaluates one complete unit of IRB input through rbgo itself — this is
// the integration seam: the REPL support library decides *when* a statement is
// ready, and this function *runs* it. The code is parsed and compiled against
// the binding's locals (so they resolve at depth 1, reaching the binding
// environment), then executed with the binding's self/definee/env inline on the
// VM goroutine. New top-level locals the input introduces are folded back into
// the binding and the unit recompiled, so an assignment on one line is visible
// on the next — the persistent-scope behaviour a REPL needs. A Ruby exception
// raised by the evaluated code is caught (so the loop survives it) and returned
// as class/message; the per-frame tracking stacks are truncated back to their
// pre-eval depths so a later statement starts from a clean backtrace state.
func (vm *VM) irbEval(b *Binding, code string) (result object.Value, errClass, errMsg string, raised bool) {
	prog, perr := irbParse(code)
	if perr != nil {
		return nil, "SyntaxError", perr.Error(), true
	}
	iseq, cerr := irbCompileWithLocals(prog, b.names)
	if cerr != nil {
		return nil, "SyntaxError", cerr.Error(), true
	}
	// The compiled unit's Locals are exactly the locals this input introduced
	// (binding locals resolve one scope up and are absent here). Persist them and
	// recompile so they bind into the binding environment rather than a per-input
	// scratch frame that is discarded when exec returns.
	if irbGrowLocals(b, iseq.Locals) {
		iseq, _ = irbCompileWithLocals(prog, b.names)
	}
	iseq.Name = "(irb)"

	nNames, nFiles := len(vm.frameNames), len(vm.frameFiles)
	nStack, nDirs := len(vm.fileStack), len(vm.requireDirs)
	defer func() {
		if r := recover(); r != nil {
			rerr, ok := r.(RubyError)
			if !ok {
				panic(r) // not a Ruby exception (a break/throw/return signal) — re-raise
			}
			// An exception unwinding past exec frames leaves their per-frame tracking
			// entries unpopped; truncate back so the next statement sees clean state.
			vm.frameNames = vm.frameNames[:nNames]
			vm.frameFiles = vm.frameFiles[:nFiles]
			vm.fileStack = vm.fileStack[:nStack]
			vm.requireDirs = vm.requireDirs[:nDirs]
			result, errClass, errMsg, raised = nil, rerr.Class, rerr.Message, true
		}
	}()
	result = vm.exec(iseq, b.self, nil, b.definee, "", b.env, nil, nil, nil)
	return result, "", "", false
}

// irbGrowLocals extends the binding with any of names it does not already carry,
// keeping the name map and environment slots aligned. It reports whether it added
// anything (so the caller knows to recompile the unit with the wider scope).
func irbGrowLocals(b *Binding, names []string) bool {
	grown := false
	for _, name := range names {
		if name != "" && b.slotOf(name) < 0 {
			b.names = append(b.names, name)
			b.env.slots = append(b.env.slots, object.NilV)
			grown = true
		}
	}
	return grown
}
