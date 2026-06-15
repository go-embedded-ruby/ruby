package vm

import (
	_ "embed"

	"github.com/go-embedded-ruby/ruby/internal/ast"
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/parser"
)

// preludeSource is the embedded-Ruby standard library loaded at VM startup
// (Comparable, Enumerable, …). See prelude.rb.
//
//go:embed prelude.rb
var preludeSource string

// loadSource compiles and runs one chunk of Ruby source against this VM,
// returning the first parse, compile, or runtime error.
func (vm *VM) loadSource(src string) error {
	prog, err := parser.Parse(src)
	if err != nil {
		return err
	}
	return vm.loadAST(prog)
}

// loadAST compiles and runs an already-parsed program. Split out from
// loadSource because a compile error is only reachable from a hand-built AST
// (the parser turns every unknown bareword into a method call), so this seam
// keeps that branch testable.
func (vm *VM) loadAST(prog *ast.Program) error {
	iseq, err := compiler.Compile(prog)
	if err != nil {
		return err
	}
	_, err = vm.Run(iseq)
	return err
}

// loadPrelude runs the prelude, panicking on failure: a broken prelude is a
// build-time bug in this package, not a user error.
func (vm *VM) loadPrelude(src string) {
	if err := vm.loadSource(src); err != nil {
		panic("vm: prelude failed to load: " + err.Error())
	}
}
