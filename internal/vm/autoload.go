// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerAutoload installs Module#autoload / #autoload? and their Kernel
// (top-level) forms. autoload records that resolving a still-undefined constant
// should first `require` a file; autoload? reports the pending path. The actual
// lazy load is driven by tryAutoload, called from the constant-resolution paths.
func (vm *VM) registerAutoload() {
	// Module#autoload(const, path): register a lazy load for const in self's
	// constant table. If the constant is already defined the registration is a
	// no-op (MRI: autoload? then reports nil). Returns nil.
	//
	// The argument contract follows MRI exactly (ruby/ruby v3_4_0 load.c
	// rb_mod_autoload → variable.c rb_autoload_str): rb_to_id on the name first
	// (TypeError for anything but a Symbol/String), then FilePathValue on the
	// path (#to_path/#to_str, so a Pathname works), then the constant-name check
	// (NameError "autoload must be constant name: x"), then the empty-feature
	// check (ArgumentError), and finally const_set — whose rb_check_frozen is
	// what makes a frozen module raise FrozenError, after all of the above.
	autoloadFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cls := self.(*RClass)
		sym := autoloadNameArg(args[0])
		path := vm.filePathArg(args[1])
		autoloadCheckConstName(sym)
		if path == "" {
			raise("ArgumentError", "empty feature name")
		}
		if cls.frozen {
			vm.raiseFrozen(cls)
		}
		vm.registerAutoloadOn(cls, sym, path)
		return object.NilV
	}
	vm.cModule.define("autoload", autoloadFn)

	// Module#autoload?(const, inherit=true): the pending autoload path String, or
	// nil. With inherit (the default) the receiver's ancestors are searched too —
	// MRI walks RCLASS_SUPER, so an autoload registered on a superclass or on an
	// included module is reported by the heir. A const already defined in this
	// class's table (not merely registered) reports nil, as does a name that is
	// not a constant name at all (MRI's rb_check_id yields no id and returns nil
	// rather than raising). Reference: ruby/ruby v3_4_0 load.c rb_mod_autoload_p →
	// variable.c rb_autoload_at_p.
	autoloadQFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cls := self.(*RClass)
		name := autoloadNameArg(args[0])
		inherit := len(args) < 2 || args[1].Truthy()
		return vm.autoloadPathFor(cls, name, inherit)
	}
	vm.cModule.define("autoload?", autoloadQFn)

	// Kernel#autoload / #autoload?: top-level forms registered on Object's table.
	// A bare `autoload` at the top level (self = main) registers on Object.
	vm.cObject.define("autoload", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return autoloadFn(vm, vm.cObject, args, nil)
	})
	vm.cObject.define("autoload?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return autoloadQFn(vm, vm.cObject, args, nil)
	})
}

// autoloadNameArg coerces an autoload constant-name argument to its string form
// the way MRI's rb_to_id / rb_check_id does: a Symbol or String passes through,
// anything else raises TypeError. Unlike constNameArg it does NOT validate the
// shape of the name — autoload and autoload? diverge there (autoload raises its
// own NameError, autoload? quietly reports nil).
func autoloadNameArg(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	default:
		raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
		return ""
	}
}

// autoloadCheckConstName raises MRI's "autoload must be constant name" NameError
// for a name that is not a well-formed constant (rb_is_const_id in
// rb_autoload_str). The message differs from every other constant-name error in
// the VM, so it gets its own check rather than reusing constNameArg.
func autoloadCheckConstName(name string) {
	if !constNameWellFormed(name) {
		raise("NameError", "autoload must be constant name: %s", name)
	}
}

// autoloadPathFor reports the pending autoload path for name on cls (searching
// cls's ancestors when inherit is set), or nil. A constant that is already
// defined, or one whose file has already been loaded — MRI's
// check_autoload_required consults rb_feature_provided, so an autoload naming a
// file that is already in $LOADED_FEATURES is considered settled — reports nil.
func (vm *VM) autoloadPathFor(cls *RClass, name string, inherit bool) object.Value {
	chain := []*RClass{cls}
	if inherit {
		chain = vm.ancestors(cls)
	}
	for _, c := range chain {
		if _, defined := c.consts[name]; defined {
			return object.NilV
		}
		if c.autoloads == nil {
			continue
		}
		p, ok := c.autoloads[name]
		if !ok {
			continue
		}
		if vm.featureLoaded(p) {
			return object.NilV
		}
		return object.NewString(p)
	}
	return object.NilV
}

