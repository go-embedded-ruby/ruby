// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build rbgo_closed

package main

import (
	"os"

	"github.com/go-embedded-ruby/ruby/internal/vm"
)

// seedProcessArgs gives the embedded program the arguments this process was
// started with. A closed-world binary IS the program — there is no interpreter
// command line in front of it — so there is nothing to parse and nothing to
// split: every argument belongs to the program, exactly as it would for a C
// program, and $0 is the binary that was invoked.
//
// This is deliberately NOT ruby.c's proc_options. `./game -W0 level.dat` passes
// "-W0" to the game; a closed binary that swallowed it as an interpreter switch
// would be lying about whose command line it is.
//
// It is shared by the native and the wasm closed mains, and it reads os.Args on
// both, so no BUILD TAG decides whether a closed binary can take arguments —
// only the host does, which is the honest answer and not a tag-selected one.
//
// What each host puts there was measured, not assumed:
//
//   - native: the process's argv, as usual.
//   - js/wasm under node: Go's own lib/wasm/wasm_exec_node.js glue passes
//     process.argv.slice(2) through, so `node wasm_exec_node.js prog.wasm a b`
//     arrives as os.Args == {"prog.wasm", "a", "b"} and ARGV == ["a", "b"].
//     Verified end to end by TestClosedWasmBuildIntegration.
//   - js/wasm in a browser: there is no process argv at all; os.Args carries only
//     "js", so ARGV is empty. Nothing here special-cases that.
//   - wasip1: os.Args comes from the WASI args_get call, so a host that passes
//     arguments delivers them. This machine has no wasip1 runtime installed, so
//     that row is reasoned from the Go runtime's own wasip1 os.Args and is the
//     one row below that was NOT measured.
func seedProcessArgs(machine *vm.VM) {
	if len(os.Args) > 0 {
		machine.SetScriptName(os.Args[0])
		machine.SetARGV(os.Args[1:])
		return
	}
	machine.SetARGV(nil)
}
