// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerI18n installs the I18n module (require "i18n"): the translate /
// localize surface (I18n.translate/#t, I18n.localize/#l), the locale accessors
// (I18n.locale, I18n.default_locale, I18n.available_locales, I18n.exists?) and
// the I18n::Backend::Simple store reached through I18n.backend. The dotted-key
// lookup, %{name} / %<count>d interpolation, :count pluralization, :default and
// fallback-locale chains and the strftime localization all live in the
// github.com/go-ruby-i18n/i18n library — the pure-Go port of the i18n gem's
// I18n core; this file is the thin shell mapping rbgo's Hash/Symbol/String
// option model onto that library and back (see i18n_bind.go for the value
// conversions and the Temporal seam). The error tree mirrors the gem
// (I18n::ArgumentError < ::ArgumentError and its MissingTranslationData /
// MissingInterpolationArgument / ReservedInterpolationKey /
// InvalidPluralizationData subclasses). Loading translation *files* is the host
// seam the gem documents; rbgo exposes the store side of it via
// I18n.backend.store_translations, matching the gem's own load path.
func (vm *VM) registerI18n() {
	mod := newClass("I18n", nil)
	mod.isModule = true
	vm.consts["I18n"] = mod

	vm.i18nInst = newI18nInstance()
	vm.registerI18nErrors(mod)
	vm.registerI18nBackend(mod)

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// I18n.locale / I18n.locale= — the current locale, a Symbol (the gem stores
	// and returns symbols).
	sm("locale", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(vm.i18nInst.Locale())
	})
	sm("locale=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.i18nInst.SetLocale(i18nLocaleArg(args[0]))
		return args[0]
	})
	sm("default_locale", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(vm.i18nInst.DefaultLocale())
	})
	sm("default_locale=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.i18nInst.SetDefaultLocale(i18nLocaleArg(args[0]))
		return args[0]
	})
	sm("available_locales", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return i18nSymbolArray(vm.i18nInst.AvailableLocales())
	})
	sm("backend", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &I18nBackend{vm: vm}
	})
	sm("exists?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		key := i18nKeyArg(args[0])
		if len(args) > 1 {
			return object.Bool(vm.i18nInst.Exists(key, i18nLocaleArg(args[1])))
		}
		return object.Bool(vm.i18nInst.Exists(key))
	})

	// I18n.translate(key, **opts) / #t and the bang forms that raise on a missing
	// key. store_translations is reached through I18n.backend.
	translate := func(bang bool) NativeFn {
		return func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
			}
			opts := vm.i18nOptions(args, bang)
			val, err := vm.i18nInst.Translate(i18nKeyArg(args[0]), opts)
			if err != nil {
				return vm.raiseI18n(err)
			}
			return vm.fromI18nValue(val)
		}
	}
	sm("translate", translate(false))
	sm("t", translate(false))
	sm("translate!", translate(true))
	sm("t!", translate(true))

	// I18n.localize(object, **opts) / #l — strftime a host Date/Time under the
	// locale's format tree.
	localize := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		format, named, opts := vm.i18nLocalizeOptions(args)
		out, err := vm.i18nInst.Localize(vm.temporalOf(args[0]), format, named, opts)
		if err != nil {
			return vm.raiseI18n(err)
		}
		return object.NewString(out)
	}
	sm("localize", localize)
	sm("l", localize)
}

// registerI18nBackend installs I18n::Backend::Simple, the store handle
// I18n.backend returns. store_translations deep-merges a locale's nested data
// into the shared instance's Simple backend; available_locales lists the stored
// locales. The backend is a thin handle over vm.i18nInst — every instance shares
// the one process-wide store, matching I18n.backend being a singleton.
func (vm *VM) registerI18nBackend(mod *RClass) {
	backendMod := newClass("I18n::Backend", nil)
	backendMod.isModule = true
	mod.consts["Backend"] = backendMod
	vm.consts["I18n::Backend"] = backendMod

	simple := newClass("I18n::Backend::Simple", vm.cObject)
	backendMod.consts["Simple"] = simple
	vm.consts["I18n::Backend::Simple"] = simple

	simple.define("store_translations", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		data := i18nDataArg(args[1])
		vm.i18nInst.Backend().StoreTranslations(i18nLocaleArg(args[0]), data)
		return args[1]
	})
	simple.define("available_locales", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return i18nSymbolArray(vm.i18nInst.Backend().AvailableLocales())
	})
}

// I18nBackend is the Ruby handle I18n.backend returns: an
// I18n::Backend::Simple over the process-wide store held on the VM. It carries
// no state of its own — every method routes to vm.i18nInst — so all handles see
// the same translations, exactly as the gem's singleton backend does.
type I18nBackend struct{ vm *VM }

func (b *I18nBackend) ToS() string     { return "#<I18n::Backend::Simple>" }
func (b *I18nBackend) Inspect() string { return "#<I18n::Backend::Simple>" }
func (b *I18nBackend) Truthy() bool    { return true }

// registerI18nErrors installs the I18n error tree mirroring the gem:
// I18n::ArgumentError < ::ArgumentError, and its MissingTranslationData,
// MissingInterpolationArgument, ReservedInterpolationKey and
// InvalidPluralizationData subclasses. Each is registered both scoped (under
// I18n) and flat in vm.consts so raise can find it by its qualified name.
func (vm *VM) registerI18nErrors(mod *RClass) {
	argErr := vm.consts["ArgumentError"].(*RClass)
	base := newClass("I18n::ArgumentError", argErr)
	mod.consts["ArgumentError"] = base
	vm.consts["I18n::ArgumentError"] = base

	for simple, qualified := range map[string]string{
		"MissingTranslationData":       "I18n::MissingTranslationData",
		"MissingInterpolationArgument": "I18n::MissingInterpolationArgument",
		"ReservedInterpolationKey":     "I18n::ReservedInterpolationKey",
		"InvalidPluralizationData":     "I18n::InvalidPluralizationData",
	} {
		c := newClass(qualified, base)
		mod.consts[simple] = c
		vm.consts[qualified] = c
	}
	// I18n::MissingTranslation is the gem's alias for the raised class; point it
	// at the same object so `rescue I18n::MissingTranslation` works.
	mtd := vm.consts["I18n::MissingTranslationData"].(*RClass)
	mod.consts["MissingTranslation"] = mtd
	vm.consts["I18n::MissingTranslation"] = mtd
}
