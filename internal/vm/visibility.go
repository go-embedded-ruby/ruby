package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Method visibility (private/protected/public) — recording on `def`, the
// visibility directives, and enforcement on the explicit-receiver send path.
//
// The model mirrors MRI: every method has an access level (public default); a
// directive can set the access level of an own method, of an inherited method
// (recorded as a per-receiver override so the shared ancestor Method is never
// mutated), or — bare, with no args — the default level for subsequent defs in
// the body. Enforcement happens only when a method is invoked through an
// explicit receiver (`obj.foo`); an implicit-receiver call (`foo`, and the
// special case `self.foo`) bypasses the private check, and protected is allowed
// when the caller's self is_a? the method's owner.

// instanceVisibility reports the effective access level of an instance method
// named name as resolved on recvClass: a per-receiver override (set by a
// visibility directive on an inherited method) wins, else the resolved Method's
// own vis, walking the ancestor chain so an override on any ancestor counts.
func instanceVisibility(recvClass *RClass, name string, m *Method) visibility {
	for c := recvClass; c != nil; c = c.super {
		if v, ok := c.visOverrides[name]; ok {
			return v
		}
		// Stop at the class that actually owns the resolved method: an override
		// must be on it or a more-derived class to apply (a base class can't make
		// a subclass-introduced method private).
		if m != nil && c == m.owner {
			break
		}
	}
	if m != nil {
		return m.vis
	}
	return visPublic
}

// classMethodVisibilityOf reports the effective access level of a class
// (singleton) method named name on cls: a per-receiver svisOverride wins
// (walking the superclass chain, since class methods are inherited), else the
// resolved Method's own vis.
func classMethodVisibilityOf(cls *RClass, name string, m *Method) visibility {
	for c := cls; c != nil; c = c.super {
		if v, ok := c.svisOverrides[name]; ok {
			return v
		}
		if m != nil && c == m.owner {
			break
		}
	}
	if m != nil {
		return m.vis
	}
	return visPublic
}

// setInstanceVisibility records vis for the named instance method on mod. If mod
// defines the method itself, the Method's own vis is set; otherwise — the method
// is inherited — a per-receiver override is recorded so the ancestor Method is
// left untouched. A name that resolves nowhere raises NameError, as MRI does.
func (vm *VM) setInstanceVisibility(mod *RClass, name string, vis visibility) {
	// Inside `class << SomeClass`, the definee is SomeClass's metaclass; an
	// instance-visibility directive there governs SomeClass's CLASS methods, so
	// route it to the class-method override chain (this is how
	// `class << self; private :new; end` makes the inherited Class#new private).
	if mod.metaOf != nil {
		vm.setClassMethodVisibility(mod.metaOf, name, vis)
		return
	}
	if own, ok := mod.methods[name]; ok && !own.undefined {
		own.vis = vis
		bumpMethodSerial()
		return
	}
	if vm.lookupForModuleOp(mod, name) == nil {
		raise("NameError", "undefined method '%s' for class '%s'", name, mod.name)
	}
	if mod.visOverrides == nil {
		mod.visOverrides = map[string]visibility{}
	}
	mod.visOverrides[name] = vis
	bumpMethodSerial()
}

// setClassMethodVisibility records vis for the named class (singleton) method on
// mod. If mod defines it as its own class method the Method's vis is set;
// otherwise it is inherited (e.g. Class#new) and a per-receiver svisOverride is
// recorded. A name that resolves to no class method raises NameError.
func (vm *VM) setClassMethodVisibility(mod *RClass, name string, vis visibility) {
	if own, ok := mod.smethods[name]; ok && !own.undefined {
		own.vis = vis
		bumpMethodSerial()
		return
	}
	// `new` and friends are reachable as instance methods of Class rather than as
	// inherited smethods; resolveClassMethod covers both.
	if vm.resolveClassMethod(mod, name) == nil {
		raise("NameError", "undefined method '%s' for class '%s'", name, mod.name)
	}
	if mod.svisOverrides == nil {
		mod.svisOverrides = map[string]visibility{}
	}
	mod.svisOverrides[name] = vis
	bumpMethodSerial()
}

