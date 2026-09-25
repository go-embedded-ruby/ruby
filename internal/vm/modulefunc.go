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
		// vm_method.c v3_4_0 rb_mod_modfunc opens with
		// `if (!RB_TYPE_P(mod, T_MODULE)) rb_raise(rb_eTypeError,
		// "module_function must be called for modules");` — reachable by binding
		// the UnboundMethod to a Class, since Class#module_function is undefined
		// rather than absent.
		if !mod.isModule {
			raise("TypeError", "module_function must be called for modules")
		}
		if len(args) == 0 {
			mod.funcMode = true
			return object.NilV
		}
		for _, a := range args {
			name := vm.defineMethodName(a)
			m := vm.lookupForModuleOp(mod, name)
			if m == nil || m.undefined {
				// rb_mod_modfunc's search is `me = search_method(m, id, 0); if (!me)
				// me = search_method(rb_cObject, id, 0);` — a name the module itself
				// does not carry is looked up on Object, which is how
				// `Module.new { module_function :require }` reaches Kernel#require.
				m = vm.lookupForModuleOp(vm.cObject, name)
			}
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
		for _, a := range visNameList(args) {
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
		// Bare `include M` at the top level mixes M into Object, so its constants
		// and methods become globally visible. MRI defines this as a PRIVATE
		// singleton method on main that forwards to Module#include on Object —
		// eval.c top_include (ruby/ruby v3_4_0:1862-1866) and its registration
		// at :2152-2154 — and it returns Object, rb_mod_include's return.
		// It has to live on main's singleton because this VM also publishes an
		// RSpec `include(...)` MATCHER as an Object instance method, which
		// otherwise shadows the real one for every receiver, main included, and
		// made `include M` a silent no-op returning a matcher. A singleton method
		// wins over Object's, so the two are told apart by their arguments: a
		// non-empty list of modules is the language construct, anything else is
		// the matcher, reached directly since this method now hides it from main.
		// The two only collide for main — inside an example self is the spec
		// context, not main — and the arguments never overlap, because MRI's
		// top-level include takes modules and nothing else (a non-module is a
		// TypeError, and no argument at all an ArgumentError).
		sc.methods["include"] = &Method{name: "include", owner: sc, vis: visPrivate,
			native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
				if len(args) > 0 && allModuleArgs(args) {
					return vm.send(vm.cObject, "include", args, nil)
				}
				return vm.invoke(vm.cObject.methods["include"], self, args, blk)
			}}
	}

	// private_class_method / public_class_method: set the named class methods'
	// visibility — including ones inherited from Class such as `new`, recorded as a
	// per-receiver override (see setClassMethodVisibility). Returns self, as MRI.
	classMethodVisibility := func(vm *VM, self object.Value, args []object.Value, vis visibility) object.Value {
		mod := self.(*RClass)
		for _, a := range visNameList(args) {
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

	// Module#ruby2_keywords(name, ...) marks the named methods so a trailing
	// keyword hash passed to them flows through their *rest as a flagged hash.
	// MRI's rb_mod_ruby2_keywords (ruby/ruby v3_4_0 vm_method.c:2568) checks the
	// arity (1+), the receiver's frozen state, then for each name: coerce it
	// (rb_check_id — TypeError for anything but a Symbol/String/#to_str),
	// resolve it on the receiver, falling back to Object for a module receiver,
	// and raise NameError when it resolves nowhere. A name the receiver does not
	// itself define, one whose body is not Ruby, and one whose parameters cannot
	// carry the flag each WARN instead of raising, through rb_warn — so the
	// warning shows at $VERBOSE == false and is silenced only by nil. It returns
	// nil whatever happened.
	//
	// The flag itself is not carried yet: MRI writes
	// ISEQ_BODY(...)->param.flags.ruby2_keywords, which rbgo's bytecode.ISeq has
	// no field for, and honouring it belongs to the splat binding. So a markable
	// method is accepted silently and behaves as it did before — the same
	// partial contract Proc#ruby2_keywords already ships in this VM. What IS
	// decided here is every observable that does not depend on the flag: the
	// method exists (a program guarded by `respond_to?(:ruby2_keywords, true)`
	// no longer dies on NoMethodError), the arity/TypeError/NameError contract,
	// and the four warnings.
	vm.cModule.define("ruby2_keywords", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := self.(*RClass)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		if mod.frozen {
			vm.raiseFrozen(mod)
		}
		skip := func(name, why string) {
			vm.rbWarn("warning: Skipping set of ruby2_keywords flag for %s (%s)", name, why)
		}
		for _, a := range args {
			name := vm.coerceNameArg(a)
			m := vm.lookupForModuleOp(mod, name)
			if m == nil && mod.isModule {
				m = vm.lookupForModuleOp(vm.cObject, name)
			}
			if m == nil || m.undefined {
				vm.raiseNameError("undefined method '"+name+"' for "+vm.moduleDescription(mod), name)
			}
			// An own entry that is an `undef` tombstone never reaches here: the
			// lookup above finds it first and the UNDEFINED_METHOD_ENTRY_P check
			// has already raised.
			switch own, isOwn := mod.methods[name]; {
			case !isOwn:
				// MRI compares the resolved entry's defined_class with the receiver
				// (and its origin): a method reached through an ancestor cannot be
				// marked from here.
				skip(name, "can only set in method defining module")
			case methodISeq(own) == nil:
				skip(name, "method not defined in Ruby")
			case !procRuby2KeywordsMarkable(methodISeq(own)):
				// has_rest and neither has_kw nor has_kwrest — and, since Ruby 4.0,
				// no post-splat positional either, which is also why 4.0 names post
				// arguments in the text where 3.4 did not.
				skip(name, "method accepts keywords or post arguments or method does not accept argument splat")
			}
		}
		return object.NilV
	})
	// rb_define_private_method(rb_cModule, "ruby2_keywords", ...): it is written
	// as a directive in a class or module body, never through a receiver.
	vm.setInstanceVisibility(vm.cModule, "ruby2_keywords", visPrivate)

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

	// Constant-visibility directives. MRI validates that every named constant is
	// defined DIRECTLY on the receiver — an inherited or missing name is a
	// NameError — then flips the entry's CONST_VISIBILITY_MASK bits and returns
	// self. Each name is a String or Symbol.
	// A pending autoload counts as defined: MRI's set_const_visibility finds it
	// through rb_const_lookup, which returns the entry autoload_synchronized
	// reserved with an undefined value. Reference: ruby/ruby v3_4_0 variable.c
	// set_const_visibility (:3749-3786) / rb_mod_private_constant (:3815) /
	// rb_mod_public_constant (:3829).
	// The flag is enforced on the qualified `Recv::NAME` path only — see
	// scopedConst and privateConstReferenced. An unqualified (lexical) read from
	// inside the module, or from a class that includes it, still resolves, which
	// is why MRI screens visibility in rb_public_const_get_from alone.
	constVisibility := func(private bool) NativeFn {
		return func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			mod := self.(*RClass)
			for _, a := range args {
				name := nameArg(a)
				if _, ok := mod.consts[name]; !ok && !hasAutoload(mod, name) {
					raise("NameError", "constant %s not defined", scopedNameFor(mod, name))
				}
				if mod.privateConsts == nil {
					mod.privateConsts = map[string]bool{}
				}
				mod.privateConsts[name] = private
			}
			return self
		}
	}
	vm.cModule.define("private_constant", constVisibility(true))
	vm.cModule.define("public_constant", constVisibility(false))

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
	// Class UNDEFINES module_function (MRI: rb_undef_method(rb_cClass,
	// "module_function") in Init_eval), so it does not appear in
	// Class.private_instance_methods(true) although Module still carries it.
	vm.cClass.methods["module_function"] = &Method{
		name: "module_function", owner: vm.cClass, vis: visPrivate, undefined: true}
}

