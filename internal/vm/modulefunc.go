package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// registerModuleExtras installs the module/class authoring directives that real
// Ruby code (notably Puppet) leans on: module_function, the visibility setters
// (private/public/protected and their _class_method forms), alias_method, and
// the constant-visibility no-ops. The visibility setters record each method's
// access level (and the body default for the no-arg form); the send path
// enforces it (see visibility.go). module_function and alias_method are fully
// functional. Constant-visibility (private_constant) is still a no-op.
func (vm *VM) registerModuleExtras() {
	// module_function: with no args, switch the module body into function mode so
	// every subsequent `def` is also copied as a module/singleton method. With
	// args, convert the named instance methods now. Returns nil (no-arg) or the
	// arg list, matching MRI.
	vm.cModule.define("module_function", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := object.Kind[*RClass](self)
		if len(args) == 0 {
			mod.funcMode = true
			return object.NilVal()
		}
		for _, a := range args {
			name := nameArg(a)
			m := lookupMethod(mod, name)
			if m == nil || m.undefined {
				raise("NameError", "undefined method '%s' for module '%s'", name, mod.name)
			}
			// A module_function method is private as an instance method but public as
			// the module/singleton method: mark the instance copy private, and the
			// singleton copy public.
			vm.setInstanceVisibility(mod, name, visPrivate)
			sm := *m
			sm.vis = visPublic
			mod.smethods[name] = &sm
		}
		bumpMethodSerial()
		if len(args) == 1 {
			return args[0]
		}
		return object.Wrap(object.NewArrayFromSlice(append([]object.Value(nil), args...)))
	})

	// Visibility setters (private / public / protected). With no args they set the
	// body's default visibility for subsequent `def`s (mod.defaultVis, consulted by
	// OpDefineMethod). With args they set each named method's visibility — own or
	// inherited (an inherited method is recorded as a per-receiver override, see
	// setInstanceVisibility) — and return the single name, the arg list, or nil for
	// the no-arg form, as MRI does. `private def foo; end` passes the symbol the
	// def evaluates to, so the single-arg case covers it.
	setVis := func(vm *VM, self object.Value, args []object.Value, vis visibility) object.Value {
		mod := object.Kind[*RClass](self)
		if len(args) == 0 {
			mod.defaultVis = vis
			return object.NilVal()
		}
		// `private [:a, :b]` (an Array argument) marks each element, returning the
		// array — MRI accepts a single Array as well as a varargs name list.
		if len(args) == 1 {
			if arr, ok := object.KindOK[*object.Array](args[0]); ok {
				for _, a := range arr.Elems {
					vm.setInstanceVisibility(mod, nameArg(a), vis)
				}
				return args[0]
			}
		}
		for _, a := range args {
			vm.setInstanceVisibility(mod, nameArg(a), vis)
		}
		if len(args) == 1 {
			return args[0]
		}
		return object.Wrap(object.NewArrayFromSlice(append([]object.Value(nil), args...)))
	}
	vm.cModule.define("private", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return setVis(vm, self, args, visPrivate)
	})
	vm.cModule.define("public", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return setVis(vm, self, args, visPublic)
	})
	vm.cModule.define("protected", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return setVis(vm, self, args, visProtected)
	})

	// private_class_method / public_class_method: set the named class methods'
	// visibility — including ones inherited from Class such as `new`, recorded as a
	// per-receiver override (see setClassMethodVisibility). Returns self, as MRI.
	classMethodVisibility := func(vm *VM, self object.Value, args []object.Value, vis visibility) object.Value {
		mod := object.Kind[*RClass](self)
		for _, a := range args {
			vm.setClassMethodVisibility(mod, nameArg(a), vis)
		}
		return self
	}
	vm.cModule.define("private_class_method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return classMethodVisibility(vm, self, args, visPrivate)
	})
	vm.cModule.define("public_class_method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return classMethodVisibility(vm, self, args, visPublic)
	})

	// alias_method: the method form of `alias new old`, returning the new name as
	// a Symbol (MRI returns a Symbol since 3.0).
	vm.cModule.define("alias_method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := object.Kind[*RClass](self)
		newName, oldName := nameArg(args[0]), nameArg(args[1])
		vm.aliasMethod(mod, newName, oldName)
		return object.SymVal(string(object.Symbol(newName)))
	})

	// undef_method: the method form of `undef name`. It installs a tombstone that
	// hides any definition (own or inherited) so a call routes to method_missing.
	// Accepts one or more names and returns self.
	vm.cModule.define("undef_method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := object.Kind[*RClass](self)
		for _, a := range args {
			vm.undefMethod(mod, nameArg(a))
		}
		return object.Wrap(mod)
	})

	// remove_method: deletes the receiver's OWN definition of each name, leaving
	// any inherited method visible again. A name not defined directly on the
	// receiver raises NameError (MRI). Returns self.
	vm.cModule.define("remove_method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := object.Kind[*RClass](self)
		for _, a := range args {
			name := nameArg(a)
			if _, ok := mod.methods[name]; !ok {
				raise("NameError", "method '%s' not defined in %s", name, mod.ToS())
			}
			delete(mod.methods, name)
		}
		bumpMethodSerial()
		return object.Wrap(mod)
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
	{
		__sw98 := v
		switch {
		case object.IsKind[object.Symbol](__sw98):
			x := object.Kind[object.Symbol](__sw98)
			_ = x
			return string(x)
		case object.IsKind[*object.String](__sw98):
			x := object.Kind[*object.String](__sw98)
			_ = x
			return x.Str()
		default:
			x := __sw98
			_ = x
			raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
			return ""
		}
	}
}
