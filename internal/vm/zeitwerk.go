// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"path/filepath"
	"strings"

	zeitwerk "github.com/go-ruby-zeitwerk/zeitwerk"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerZeitwerk installs the native Zeitwerk module (require "zeitwerk"),
// backed by github.com/go-ruby-zeitwerk/zeitwerk — the pure-Go (cgo-free) engine
// of Ruby's Zeitwerk autoloader. The library owns everything that is pure logic:
// scanning the managed directory trees, computing the bidirectional
// constant<->path map (honouring namespaces, collapsed directories and ignored
// paths), and driving the setup / eager-load / reload / unload lifecycle and its
// callbacks. Everything that touches a running Ruby — registering an autoload,
// requiring a file, removing a constant — is left to the host through the
// library's DefineAutoload / Load / OnUnload seams, which this file wires to
// rbgo's own Module#autoload (registerAutoloadOn / tryAutoload), Kernel#require
// (doRequire) and constant table (vm.consts). So `require "zeitwerk"; loader =
// Zeitwerk::Loader.new; loader.push_dir(dir); loader.setup` makes the .rb files
// under dir lazily autoloadable by their inflected constant names, and
// loader.eager_load requires them all up front, exactly as the gem does.
//
// Divergences from the gem, all documented at their call sites: the on_load /
// on_unload block callbacks yield (cpath, abspath) rather than the gem's
// (value, abspath) / (cpath, value, abspath), since rbgo drives them off the
// engine's path-oriented seam; and a directory that maps to an implicit
// namespace is autovivified as an empty Module at setup time (the gem does it
// lazily on first reference) — the observable result, that the namespace is a
// Module, is the same after setup.
func (vm *VM) registerZeitwerk() {
	mod := newClass("Zeitwerk", nil)
	mod.isModule = true
	vm.consts["Zeitwerk"] = mod

	vm.registerZeitwerkErrors(mod)
	vm.registerZeitwerkInflector(mod)
	vm.registerZeitwerkLoader(mod)
}

// registerZeitwerkErrors installs the Zeitwerk error tree, mirroring the gem:
// Zeitwerk::Error < StandardError, Zeitwerk::SetupRequired < Zeitwerk::Error, and
// Zeitwerk::NameError < ::NameError. Each is registered both scoped (under
// Zeitwerk) and flat in vm.consts so raise can find it by its qualified name.
func (vm *VM) registerZeitwerkErrors(mod *RClass) {
	base := newClass("Zeitwerk::Error", vm.consts["StandardError"].(*RClass))
	mod.consts["Error"] = base
	vm.consts["Zeitwerk::Error"] = base

	setupReq := newClass("Zeitwerk::SetupRequired", base)
	mod.consts["SetupRequired"] = setupReq
	vm.consts["Zeitwerk::SetupRequired"] = setupReq

	nameErr := newClass("Zeitwerk::NameError", vm.consts["NameError"].(*RClass))
	mod.consts["NameError"] = nameErr
	vm.consts["Zeitwerk::NameError"] = nameErr
}

// registerZeitwerkInflector installs Zeitwerk::Inflector, the default inflector
// that maps a file/directory basename to the constant name it defines. .new
// builds a fresh engine inflector; #camelize applies the snake_case -> CamelCase
// rule; #inflect registers hard-coded acronym overrides (e.g. "html_parser" =>
// "HTMLParser").
func (vm *VM) registerZeitwerkInflector(mod *RClass) {
	c := newClass("Zeitwerk::Inflector", vm.cObject)
	mod.consts["Inflector"] = c
	vm.consts["Zeitwerk::Inflector"] = c

	c.smethods["new"] = &Method{name: "new", owner: c, native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ZeitwerkInflector{in: zeitwerk.NewInflector()}
	}}

	// #camelize(basename, abspath = nil): the abspath argument the gem accepts is
	// unused by the default inflector, so it is ignored here.
	c.define("camelize", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		return object.NewString(self.(*ZeitwerkInflector).in.Camelize(strArg(args[0])))
	})

	// #inflect(hash): register basename/word -> constant-name overrides.
	c.define("inflect", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		h, ok := args[0].(*object.Hash)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Hash", vm.classOf(args[0]).name)
		}
		self.(*ZeitwerkInflector).in.Inflect(zeitwerkInflectMap(h))
		return args[0]
	})
}

// zeitwerkInflectMap converts a Ruby Hash of basename/word => constant-name pairs
// (String or Symbol on either side) to the string map the engine's Inflect takes.
func zeitwerkInflectMap(h *object.Hash) map[string]string {
	m := make(map[string]string, h.Len())
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		m[zeitwerkCpath(k)] = zeitwerkCpath(v)
	}
	return m
}

