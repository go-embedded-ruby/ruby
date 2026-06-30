// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	find "github.com/go-ruby-find/find"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-find/find — the pure-Go, MRI-4.0.5 faithful
// port of the recursive-traversal algorithm of Ruby's `find` stdlib — into rbgo.
// The library owns the traversal engine: the depth-first walk over a set of start
// paths, MRI's exact ascending-sorted visit order, the Find.prune throw/catch
// control flow and the missing-start-path / per-entry error policy. This file is
// the thin shell that supplies the real filesystem seam (a find.Lister backed by
// rbgo's own Dir.children / File.exist? / File.lstat(...).directory?), drives the
// engine for Ruby's Find.find(*paths){ |path| } and maps the library's control
// signals back to Ruby:
//
//   - Find.prune is `throw :prune`; when the Ruby block raises that throw it
//     surfaces here as a throwSignal panic, which findYield recovers and turns
//     into the library's find.ErrPrune so the engine prunes the current path.
//   - a *find.MissingPathError (a start path that does not exist) is mapped back
//     to Errno::ENOENT, matching MRI raising it for a missing top-level path.
//
// Find was previously implemented in the frozen prelude; this replaces that
// inline Ruby with the library, leaving only the module shell to the native
// installer below (mirroring MRI where lib/find.rb defines Find on load).

// registerFind records the require "find" feature hook. Nothing is installed
// eagerly: the hook (run once by doRequire on the first `require "find"`) creates
// the Find module and its find / prune module-functions, mirroring MRI where
// lib/find.rb defines them only when loaded. The featureHooks map is already
// created by registerPrime (which runs first), so this only records the hook.
func (vm *VM) registerFind() {
	vm.featureHooks["find"] = vm.installFind
}

// installFind builds the Find module — the body MRI's lib/find.rb runs on load:
// the VERSION constant and the find / prune module-functions (callable as
// Find.find / Find.prune and, like module_function, as private instance methods
// on includers).
func (vm *VM) installFind() {
	mod := newClass("Find", nil)
	mod.isModule = true
	vm.consts["Find"] = mod

	mod.consts["VERSION"] = object.NewString("0.2.0")

	// define installs fn as both a module-method (Find.fn) and an instance method,
	// mirroring Ruby's `module_function :find, :prune`.
	define := func(name string, fn NativeFn) {
		m := &Method{name: name, owner: mod, native: fn}
		mod.smethods[name] = m
		mod.methods[name] = m
	}

	// Find.find(*paths, ignore_error: true) { |path| ... } walks each path
	// top-down, yielding every file and directory reached, in MRI's exact order.
	// With no block it returns an Enumerator. A missing start path raises
	// Errno::ENOENT; per-entry errors are swallowed when ignore_error is true
	// (the default), propagated otherwise.
	define("find", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		paths, ignoreError := findArgs(vm, args)
		if blk == nil {
			// MRI returns enum_for(:find, *paths, ignore_error:) — replay the call
			// with the original args (start paths plus any trailing kwargs hash).
			return enumFor(self, "find", args...)
		}
		findWalk(vm, findLister{vm: vm}, paths, ignoreError, func(path object.Value) {
			vm.callBlock(blk, []object.Value{path})
		})
		return object.NilV
	})

	// Find.prune skips the current file or directory; inside the block passed to
	// Find.find it is `throw :prune`, recovered by findYield and turned into
	// find.ErrPrune so the engine prunes the current path.
	define("prune", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		panic(throwSignal{tag: pruneTag, value: object.NilV})
	})
}

// pruneTag is the throw tag Find.prune uses (:prune). It is a Symbol value, so it
// matches by value exactly like Ruby's `catch(:prune) { ... throw :prune }`.
var pruneTag object.Value = object.Symbol("prune")

// findArgs splits Find.find's arguments into the start paths and the ignore_error
// flag. The trailing argument may be a kwargs Hash carrying :ignore_error
// (defaulting to true, MRI's default); every other argument is a start path,
// coerced to a String the way MRI's File entry points do (#to_path / #to_str).
func findArgs(vm *VM, args []object.Value) (paths []string, ignoreError bool) {
	ignoreError = true
	if h, ok := trailingHash(args); ok {
		if v, ok := h.Get(object.Symbol("ignore_error")); ok {
			ignoreError = v.Truthy()
		}
		args = args[:len(args)-1]
	}
	paths = make([]string, len(args))
	for i, a := range args {
		paths[i] = pathArg(vm, a)
	}
	return paths, ignoreError
}

// findWalk drives the library's Walk over paths with the given Lister (rbgo's
// real-filesystem findLister in production), invoking emit for every visited path
// in MRI's order. A *find.MissingPathError (a missing start path) is mapped to
// Errno::ENOENT; a wrapped Ruby exception (a per-entry failure under
// ignore_error: false) is re-raised as the original it came from.
func findWalk(vm *VM, lister find.Lister, paths []string, ignoreError bool, emit func(object.Value)) {
	err := find.Walk(paths, lister, func(path string) error {
		return findYield(vm, emit, path)
	}, ignoreError)
	if err == nil {
		return
	}
	if mp, ok := err.(*find.MissingPathError); ok {
		raise("Errno::ENOENT", "No such file or directory - %s", mp.Path)
	}
	// A non-prune yield error (under ignore_error: false) is a wrapped Ruby
	// exception raised by the Lister; re-raise the original so `rescue` sees it.
	if re, ok := err.(findRubyError); ok {
		panic(re.err)
	}
	// Defensive: any other engine error renders as a generic RuntimeError. The
	// library only returns MissingPathError, ErrPrune (consumed in findYield) or a
	// yield error, so this is unreachable in practice.
	raise("RuntimeError", "%s", err.Error())
}

