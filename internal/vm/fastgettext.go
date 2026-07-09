// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	fastgettext "github.com/go-ruby-fast-gettext/fast-gettext"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the FastGettext module and its Ruby-facing API (require
// "fast_gettext"). The translation engine — text domains, pluggable
// repositories, the current-locale / current-text-domain state and the
// gettext-style lookup helpers with plural-form selection — lives in
// github.com/go-ruby-fast-gettext/fast-gettext, a pure-Go port of the
// fast_gettext gem. rbgo owns only the object-model bridge: the Ruby FastGettext
// module surface. The engine's global state (current domain, locale, cache and
// registered domains) is held per-VM on an *Instance so translations configured
// in one interpreter never leak into another.

// registerFastGettext installs the FastGettext module (require "fast_gettext"):
// text_domain=/locale=/available_locales= configure the per-VM instance,
// add_text_domain registers an in-memory catalog, and _/n_/s_/p_ translate.
func (vm *VM) registerFastGettext() {
	vm.fastGettext = fastgettext.New()

	mod := newClass("FastGettext", nil)
	mod.isModule = true
	vm.consts["FastGettext"] = mod
	// FastGettext::Translation is the mixin the gem's helpers live on; alias it to
	// the module so `FastGettext::Translation._` also resolves.
	mod.consts["Translation"] = mod
	vm.consts["FastGettext::Translation"] = mod

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// Current text domain and locale.
	sm("text_domain", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return strOrNil(vm.fastGettext.TextDomain())
	})
	sm("text_domain=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.fastGettext.SetTextDomain(fgStrArg(args))
		return args[0]
	})
	sm("locale", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.fastGettext.Locale())
	})
	sm("locale=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.fastGettext.SetLocale(fgStrArg(args))
		return args[0]
	})
	sm("set_locale", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.fastGettext.SetLocale(fgStrArg(args)))
	})

	// Available and default locales / default text domain.
	sm("available_locales", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return strSliceToRuby(vm.fastGettext.AvailableLocales())
	})
	sm("available_locales=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.fastGettext.SetAvailableLocales(fgStrSliceArg(args))
		return args[0]
	})
	sm("default_locale", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return strOrNil(vm.fastGettext.DefaultLocale())
	})
	sm("default_locale=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.fastGettext.SetDefaultLocale(fgStrArg(args))
		return args[0]
	})
	sm("default_text_domain", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return strOrNil(vm.fastGettext.DefaultTextDomain())
	})
	sm("default_text_domain=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.fastGettext.SetDefaultTextDomain(fgStrArg(args))
		return args[0]
	})

	// add_text_domain(name, translations: {locale => {key => value}}[, plurals:
	// {locale => {[singular, plural] => [form0, form1, ...]}}]) — register an
	// in-memory catalog for the domain.
	sm("add_text_domain", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.fastGettextAddDomain(args)
		return object.NilV
	})

	// Lookup helpers: _ / n_ / s_ / p_ and key_exist?.
	sm("_", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(vm.fastGettext.Gettext(fgStrArg(args)))
	})
	sm("n_", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 3)", len(args))
		}
		return object.NewString(vm.fastGettext.NGettext(args[0].ToS(), args[1].ToS(), fgIntArg(args[2])))
	})
	sm("s_", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 1 {
			return object.NewString(vm.fastGettext.SGettextSep(args[0].ToS(), args[1].ToS()))
		}
		return object.NewString(vm.fastGettext.SGettext(fgStrArg(args)))
	})
	sm("p_", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		return object.NewString(vm.fastGettext.PGettext(args[0].ToS(), args[1].ToS()))
	})
	sm("key_exist?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.fastGettext.KeyExist(fgStrArg(args)))
	})

	// with_locale(loc) { ... } / with_domain(name) { ... } — run the block with
	// the locale / text domain temporarily switched, then restore it.
	sm("with_locale", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		var result object.Value = object.NilV
		vm.fastGettext.WithLocale(fgStrArg(args), func() {
			result = vm.callBlock(blk, nil)
		})
		return result
	})
	sm("with_domain", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		var result object.Value = object.NilV
		vm.fastGettext.WithDomain(fgStrArg(args), func() {
			result = vm.callBlock(blk, nil)
		})
		return result
	})
}
