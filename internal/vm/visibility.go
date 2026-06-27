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
	if lookupMethod(mod, name) == nil {
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

// enforceSendVis is the slow-path visibility gate used by the OpSend* handlers:
// when the send had an explicit (non-self) receiver, it resolves the target
// method and enforces its access level (raising NoMethodError for a private or
// out-of-scope protected call). An implicit / self send (flag clear) is a no-op.
// A name that resolves to no method (→ method_missing) is left to dispatch.
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
	if m == nil {
		return
	}
	var vis visibility
	if cls, ok := recv.(*RClass); ok && lookupSMethod(cls, name) == nil && m.owner != vm.cClass {
		// Not a class-method dispatch (the method came from the generic Class/Module
		// instance methods, i.e. a normal instance-method visibility applies).
		vis = instanceVisibility(vm.classOf(recv), name, m)
	} else if cls, ok := recv.(*RClass); ok {
		vis = classMethodVisibilityOf(cls, name, m)
	} else {
		vis = instanceVisibility(vm.dispatchClass(recv), name, m)
	}
	switch vis {
	case visPrivate:
		raise("NoMethodError", "private method '%s' called for %s", name, vm.recvDesc(recv))
	case visProtected:
		// Allowed when the caller's self is an instance of the method's owner (or a
		// descendant) — the classic protected use (a == comparing two instances).
		if caller != nil && classIsA(vm.classOf(caller), m.owner) {
			return
		}
		raise("NoMethodError", "protected method '%s' called for %s", name, vm.recvDesc(recv))
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
