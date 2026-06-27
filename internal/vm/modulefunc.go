package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// registerModuleExtras installs the module/class authoring directives that real
// Ruby code (notably Puppet) leans on: module_function, the visibility setters
// (private/public/protected and their _class_method forms), alias_method, and
// the constant-visibility no-ops. Method visibility itself is not enforced by
// this VM, so the visibility setters validate and return the MRI value but do
// not restrict dispatch; module_function and alias_method are fully functional.
func (vm *VM) registerModuleExtras() {
	// module_function: with no args, switch the module body into function mode so
	// every subsequent `def` is also copied as a module/singleton method. With
	// args, convert the named instance methods now. Returns nil (no-arg) or the
	// arg list, matching MRI.
	vm.cModule.define("module_function", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := self.(*RClass)
		if len(args) == 0 {
			mod.funcMode = true
			return object.NilV
		}
		for _, a := range args {
			name := nameArg(a)
			m := lookupMethod(mod, name)
			if m == nil || m.undefined {
				raise("NameError", "undefined method '%s' for module '%s'", name, mod.name)
			}
			sm := *m
			mod.smethods[name] = &sm
		}
		bumpMethodSerial()
		if len(args) == 1 {
			return args[0]
		}
		return &object.Array{Elems: append([]object.Value(nil), args...)}
	})

	// Visibility setters. With no args they would set the body's default
	// visibility (not modelled — every method stays callable); with args they
	// return the single name, the arg list, or nil for the no-arg form, as MRI
	// does. They accept names/symbols and validate that each method exists.
	visibility := func(self object.Value, args []object.Value) object.Value {
		mod := self.(*RClass)
		for _, a := range args {
			name := nameArg(a)
			if m := lookupMethod(mod, name); m == nil || m.undefined {
				raise("NameError", "undefined method '%s' for class '%s'", name, mod.name)
			}
		}
		switch len(args) {
		case 0:
			return object.NilV
		case 1:
			return args[0]
		default:
			return &object.Array{Elems: append([]object.Value(nil), args...)}
		}
	}
	vm.cModule.define("private", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return visibility(self, args)
	})
	vm.cModule.define("public", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return visibility(self, args)
	})
	vm.cModule.define("protected", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return visibility(self, args)
	})

	// private_class_method / public_class_method: validate the named class
	// methods exist (visibility itself is not enforced). Returns self, as MRI.
	classMethodVisibility := func(self object.Value, args []object.Value) object.Value {
		mod := self.(*RClass)
		for _, a := range args {
			name := nameArg(a)
			if lookupSMethod(mod, name) == nil {
				raise("NameError", "undefined method '%s' for class '%s'", name, mod.name)
			}
		}
		return self
	}
	vm.cModule.define("private_class_method", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return classMethodVisibility(self, args)
	})
	vm.cModule.define("public_class_method", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return classMethodVisibility(self, args)
	})

	// alias_method: the method form of `alias new old`, returning the new name as
	// a Symbol (MRI returns a Symbol since 3.0).
	vm.cModule.define("alias_method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := self.(*RClass)
		newName, oldName := nameArg(args[0]), nameArg(args[1])
		vm.aliasMethod(mod, newName, oldName)
		return object.Symbol(newName)
	})

	// Constant-visibility directives: not enforced (constants are not access-
	// controlled here), so accept the names and return self, as MRI does.
	constVisibility := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	}
	vm.cModule.define("private_constant", constVisibility)
	vm.cModule.define("public_constant", constVisibility)
}

// nameArg coerces a method-name argument (a String or Symbol) to its string,
// raising TypeError for anything else (matching MRI's "not a symbol nor a
// string").
func nameArg(v object.Value) string {
	switch x := v.(type) {
	case object.Symbol:
		return string(x)
	case *object.String:
		return x.Str()
	default:
		raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
		return ""
	}
}
