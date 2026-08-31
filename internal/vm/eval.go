package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// instanceEvalString backs the String form of BasicObject#instance_eval: it
// compiles src and runs it with self as the receiver and the receiver's
// singleton class as the definee, so a `def` in the source becomes a singleton
// method and `self`/instance variables resolve to the receiver. An immediate
// receiver has no singleton class, so its own class stands in as the definee
// (source that only reads still works).
func (vm *VM) instanceEvalString(self object.Value, src, file string) object.Value {
	iseq, cerr := parseCompileFn(src)
	if cerr != nil {
		return raise("SyntaxError", "%s", cerr.Error())
	}
	iseq.Name = "(eval)"
	iseq.File = file // the given filename, or the caller's file by default
	definee, ok := vm.ensureSingleton(self)
	if !ok {
		definee = vm.classOf(self)
	}
	vm.pendingMethodCtx = vm.currentMethodCtxPtr()
	return vm.exec(iseq, self, nil, definee, "", nil, nil, nil, nil, nil)
}

// registerEval installs Kernel#eval — the embedded front-end's reason for being.
// Because the lexer/parser/compiler ship inside the binary, a running program
// can compile and run new Ruby at runtime.
//
// eval(str) runs str against the caller's `self` (so `self`, instance variables,
// methods and constants are visible, and a `def` lands where it would in the
// caller's context), with a fresh local scope. To run against the caller's
// *local variables* too, capture a `binding` and pass it: eval(str, binding) —
// see bindingEval in binding.go.
func (vm *VM) registerEval() {
	vm.cObject.define("eval", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// eval(str, binding) runs against the binding's captured locals/self.
		if len(args) > 1 {
			if b, ok := args[1].(*Binding); ok {
				return vm.bindingEval(b, args[0])
			}
		}
		s, ok := args[0].(*object.String)
		if !ok {
			return raise("TypeError", "no implicit conversion of %s into String", vm.classOf(args[0]).name)
		}
		iseq, cerr := parseCompileFn(string(s.Bytes()))
		if cerr != nil {
			return raise("SyntaxError", "%s", cerr.Error())
		}
		iseq.Name = "(eval)"
		// Run against the caller's self/definee with a fresh local scope; a runtime
		// RubyError from the evaluated code propagates (and is rescuable) as usual.
		// eval is transparent to Kernel#__method__ / #__callee__: the evaluated code
		// inherits the caller's method context (eval "__method__" inside a method
		// reports that method), so hand exec the caller's pair.
		vm.pendingMethodCtx = vm.currentMethodCtxPtr()
		return vm.exec(iseq, self, nil, vm.classOf(self), "", nil, nil, nil, nil, nil)
	})
}
