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
		mod := self.(*RClass)
		if len(args) == 0 {
			mod.funcMode = true
			return object.NilV
		}
		for _, a := range args {
			name := vm.defineMethodName(a)
			m := vm.lookupForModuleOp(mod, name)
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
		return object.NewArrayFromSlice(append([]object.Value(nil), args...))
	})

	// Visibility setters (private / public / protected). With no args they set the
	// body's default visibility for subsequent `def`s (mod.defaultVis, consulted by
	// OpDefineMethod). With args they set each named method's visibility — own or
	// inherited (an inherited method is recorded as a per-receiver override, see
	// setInstanceVisibility) — and return the single name, the arg list, or nil for
	// the no-arg form, as MRI does. `private def foo; end` passes the symbol the
	// def evaluates to, so the single-arg case covers it.
	setVis := func(vm *VM, self object.Value, args []object.Value, vis visibility) object.Value {
		mod := self.(*RClass)
		if len(args) == 0 {
			mod.defaultVis = vis
			return object.NilV
		}
		// `private [:a, :b]` (an Array argument) marks each element, returning the
		// array — MRI accepts a single Array as well as a varargs name list.
		if len(args) == 1 {
			if arr, ok := args[0].(*object.Array); ok {
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
		return object.NewArrayFromSlice(append([]object.Value(nil), args...))
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

	// Top-level private/public/protected: at the top level self is `main`, whose
	// default definee is Object, so `private :foo` (or a bare `private`) sets the
	// visibility of Object's instance methods — exactly as MRI's main object does
	// through private methods on its singleton class.
	if sc, ok := vm.ensureSingleton(vm.main); ok {
		for _, tv := range []struct {
			name string
			vis  visibility
		}{
			{"private", visPrivate},
			{"public", visPublic},
			{"protected", visProtected},
		} {
			vis := tv.vis
			sc.methods[tv.name] = &Method{name: tv.name, owner: sc,
				native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
					return setVis(vm, vm.cObject, args, vis)
				}}
		}
		// Top-level define_method also operates on Object, the default definee:
		// `define_method(:m){…}` defines Object#m by forwarding to the Module
		// method on Object.
		sc.methods["define_method"] = &Method{name: "define_method", owner: sc,
			native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
				return vm.send(vm.cObject, "define_method", args, blk)
			}}
	}

	// private_class_method / public_class_method: set the named class methods'
	// visibility — including ones inherited from Class such as `new`, recorded as a
	// per-receiver override (see setClassMethodVisibility). Returns self, as MRI.
	classMethodVisibility := func(vm *VM, self object.Value, args []object.Value, vis visibility) object.Value {
		mod := self.(*RClass)
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
		mod := self.(*RClass)
		if mod.frozen {
			vm.raiseFrozen(mod)
		}
		newName, oldName := vm.defineMethodName(args[0]), vm.defineMethodName(args[1])
		vm.aliasMethod(mod, newName, oldName)
		return object.Symbol(newName)
	})

	// undef_method: the method form of `undef name`. It installs a tombstone that
	// hides any definition (own or inherited) so a call routes to method_missing.
	// Accepts one or more names and returns self.
	vm.cModule.define("undef_method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := self.(*RClass)
		for _, a := range args {
			// MRI coerces each name (a TypeError on a non-name) BEFORE it consults
			// the receiver's frozen state, and a frozen check (per name) precedes the
			// existence check — so a bad name beats FrozenError, FrozenError beats the
			// missing-name NameError, and no arguments raise nothing at all.
			name := vm.defineMethodName(a)
			if mod.frozen {
				vm.raiseFrozen(mod)
			}
			// A name defined nowhere in the receiver's own+ancestor chain is a
			// NameError whose wording distinguishes a module from a class and names
			// the receiver by its #to_s (MRI). The default undefMethod message says
			// "class" for every receiver, so screen the miss here first.
			if m := lookupMethod(mod, name); m == nil || m.undefined {
				kind := "class"
				if mod.isModule {
					kind = "module"
				}
				raise("NameError", "undefined method '%s' for %s '%s'", name, kind, mod.ToS())
			}
			vm.undefMethod(mod, name)
		}
		return mod
	})

	// remove_method: deletes the receiver's OWN definition of each name, leaving
	// any inherited method visible again. A name not defined directly on the
	// receiver raises NameError (MRI). Returns self.
	vm.cModule.define("remove_method", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := self.(*RClass)
		for _, a := range args {
			// Same ordering as undef_method: coerce the name (TypeError) before the
			// frozen check, and raise nothing for a call with no arguments.
			name := vm.defineMethodName(a)
			if mod.frozen {
				vm.raiseFrozen(mod)
			}
			if _, ok := mod.methods[name]; !ok {
				raise("NameError", "method '%s' not defined in %s", name, mod.ToS())
			}
			delete(mod.methods, name)
		}
		bumpMethodSerial()
		return mod
	})

	// Constant-visibility directives: not enforced (constants are not access-
	// controlled here), so accept the names and return self, as MRI does.
	constVisibility := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	}
	vm.cModule.define("private_constant", constVisibility)
	vm.cModule.define("public_constant", constVisibility)

	// Module#deprecate_constant(*names): mark existing constants so that reading
	// them warns (when Warning[:deprecated] is on). An undefined name is a
	// NameError.
	vm.cModule.define("deprecate_constant", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := self.(*RClass)
		for _, a := range args {
			name := nameArg(a)
			if _, ok := mod.consts[name]; !ok {
				raise("NameError", "constant %s not defined", scopedNameFor(mod, name))
			}
			if mod.deprecatedConsts == nil {
				mod.deprecatedConsts = map[string]bool{}
			}
			mod.deprecatedConsts[name] = true
		}
		return mod
	})

	// module_function and the bare visibility directives are PRIVATE instance
	// methods of Module (MRI): usable as a functional call inside a class/module
	// body but not as `mod.private(:x)` through an explicit receiver.
	for _, n := range []string{"module_function", "private", "public", "protected"} {
		vm.cModule.methods[n].vis = visPrivate
	}
}