// registerZeitwerkLoader installs Zeitwerk::Loader, the autoloader handle. .new
// and .for_gem build a loader whose engine seams are wired to rbgo; the instance
// methods forward configuration (push_dir / ignore / collapse / enable_reloading)
// and the lifecycle (setup / eager_load / reload / unload) to the engine, and the
// callback methods (on_load / on_setup / on_unload) register Ruby blocks with it.
func (vm *VM) registerZeitwerkLoader(mod *RClass) {
	c := newClass("Zeitwerk::Loader", vm.cObject)
	mod.consts["Loader"] = c
	vm.consts["Zeitwerk::Loader"] = c

	c.smethods["new"] = &Method{name: "new", owner: c, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.newZeitwerkLoader()
	}}

	// .for_gem(warn_on_extra_files: true): the gem convenience that manages the
	// directory the calling file lives in (a gem's lib/), ignoring that entry file
	// itself. rbgo roots it at the directory of the file currently being required
	// (the require stack's top, or the script when called from -e), matching the
	// gem's caller-based root. The keyword argument is accepted and ignored.
	c.smethods["for_gem"] = &Method{name: "for_gem", owner: c, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		z := vm.newZeitwerkLoader()
		caller := vm.currentFile()
		dir := filepath.Dir(caller)
		if err := z.l.PushDir(dir, ""); err != nil {
			raise("Zeitwerk::Error", "%s", err.Error())
		}
		if caller != "" {
			z.l.Ignore(caller)
		}
		return z
	}}

	d := func(name string, fn NativeFn) { c.define(name, fn) }

	// #push_dir(path, namespace: Object): register a root directory whose children
	// map into namespace (top level by default). Raises Zeitwerk::Error if the
	// directory does not exist, as the gem does.
	d("push_dir", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		z := self.(*ZeitwerkLoader)
		ns := ""
		if h, ok := lastHash(args); ok {
			if v, ok := h.Get(object.Symbol("namespace")); ok {
				ns = zeitwerkNamespace(v)
			}
		}
		if err := z.l.PushDir(strArg(args[0]), ns); err != nil {
			raise("Zeitwerk::Error", "%s", err.Error())
		}
		return object.NilV
	})

	// #ignore(*globs) / #collapse(*globs): exclude matching paths / promote a
	// collapsed directory's children into its parent namespace.
	d("ignore", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ZeitwerkLoader).l.Ignore(zeitwerkGlobs(args)...)
		return object.NilV
	})
	d("collapse", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ZeitwerkLoader).l.Collapse(zeitwerkGlobs(args)...)
		return object.NilV
	})

	// #enable_reloading: permit #reload; must be called before #setup.
	d("enable_reloading", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		self.(*ZeitwerkLoader).l.EnableReloading()
		return object.NilV
	})

	// #setup: scan the roots and wire an autoload for every managed constant (the
	// DefineAutoload seam, wired in newZeitwerkLoader, calls rbgo Module#autoload).
	d("setup", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self.(*ZeitwerkLoader).l.Setup(); err != nil {
			raise("Zeitwerk::Error", "%s", err.Error())
		}
		return object.NilV
	})

	// #eager_load(force: false): require every managed file up front through the
	// Load seam (rbgo Kernel#require), firing on_load for each constant. Raises
	// Zeitwerk::SetupRequired if #setup has not run.
	d("eager_load", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self.(*ZeitwerkLoader).l.EagerLoad(); err != nil {
			raise("Zeitwerk::SetupRequired", "%s", err.Error())
		}
		return object.NilV
	})

	// #reload: unload then set up again, picking up filesystem changes. Raises
	// Zeitwerk::Error if reloading was not enabled, Zeitwerk::SetupRequired if
	// #setup has not run.
	d("reload", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		err := self.(*ZeitwerkLoader).l.Reload()
		switch err.(type) {
		case nil:
			return object.NilV
		case *zeitwerk.SetupRequired:
			return raise("Zeitwerk::SetupRequired", "%s", err.Error())
		default:
			return raise("Zeitwerk::Error", "%s", err.Error())
		}
	})

	// #unload: remove every managed constant (the OnUnload seam) and clear the map,
	// requiring a fresh #setup afterwards.
	d("unload", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		self.(*ZeitwerkLoader).l.Unload()
		return object.NilV
	})

	// #on_load(cpath = :ANY, &block): fire block when the constant is loaded (during
	// eager_load here), yielding (cpath, abspath). #on_setup(&block) fires after
	// every setup; #on_unload(cpath = :ANY, &block) fires per constant at unload,
	// yielding (cpath, abspath).
	d("on_load", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		cpath := "ANY"
		if len(args) > 0 {
			cpath = zeitwerkCpath(args[0])
		}
		if blk != nil {
			self.(*ZeitwerkLoader).l.OnLoad(cpath, func(cp, fp string) {
				vm.callBlock(blk, []object.Value{object.NewString(cp), object.NewString(fp)})
			})
		}
		return object.NilV
	})
	d("on_setup", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk != nil {
			self.(*ZeitwerkLoader).l.OnSetup(func() { vm.callBlock(blk, nil) })
		}
		return object.NilV
	})
	d("on_unload", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		target := "ANY"
		if len(args) > 0 {
			target = zeitwerkCpath(args[0])
		}
		if blk != nil {
			self.(*ZeitwerkLoader).l.OnUnload(func(cp, fp string) {
				if target == "ANY" || target == cp {
					vm.callBlock(blk, []object.Value{object.NewString(cp), object.NewString(fp)})
				}
			})
		}
		return object.NilV
	})

	// #inflector: the loader's inflector, so callers can register acronym overrides
	// (loader.inflector.inflect("html_parser" => "HTMLParser")) that the scan honours.
	d("inflector", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*ZeitwerkLoader).inf
	})
}

