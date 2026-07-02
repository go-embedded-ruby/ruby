// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// PublicSuffixDomain wraps a *publicsuffix.Domain as a Ruby PublicSuffix::Domain
// object. The Public-Suffix-List decomposition (tld / sld / trd and the
// registrable-domain / subdomain views) lives in the
// github.com/go-ruby-public-suffix/public-suffix library; this shell only reports
// the Ruby class and delegates each reader (see publicsuffix_bind.go).
type PublicSuffixDomain struct{ d publicSuffixDomain }

func (d *PublicSuffixDomain) ToS() string     { return d.d.Name() }
func (d *PublicSuffixDomain) Inspect() string { return "#<PublicSuffix::Domain " + d.d.Name() + ">" }
func (d *PublicSuffixDomain) Truthy() bool    { return true }

// registerPublicSuffix installs the PublicSuffix module (require "public_suffix"):
// PublicSuffix.parse (→ a PublicSuffix::Domain), PublicSuffix.domain,
// PublicSuffix.valid? and the DomainInvalid / DomainNotAllowed error tree. The
// parser and the list itself live in the go-ruby-public-suffix library; this
// module is the thin wiring that maps a Ruby hostname String (and the
// ignore_private: / default_rule: keyword options) to a publicsuffix.Parse /
// Valid / RegistrableDomain call (see publicsuffix_bind.go).
func (vm *VM) registerPublicSuffix() {
	mod := newClass("PublicSuffix", nil)
	mod.isModule = true
	vm.consts["PublicSuffix"] = mod
	vm.registerPublicSuffixErrors(mod)

	domCls := newClass("PublicSuffix::Domain", vm.cObject)
	mod.consts["Domain"] = domCls
	vm.consts["PublicSuffix::Domain"] = domCls
	vm.registerPublicSuffixDomain(domCls)

	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// PublicSuffix.parse(name, **opts) decomposes a hostname into a
	// PublicSuffix::Domain, raising PublicSuffix::DomainInvalid on a name with no
	// valid registrable domain.
	def("parse", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		return publicSuffixParse(strArg(args[0]), publicSuffixOpts(args))
	})

	// PublicSuffix.domain(name, **opts) returns just the registrable domain
	// String (sld.tld), or nil when the name has none.
	def("domain", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		return publicSuffixRegistrable(strArg(args[0]), publicSuffixOpts(args))
	})

	// PublicSuffix.valid?(name, **opts) reports whether the name has a valid
	// registrable domain under the list.
	def("valid?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		return object.Bool(publicSuffixValid(strArg(args[0]), publicSuffixOpts(args)))
	})
}

// registerPublicSuffixErrors installs the PublicSuffix error tree mirroring the
// gem (Error < StandardError; DomainInvalid < Error; DomainNotAllowed <
// DomainInvalid). Each class is registered both as a nested constant of
// PublicSuffix (so Ruby `PublicSuffix::DomainInvalid` resolves it) and under its
// qualified name in the top-level table (so a re-raised library error's
// exceptionObject lookup finds the very same class).
func (vm *VM) registerPublicSuffixErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("Error", "PublicSuffix::Error", std)
	invalid := reg("DomainInvalid", "PublicSuffix::DomainInvalid", base)
	reg("DomainNotAllowed", "PublicSuffix::DomainNotAllowed", invalid)
}

// registerPublicSuffixDomain installs the PublicSuffix::Domain instance surface:
// the level readers (tld / sld / trd), the registrable domain / subdomain views
// and the domain? / subdomain? predicates.
func (vm *VM) registerPublicSuffixDomain(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) publicSuffixDomain { return v.(*PublicSuffixDomain).d }

	// tld / sld / trd are the three levels; each is nil (not "") when absent, as
	// the gem models a missing level.
	d("tld", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return publicSuffixLevel(self(v).GetTLD(), self(v).HasTLD())
	})
	d("sld", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return publicSuffixLevel(self(v).GetSLD(), self(v).HasSLD())
	})
	d("trd", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return publicSuffixLevel(self(v).GetTRD(), self(v).HasTRD())
	})
	// name is the full hostname (trd.sld.tld); domain is the registrable domain
	// (sld.tld); subdomain is trd.sld.tld — each nil when its parts are absent.
	d("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Name())
	})
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Name())
	})
	d("domain", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return publicSuffixOptStr(self(v).DomainName())
	})
	d("subdomain", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return publicSuffixOptStr(self(v).Subdomain())
	})
	d("domain?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).IsDomain())
	})
	d("subdomain?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).IsSubdomain())
	})
}
