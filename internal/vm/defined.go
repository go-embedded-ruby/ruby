package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// definedTag returns the canonical String for a `defined?` tag, through the one
// constructor the compiler half uses too (bytecode.DefinedTag). It is FROZEN:
// MRI builds every defined? answer with rb_iseq_defined_string, which returns
// rb_fstring_cstr(...) (iseq.c v3_4_0:3692), and language/defined_spec.rb
// asserts `.frozen?` on the tags it names.
func definedTag(tag string) object.Value { return bytecode.DefinedTag(tag) }

// hasScopedConst reports whether cls or its ancestors define name — the
// non-raising form of scopedConst (used by defined?(A::B)). A pending autoload on
// the receiver or an ancestor counts as defined, without triggering the require.
func (vm *VM) hasScopedConst(cls *RClass, name string) bool {
	if _, ok := vm.constInAncestors(cls, name); ok {
		return true
	}
	for _, c := range vm.ancestors(cls) {
		if c == vm.cObject || c == vm.cBasicObject {
			if cls != vm.cObject && cls != vm.cBasicObject {
				continue
			}
		}
		if _, ok := c.autoloads[name]; ok {
			return true
		}
	}
	return false
}

// autoloadPending reports whether a pending autoload for name exists anywhere up
// cref's lexical nesting or ancestor chain (or on Object). It never triggers the
// require — used by defined? to report a registered-but-unloaded constant.
func (vm *VM) autoloadPending(cref *RClass, name string) bool {
	for _, c := range vm.nesting(cref) {
		if _, ok := c.autoloads[name]; ok {
			return true
		}
	}
	if cref != nil {
		for _, c := range vm.ancestors(cref) {
			if _, ok := c.autoloads[name]; ok {
				return true
			}
		}
	}
	_, ok := vm.cObject.autoloads[name]
	return ok
}

// gvarDefined reports whether a global variable is set. User globals live in
// vm.globals; a handful of regexp specials ($~, $1…, $&, …) are always
// considered defined when a last match exists. Anything else is undefined.
func (vm *VM) gvarDefined(name string) bool {
	if _, ok := vm.globals[name]; ok {
		return true
	}
	// The process/exception specials ($0, $$, $!, $PROGRAM_NAME) and their English
	// aliases are always defined: they resolve through specialGvar regardless of
	// the globals map.
	if _, handled := vm.specialGvar(name); handled {
		return true
	}
	// $~ is always considered defined (the last-match special exists even before
	// any match). The other match specials are defined only once a match exists;
	// a numbered group ($1…) is defined only if that group participated.
	if name == "$~" {
		return true
	}
	md, haveMatch := vm.lastMatch.(*MatchData)
	switch name {
	case "$&", "$`", "$'", "$+":
		return haveMatch
	}
	if n, isGroup := gvarGroup(name); isGroup {
		return haveMatch && n <= md.md.NGroups() && md.md.Begin(n) >= 0
	}
	return false
}

// respondsTo reports whether recv would answer name through real dispatch — a
// resolvable method (singleton, class chain, modules) or one of the
// compiler-fast-path operators. It backs `defined?(recv.m)` / `defined?(a OP b)`
// and never invokes the method.
// respondsToDynamic reports whether recv answers name the way Ruby's
// Object#respond_to? does — including methods provided only through
// method_missing and reported by respond_to_missing?. Type-coercion paths
// (#to_str / #to_hash / #to_int) use it so a proxy object that defines the
// conversion dynamically (a spec mock, a DelegateClass) is coerced, matching
// MRI's rb_respond_to.
func (vm *VM) respondsToDynamic(recv object.Value, name string) bool {
	if vm.respondsTo(recv, name) {
		return true
	}
	return vm.send(recv, "respond_to?", []object.Value{object.Symbol(name)}, nil).Truthy()
}