// mirrorModuleFunction applies a module body's module_function toggle to a
// method that Module#define_method just installed: the instance copy becomes
// private and a public copy lands on the module's singleton, exactly as a `def`
// in the same body would. vm_method.c v3_4_0 rb_mod_define_method reads the same
// scope visibility a def does — `if (scope_visi->module_func)
// rb_method_entry_set(rb_singleton_class(mod), id, me, METHOD_VISI_PUBLIC);`
// with the instance entry taking scope_visi->method_visi, which the toggle set
// to private. Witnessed on 4.0.5: a define_method'd name shows up in both
// private_instance_methods(false) and singleton_methods(false).
func (vm *VM) mirrorModuleFunction(cls *RClass, name string) {
	if !cls.funcMode {
		return
	}
	// Every call site invokes this immediately after storing the method, so the
	// entry is always there.
	m := cls.methods[name]
	m.vis = visPrivate
	sm := *m
	sm.vis = visPublic
	cls.smethods[name] = &sm
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

// allModuleArgs reports whether every argument is a Module or Class — what MRI's
// top-level include accepts, and what the RSpec include matcher is never called
// with. It tells the two apart on main; see the singleton `include` above.
func allModuleArgs(args []object.Value) bool {
	for _, a := range args {
		if _, ok := a.(*RClass); !ok {
			return false
		}
	}
	return true
}

// visNameList expands the argument list of a visibility directive into the
// method names it names. MRI's set_method_visibility (ruby/ruby v3_4_0
// vm_method.c:2400) treats a SINGLE Array argument as the list itself —
// `private [:a, :b]` and `private_class_method [:foo]` mark each element —
// and any other shape as a varargs name list. It is one rule serving every
// directive that routes through set_method_visibility: private / public /
// protected (through set_visibility) and private_class_method /
// public_class_method (rb_mod_private_method / rb_mod_public_method), so it
// lives here once rather than at each call site.
func visNameList(args []object.Value) []object.Value {
	if len(args) == 1 {
		if arr, ok := args[0].(*object.Array); ok {
			return arr.Elems
		}
	}
	return args
}
