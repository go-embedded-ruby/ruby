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
	// block, when set, is an Enumerator.new { |y| … } generator: it is run with a
	// yielder rather than driving recv.meth.
	block *Proc

	buf  []object.Value // materialised on first next/peek/to_a
	have bool
	pos  int
}

// yielder is the object passed to an Enumerator.new generator block; `y << v`
// and `y.yield(v)` feed values into the enumeration.
type yielder struct{ emit func(args []object.Value) }

func (y *yielder) ToS() string     { return "#<Enumerator::Yielder>" }
func (y *yielder) Inspect() string { return y.ToS() }
func (y *yielder) Truthy() bool    { return true }

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
	if en, ok := object.KindOK[*RClass](vm.consts["Enumerable"]); ok {
		vm.cEnumerator.includes = append(vm.cEnumerator.includes, en)
	}

	// Enumerator::Yielder — `y << v` / `y.yield(v)` feed the generator's values in.
	vm.cYielder = newClass("Enumerator::Yielder", vm.cObject)
	vm.cYielder.consts = vm.cEnumerator.consts // (scope is cosmetic; share the map)
	vm.cEnumerator.consts["Yielder"] = vm.cYielder
	vm.cYielder.define("<<", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		object.Kind[*yielder](self).emit(args)
		return self // << chains
	})
	vm.cYielder.define("yield", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		object.Kind[*yielder](self).emit(args)
		return object.NilV
	})
	// Enumerator.new { |y| … } builds a generator-block enumerator.
	vm.cEnumerator.smethods["new"] = &Method{name: "new", owner: vm.cEnumerator,
		native: func(_ *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
			if blk == nil {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
			}
			return &Enumerator{block: blk}
		}}

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
		e := object.Kind[*Enumerator](self)
		if blk == nil {
			return e
		}
		if e.block != nil { // generator: run it with a yielder forwarding to blk
			return vm.callBlock(e.block, []object.Value{&yielder{emit: func(args []object.Value) {
				vm.callBlock(blk, args)
			}}})
		}
		return vm.send(e.recv, e.meth, e.args, blk)
	})
	d("to_a", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(vm.enumMaterialize(object.Kind[*Enumerator](self)))
	})
	d("size", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(len(vm.enumMaterialize(object.Kind[*Enumerator](self)))))
	})
	d("next", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		e := object.Kind[*Enumerator](self)
		buf := vm.enumBuffer(e)
		if e.pos >= len(buf) {
			raise("StopIteration", "iteration reached an end")
		}
		v := buf[e.pos]
		e.pos++
		return v
	})
	d("peek", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		e := object.Kind[*Enumerator](self)
		buf := vm.enumBuffer(e)
		if e.pos >= len(buf) {
			raise("StopIteration", "iteration reached an end")
		}
		return buf[e.pos]
	})
	d("rewind", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		object.Kind[*Enumerator](self).pos = 0
		return self
	})
	withIndex := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		e := object.Kind[*Enumerator](self)
		off := int64(0)
		if len(args) > 0 {
			off = intArg(args[0])
		}
		if blk == nil {
			// No block: an Enumerator that yields [element, index] pairs.
			elems := vm.enumMaterialize(e)
			pairs := make([]object.Value, len(elems))
			for i, v := range elems {
				pairs[i] = object.NewArray(v, object.IntValue(off+int64(i)))
			}
			return enumFor(object.NewArrayFromSlice(pairs), "each")
		}
		// With a block, re-run the source, appending the running index to each
		// yield and forwarding the block's result — so map collects, each returns
		// the receiver, etc., exactly as the wrapped method would.
		i := off
		wrapper := &Proc{native: func(_ *VM, cargs []object.Value) object.Value {
			withIdx := append(append([]object.Value{}, cargs...), object.IntValue(i))
			i++
			return vm.callBlock(blk, withIdx)
		}}
		if e.block != nil { // generator: drive it with the indexing wrapper as the yielder
			return vm.callBlock(e.block, []object.Value{&yielder{emit: func(args []object.Value) {
				wrapper.native(vm, args)
			}}})
		}
		return vm.send(e.recv, e.meth, e.args, wrapper)
	}
	d("with_index", withIndex)
	d("each_with_index", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return withIndex(vm, self, nil, blk) // each_with_index ignores any offset
	})
	// first/take pull only as many elements as requested, so they terminate even
	// for unbounded enumerators such as Array#cycle without a block.
	d("first", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		e := object.Kind[*Enumerator](self)
		if len(args) == 0 {
			got := vm.enumTake(e, 1)
			if len(got) == 0 {
				return object.NilV
			}
			return got[0]
		}
		return object.NewArrayFromSlice(vm.enumTake(e, int(intArg(args[0]))))
	})
	d("take", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(vm.enumTake(object.Kind[*Enumerator](self), int(intArg(args[0]))))
	})
}

// enumStop is the sentinel panic used by enumTake to unwind out of a possibly
// unbounded #each once enough elements have been collected.
type enumStop struct{}

// enumTake drives the enumerator's #each and collects at most n elements,
// aborting the (possibly infinite) iteration as soon as the quota is met.
func (vm *VM) enumTake(e *Enumerator, n int) (out []object.Value) {
	if n <= 0 {
		return []object.Value{}
	}
	out = make([]object.Value, 0, n)
	collect := &Proc{native: func(_ *VM, args []object.Value) object.Value {
		if len(args) == 1 {
			out = append(out, args[0])
		} else {
			out = append(out, object.NewArrayFromSlice(append([]object.Value{}, args...)))
		}
		if len(out) >= n {
			panic(enumStop{})
		}
		return object.NilV
	}}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(enumStop); ok {
				return
			}
			panic(r)
		}
	}()
	if e.block != nil { // generator block: drive it with a collecting yielder
		vm.callBlock(e.block, []object.Value{&yielder{emit: func(args []object.Value) {
			collect.native(vm, args)
		}}})
		return out
	}
	vm.send(e.recv, e.meth, e.args, collect)
	return out
}

// enumMaterialize runs the underlying method with a collecting block and returns
// the yielded elements.
func (vm *VM) enumMaterialize(e *Enumerator) []object.Value {
	out := []object.Value{}
	collect := func(args []object.Value) {
		if len(args) == 1 {
			out = append(out, args[0])
		} else {
			out = append(out, object.NewArrayFromSlice(append([]object.Value{}, args...)))
		}
	}
	if e.block != nil { // generator block: run it with a collecting yielder
		vm.callBlock(e.block, []object.Value{&yielder{emit: collect}})
		return out
	}
	vm.send(e.recv, e.meth, e.args, &Proc{native: func(_ *VM, args []object.Value) object.Value {
		collect(args)
		return object.NilV
	}})
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
