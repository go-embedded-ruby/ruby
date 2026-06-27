package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// definedTag returns the canonical (frozen) String for a `defined?` tag. The
// String is freshly built each call to match MRI, which returns a new String
// object from defined?; callers never mutate it.
func definedTag(tag string) object.Value { return object.NewString(tag) }

// hasScopedConst reports whether cls or its ancestors define name — the
// non-raising form of scopedConst (used by defined?(A::B)).
func (vm *VM) hasScopedConst(cls *RClass, name string) bool {
	_, ok := vm.constInAncestors(cls, name)
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
	// Unary `-@`/`+@`/`~` are fast-path ops on the numeric tower rather than
	// method-table entries; numbers respond to them as in MRI.
	switch name {
	case "-@", "+@", "~":
		switch recv.(type) {
		case object.Integer, *object.Bignum, object.Float, *object.Rational, *object.Complex:
			return true
		}
	}
	return false
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
	return vm.exec(child, self, nil, definee, "", parentEnv, block, nil)
}