// findYield invokes the Ruby block for one visited path, translating its control
// flow into the library's. A `throw :prune` (Find.prune) surfaces as a throwSignal
// panic and becomes find.ErrPrune, pruning the current path. Any Ruby exception
// the block raises is wrapped as a findRubyError so findWalk can re-raise the
// original after the engine returns it.
func findYield(vm *VM, emit func(object.Value), path string) (yieldErr error) {
	// Snapshot the per-frame tracking-stack depths so a `throw :prune` that unwinds
	// the block's frames straight to this recover (bypassing each frame's normal
	// pop) leaves __FILE__ / caller / backtraces consistent — the Kernel#catch idiom.
	fileStackDepth := len(vm.fileStack)
	frameNamesDepth := len(vm.frameNames)
	frameFilesDepth := len(vm.frameFiles)
	requireDirsDepth := len(vm.requireDirs)
	defer func() {
		if r := recover(); r != nil {
			if sig, ok := r.(throwSignal); ok && sig.tag == pruneTag {
				vm.fileStack = vm.fileStack[:fileStackDepth]
				vm.frameNames = vm.frameNames[:frameNamesDepth]
				vm.frameFiles = vm.frameFiles[:frameFilesDepth]
				vm.requireDirs = vm.requireDirs[:requireDirsDepth]
				yieldErr = find.ErrPrune
				return
			}
			// A Ruby exception from the block: hand it to the engine wrapped so the
			// walk stops and findWalk re-raises the original.
			if re, ok := r.(RubyError); ok {
				yieldErr = findRubyError{err: re}
				return
			}
			panic(r)
		}
	}()
	emit(object.NewString(path))
	return nil
}

// findRubyError wraps a Ruby exception raised inside the Find.find block so it can
// travel back through the library's error channel and be re-raised verbatim.
type findRubyError struct{ err RubyError }

func (e findRubyError) Error() string { return e.err.Error() }

// findLister is the find.Lister rbgo supplies: it answers the engine's filesystem
// questions through rbgo's own File / Dir methods (vm.send), so a script that
// redefines File.exist? / File.lstat / Dir.children sees its overrides honoured,
// exactly as MRI's lib/find.rb does. Errors raised by those methods (a path that
// vanished mid-walk, EACCES, …) are returned to the engine, which applies the
// ignore_error policy.
type findLister struct{ vm *VM }

// Exist reports whether a START path exists (Ruby File.exist?). The engine calls
// it only for the start paths; a false makes Walk fail before any yield, mapping
// to Errno::ENOENT as MRI does.
func (l findLister) Exist(path string) bool {
	cFile := l.vm.consts["File"]
	return l.vm.send(cFile, "exist?", []object.Value{object.NewString(path)}, nil).Truthy()
}

// IsDir reports whether path is a directory to descend into, via
// File.lstat(path).directory? — lstat does NOT follow symlinks, so a symlink to a
// directory reports false, matching MRI's lib/find.rb. A lstat failure (the path
// vanished mid-walk) is returned as a Go error for the engine to apply the
// ignore_error policy.
func (l findLister) IsDir(path string) (bool, error) {
	cFile := l.vm.consts["File"]
	isDir, err := findCall(l.vm, func() object.Value {
		st := l.vm.send(cFile, "lstat", []object.Value{object.NewString(path)}, nil)
		return l.vm.send(st, "directory?", nil, nil)
	})
	if err != nil {
		return false, err
	}
	return isDir.Truthy(), nil
}

// Children lists the entries of dir (Ruby Dir.children): base names only, no "."
// or "..". The engine calls it only when IsDir(dir) was true. A failure is
// returned as a Go error for the ignore_error policy.
func (l findLister) Children(dir string) ([]string, error) {
	cDir := l.vm.consts["Dir"]
	v, err := findCall(l.vm, func() object.Value {
		return l.vm.send(cDir, "children", []object.Value{object.NewString(dir)}, nil)
	})
	if err != nil {
		return nil, err
	}
	arr := arrArg(v)
	out := make([]string, len(arr.Elems))
	for i, e := range arr.Elems {
		out[i] = strArg(e)
	}
	return out, nil
}

// findCall runs a Lister probe (lstat / Dir.children), recovering a Ruby exception
// it raises and returning it as a Go error so the engine can apply the
// ignore_error policy. A non-RubyError panic (a real Go bug) is re-raised.
func findCall(vm *VM, fn func() object.Value) (result object.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok {
				err = findRubyError{err: re}
				return
			}
			panic(r)
		}
	}()
	return fn(), nil
}
