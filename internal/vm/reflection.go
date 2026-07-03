package vm

import (
	"fmt"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// UnboundMethod is a method detached from any receiver, produced by
// Module#instance_method or Method#unbind. It is re-bound to a compatible
// receiver with #bind / #bind_call.
type UnboundMethod struct {
	name  string
	owner *RClass // the module/class the method was extracted from
	m     *Method
}

func (u *UnboundMethod) ToS() string {
	return fmt.Sprintf("#<UnboundMethod: %s#%s>", u.owner.name, u.name)
}
func (u *UnboundMethod) Inspect() string { return u.ToS() }
func (u *UnboundMethod) Truthy() bool    { return true }

// registerReflection installs the reflection API: Module#instance_method,
// Object#method/#singleton_class, the UnboundMethod class, and Method#unbind. It
// also teaches define_method to accept a Method/UnboundMethod body.
func (vm *VM) registerReflection() {
	cUnbound := newClass("UnboundMethod", vm.cObject)
	vm.consts["UnboundMethod"] = cUnbound

	// Module#instance_method(:m) → UnboundMethod resolved up the ancestor chain.
	vm.cModule.define("instance_method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := object.Kind[*RClass](self)
		name := nameArg(args[0])
		m := lookupMethod(mod, name)
		if m == nil || m.undefined {
			raise("NameError", "undefined method '%s' for class '%s'", name, mod.name)
		}
		return &UnboundMethod{name: name, owner: m.owner, m: m}
	})

	// Module#method_defined?(:m): true if m resolves up the ancestor chain.
	vm.cModule.define("method_defined?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := object.Kind[*RClass](self)
		m := lookupMethod(mod, nameArg(args[0]))
		return object.Bool(m != nil && !m.undefined)
	})

	// Module#public_method_defined?/#private_method_defined?/#protected_method_defined?:
	// like #method_defined? but additionally require the resolved method's
	// effective access level (honouring any inherited visibility override) to
	// match. Used by Puppet's metaprogramming to probe accessor visibility.
	definedWithVis := func(want visibility) NativeFn {
		return func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			mod := object.Kind[*RClass](self)
			name := nameArg(args[0])
			m := lookupMethod(mod, name)
			if m == nil || m.undefined {
				return object.False
			}
			return object.Bool(instanceVisibility(mod, name, m) == want)
		}
	}
	vm.cModule.define("public_method_defined?", definedWithVis(visPublic))
	vm.cModule.define("private_method_defined?", definedWithVis(visPrivate))
	vm.cModule.define("protected_method_defined?", definedWithVis(visProtected))

	cUnbound.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(object.Kind[*UnboundMethod](self).name)
	})
	cUnbound.define("owner", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Kind[*UnboundMethod](self).owner
	})
	cUnbound.define("arity", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(methodArity(object.Kind[*UnboundMethod](self).m)))
	})
	// UnboundMethod#bind(obj) → Method; obj must be a kind_of? the owner.
	cUnbound.define("bind", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		u := object.Kind[*UnboundMethod](self)
		vm.checkBindable(u, args[0])
		return &BoundMethod{recv: args[0], name: u.name, m: u.m}
	})
	// UnboundMethod#bind_call(obj, *args, &blk): bind then invoke in one step.
	cUnbound.define("bind_call", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		u := object.Kind[*UnboundMethod](self)
		vm.checkBindable(u, args[0])
		return vm.invoke(u.m, args[0], args[1:], blk)
	})

	// Object#singleton_class: the per-object singleton (meta) class, created on
	// demand. Immediate values (Integer/Symbol/true/false/nil) have none in MRI.
	vm.cObject.define("singleton_class", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		sc, ok := vm.singletonDefinee(self)
		if !ok {
			raise("TypeError", "can't define singleton")
		}
		return sc
	})

	// Method#unbind → UnboundMethod.
	vm.cMethod.define("unbind", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		b := object.Kind[*BoundMethod](self)
		return &UnboundMethod{name: b.name, owner: b.m.owner, m: b.m}
	})
}

// checkBindable raises TypeError unless recv is an instance of the
// UnboundMethod's owner (Ruby's bind compatibility rule).
func (vm *VM) checkBindable(u *UnboundMethod, recv object.Value) {
	// An UnboundMethod taken from a per-object singleton class (the Trollop
	// `cloaker_` trick: `instance_method` on `class << self`) is owned by that
	// singleton class. classOf(recv) returns the regular class and never reaches
	// the singleton in its super chain, so also walk from recv's own singleton
	// class — recv is trivially an instance of it — before rejecting the bind.
	if classIsA(vm.classOf(recv), u.owner) {
		return
	}
	if sc := vm.objSingleton(recv); sc != nil && classIsA(sc, u.owner) {
		return
	}
	raise("TypeError", "bind argument must be an instance of %s", u.owner.name)
}
