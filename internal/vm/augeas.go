// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

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
type AugeasObj struct{ a *augeas.Augeas }

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

	// Augeas.create(root=nil, loadpath=nil, flags=0) / Augeas.open(...) — build a
	// handle. With no arguments the engine starts on an empty tree.
	open := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			return &AugeasObj{a: augeas.New()}
		}
		return &AugeasObj{a: augeas.Open(augStrArg(args, 0), augStrArg(args, 1), augIntArg(args, 2))}
	}
	cls.smethods["create"] = &Method{name: "create", owner: cls, native: open}
	cls.smethods["open"] = &Method{name: "open", owner: cls, native: open}

	vm.registerAugeasMethods(cls)
}