// resolveClassMethod finds the Method a `cls.name` send would dispatch to: the
// singleton-method chain (def self.foo and inherited class methods) first, then
// the Class instance methods (where `new`, `allocate`, … live). Returns nil when
// only method_missing would handle it.
func (vm *VM) resolveClassMethod(cls *RClass, name string) *Method {
	if m := lookupSMethod(cls, name); m != nil {
		return m
	}
	return undefAsNil(lookupMethod(vm.cClass, name))
}

// enforceSendVisRoute is the slow-path visibility gate used by the OpSend*
// handlers for a send with an explicit (non-self) receiver. It resolves the
// target method and, when the call is a blocked private/protected access, either
// routes it to the receiver's #method_missing (if the receiver defines one) or
// raises the matching NoMethodError — returning (result, true) in that case so
// the caller uses it instead of dispatching. A public (or unresolved →
// method_missing) call, and any implicit/self send, returns (nil, false) so the
// caller dispatches normally.
func (vm *VM) enforceSendVisRoute(flags int, recv object.Value, name string, args []object.Value, blk *Proc, caller object.Value) (object.Value, bool) {
	if flags&bytecode.FlagSendExplicit == 0 {
		return nil, false
	}
	var m *Method
	if cls, ok := recv.(*RClass); ok {
		m = vm.resolveClassMethod(cls, name)
	} else {
		m = undefAsNil(lookupMethod(vm.dispatchClass(recv), name))
	}
	return vm.explicitBlockedRoute(recv, name, m, args, blk, caller)
}

// enforceSendVis is the raise-only visibility gate still used by the splat /
// block-argument OpSend* handlers (foo(*a), foo(&b), foo(*a, &b)). It resolves
// the target and raises the visibility NoMethodError for a blocked private or
// out-of-scope protected call (with #method_missing NOT intercepted here — the
// plain OpSend paths, which cover the method_missing protocol, use
// enforceSendVisRoute instead). An implicit/self send is a no-op.
func (vm *VM) enforceSendVis(flags int, recv object.Value, name string, caller object.Value) {
	if flags&bytecode.FlagSendExplicit == 0 {
		return
	}
	var m *Method
	if cls, ok := recv.(*RClass); ok {
		m = vm.resolveClassMethod(cls, name)
	} else {
		m = undefAsNil(lookupMethod(vm.dispatchClass(recv), name))
	}
	vm.checkVisibility(recv, name, m, caller)
}

// explicitBlockedRoute handles an explicit-receiver send whose target method m is
// already resolved: if the access is blocked (private, or protected called from
// outside), it routes to the receiver's user #method_missing or raises the
// visibility NoMethodError, returning (result, true). A public/allowed call
// returns (nil, false) so the caller invokes m directly.
func (vm *VM) explicitBlockedRoute(recv object.Value, name string, m *Method, args []object.Value, blk *Proc, caller object.Value) (object.Value, bool) {
	kind := vm.visBlockedKind(recv, name, m, caller)
	if kind != visPrivate && kind != visProtected {
		return nil, false
	}
	// MRI: a private/protected call reaches #method_missing when the receiver
	// defines one, rather than raising outright.
	if mm := vm.userMethodMissing(recv); mm != nil {
		mmArgs := append([]object.Value{object.SymVal(name)}, args...)
		return vm.invoke(mm, recv, mmArgs, blk), true
	}
	vm.raiseVisibilityError(recv, name, kind, args)
	return nil, true
}

// userMethodMissing returns recv's #method_missing when it is a user override
// (not the BasicObject default and not undef'd), else nil — mirroring send's
// resolution order so a class/singleton method_missing is seen.
func (vm *VM) userMethodMissing(recv object.Value) *Method {
	m := vm.resolveSingletonHook(recv, "method_missing")
	if m == nil || m.undefined || m.owner == vm.cBasicObject {
		return nil
	}
	return m
}

