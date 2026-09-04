package vm

import (
	"reflect"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// UnboundMethod is a method detached from any receiver, produced by
// Module#instance_method or Method#unbind. It is re-bound to a compatible
// receiver with #bind / #bind_call.
type UnboundMethod struct {
	name  string
	owner *RClass // the module/class the method was extracted from
	m     *Method
	// vm is the interpreter the method belongs to, kept so ToS can render the
	// method's parameters (MRI's #<UnboundMethod: …> form).
	vm *VM
}

func (u *UnboundMethod) ToS() string {
	return u.vm.formatCallableString("UnboundMethod", nil, u.name, u.m)
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
		mod := self.(*RClass)
		name := nameArg(args[0])
		m := vm.lookupForModuleOp(mod, name)
		if m == nil || m.undefined {
			raise("NameError", "undefined method '%s' for class '%s'", name, mod.name)
		}
		return &UnboundMethod{name: name, owner: m.owner, m: m, vm: vm}
	})

	// Module#public_instance_method(:m): like #instance_method, but the resolved
	// method must be public — a private or protected one raises NameError.
	vm.cModule.define("public_instance_method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := self.(*RClass)
		name := nameArg(args[0])
		m := vm.lookupForModuleOp(mod, name)
		if m == nil || m.undefined {
			raise("NameError", "undefined method '%s' for class '%s'", name, mod.name)
		}
		if vis := instanceVisibility(mod, name, m); vis != visPublic {
			kind := "private"
			if vis == visProtected {
				kind = "protected"
			}
			raise("NameError", "method '%s' for class '%s' is %s", name, mod.name, kind)
		}
		return &UnboundMethod{name: name, owner: m.owner, m: m, vm: vm}
	})

	// Module#singleton_class?: true when the receiver is a singleton class — a
	// per-object singleton or a class metaclass — false for an ordinary class or
	// module.
	vm.cModule.define("singleton_class?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*RClass).isSingleton)
	})

	// Module#set_temporary_name(name) gives an anonymous module a temporary name
	// (or clears it with nil) without making it permanent, so a later constant
	// assignment can still name it. A permanently-named module, a constant path,
	// or an empty string is rejected the way MRI does.
	vm.cModule.define("set_temporary_name", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := self.(*RClass)
		if mod.named {
			raise("RuntimeError", "can't change permanent name")
		}
		if object.IsNil(args[0]) {
			mod.name = ""
			return mod
		}
		name := strArg(args[0])
		switch {
		case name == "":
			raise("ArgumentError", "empty class/module name")
		case strings.Contains(name, "::") || constNameWellFormed(name):
			// A name that reads as a constant (or a constant path) is rejected, so a
			// temporary name is never mistaken for a real, permanently-assigned one.
			raise("ArgumentError", "the temporary name must not be a constant path to avoid confusion")
		}
		mod.name = name
		return mod
	})

	// Module#method_defined?(:m): true if m resolves up the ancestor chain.
	vm.cModule.define("method_defined?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := self.(*RClass)
		m := vm.lookupForModuleOp(mod, nameArg(args[0]))
		return object.Bool(m != nil && !m.undefined)
	})

	// Module#public_method_defined?/#private_method_defined?/#protected_method_defined?:
	// like #method_defined? but additionally require the resolved method's
	// effective access level (honouring any inherited visibility override) to
	// match. Used by Puppet's metaprogramming to probe accessor visibility.
	definedWithVis := func(want visibility) NativeFn {
		return func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			mod := self.(*RClass)
			name := nameArg(args[0])
			m := vm.lookupForModuleOp(mod, name)
			if m == nil || m.undefined {
				return object.False
			}
			return object.Bool(instanceVisibility(mod, name, m) == want)
		}
	}
	vm.cModule.define("public_method_defined?", definedWithVis(visPublic))
	vm.cModule.define("private_method_defined?", definedWithVis(visPrivate))
	vm.cModule.define("protected_method_defined?", definedWithVis(visProtected))

	// Two UnboundMethods are equal when they wrap the same underlying method
	// definition extracted from the same class — so an alias (which shares the
	// original Method record) compares equal to its original, matching MRI.
	unboundEq := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		u := self.(*UnboundMethod)
		o, ok := args[0].(*UnboundMethod)
		return object.Bool(ok && u.owner == o.owner && methodDefKey(u.m) == methodDefKey(o.m))
	}
	cUnbound.define("==", unboundEq)
	// UnboundMethod#eql? is an alias of UnboundMethod#== (shared record).
	aliasBuiltin(cUnbound, "eql?", "==")
	cUnbound.define("hash", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		u := self.(*UnboundMethod)
		return object.IntValue(int64(reflect.ValueOf(u.owner).Pointer()) ^ int64(methodDefKey(u.m)))
	})
	cUnbound.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(self.(*UnboundMethod).name)
	})
	cUnbound.define("owner", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*UnboundMethod).owner
	})
	cUnbound.define("arity", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(methodArity(self.(*UnboundMethod).m)))
	})
	// UnboundMethod#bind(obj) → Method; obj must be a kind_of? the owner.
	cUnbound.define("bind", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		u := self.(*UnboundMethod)
		vm.checkBindable(u, args[0])
		return vm.newBoundMethod(args[0], u.name, u.m)
	})
	// UnboundMethod#bind_call(obj, *args, &blk): bind then invoke in one step.
	cUnbound.define("bind_call", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		u := self.(*UnboundMethod)
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
		b := self.(*BoundMethod)
		return &UnboundMethod{name: b.name, owner: b.m.owner, m: b.m, vm: vm}
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
	// A method whose owner is a Module (not a Class) may be bound to ANY
	// object — MRI only enforces the kind_of? rule for methods owned by a
	// Class. This covers `Mod.instance_method(:foo).bind(anything)` and Kernel
	// methods (`Object.instance_method(:instance_of?)`) bound onto unrelated
	// receivers such as a BasicObject.
	if u.owner != nil && u.owner.isModule {
		return
	}
	if classIsA(vm.classOf(recv), u.owner) {
		return
	}
	if sc := vm.objSingleton(recv); sc != nil && classIsA(sc, u.owner) {
		return
	}
	raise("TypeError", "bind argument must be an instance of %s", u.owner.name)
}