// newZeitwerkLoader builds a Zeitwerk::Loader wrapper and wires the engine's three
// host seams to rbgo: DefineAutoload -> rbgo Module#autoload (per managed
// constant, at setup), Load -> Kernel#require (per managed file, at eager_load),
// and OnUnload -> constant removal (per managed constant, at unload/reload). The
// inflector wrapper is cached so Loader#inflector returns a stable object.
func (vm *VM) newZeitwerkLoader() *ZeitwerkLoader {
	l := zeitwerk.NewLoader()
	z := &ZeitwerkLoader{vm: vm, l: l, inf: &ZeitwerkInflector{in: l.Inflector()}}
	l.SetDefineAutoload(func(cpath, filePath string, isDir bool) {
		vm.zeitwerkDefineAutoload(cpath, filePath, isDir)
	})
	l.SetLoad(func(filePath string) error {
		vm.doRequire(filePath, false)
		return nil
	})
	l.OnUnload(func(cpath, _ string) {
		vm.zeitwerkRemoveConst(cpath)
	})
	return z
}

// zeitwerkDefineAutoload is the DefineAutoload seam: it wires one managed constant
// into rbgo. A managed .rb file becomes a real Module#autoload on its parent
// namespace (referencing the constant then requires the file); a managed directory
// (an implicit namespace) is autovivified as an empty Module now, since rbgo's
// autoload is file-based and a namespace directory has no file to require.
func (vm *VM) zeitwerkDefineAutoload(cpath, filePath string, isDir bool) {
	parent, cname := vm.zeitwerkResolveParent(cpath)
	if isDir {
		if _, ok := parent.consts[cname].(*RClass); !ok {
			vm.zeitwerkAutovivify(parent, cname)
		}
		return
	}
	vm.registerAutoloadOn(parent, cname, filePath)
}

// zeitwerkResolveParent walks the "::"-separated cpath to the RClass that should
// hold its final segment, returning that parent and the final constant name. Each
// intermediate namespace is descended into if already a defined constant, loaded
// first if it has a pending autoload (an explicit-namespace file), or otherwise
// autovivified as an empty Module — so a child constant can always be attached to
// a real module object.
func (vm *VM) zeitwerkResolveParent(cpath string) (*RClass, string) {
	segs := strings.Split(cpath, "::")
	parent := vm.cObject
	for i := 0; i < len(segs)-1; i++ {
		seg := segs[i]
		if child, ok := parent.consts[seg].(*RClass); ok {
			parent = child
			continue
		}
		if vm.tryAutoload(parent, seg) {
			if child, ok := parent.consts[seg].(*RClass); ok {
				parent = child
				continue
			}
		}
		parent = vm.zeitwerkAutovivify(parent, seg)
	}
	return parent, segs[len(segs)-1]
}

// zeitwerkAutovivify defines an empty namespace Module named `name` in parent's
// constant table, registering it both there and flat in vm.consts under its
// qualified path so it resolves like any other constant. It mirrors Zeitwerk
// autovivifying an implicit-namespace module for a managed directory.
func (vm *VM) zeitwerkAutovivify(parent *RClass, name string) *RClass {
	qualified := name
	if parent != vm.cObject && parent.name != "" {
		qualified = parent.name + "::" + name
	}
	m := newClass(qualified, nil)
	m.isModule = true
	parent.consts[name] = m
	vm.consts[qualified] = m
	return m
}

// zeitwerkRemoveConst is the OnUnload seam: it removes a managed constant from its
// parent's table (and any pending autoload for it) plus the flat vm.consts entry,
// so unload/reload truly clears the managed constants. A cpath whose namespace is
// already gone is a no-op.
func (vm *VM) zeitwerkRemoveConst(cpath string) {
	segs := strings.Split(cpath, "::")
	parent := vm.cObject
	for i := 0; i < len(segs)-1; i++ {
		child, ok := parent.consts[segs[i]].(*RClass)
		if !ok {
			return
		}
		parent = child
	}
	name := segs[len(segs)-1]
	delete(parent.consts, name)
	delete(parent.autoloads, name) // a no-op when the map is nil
	delete(vm.consts, cpath)
}
