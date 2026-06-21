package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerEval installs Kernel#eval — the embedded front-end's reason for being.
// Because the lexer/parser/compiler ship inside the binary, a running program
// can compile and run new Ruby at runtime.
//
// eval(str) runs str against the caller's `self` (so `self`, instance variables,
// methods and constants are visible, and a `def` lands where it would in the
// caller's context), with a fresh local scope. Capturing the caller's *local
// variables* needs a Binding and is not yet supported — see the limitation note
// in the docs.
func (vm *VM) registerEval() {
	vm.cObject.define("eval", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, ok := args[0].(*object.String)
		if !ok {
			return raise("TypeError", "no implicit conversion of %s into String", vm.classOf(args[0]).name)
		}
		prog, perr := parser.Parse(string(s.B))
		if perr != nil {
			return raise("SyntaxError", "%s", perr.Error())
		}
		iseq, cerr := compiler.Compile(prog)
		if cerr != nil {
			return raise("SyntaxError", "%s", cerr.Error())
		}
		iseq.Name = "(eval)"
		// Run against the caller's self/definee with a fresh local scope; a runtime
		// RubyError from the evaluated code propagates (and is rescuable) as usual.
		return vm.exec(iseq, self, nil, vm.classOf(self), "", nil, nil, nil)
	})
}
