// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	confd "github.com/go-ruby-confd/confd"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the Confd module and its Ruby-facing API (require "confd").
// The template engine — Go text/template driven by confd's own function map
// (getv/getvs/gets/exists/base64Encode/json/toUpper/…) and its byte-level
// renderer — lives in github.com/abtreece/confd (the maintained pure-Go confd
// fork), wrapped by the Ruby-facing adapter github.com/go-ruby-confd/confd. rbgo
// owns only the object-model bridge: the Ruby Confd surface and the Ruby⇄Go
// value conversion. confd has no Ruby gem, so this is a clean idiomatic API
// rather than a port of one.
//
// Confd.render(template, vars) is the whole common path: it seeds confd's
// in-memory backend (confd.MemoryBackend, no disk, no network) from a Ruby Hash
// — string values keyed by confd's "/"-delimited paths, with nested Hashes
// flattened into path segments — and runs confd.RenderString, the adapter's
// hermetic single-template renderer. A bad template or a missing key raises
// Confd::Error. The on-disk Processor (conf.d/*.toml + templates/*.tmpl over a
// live backend, writing target files) is deliberately NOT exposed: it needs a
// real confd directory layout and performs filesystem side effects, which do not
// map onto a hermetic, value-in/string-out Ruby call — render covers the
// no-disk/no-network path the binding is meant to offer. See confd_bind.go for
// the value bridge.

// registerConfd installs the Confd module (require "confd"): Confd.render renders
// a confd template against key/value data supplied as a Ruby Hash, plus the
// Confd::Error raised on a bad template or a missing key.
func (vm *VM) registerConfd() {
	mod := newClass("Confd", nil)
	mod.isModule = true
	vm.consts["Confd"] = mod
	vm.registerConfdError(mod)

	// confd's engine logs the (internally managed) staged-file lifecycle at INFO;
	// raise the floor to error so a successful render is silent and only genuine
	// failures — which rbgo re-raises as Confd::Error anyway — reach the log.
	_ = confd.SetLogLevel("error")

	mod.smethods["render"] = &Method{name: "render", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.confdRender(args)
	}}
}

// registerConfdError installs Confd::Error < StandardError, registered both as the
// nested Confd::Error constant and under its qualified name in the top-level
// table so a re-raised render failure rescues as the right Ruby class.
func (vm *VM) registerConfdError(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	c := newClass("Confd::Error", std)
	mod.consts["Error"] = c
	vm.consts["Confd::Error"] = c
}
