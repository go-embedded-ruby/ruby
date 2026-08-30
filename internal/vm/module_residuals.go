// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"unicode"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerModuleResiduals installs the remaining Module/Class reflection surface
// toward MRI 3.4/4.0: a proper Module#const_get / #const_defined? (String or
// Symbol name, "A::B" scoped paths, a leading "::" toplevel qualifier, the
// inherit flag, #to_str coercion and the #const_missing hook), the default
// Module#const_missing (raising a NameError that carries the constant name),
// Module#included_modules and Module#remove_class_variable. const_get and
// const_defined? are (re)defined here, replacing the single-name versions that
// used to live in builtins.go.
func (vm *VM) registerModuleResiduals() {
	// Module#const_get(name, inherit=true): resolve a constant by Symbol or String
	// name, honouring scoped paths and the inherit flag, and routing an
	// unresolved name through #const_missing on the module where it was sought.
	vm.cModule.define("const_get", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		inherit := len(args) < 2 || args[1].Truthy()
		mod := self.(*RClass)
		v := vm.moduleConstGet(mod, args[0], inherit)
		if mod.deprecatedConsts != nil {
			if s, ok := args[0].(*object.String); ok && mod.deprecatedConsts[s.Str()] {
				vm.warnDeprecatedConst(mod, s.Str())
			} else if sym, ok := args[0].(object.Symbol); ok && mod.deprecatedConsts[string(sym)] {
				vm.warnDeprecatedConst(mod, string(sym))
			}
		}
		return v
	})

	// Module#const_defined?(name, inherit=true): report whether name resolves,
	// without triggering an autoload require or calling #const_missing.
	vm.cModule.define("const_defined?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		inherit := len(args) < 2 || args[1].Truthy()
		return object.Bool(vm.moduleConstDefined(self.(*RClass), args[0], inherit))
	})

	// Module#const_missing(sym): the default hook, raising a NameError naming the
	// missing constant. The toplevel (Object) form omits the "Object::" qualifier,
	// matching MRI. The NameError carries the name so NameError#name returns it.
	vm.cModule.define("const_missing", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		mod := self.(*RClass)
		name := nameArg(args[0])
		var msg string
		if mod == vm.cObject || mod.name == "" {
			msg = "uninitialized constant " + name
		} else {
			msg = "uninitialized constant " + mod.name + "::" + name
		}
		return vm.raiseNameError(msg, name)
	})

	// Module#included_modules: the modules (not classes) in the receiver's ancestor
	// chain — its includes and prepends, and those of its ancestors — excluding the
	// receiver itself. Order follows the ancestor chain.
	vm.cModule.define("included_modules", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		me := self.(*RClass)
		var out []object.Value
		for _, c := range vm.ancestors(me) {
			if c != me && c.isModule {
				out = append(out, c)
			}
		}
		return object.NewArrayFromSlice(out)
	})

	// Module#remove_class_variable(sym): remove a class variable defined DIRECTLY
	// on the receiver (never one inherited or mixed in) and return its value,
	// raising NameError when the name is malformed or the variable is absent.
	vm.cModule.define("remove_class_variable", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := cvarNameArg(args[0])
		cls := self.(*RClass)
		if v, ok := cls.cvars[name]; ok {
			delete(cls.cvars, name)
			return v
		}
		return raise("NameError", "class variable %s not defined for %s", name, cls.ToS())
	})
}

// moduleConstGet implements Module#const_get for cls: it parses arg into a scope
// (receiver or the top level) and one or more path segments, resolves each
// segment in turn, and — on a miss — invokes #const_missing on the module where
// the segment was looked up (whose default raises NameError).
func (vm *VM) moduleConstGet(cls *RClass, arg object.Value, inherit bool) object.Value {
	segs, topLevel, orig := vm.constPathSegs(arg)
	mod := cls
	if topLevel {
		mod = vm.cObject
	}
	var result object.Value
	for i, seg := range segs {
		if !constNameWellFormed(seg) {
			raise("NameError", "wrong constant name %s", orig)
		}
		v, ok := vm.constSegGet(mod, seg, inherit, i == 0)
		if !ok {
			return vm.constMissing(mod, seg)
		}
		if i < len(segs)-1 {
			nc, isCls := v.(*RClass)
			if !isCls {
				raise("TypeError", "%s does not refer to class/module", orig)
			}
			mod = nc
		}
		result = v
	}
	return result
}

// moduleConstDefined implements Module#const_defined? for cls, mirroring
// moduleConstGet's path handling but reporting presence (never triggering an
// autoload require or #const_missing).
func (vm *VM) moduleConstDefined(cls *RClass, arg object.Value, inherit bool) bool {
	segs, topLevel, orig := vm.constPathSegs(arg)
	mod := cls
	if topLevel {
		mod = vm.cObject
	}
	for i, seg := range segs {
		if !constNameWellFormed(seg) {
			raise("NameError", "wrong constant name %s", orig)
		}
		v, ok := vm.constSegDefined(mod, seg, inherit, i == 0)
		if !ok {
			return false
		}
		if i < len(segs)-1 {
			nc, isCls := v.(*RClass)
			if !isCls {
				raise("TypeError", "%s does not refer to class/module", orig)
			}
			mod = nc
		}
	}
	return true
}