func (vm *VM) respondsTo(recv object.Value, name string) bool {
	if vm.findMethod(recv, name) != nil {
		return true
	}
	if _, ok := operatorOpcode(name); ok {
		return true
	}
	// `!`/`!=`/`==` are now real BasicObject methods and the unary `-@`/`+@`/`~`
	// are Numeric/Integer methods, so findMethod above already answers for them;
	// no operator fast-path special-case is needed here.
	return false
}

// definedResponds answers the two `defined?` method probes, which MRI keeps
// apart (vm_insnhelper.c v3_4_0:5472-5498):
//
//   - DEFINED_FUNC, an implicit receiver (`defined?(foo)`), is
//     rb_ec_obj_respond_to(..., TRUE): ANY visibility counts, so a private
//     method called without a receiver is "method".
//   - DEFINED_METHOD, an explicit receiver (`defined?(obj.foo)`), inspects the
//     method entry's visibility: private is NOT defined?, and protected only
//     when the calling self is a kind of the method's owner. That is the same
//     rule an explicit-receiver SEND enforces, so it is asked of the same
//     helper (visBlockedKind) rather than restated here.
//
// Both fall back to respond_to_missing? when there is no method entry at all
// (check_respond_to_missing in the DEFINED_METHOD arm, and rb_obj_respond_to's
// own fallback in the DEFINED_FUNC one), which is how a proxy that answers
// through method_missing reports as defined.
//
// caller is the self of the frame the defined? was written in.
func (vm *VM) definedResponds(caller, recv object.Value, name string, explicit bool) bool {
	if m := vm.findMethod(recv, name); m != nil {
		if !explicit {
			return true
		}
		return vm.visBlockedKind(recv, name, m, caller) == visPublic
	}
	// The arithmetic operators are a compiler fast path in rbgo, not method-table
	// entries, so a receiver that HAS them answers nothing above. They are still
	// methods in MRI, hence the fallback — but only for a receiver the fast path
	// actually computes on: `nil / 2` raises NoMethodError here exactly as in
	// MRI, so `defined?(nil / 2)` must be nil rather than "method".
	if _, ok := operatorOpcode(name); ok && hasInlineArith(recv) {
		return true
	}
	return vm.respondToMissing(recv, name)
}

// hasInlineArith reports whether the arithmetic fast path would compute for
// recv rather than raise NoMethodError. It names the receivers it EXCLUDES,
// because the fast path's own fallback (arith.go's binary) covers the built-in
// value types generically: nil, true/false and a Symbol have no arithmetic in
// any Ruby, and a user object with no built-in backing that reached here has no
// operator method either, so it belongs to the respond_to_missing? path — which
// is where MRI sends it too.
func hasInlineArith(recv object.Value) bool {
	switch o := recv.(type) {
	case object.Nil, object.Bool, object.Symbol:
		return false
	case *RObject:
		return !object.IsNil(o.builtin)
	}
	return true
}

// respondToMissing runs MRI's check_respond_to_missing: it asks recv's
// respond_to_missing?(name, true), which is how an object that serves a method
// through method_missing reports it as defined. A receiver with no override
// answers false through Object#respond_to_missing?.
func (vm *VM) respondToMissing(recv object.Value, name string) bool {
	if vm.findMethod(recv, "respond_to_missing?") == nil {
		return false
	}
	return vm.send(recv, "respond_to_missing?", []object.Value{object.Symbol(name), object.True}, nil).Truthy()
}

// runDefinedGuard executes a `defined?` guard child ISeq sharing the enclosing
// frame's scope (parentEnv), self, definee and block, mapping any raise inside
// to nil. The child always leaves exactly one value via OpReturn.
func (vm *VM) runDefinedGuard(child *bytecode.ISeq, self object.Value, definee *RClass, parentEnv *Env, block *Proc) (res object.Value) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(RubyError); ok {
				res = object.NilV
				return
			}
			panic(r) // control-flow signals / internal bugs propagate
		}
	}()
	return vm.exec(child, self, nil, definee, "", parentEnv, block, nil, block, nil)
}
