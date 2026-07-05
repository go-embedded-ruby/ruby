// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	bundler "github.com/go-ruby-bundler/bundler"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// bundlerVERSION is the Bundler release the library targets (2.6.x), surfaced as
// Bundler::VERSION.
const bundlerVERSION = "2.6.9"

// registerBundler installs the Bundler module (require "bundler"): the pure
// compute core of Ruby's Bundler backed by github.com/go-ruby-bundler/bundler.
// The faithful surface is the Gemfile.lock codec (Bundler::LockfileParser, a
// byte-for-byte round-trip via #to_lock), the canonical Gemfile DSL reader
// (Bundler::Dsl.evaluate → dependencies/sources/ruby_version, raising
// Bundler::GemfileError with the offending line on an arbitrary-Ruby form), and
// the backtracking resolver (Bundler.resolve over an in-memory Bundler::Index,
// yielding a resolved Bundler::LazySpecification set or raising
// Bundler::VersionConflict). It builds on the prelude's Gem::Version /
// Gem::Requirement (RubyGems is preloaded), so no Gem::* is re-registered here.
// The error tree mirrors the gem (Bundler::BundlerError < StandardError, with
// GemfileError/LockfileError/VersionConflict/… beneath it). The network index
// fetch and the install filesystem writes are host-side seams (see
// bundler_bind.go for the wrappers and value conversions).
func (vm *VM) registerBundler() {
	mod := newClass("Bundler", nil)
	mod.isModule = true
	vm.consts["Bundler"] = mod
	mod.consts["VERSION"] = object.NewString(bundlerVERSION)

	vm.registerBundlerErrors(mod)

	cLock := vm.bundlerClass(mod, "LockfileParser", "Bundler::LockfileParser")
	cSpec := vm.bundlerClass(mod, "LazySpecification", "Bundler::LazySpecification")
	cDep := vm.bundlerClass(mod, "Dependency", "Bundler::Dependency")
	cDsl := vm.bundlerClass(mod, "Dsl", "Bundler::Dsl")
	cIndex := vm.bundlerClass(mod, "Index", "Bundler::Index")

	// Bundler.resolve(dependencies, index) runs the backtracking resolver over an
	// in-memory Bundler::Index (the gem-index fetch is a host seam).
	mod.smethods["resolve"] = &Method{name: "resolve", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.bundlerResolve(args)
		}}

	vm.registerBundlerLockfile(cLock)
	vm.registerBundlerSpec(cSpec)
	vm.registerBundlerDependency(cDep)
	vm.registerBundlerDsl(cDsl)
	vm.registerBundlerIndex(cIndex)
}

// bundlerClass creates a Bundler::* class under cObject, records it flat (for
// classOf) and nests it under the Bundler module by its simple name.
func (vm *VM) bundlerClass(mod *RClass, simple, qualified string) *RClass {
	c := newClass(qualified, vm.cObject)
	vm.consts[qualified] = c
	mod.consts[simple] = c
	return c
}

// registerBundlerErrors installs the Bundler error tree, mirroring the gem: the
// root Bundler::BundlerError < StandardError and the failure-specific subclasses
// beneath it. The three the library actually raises — GemfileError (an
// unparseable Gemfile line), LockfileError (a malformed Gemfile.lock) and
// VersionConflict (an unsatisfiable resolution) — are keyed by name so a library
// error maps to its Ruby class; the rest complete the tree the gem defines.
func (vm *VM) registerBundlerErrors(mod *RClass) {
	defs := []struct{ qualified, parent string }{
		{"Bundler::BundlerError", "StandardError"},
		{"Bundler::GemfileError", "Bundler::BundlerError"},
		{"Bundler::LockfileError", "Bundler::BundlerError"},
		{"Bundler::VersionConflict", "Bundler::BundlerError"},
		{"Bundler::GemNotFound", "Bundler::BundlerError"},
		{"Bundler::GemfileNotFound", "Bundler::BundlerError"},
		{"Bundler::InstallError", "Bundler::BundlerError"},
	}
	for _, d := range defs {
		parent := vm.consts[d.parent].(*RClass)
		cls := newClass(d.qualified, parent)
		vm.consts[d.qualified] = cls
		mod.consts[d.qualified[len("Bundler::"):]] = cls
	}
}

// registerBundlerLockfile installs Bundler::LockfileParser: the .new(contents)
// parse (raising Bundler::LockfileError on a malformed lock) and the read surface
// over the parsed lock, including the byte-exact re-emission #to_lock.
func (vm *VM) registerBundlerLockfile(c *RClass) {
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			l, err := bundler.ParseLockfile(strArg(args[0]))
			if err != nil {
				return raise("Bundler::LockfileError", "%s", err.Error())
			}
			return &BundlerLockfile{l}
		}}

	lf := func(self object.Value) *bundler.Lockfile { return self.(*BundlerLockfile).l }
	c.define("specs", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bundlerSpecs(bundlerAllSpecs(lf(self)))
	})
	c.define("dependencies", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bundlerDeps(lf(self).Dependencies)
	})
	c.define("platforms", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bundlerStrArray(lf(self).Platforms)
	})
	c.define("bundler_version", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bundlerStrOrNil(lf(self).BundledWith)
	})
	c.define("ruby_version", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bundlerStrOrNil(lf(self).RubyVersion)
	})
	// #to_lock re-emits the canonical lockfile bytes; on a well-formed lock it
	// reproduces the input exactly.
	c.define("to_lock", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(lf(self).String())
	})
	c.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(lf(self).String())
	})
}

