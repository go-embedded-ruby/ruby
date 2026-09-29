// Package ruby is the public embedding API for go-embedded-ruby: compile and run
// Ruby source on a fresh VM. It is what an external Go program — or a custom
// WebAssembly build that bakes a Ruby program in with //go:embed — imports to
// embed the interpreter without reaching into the internal packages.
package ruby

import (
	"io"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

// Run parses, compiles and executes src on a new VM, writing the program's output
// to out. It returns a parse, compile or runtime error, or nil on success. The VM
// stays alive after Run returns as long as something still references it (e.g. a
// browser event handler the program registered through the JS bridge), which is
// what lets an embedded, event-driven program keep running.
func Run(src string, out io.Writer) error {
	prog, err := parser.Parse(src)
	if err != nil {
		return err
	}
	// User source, so its magic comments count -- the same reading cmd/rbgo
	// gives a script file. Compile(prog) would silently ignore both pragmas,
	// making an embedded program disagree with the CLI about its own file.
	iseq, err := compiler.CompileWithMagic(prog, compiler.MagicComments(src))
	if err != nil {
		return err
	}
	_, err = vm.New(out).Run(iseq)
	return err
}