// constPathSegs coerces a const_get / const_defined? name argument (Symbol,
// String, or an object with #to_str) into its path segments, reporting whether a
// leading "::" selected the top level. A Symbol may not carry a scope path or
// qualifier — MRI treats "::" inside a Symbol name as a malformed constant name.
func (vm *VM) constPathSegs(arg object.Value) (segs []string, topLevel bool, orig string) {
	name, isSym := vm.constArgToString(arg)
	orig = name
	if isSym {
		if strings.Contains(name, "::") {
			raise("NameError", "wrong constant name %s", name)
		}
		return []string{name}, false, orig
	}
	rest := name
	if strings.HasPrefix(rest, "::") {
		topLevel = true
		rest = rest[2:]
	}
	segs = strings.Split(rest, "::")
	// An empty segment — a trailing "::", successive "::::" or a bare "::" — is a
	// malformed name that MRI rejects up front, before resolving any earlier
	// segment. (A non-empty but mis-capitalised segment is checked lazily, only
	// once resolution reaches it.)
	for _, s := range segs {
		if s == "" {
			raise("NameError", "wrong constant name %s", orig)
		}
	}
	return segs, topLevel, orig
}

// constArgToString coerces a constant-name argument to its text, reporting
// whether the source was a Symbol. A non-String/Symbol is converted with #to_str
// (a missing or non-String #to_str raises TypeError), matching MRI.
func (vm *VM) constArgToString(v object.Value) (string, bool) {
	switch n := v.(type) {
	case object.Symbol:
		return string(n), true
	case *object.String:
		return n.Str(), false
	default:
		if vm.respondsToDynamic(v, "to_str") {
			r := vm.send(v, "to_str", nil, nil)
			if s, ok := r.(*object.String); ok {
				return s.Str(), false
			}
			raise("TypeError", "can't convert %s to String (%s#to_str gives %s)",
				vm.classOf(v).name, vm.classOf(v).name, vm.classOf(r).name)
		}
		raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
		return "", false
	}
}

// constSegGet resolves one path segment in mod for const_get: with inherit it
// searches mod's ancestors (triggering a pending autoload), then — for the first
// segment of a non-Object receiver — the top level; without inherit only mod's
// own table is consulted.
func (vm *VM) constSegGet(mod *RClass, name string, inherit, isFirst bool) (object.Value, bool) {
	if !inherit {
		v, ok := mod.consts[name]
		return v, ok
	}
	if v, ok := vm.constInAncestors(mod, name); ok {
		return v, true
	}
	if vm.autoloadInAncestors(mod, name) {
		if v, ok := vm.constInAncestors(mod, name); ok {
			return v, true
		}
	}
	if isFirst && mod != vm.cObject {
		if v, ok := vm.cObject.consts[name]; ok {
			return v, true
		}
	}
	return object.NilVal(), false
}

// constSegDefined is constSegGet for const_defined?: it reports presence
// (including a pending, not-yet-run autoload) without requiring the autoload
// file.
func (vm *VM) constSegDefined(mod *RClass, name string, inherit, isFirst bool) (object.Value, bool) {
	if !inherit {
		if v, ok := mod.consts[name]; ok {
			return v, true
		}
		return object.NilVal(), hasAutoload(mod, name)
	}
	if v, ok := vm.constInAncestors(mod, name); ok {
		return v, true
	}
	if vm.autoloadPendingInAncestors(mod, name) {
		return object.NilVal(), true
	}
	if isFirst && mod != vm.cObject {
		if v, ok := vm.cObject.consts[name]; ok {
			return v, true
		}
		if hasAutoload(vm.cObject, name) {
			return object.NilVal(), true
		}
	}
	return object.NilVal(), false
}

// constMissing invokes #const_missing on mod with the missing constant's name,
// resolving a user override (a private one included) before the default. Its
// result is const_get's result when overridden; the default raises NameError.
func (vm *VM) constMissing(mod *RClass, name string) object.Value {
	return vm.send(mod, "const_missing", []object.Value{object.Symbol(name)}, nil)
}

// raiseNameError raises a NameError whose exception object carries name in its
// @name ivar, so NameError#name reports the offending constant/name (MRI).
func (vm *VM) raiseNameError(msg, name string) object.Value {
	obj := vm.buildException("NameError", msg)
	setIvar(obj, "@name", object.Symbol(name))
	panic(RubyError{Class: "NameError", Message: msg, Obj: obj})
}

// autoloadPendingInAncestors reports whether a pending (not-yet-run) autoload of
// name is registered on mod or an ancestor, without running it — the presence
// check const_defined? needs. It mirrors autoloadInAncestors' Object-skipping.
func (vm *VM) autoloadPendingInAncestors(mod *RClass, name string) bool {
	for _, c := range vm.ancestors(mod) {
		if (c == vm.cObject || c == vm.cBasicObject) && mod != vm.cObject && mod != vm.cBasicObject {
			continue
		}
		if hasAutoload(c, name) {
			return true
		}
	}
	return false
}

// hasAutoload reports whether c has a pending autoload registered for name.
func hasAutoload(c *RClass, name string) bool {
	if c == nil || c.autoloads == nil {
		return false
	}
	_, ok := c.autoloads[name]
	return ok
}

// validConstName reports whether s is a well-formed constant name: an uppercase
// first letter followed by letters, digits or underscores. Used to reject names
// like "name", "__X__", "X=" or "X?" as MRI's const_get/const_defined? do.
func constNameWellFormed(s string) bool {
	r := []rune(s)
	if len(r) == 0 || !unicode.IsUpper(r[0]) {
		return false
	}
	for _, c := range r[1:] {
		if c != '_' && !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}