// registerBundlerSpec installs Bundler::LazySpecification: the read surface over
// one resolved/locked spec (name, version, platform, full_name and the spec's own
// runtime dependencies).
func (vm *VM) registerBundlerSpec(c *RClass) {
	sp := func(self object.Value) *bundler.Spec { return self.(*BundlerSpec).s }
	c.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(sp(self).Name)
	})
	c.define("version", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(sp(self).Version.String())
	})
	c.define("platform", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bundlerPlatform(sp(self).Platform)
	})
	c.define("full_name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(bundlerFullName(sp(self)))
	})
	c.define("dependencies", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bundlerSpecDeps(sp(self).Dependencies)
	})
	c.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(bundlerFullName(sp(self)))
	})
}

// registerBundlerDependency installs Bundler::Dependency: the .new(name,
// *constraints) constructor (raising ArgumentError on a malformed constraint) and
// the read surface (name, requirement, groups, platforms) shared with the parsed
// Gemfile/lockfile dependencies.
func (vm *VM) registerBundlerDependency(c *RClass) {
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
			}
			cons := make([]string, 0, len(args)-1)
			for _, a := range args[1:] {
				cons = append(cons, strArg(a))
			}
			d, err := bundler.NewDependency(strArg(args[0]), cons...)
			if err != nil {
				return raise("ArgumentError", "%s", err.Error())
			}
			return &BundlerDependency{d}
		}}

	dp := func(self object.Value) *bundler.Dependency { return self.(*BundlerDependency).d }
	c.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(dp(self).Name)
	})
	c.define("requirement", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(bundlerReqString(dp(self).Requirement))
	})
	c.define("groups", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bundlerSymArray(dp(self).Groups)
	})
	c.define("platforms", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bundlerStrArray(dp(self).Platforms)
	})
	c.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		d := dp(self)
		return object.NewString(d.Name + " (" + bundlerReqString(d.Requirement) + ")")
	})
}

// registerBundlerDsl installs Bundler::Dsl: the .evaluate(contents) reader of the
// canonical static Gemfile forms (raising Bundler::GemfileError, naming the line,
// on an arbitrary-Ruby form) and the read surface over the result.
func (vm *VM) registerBundlerDsl(c *RClass) {
	c.smethods["evaluate"] = &Method{name: "evaluate", owner: c,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			gf, err := bundler.ParseGemfile(strArg(args[0]))
			if err != nil {
				return raise("Bundler::GemfileError", "%s", err.Error())
			}
			return &BundlerGemfile{gf}
		}}

	gf := func(self object.Value) *bundler.Gemfile { return self.(*BundlerGemfile).gf }
	c.define("dependencies", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bundlerDeps(gf(self).Dependencies())
	})
	c.define("sources", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bundlerStrArray(gf(self).Sources)
	})
	c.define("ruby_version", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return bundlerStrOrNil(gf(self).RubyVersion)
	})
}

// registerBundlerIndex installs Bundler::Index: the in-memory resolution index
// the host fills with .add_gem(name, version, deps) and feeds to Bundler.resolve.
// A real gem-index fetch (network / compact index) is a host seam.
func (vm *VM) registerBundlerIndex(c *RClass) {
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &BundlerIndex{m: bundler.MapIndex{}}
		}}

	// #add_gem(name, version, deps = {}) registers one candidate gem version and
	// its runtime dependencies (a Hash of gem-name => constraint-string), raising
	// ArgumentError on a malformed version.
	c.define("add_gem", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
		}
		var deps []bundler.SpecDependency
		if len(args) > 2 {
			if h, ok := args[2].(*object.Hash); ok {
				for _, k := range h.Keys {
					v, _ := h.Get(k)
					deps = append(deps, bundler.Dep(k.ToS(), v.ToS()))
				}
			}
		}
		if err := self.(*BundlerIndex).m.AddGem(strArg(args[0]), strArg(args[1]), deps...); err != nil {
			return raise("ArgumentError", "%s", err.Error())
		}
		return self
	})
}

// bundlerResolve implements Bundler.resolve(dependencies, index): it collects the
// Bundler::Dependency roots and the Bundler::Index, runs the library's
// backtracking resolver, and returns the resolved Bundler::LazySpecification set
// (sorted by name) or raises Bundler::VersionConflict.
func (vm *VM) bundlerResolve(args []object.Value) object.Value {
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Array", classNameOf(args[0]))
	}
	idx, ok := args[1].(*BundlerIndex)
	if !ok {
		raise("TypeError", "expected a Bundler::Index")
	}
	deps := make([]*bundler.Dependency, 0, len(arr.Elems))
	for _, e := range arr.Elems {
		bd, ok := e.(*BundlerDependency)
		if !ok {
			raise("TypeError", "expected a Bundler::Dependency")
		}
		deps = append(deps, bd.d)
	}
	res, err := bundler.Resolve(deps, idx.m, bundlerGemSource())
	if err != nil {
		return raise("Bundler::VersionConflict", "%s", err.Error())
	}
	return bundlerSpecs(res.Specs())
}
