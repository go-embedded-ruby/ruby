// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// registerAutoload installs Module#autoload / #autoload? and their Kernel
// (top-level) forms. autoload records that resolving a still-undefined constant
// should first `require` a file; autoload? reports the pending path. The actual
// lazy load is driven by tryAutoload, called from the constant-resolution paths.
func (vm *VM) registerAutoload() {
	// Module#autoload(const, path): register a lazy load for const in self's
	// constant table. If the constant is already defined the registration is a
	// no-op (MRI: autoload? then reports nil). Returns nil.
	autoloadFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cls := object.Kind[*RClass](self)
		name := constNameArg(args[0])
		path := autoloadPathArg(vm, args[1])
		vm.registerAutoloadOn(cls, name, path)
		return object.NilVal()
	}
	vm.cModule.define("autoload", autoloadFn)

	// Module#autoload?(const): the pending autoload path String, or nil. A const
	// already defined in this class's table (not merely registered) reports nil.
	autoloadQFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cls := object.Kind[*RClass](self)
		name := constNameArg(args[0])
		if _, defined := cls.consts[name]; defined {
			return object.NilVal()
		}
		if cls.autoloads != nil {
			if p, ok := cls.autoloads[name]; ok {
				return object.Wrap(object.NewString(p))
			}
		}
		return object.NilVal()
	}
	vm.cModule.define("autoload?", autoloadQFn)

	// Kernel#autoload / #autoload?: top-level forms registered on Object's table.
	// A bare `autoload` at the top level (self = main) registers on Object.
	vm.cObject.define("autoload", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		name := constNameArg(args[0])
		path := autoloadPathArg(vm, args[1])
		vm.registerAutoloadOn(vm.cObject, name, path)
		return object.NilVal()
	})
	vm.cObject.define("autoload?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		name := constNameArg(args[0])
		if _, defined := vm.cObject.consts[name]; defined {
			return object.NilVal()
		}
		if vm.cObject.autoloads != nil {
			if p, ok := vm.cObject.autoloads[name]; ok {
				return object.Wrap(object.NewString(p))
			}
		}
		return object.NilVal()
	})
}

// autoloadPathArg coerces the second autoload argument to its file-name String,
// raising TypeError otherwise — matching MRI's "no implicit conversion" error.
func autoloadPathArg(vm *VM, v object.Value) string {
	s, ok := object.KindOK[*object.String](v)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", vm.classOf(v).name)
	}
	return s.Str()
}

// registerAutoloadOn records (or replaces) a pending autoload for name on cls.
// When the constant is already defined in cls's own table the registration is
// dropped, matching MRI where autoload of a defined constant is inert.
func (vm *VM) registerAutoloadOn(cls *RClass, name, path string) {
	if _, defined := cls.consts[name]; defined {
		return
	}
	if cls.autoloads == nil {
		cls.autoloads = map[string]string{}
	}
	cls.autoloads[name] = path
}

// tryAutoload checks whether name has a pending autoload registered directly on
// cls; if so it consumes the entry, requires the recorded path, and reports
// whether the require ran. The constant is NOT looked up here — the caller
// re-resolves afterwards. A pending entry is cleared before the require so a
// re-entrant resolution of the same constant does not loop.
func (vm *VM) tryAutoload(cls *RClass, name string) bool {
	if cls == nil || cls.autoloads == nil {
		return false
	}
	path, ok := cls.autoloads[name]
	if !ok {
		return false
	}
	delete(cls.autoloads, name)
	vm.doRequire(path, false)
	return true
}

// autoloadInLexical walks cref's lexical nesting then its ancestor chain looking
// for a pending autoload of name, runs the first one found and returns true.
// Used by resolveConst (bare-constant lookup) so an autoload registered in an
// enclosing scope fires on first reference.
func (vm *VM) autoloadInLexical(cref *RClass, name string) bool {
	for _, c := range vm.nesting(cref) {
		if vm.tryAutoload(c, name) {
			return true
		}
	}
	if cref != nil {
		for _, c := range vm.ancestors(cref) {
			if vm.tryAutoload(c, name) {
				return true
			}
		}
	}
	return vm.tryAutoload(vm.cObject, name)
}

// autoloadInAncestors walks cls's ancestor chain looking for a pending autoload
// of name and runs the first one found, returning true. Used by scopedConst and
// const_get (Recv::Name / Recv.const_get), which only consult the receiver and
// its ancestors — not the lexical nesting.
func (vm *VM) autoloadInAncestors(cls *RClass, name string) bool {
	for _, c := range vm.ancestors(cls) {
		if c == vm.cObject || c == vm.cBasicObject {
			if cls != vm.cObject && cls != vm.cBasicObject {
				continue
			}
		}
		if vm.tryAutoload(c, name) {
			return true
		}
	}
	return false
}