// raiseVisibilityError raises the NoMethodError for a blocked private/protected
// call, stamping @name/@receiver/@args so NoMethodError#receiver (and #name/#args)
// report the failed call, as MRI.
func (vm *VM) raiseVisibilityError(recv object.Value, name string, kind visibility, args []object.Value) {
	word := "private"
	if kind == visProtected {
		word = "protected"
	}
	var callArgs object.Value
	if len(args) > 0 {
		callArgs = object.NewArrayFromSlice(append([]object.Value(nil), args...))
	}
	vm.raiseWithIvars("NoMethodError",
		word+" method '"+name+"' called for "+vm.recvDesc(recv),
		map[string]object.Value{"@name": object.SymVal(name), "@receiver": recv, "@args": callArgs})
}

// visBlockedKind reports the access level that blocks an explicit-receiver send:
// visPrivate or visProtected when the call is not permitted, else visPublic. It
// mirrors checkVisibility's receiver resolution without raising.
func (vm *VM) visBlockedKind(recv object.Value, name string, m *Method, caller object.Value) visibility {
	if m == nil {
		return visPublic // unresolved: dispatch routes it to method_missing
	}
	var vis visibility
	if cls, ok := recv.(*RClass); ok && lookupSMethod(cls, name) == nil && m.owner != vm.cClass {
		vis = instanceVisibility(vm.classOf(recv), name, m)
	} else if cls, ok := recv.(*RClass); ok {
		vis = classMethodVisibilityOf(cls, name, m)
	} else {
		vis = instanceVisibility(vm.dispatchClass(recv), name, m)
	}
	if vis == visProtected && caller != nil && classIsA(vm.classOf(caller), m.owner) {
		return visPublic // the classic allowed protected call (caller is a kin instance)
	}
	return vis
}

// sendVisibilityOf reports the effective access level a `recv.name` send would
// see for an already-resolved method m. It mirrors the receiver resolution in
// checkVisibility: a class receiver dispatching a class method uses the class-
// method override chain, anything else uses the instance-method chain. Used by
// respond_to? and public_send, which need the level without raising.
func (vm *VM) sendVisibilityOf(recv object.Value, name string, m *Method) visibility {
	if cls, ok := recv.(*RClass); ok {
		if lookupSMethod(cls, name) != nil || m.owner == vm.cClass {
			return classMethodVisibilityOf(cls, name, m)
		}
		return instanceVisibility(vm.classOf(recv), name, m)
	}
	return instanceVisibility(vm.dispatchClass(recv), name, m)
}

// checkVisibility enforces an explicit-receiver send's access level, raising the
// matching NoMethodError when the call is not permitted. caller is the self of
// the calling frame (for the protected check). It is called only for explicit
// receivers; implicit-receiver and self-receiver sends never reach here.
func (vm *VM) checkVisibility(recv object.Value, name string, m *Method, caller object.Value) {
	if kind := vm.visBlockedKind(recv, name, m, caller); kind == visPrivate || kind == visProtected {
		vm.raiseVisibilityError(recv, name, kind, nil)
	}
}

// dispatchClass returns the class whose instance-method table backs recv's
// dispatch: its singleton class when it has one (per-object methods), else its
// ordinary class. Mirrors the receiver resolution in send.
func (vm *VM) dispatchClass(recv object.Value) *RClass {
	if sc := vm.objSingleton(recv); sc != nil {
		return sc
	}
	return vm.classOf(recv)
}

// recvDesc renders a receiver as MRI does in a NoMethodError: "class C" for a
// class/module receiver, otherwise "an instance of C".
func (vm *VM) recvDesc(recv object.Value) string {
	if c, ok := recv.(*RClass); ok {
		kind := "class"
		if c.isModule {
			kind = "module"
		}
		return kind + " " + c.name
	}
	return "an instance of " + vm.classOf(recv).name
}
