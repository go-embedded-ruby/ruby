//go:build rbgo_closed && !(js && wasm)

package main

import (
	"fmt"
	"os"

	"github.com/go-embedded-ruby/ruby/internal/vm"
)

// main runs the single program baked into this closed-world binary. The program
// is embedded as bytecode by `rbgo build --closed` (embeddedProgram, generated
// by FreezeISeq and injected via -overlay), so no source file is read and no
// lexer/parser/compiler is linked. The prelude likewise loads from its frozen
// bytecode (see prelude_closed.go).
//
// This is the NATIVE closed main; the wasm closed main (closed_main_wasm.go)
// runs the same embedded program but blocks on select{} afterwards so the Go
// runtime stays alive for browser callbacks.
func main() {
	machine := vm.New(os.Stdout)
	if _, err := machine.Run(embeddedProgram()); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
