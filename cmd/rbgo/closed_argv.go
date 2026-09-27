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
// both, so no build tag decides whether a closed binary can take arguments. What
// differs is what the host puts there: wasip1 fills os.Args from the WASI
// args_get call, so `wasmtime prog.wasm a b` arrives intact, while a js/wasm
// module loaded by a browser has no process arguments at all and os.Args carries
// only the program name — ARGV is then empty, which is the honest answer rather
// than a tag-selected one.
func seedProcessArgs(machine *vm.VM) {
	if len(os.Args) > 0 {
		machine.SetScriptName(os.Args[0])
		machine.SetARGV(os.Args[1:])
		return
	}
	machine.SetARGV(nil)
}