// registerAutoloadOn records (or replaces) a pending autoload for name on cls.
// When the constant is already defined in cls's own table the registration is
// dropped, matching MRI where autoload of a defined constant is inert. A fresh
// registration fires const_added, as MRI's rb_autoload_str does once
// autoload_synchronized reports the constant was newly reserved.
func (vm *VM) registerAutoloadOn(cls *RClass, name, path string) {
	if _, defined := cls.consts[name]; defined {
		return
	}
	if cls.autoloads == nil {
		cls.autoloads = map[string]string{}
	}
	_, existed := cls.autoloads[name]
	cls.autoloads[name] = path
	if !existed {
		vm.fireConstAdded(cls, name)
	}
}

// tryAutoload checks whether name has a pending autoload registered directly on
// cls; if so it consumes the entry, requires the recorded path, and reports
// whether the require ran. The constant is NOT looked up here — the caller
// re-resolves afterwards. A pending entry is cleared before the require so a
// re-entrant resolution of the same constant does not loop, and is PUT BACK if
// the require raises: MRI keeps a failed autoload registered (the constant stays
// in Module#constants and autoload? still reports the path), so a later
// reference tries the file again. A require that completes without defining the
// constant, by contrast, retires the entry — MRI does not load such a file twice.
func (vm *VM) tryAutoload(cls *RClass, name string) bool {
	if cls == nil || cls.autoloads == nil {
		return false
	}
	path, ok := cls.autoloads[name]
	if !ok {
		return false
	}
	delete(cls.autoloads, name)
	done := false
	defer func() {
		if !done {
			vm.registerAutoloadOn(cls, name, path)
		}
	}()
	// MRI loads the file through main.require, so a redefined (or mocked)
	// Kernel#require is what runs — ruby/spec exercises exactly that.
	vm.send(vm.main, "require", []object.Value{object.NewString(path)}, nil)
	done = true
	return true
}

// featureLoaded reports whether path names a file that has already been loaded,
// resolved the way require would resolve it. $LOADED_FEATURES is the authority
// (doRequire records each file there and featureDropped retires a cache entry
// the program has deleted from $"), so a suite that saves and restores $" around
// each example — ruby/spec does — is obeyed.
func (vm *VM) featureLoaded(path string) bool {
	abs := vm.featureAbsPath(path)
	return abs != "" && vm.loaded[abs] && !vm.featureDropped(abs)
}

// featureAbsPath resolves a require argument to the absolute path doRequire
// would load, or "" when no candidate exists. The .rb suffix is appended as
// doRequire appends it.
func (vm *VM) featureAbsPath(name string) string {
	file := name
	if filepath.Ext(file) != ".rb" {
		file += ".rb"
	}
	for _, cand := range vm.requireCandidates(file, false) {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			abs, err := filepath.Abs(cand)
			if err != nil {
				return ""
			}
			return abs
		}
	}
	return ""
}

// noteLoadedFeature appends path to $LOADED_FEATURES (and its $" alias, the same
// Array object) once, mirroring MRI's rb_provide_feature. Keeping that list
// truthful is what lets a program which restores $" force a re-require.
func (vm *VM) noteLoadedFeature(path string) {
	arr, ok := vm.globals["$LOADED_FEATURES"].(*object.Array)
	if !ok {
		return
	}
	if featureListed(arr, path) {
		return
	}
	arr.Elems = append(arr.Elems, object.NewString(path))
}

// featureDropped reports whether path was loaded by this VM but has since been
// removed from $LOADED_FEATURES by the program. MRI decides "already required?"
// by consulting $" alone, so dropping an entry there makes the next require run
// the file again; rbgo caches the answer in vm.loaded, and this is the check
// that keeps that cache honest.
func (vm *VM) featureDropped(abs string) bool {
	if !vm.loaded[abs] {
		return false
	}
	arr, ok := vm.globals["$LOADED_FEATURES"].(*object.Array)
	if !ok {
		return false
	}
	return !featureListed(arr, abs)
}

// featureListed reports whether arr holds the String path.
func featureListed(arr *object.Array, path string) bool {
	for _, v := range arr.Elems {
		if s, ok := v.(*object.String); ok && s.Str() == path {
			return true
		}
	}
	return false
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

// forgetLoadedFeature undoes the bookkeeping of a require that did not finish:
// the file is dropped from vm.loaded and from $LOADED_FEATURES, so requiring it
// again runs it again. MRI does this when the loaded file raises — a failed
// require is not a completed one.
func (vm *VM) forgetLoadedFeature(abs string) {
	delete(vm.loaded, abs)
	arr, ok := vm.globals["$LOADED_FEATURES"].(*object.Array)
	if !ok {
		return
	}
	for i, v := range arr.Elems {
		if s, ok := v.(*object.String); ok && s.Str() == abs {
			arr.Elems = append(arr.Elems[:i], arr.Elems[i+1:]...)
			return
		}
	}
}
