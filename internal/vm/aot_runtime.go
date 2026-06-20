package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// Helpers called by AOT-compiled method bodies (see internal/aot). Each mirrors
// the corresponding interpreter opcode so compiled and interpreted code behave
// identically; they live here so the generated Go can stay terse.

// aotConst backs OpGetConst: a constant lookup that raises NameError when unset.
func (vm *VM) aotConst(name string) object.Value {
	v, ok := vm.consts[name]
	if !ok {
		raise("NameError", "uninitialized constant %s", name)
	}
	return v
}

// aotYield backs OpInvokeBlock: invoke the frame's block, raising when absent.
func (vm *VM) aotYield(block *Proc, args []object.Value) object.Value {
	if block == nil {
		raise("LocalJumpError", "no block given (yield)")
	}
	return vm.callBlock(block, args)
}

// aotConcat backs OpConcatArray: concatenate two arrays into a fresh one.
func aotConcat(a, b object.Value) object.Value {
	ae := a.(*object.Array).Elems
	be := b.(*object.Array).Elems
	out := make([]object.Value, 0, len(ae)+len(be))
	out = append(append(out, ae...), be...)
	return &object.Array{Elems: out}
}

// aotSplat backs OpSplatToArray: pass an array through, else wrap a scalar.
func aotSplat(v object.Value) object.Value {
	if a, ok := v.(*object.Array); ok {
		return a
	}
	return &object.Array{Elems: []object.Value{v}}
}
