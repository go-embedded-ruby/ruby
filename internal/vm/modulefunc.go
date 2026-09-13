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
			// A bare public/private/protected also cancels a module_function toggle:
			// MRI keeps one scope visibility per frame, and rb_scope_visibility_set
			// writes both halves of it — the level AND the module_function flag — so
			// the later directive wins outright. Reference: ruby/ruby v3_4_0
			// vm_method.c rb_scope_visibility_set / rb_mod_modfunc's
			// SCOPE_SET(METHOD_VISI_PUBLIC | SCOPE_VISI_MODULE_FUNC).
			mod.defaultVis, mod.funcMode = vis, false
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
		// A handful of names (the initialize family and respond_to_missing?) are
		// always private in MRI, whatever the source method's visibility — defining
		// one under such a name, alias included, forces it private (vm_method.c
		// check_definition_visibility / rb_scope_visibility_set special-cases).
		if alwaysPrivateName(newName) {
			// aliasMethod always installs mod.methods[newName], so it is present here.
			mod.methods[newName].vis = visPrivate
		}
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
			if !vm.resolvableForUndef(mod, name) {
				kind := "class"
				if mod.isModule {
					kind = "module"
				}
				// A class/module metaclass names the class it is the metaclass of
				// ("String", not "#<Class:String>") in this error, matching MRI's
				// rb_class_name resolution for the receiver.
				recv := vm.moduleToSStr(mod)
				if mod.isSingleton && mod.metaOf != nil {
					recv = vm.moduleToSStr(mod.metaOf)
				}
				raise("NameError", "undefined method '%s' for %s '%s'", name, kind, recv)
			}
			vm.undefMethod(mod, name)
			// undefMethod fires singleton_method_undefined for a singleton class; for
			// an ordinary class/module the corresponding hook is Module#method_undefined.
			if !mod.isSingleton {
				vm.fireModuleMethodHook(mod, "method_undefined", name)
			}
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
			bumpMethodSerial()
			if mod.isSingleton {
				// Removing a method from a singleton class (class << obj; remove_method :m)
				// fires singleton_method_removed on the attached object.
				vm.fireSingletonMethodHook(vm.attachedObject(mod), "singleton_method_removed", name)
			} else {
				// An ordinary class/module fires Module#method_removed instead.
				vm.fireModuleMethodHook(mod, "method_removed", name)
			}
		}
		return mod
	})

	// Constant-visibility directives: the access control itself is not enforced
	// (reads are not screened here), but MRI validates that every named constant is
	// defined DIRECTLY on the receiver — an inherited or missing name is a NameError
	// — before returning self. Each name is a String or Symbol.
	// A pending autoload counts as defined: MRI's set_const_visibility finds it
	// through rb_const_lookup, which returns the entry autoload_synchronized
	// reserved with an undefined value. Reference: ruby/ruby v3_4_0 variable.c
	// set_const_visibility / rb_mod_private_constant.
	constVisibility := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := self.(*RClass)
		for _, a := range args {
			name := nameArg(a)
			if _, ok := mod.consts[name]; !ok && !hasAutoload(mod, name) {
				raise("NameError", "constant %s not defined", scopedNameFor(mod, name))
			}
		}
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
			// A pending autoload is a reserved (undefined-valued) constant entry in
			// MRI, so deprecate_constant accepts it exactly as private_constant does.
			if _, ok := mod.consts[name]; !ok && !hasAutoload(mod, name) {
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
		object.NewString("warning: constant " + vm.qualifiedConstName(scope, name) + " is deprecated\n"),
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

// coerceNameArg converts a name argument (a method, constant or class-variable
// name) to its string form the way MRI's rb_check_id/rb_to_id does: a Symbol or
// String is taken directly, and any other object is converted through #to_str.
// A missing #to_str raises TypeError "X is not a symbol nor a string"; a #to_str
// that returns a non-String raises "can't convert A to String (A#to_str gives
// B)". It performs no name-shape validation — callers that need it (const/cvar)
// layer their own check on the result. Reference: ruby/ruby v3_4_0 vm_method.c
// rb_check_id / rb_to_id and variable.c rb_check_id_cstr callers.
func (vm *VM) coerceNameArg(v object.Value) string {
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
		cn := vm.classOf(v).name
		raise("TypeError", "can't convert %s to String (%s#to_str gives %s)",
			cn, cn, vm.classOf(r).name)
	}
	raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
	return ""
}

// alwaysPrivateName reports whether name is one that MRI keeps private no matter
// how it is defined — the initialize family and respond_to_missing? (vm_method.c
// forces their visibility to private). method_missing is deliberately NOT here:
// MRI leaves it at the caller's visibility.
func alwaysPrivateName(name string) bool {
	switch name {
	case "initialize", "initialize_copy", "initialize_clone", "initialize_dup", "respond_to_missing?":
		return true
	}
	return false
}

