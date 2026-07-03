// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerRipper installs a loadable shell for the ripper standard library
// (require "ripper"). Ripper is a full Ruby-source S-expression/event parser — a
// large subsystem of its own; for now the class exists so `require "ripper"`
// completes and load-time references resolve (Puppet requires ripper at load but
// only calls Ripper.sexp from a method body). The class methods that actually
// parse source (sexp / sexp_raw / lex / tokenize / parse / slice) raise
// NotImplementedError until a real Ripper front-end lands.
func (vm *VM) registerRipper() {
	rip := newClass("Ripper", vm.cObject)
	vm.consts["Ripper"] = object.Wrap(rip)

	notImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "Ripper.%s not yet supported (real Ripper front-end pending)", what)
		}
	}
	for _, m := range []string{"sexp", "sexp_raw", "lex", "tokenize", "parse", "slice"} {
		rip.smethods[m] = &Method{name: m, owner: rip, native: notImpl(m)}
	}
}
