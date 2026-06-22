//go:build !rbgo_closed

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
	"github.com/go-ruby-parser/parser/ast"
)

// installPrelude loads the embedded-Ruby standard library by parsing and running
// it. A closed-world build replaces this with prelude_closed.go, which runs the
// prelude's frozen bytecode instead (no front-end).
func (vm *VM) installPrelude() {
	vm.loadPrelude(preludeSource)
}

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
