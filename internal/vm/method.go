package vm

import (
	"fmt"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// BoundMethod is a Method object: a callable bound to a receiver, produced by
// Object#method(:name).
type BoundMethod struct {
	recv object.Value
	name string
	m    *Method
}

// every Method created by define / OpDefineMethod / define_(singleton_)method
// carries a non-nil owner, so b.m.owner is always set.
func (b *BoundMethod) ToS() string     { return fmt.Sprintf("#<Method: %s#%s>", b.m.owner.name, b.name) }
func (b *BoundMethod) Inspect() string { return b.ToS() }
func (b *BoundMethod) Truthy() bool    { return true }

// registerMethod installs the Method class and Object#method.
func (vm *VM) registerMethod() {
	vm.cMethod = newClass("Method", vm.cObject)
	vm.consts["Method"] = vm.cMethod

	vm.cObject.define("method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := args[0].ToS()
		m := vm.resolveMethod(self, name)
		if m == nil {
			return raise("NameError", "undefined method '%s' for %s", name, vm.classOf(self).name)
		}
		return &BoundMethod{recv: self, name: name, m: m}
	})

	call := func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		b := self.(*BoundMethod)
		return vm.invoke(b.m, b.recv, args, blk)
	}
	vm.cMethod.define("call", call)
	vm.cMethod.define("[]", call)
	vm.cMethod.define("===", call)
	vm.cMethod.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(self.(*BoundMethod).name)
	})
	vm.cMethod.define("arity", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(methodArity(self.(*BoundMethod).m))
	})
	vm.cMethod.define("owner", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*BoundMethod).m.owner // always non-nil for a resolved method
	})
	vm.cMethod.define("receiver", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*BoundMethod).recv
	})
	vm.cMethod.define("to_proc", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := self.(*BoundMethod)
		return &Proc{
			native:      func(vm *VM, args []object.Value) object.Value { return vm.send(b.recv, b.name, args, nil) },
			nativeArity: methodArity(b.m),
			isLambda:    true,
		}
	})
}

// resolveMethod finds the Method that would handle recv.name (singleton methods
// and the class chain), or nil. Operators handled by the compiler fast path are
// not real methods and won't resolve here.
func (vm *VM) resolveMethod(recv object.Value, name string) *Method {
	if cls, ok := recv.(*RClass); ok {
		if m := lookupSMethod(cls, name); m != nil {
			return m
		}
	}
	c := vm.classOf(recv)
	if o, ok := recv.(*RObject); ok && o.singleton != nil {
		c = o.singleton
	}
	return undefAsNil(lookupMethod(c, name))
}

// methodArity reports a method's arity in Ruby's convention: the required count,
// or -(required+1) when there are optional/splat params. Native methods report
// -1 (variadic).
func methodArity(m *Method) int {
	switch {
	case m.iseq != nil:
		iseq := m.iseq
		if iseq.SplatIndex < 0 && iseq.NumRequired == len(iseq.Params) {
			return iseq.NumRequired
		}
		return -(iseq.NumRequired + 1)
	case m.proc != nil:
		return m.proc.arityVal()
	default:
		return -1
	}
}
