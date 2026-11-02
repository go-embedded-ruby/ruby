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

// splatToArray implements Ruby's splat (`*v`) coercion: an Array — including an
// instance of a user subclass — passes through; any other object that responds
// to #to_a is converted with it (MatchData, nil, Set, …) provided the result is
// an Array; everything else is wrapped in a one-element Array. Note MRI uses
// #to_a here, not #to_ary.
//
// "Responds to" is respondsToConversion, not respondsTo, because MRI sends the
// conversion through rb_check_funcall, which consults an OVERRIDDEN #respond_to?
// first and does not call the method when that answers false
// (vm_eval.c check_funcall_respond_to). Asking the method table directly instead
// made `*obj` call #to_a on an object that had just said it does not respond to
// it, and take the result.
func (vm *VM) splatToArray(v object.Value) object.Value {
	if a, ok := v.(*object.Array); ok {
		return a
	}
	// An Array SUBCLASS is still an Array. MRI reaches the conversion through
	// rb_check_to_array -> rb_check_convert_type_with_id(ary, T_ARRAY, "Array",
	// idTo_a), which returns the value untouched whenever TYPE(val) is already
	// T_ARRAY — and an instance of `Class.new(Array)` is T_ARRAY — so #to_a is
	// never sent. Here that shape is an RObject whose `builtin` holds the wrapped
	// array, which is this VM's spelling of the same question.
	//
	// ⛔ The copy is not incidental, and returning the subclass instance itself
	// would fix one example by breaking its neighbour. vm_splat_array returns
	// rb_ary_dup(tmp), so `(a = *subclass)` is a PLAIN Array:
	// language/variables_spec.rb asserts `x.should.instance_of?(Array)` in
	// "returns an Array when the splatted object is an Array subclass", and until
	// this change that example passed only BECAUSE #to_a was wrongly sent and
	// Array#to_a happens to answer with a plain copy. The dup also keeps the
	// splat from aliasing the subclass instance's own storage.
	if o, ok := v.(*RObject); ok {
		if a, ok := o.builtin.(*object.Array); ok {
			return object.NewArrayFromSlice(append([]object.Value(nil), a.Elems...))
		}
	}
	if vm.respondsToConversion(v, "to_a") {
		r := vm.send(v, "to_a", nil, nil)
		if a, ok := r.(*object.Array); ok {
			return a
		}
		// MRI treats a nil #to_a result as "no conversion available" and wraps the
		// object in a one-element Array, exactly as for an object without #to_a; a
		// non-nil, non-Array result is the error.
		if object.IsNil(r) {
			return object.NewArray(v)
		}
		raise("TypeError", "can't convert %s to Array (%s#to_a gives %s)",
			vm.classOf(v).name, vm.classOf(v).name, vm.classOf(r).name)
	}
	return object.NewArray(v)
}
