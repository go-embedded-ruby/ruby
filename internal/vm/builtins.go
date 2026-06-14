package vm

import (
	"fmt"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerBuiltins installs the Phase 0 Kernel methods. Phase 1 moves these
// onto the Kernel module in the real object model.
func (vm *VM) registerBuiltins() {
	vm.builtins["puts"] = builtinPuts
	vm.builtins["print"] = builtinPrint
	vm.builtins["p"] = builtinP
}

func builtinPuts(vm *VM, _ object.Value, args []object.Value) object.Value {
	if len(args) == 0 {
		fmt.Fprintln(vm.out)
		return object.NilV
	}
	for _, a := range args {
		fmt.Fprintln(vm.out, a.ToS())
	}
	return object.NilV
}

func builtinPrint(vm *VM, _ object.Value, args []object.Value) object.Value {
	for _, a := range args {
		fmt.Fprint(vm.out, a.ToS())
	}
	return object.NilV
}

func builtinP(vm *VM, _ object.Value, args []object.Value) object.Value {
	for _, a := range args {
		fmt.Fprintln(vm.out, a.Inspect())
	}
	switch len(args) {
	case 0:
		return object.NilV
	case 1:
		return args[0]
	default:
		// Ruby returns the argument array here; arrays arrive in Phase 2.
		return object.NilV
	}
}