// coerceToString converts v to a Go string through MRI's implicit String
// conversion (#to_str): a String is taken directly, another object is sent
// #to_str, and a missing #to_str or a non-String result raises TypeError "no
// implicit conversion of X into String". Used where MRI applies rb_to_str /
// rb_check_string_type (e.g. the eval-string and filename of Module#module_eval).
func (vm *VM) coerceToString(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return s.Str()
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", classNameOf(v))
	return ""
}

// coerceConstName is coerceNameArg followed by MRI's constant-name shape check
// (an uppercase first letter): the name must read as a constant or a NameError
// "wrong constant name X" is raised. Used by Module#const_set / #remove_const.
func (vm *VM) coerceConstName(v object.Value) string {
	name := vm.coerceNameArg(v)
	if !constNameWellFormed(name) {
		raise("NameError", "wrong constant name %s", name)
	}
	return name
}

// coerceCvarName is coerceNameArg followed by MRI's class-variable-name shape
// check (a "@@" prefix and at least one further character), raising NameError
// otherwise. Used by Module#class_variable_get / _set / _defined?.
func (vm *VM) coerceCvarName(v object.Value) string {
	name := vm.coerceNameArg(v)
	if len(name) < 3 || name[0] != '@' || name[1] != '@' {
		raise("NameError", "`%s' is not allowed as a class variable name", name)
	}
	return name
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
	vm.fireModuleMethodHook(cls, "method_added", name)
}

// fireModuleMethodHook invokes cls.<hook>(:name) when cls defines the hook as a
// class/singleton method (e.g. def self.method_removed) — the definition-tracking
// hooks method_added / method_removed / method_undefined. The default is a private
// no-op, so only a user override is observable. Reference: ruby/ruby v3_4_0
// vm_method.c rb_add_method / remove_method / undef_method calling the hooks.
func (vm *VM) fireModuleMethodHook(cls *RClass, hook, name string) {
	if h := lookupSMethod(cls, hook); h != nil {
		vm.invoke(h, cls, []object.Value{object.SymVal(name)}, nil)
	}
}

// attachedObject returns the object a singleton class belongs to — the class
// itself for a metaclass (def self.foo lands on its owner's class methods), and
// the recorded object for a per-object singleton class. It is the receiver of the
// singleton_method_added/removed/undefined hooks for defs made in that class.
func (vm *VM) attachedObject(sc *RClass) object.Value {
	if sc.metaOf != nil {
		return sc.metaOf
	}
	return sc.attached
}

// fireMethodDefined runs the definition hook for a method just installed on cls:
// singleton_method_added on the attached object when cls is a singleton class
// (per-object singleton or a class metaclass), and method_added otherwise — the
// same split MRI makes.
func (vm *VM) fireMethodDefined(cls *RClass, name string) {
	if cls.isSingleton {
		vm.fireSingletonMethodHook(vm.attachedObject(cls), "singleton_method_added", name)
		return
	}
	vm.fireMethodAdded(cls, name)
}

// fireSingletonMethodHook invokes recv.<hook>(:name) — one of
// singleton_method_added / singleton_method_removed / singleton_method_undefined
// — when defining, removing, or undefining a singleton method of recv. MRI fires
// these on the object that owns the singleton method. The BasicObject default is
// a private no-op, so the hook is dispatched only when recv provides an override
// (owner is not BasicObject) or has had the hook undefined — in which case the
// send routes to method_missing, raising NoMethodError by default, exactly as MRI
// does for an undef'd hook.
func (vm *VM) fireSingletonMethodHook(recv object.Value, hook, name string) {
	m := vm.resolveSingletonHook(recv, hook)
	if m == nil || (!m.undefined && m.owner == vm.cBasicObject) {
		// No hook resolves, or it is the BasicObject default no-op: nothing observable.
		return
	}
	if m.undefined {
		// The hook was undef'd on recv, so the call routes to method_missing — which
		// raises NoMethodError by default. Invoke it directly (rather than through
		// send) because an undef'd class method is found as a tombstone by
		// lookupSMethod, which send would invoke instead of routing to method_missing.
		mm := vm.resolveSingletonHook(recv, "method_missing")
		vm.invoke(mm, recv, []object.Value{object.SymVal(hook), object.SymVal(name)}, nil)
		return
	}
	vm.invoke(m, recv, []object.Value{object.SymVal(name)}, nil)
}

// resolveSingletonHook finds the method recv would dispatch for a hook name,
// mirroring send's resolution order (a class receiver's singleton/class methods
// first, then a per-object singleton class, then the instance-method chain) but
// returning an undef'd tombstone rather than nil so callers can tell a removed
// hook apart from the BasicObject default.
func (vm *VM) resolveSingletonHook(recv object.Value, name string) *Method {
	if cls, ok := recv.(*RClass); ok {
		if m := lookupSMethod(cls, name); m != nil {
			return m
		}
	}
	c := vm.classOf(recv)
	if sc := vm.objSingleton(recv); sc != nil {
		c = sc
	}
	return lookupMethod(c, name)
}
