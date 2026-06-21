package vm

import (
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Enumerator is an external iterator over recv.meth(*args): #each forwards a
// block to the underlying method, while #to_a/#next/#peek materialise the
// yielded elements eagerly (so this covers finite sequences; true laziness is a
// separate concern). An element is the single yielded value, or an array of the
// values when several are yielded at once.
type Enumerator struct {
	recv object.Value
	meth string
	args []object.Value

	buf  []object.Value // materialised on first next/peek/to_a
	have bool
	pos  int
}

// Inspect renders the MRI form #<Enumerator: recv:meth(args)>. (MRI's #to_s
// shows the object address instead, which we can't reproduce deterministically,
// so ToS reuses Inspect.)
func (e *Enumerator) Inspect() string {
	s := "#<Enumerator: " + e.recv.Inspect() + ":" + e.meth
	if len(e.args) > 0 {
		parts := make([]string, len(e.args))
		for i, a := range e.args {
			parts[i] = a.Inspect()
		}
		s += "(" + strings.Join(parts, ", ") + ")"
	}
	return s + ">"
}
func (e *Enumerator) ToS() string  { return e.Inspect() }
func (e *Enumerator) Truthy() bool { return true }

// enumFor builds an Enumerator for recv.meth(*args).
func enumFor(recv object.Value, meth string, args ...object.Value) *Enumerator {
	return &Enumerator{recv: recv, meth: meth, args: args}
}

func (vm *VM) registerEnumerator() {
	vm.cEnumerator = newClass("Enumerator", vm.cObject)
	vm.consts["Enumerator"] = vm.cEnumerator
	// Mix in Enumerable so map/select/reduce/… work via #each.
	if en, ok := vm.consts["Enumerable"].(*RClass); ok {
		vm.cEnumerator.includes = append(vm.cEnumerator.includes, en)
	}

	// Kernel#enum_for / #to_enum: build an Enumerator for self.meth(*rest).
	enumForFn := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		meth, rest := "each", []object.Value(nil)
		if len(args) > 0 {
			meth, rest = args[0].ToS(), args[1:]
		}
		return &Enumerator{recv: self, meth: meth, args: rest}
	}
	vm.cObject.define("enum_for", enumForFn)
	vm.cObject.define("to_enum", enumForFn)

	d := func(name string, fn NativeFn) { vm.cEnumerator.define(name, fn) }
	d("each", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		e := self.(*Enumerator)
		if blk == nil {
			return e
		}
		return vm.send(e.recv, e.meth, e.args, blk)
	})
	d("to_a", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &object.Array{Elems: vm.enumMaterialize(self.(*Enumerator))}
	})
	d("size", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(len(vm.enumMaterialize(self.(*Enumerator))))
	})
	d("next", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		buf := vm.enumBuffer(e)
		if e.pos >= len(buf) {
			raise("StopIteration", "iteration reached an end")
		}
		v := buf[e.pos]
		e.pos++
		return v
	})
	d("peek", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		e := self.(*Enumerator)
		buf := vm.enumBuffer(e)
		if e.pos >= len(buf) {
			raise("StopIteration", "iteration reached an end")
		}
		return buf[e.pos]
	})
	d("rewind", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		self.(*Enumerator).pos = 0
		return self
	})
	withIndex := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		e := self.(*Enumerator)
		off := int64(0)
		if len(args) > 0 {
			off = intArg(args[0])
		}
		if blk == nil {
			// No block: an Enumerator that yields [element, index] pairs.
			elems := vm.enumMaterialize(e)
			pairs := make([]object.Value, len(elems))
			for i, v := range elems {
				pairs[i] = &object.Array{Elems: []object.Value{v, object.Integer(off + int64(i))}}
			}
			return enumFor(&object.Array{Elems: pairs}, "each")
		}
		// With a block, re-run the underlying method, appending the running index
		// to each yield and forwarding the block's result — so map collects, each
		// returns the receiver, etc., exactly as the wrapped method would.
		i := off
		wrapper := &Proc{native: func(_ *VM, cargs []object.Value) object.Value {
			withIdx := append(append([]object.Value{}, cargs...), object.Integer(i))
			i++
			return vm.callBlock(blk, withIdx)
		}}
		return vm.send(e.recv, e.meth, e.args, wrapper)
	}
	d("with_index", withIndex)
	d("each_with_index", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return withIndex(vm, self, nil, blk) // each_with_index ignores any offset
	})
}

// enumMaterialize runs the underlying method with a collecting block and returns
// the yielded elements.
func (vm *VM) enumMaterialize(e *Enumerator) []object.Value {
	out := []object.Value{}
	collector := &Proc{native: func(_ *VM, args []object.Value) object.Value {
		if len(args) == 1 {
			out = append(out, args[0])
		} else {
			out = append(out, &object.Array{Elems: append([]object.Value{}, args...)})
		}
		return object.NilV
	}}
	vm.send(e.recv, e.meth, e.args, collector)
	return out
}

// enumBuffer materialises (once) and caches the element buffer for next/peek.
func (vm *VM) enumBuffer(e *Enumerator) []object.Value {
	if !e.have {
		e.buf = vm.enumMaterialize(e)
		e.have = true
	}
	return e.buf
}
