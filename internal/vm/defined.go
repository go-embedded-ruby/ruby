package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// definedTag returns the canonical (frozen) String for a `defined?` tag. The
// String is freshly built each call to match MRI, which returns a new String
// object from defined?; callers never mutate it.
func definedTag(tag string) object.Value { return object.Wrap(object.NewString(tag)) }

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
	md, haveMatch := object.KindOK[*MatchData](vm.lastMatch)
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
func (vm *VM) respondsTo(recv object.Value, name string) bool {
	if vm.findMethod(recv, name) != nil {
		return true
	}
	if _, ok := operatorOpcode(name); ok {
		return true
	}
	// `!` is a universal BasicObject method (the compiler implements it as a fast
	// path rather than a method table entry), so every object responds to it.
	if name == "!" {
		return true
	}
	// Unary `-@`/`+@` are now generic Numeric methods and `~` is an Integer method,
	// so they are all found by findMethod above; no numeric fast-path special-case
	// is needed here.
	return false
}

// runDefinedGuard executes a `defined?` guard child ISeq sharing the enclosing
// frame's scope (parentEnv), self, definee and block, mapping any raise inside
// to nil. The child always leaves exactly one value via OpReturn.
func (vm *VM) runDefinedGuard(child *bytecode.ISeq, self object.Value, definee *RClass, parentEnv *Env, block *Proc) (res object.Value) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(RubyError); ok {
				res = object.NilVal()
				return
			}
			panic(r) // control-flow signals / internal bugs propagate
		}
	}()
	return vm.exec(child, self, nil, definee, "", parentEnv, block, nil, nil)
}