// warnDeprecatedConst emits MRI's "constant X::Y is deprecated" warning when a
// deprecated constant is read, but only while Warning[:deprecated] is enabled.
func (vm *VM) warnDeprecatedConst(scope *RClass, name string) {
	w := vm.consts["Warning"]
	if !vm.send(w, "[]", []object.Value{object.Symbol("deprecated")}, nil).Truthy() {
		return
	}
	vm.send(w, "warn", []object.Value{
		object.NewString("warning: constant " + scopedNameFor(scope, name) + " is deprecated\n"),
	}, nil)
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

// defineMethodName coerces define_method's name argument. A Symbol or String is
// taken directly; any other object is converted through #to_str, and a #to_str
// that returns a non-String raises TypeError with MRI's "can't convert" message.
// An object without #to_str raises the "is not a symbol nor a string" TypeError.
func (vm *VM) defineMethodName(v object.Value) string {
	switch x := v.(type) {
	case object.Symbol:
		return string(x)
	case *object.String:
		return x.Str()
	}
	if vm.respondsToDynamic(v, "to_str") {
		r := vm.send(v, "to_str", nil, nil)
		if s, ok := r.(*object.String); ok {
			return s.Str()
		}
		raise("TypeError", "can't convert %s to String (%s#to_str gives %s)",
			classNameOf(v), classNameOf(v), classNameOf(r))
	}
	raise("TypeError", "%s is not a symbol nor a string", vm.inspectStr(v))
	return ""
}

// defineMethodVis is the visibility a method created by define_method receives.
// :initialize and :initialize_copy are always private. Otherwise the method takes
// the receiver's current default visibility: MRI uses the caller frame's default
// visibility only when the definee equals the receiver module, and public
// otherwise — and a `private`/`public` directive lands on its frame's self, so the
// receiver's own defaultVis carries the directive exactly in that "definee equals
// receiver" case (a class/module body or a class_eval on the receiver) and stays
// at the receiver's untouched default when the directive was issued elsewhere.
func (vm *VM) defineMethodVis(cls *RClass, name string) visibility {
	if name == "initialize" || name == "initialize_copy" {
		return visPrivate
	}
	return cls.defaultVis
}

// checkTransplantBindable raises TypeError when a Method/UnboundMethod whose
// method is owned by owner cannot be re-homed onto cls by define_method. A method
// owned by a Class (or a singleton class) may only move onto that class or one of
// its subclasses; a method owned by an ordinary Module may move anywhere. The
// singleton case carries MRI's distinct "different class" message.
func (vm *VM) checkTransplantBindable(cls, owner *RClass) {
	if owner == nil || owner.isModule {
		return
	}
	// rbgo models the Kernel methods (instance_of?, respond_to?, …) as Object
	// instance methods, so an UnboundMethod pulled from Object reports Object as its
	// owner where MRI reports Kernel (a Module). Treat the universal roots as
	// permissive owners so such a method can be re-homed onto any class, including a
	// BasicObject subclass — matching MRI's module-owner rule.
	if owner == vm.cObject || owner == vm.cBasicObject {
		return
	}
	if classIsA(cls, owner) {
		return
	}
	if owner.isSingleton {
		raise("TypeError", "can't bind singleton method to a different class")
	}
	raise("TypeError", "bind argument must be a subclass of %s", owner.name)
}

// fireMethodAdded invokes cls.method_added(:name) when cls defines that hook as a
// singleton method, mirroring the OpDefineMethod path for `def`.
func (vm *VM) fireMethodAdded(cls *RClass, name string) {
	if hook := lookupSMethod(cls, "method_added"); hook != nil {
		vm.invoke(hook, cls, []object.Value{object.SymVal(name)}, nil)
	}
}
