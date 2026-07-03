package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// Helpers called by AOT-compiled method bodies (see internal/aot). Each mirrors
// the corresponding interpreter opcode so compiled and interpreted code behave
// identically; they live here so the generated Go can stay terse.

// aotConst backs OpGetConst in AOT-compiled bodies: a top-level (Object)
// constant lookup that raises NameError when unset. AOT lowering is closed-world
// and runs at top-level scope, so lexical nesting is not threaded here.
func (vm *VM) aotConst(name string) object.Value {
	v, ok := vm.cObject.consts[name]
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
	return object.NewArrayFromSlice(out)
}

// aotSplat backs OpSplatToArray in AOT-compiled bodies; see splatToArray.
func (vm *VM) aotSplat(v object.Value) object.Value {
	return vm.splatToArray(v)
}

// splatToArray implements Ruby's splat (`*v`) coercion: an Array passes through;
// any other object that responds to #to_a is converted with it (MatchData, nil,
// Set, …) provided the result is an Array; everything else is wrapped in a
// one-element Array. Note MRI uses #to_a here, not #to_ary.
func (vm *VM) splatToArray(v object.Value) object.Value {
	if a, ok := v.(*object.Array); ok {
		return a
	}
	if vm.respondsTo(v, "to_a") {
		r := vm.send(v, "to_a", nil, nil)
		if a, ok := r.(*object.Array); ok {
			return a
		}
		raise("TypeError", "can't convert %s to Array (%s#to_a gives %s)",
			vm.classOf(v).name, vm.classOf(v).name, vm.classOf(r).name)
	}
	return object.NewArray(v)
}
