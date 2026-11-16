// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	augeas "github.com/go-ruby-augeas/augeas"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the Augeas class and its Ruby-facing API (require
// "augeas"). The configuration-tree engine — the tree model, the XPath-like
// path language, the get/set/insert/move/remove editing operations and the lens
// framework with its built-in lenses — lives in github.com/go-augeas/augeas,
// wrapped by the ruby-augeas-shaped adapter github.com/go-ruby-augeas/augeas.
// rbgo owns only the object-model bridge: the Ruby Augeas class and its method
// surface. Each Augeas.create/open builds one per-VM engine tree so edits never
// leak across interpreters.
//
// Supported: the in-memory tree surface — get, exists?, set, setm, insert, rm,
// mv, match, defvar, defnode, label — plus the lens seam by NAME (text_store /
// text_retrieve over the built-in Hosts/Fstab/Shellvars/Simplevars/Ini/Keyvalue
// lenses), and the root/load_path/flags/error introspection.
//
// Deferred: file load / save. The gem's #load and #save resolve a lens from a
// module path and read/write files through the interpreter's filesystem; that
// wiring (module autoload + a Ruby-facing FileSystem seam) is out of scope for
// this binding. Callers parse and serialise text directly with text_store /
// text_retrieve by lens name instead.

// AugeasObj is a Ruby Augeas instance: one go-ruby-augeas handle over an engine
// tree.
//
// closed is what #close leaves behind. ruby-augeas's C layer frees the handle
// and sets the typed-data pointer to NULL (`aug_close(aug); DATA_PTR(s) = NULL;`
// in ext/augeas/_augeas.c), after which every method raises because they all go
// through aug_handle, whose nil check is
// `rb_raise(rb_eSystemCallError, "Failed to retrieve connection")`. There is
// nothing to free here — go-ruby-augeas's handle is an in-memory tree and
// exposes no Close — so the flag IS the whole mechanism, and the guard lives in
// the single `self` accessor that every instance method already calls
// (augeas_bind.go). ⛔ That is why "#close is blocked on a missing Close in the
// dependency", which this repository believed until #950 was looked at again,
// was the wrong layer: the question is not what #close FREES, it is what #close
// makes OBSERVABLE.
type AugeasObj struct {
	a      *augeas.Augeas
	closed bool
}

func (o *AugeasObj) ToS() string     { return "#<Augeas>" }
func (o *AugeasObj) Inspect() string { return o.ToS() }
func (o *AugeasObj) Truthy() bool    { return true }

// registerAugeas installs the Augeas class (require "augeas"): Augeas.create /
// Augeas.open build a handle, and the instance methods edit and query the tree.
func (vm *VM) registerAugeas() {
	cls := newClass("Augeas", vm.cObject)
	vm.consts["Augeas"] = cls

	std := vm.consts["StandardError"].(*RClass)
	augErr := newClass("Augeas::Error", std)
	cls.consts["Error"] = augErr
	vm.consts["Augeas::Error"] = augErr

	// The AUG_* flag constants, mirroring the ruby-augeas gem.
	for name, val := range map[string]int{
		"NONE":             augeas.None,
		"SAVE_BACKUP":      augeas.SaveBackup,
		"SAVE_NEWFILE":     augeas.SaveNewFile,
		"TYPE_CHECK":       augeas.TypeCheck,
		"NO_STDINC":        augeas.NoStdinc,
		"SAVE_NOOP":        augeas.SaveNoop,
		"NO_LOAD":          augeas.NoLoad,
		"NO_MODL_AUTOLOAD": augeas.NoModlAutoload,
		"ENABLE_SPAN":      augeas.EnableSpan,
	} {
		cls.consts[name] = object.IntValue(int64(val))
	}

	// ⛔ create AND open, NOT one function under two names. This binding used to
	// install the same positional native for both, which is wrong about `create`:
	// in the gem the two constructors have DIFFERENT signatures and only `open`
	// is positional.
	//
	//	lib/augeas.rb:   def self.open(root = nil, loadpath = nil, flags = NONE, &block)
	//	lib/augeas.rb:   def self.create(opts={}, &block)   -> Augeas::Facade::create
	//
	// Both honour a block the same way — `yield aug` with `ensure aug.close`,
	// answering the block's value — which is socketOpenWithBlock's contract
	// exactly, so each is wrapped in it rather than growing its own copy.
	//
	// augeas.New() is Open("", "", 0) (New, then three zero assignments), so the
	// no-argument case needs no branch of its own.

	// Augeas.open(root=nil, loadpath=nil, flags=NONE) — the positional form.
	openFn := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 0..3)", len(args))
		}
		return &AugeasObj{a: augeas.Open(augStrArg(args, 0), augStrArg(args, 1), augIntArg(args, 2))}
	}
	cls.smethods["open"] = &Method{name: "open", owner: cls, native: socketOpenWithBlock(cls, openFn)}

	// Augeas.create(opts={}) — the options-hash form.
	createFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		root, loadPath, flags := augCreateOpts(vm, args)
		return &AugeasObj{a: augeas.Open(root, loadPath, flags)}
	}
	cls.smethods["create"] = &Method{name: "create", owner: cls, native: socketOpenWithBlock(cls, createFn)}

	vm.registerAugeasMethods(cls)
}
