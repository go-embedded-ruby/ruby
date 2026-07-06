// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rails "github.com/go-ruby-rails/rails"
	railties "github.com/go-ruby-railties/railties"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerRails layers the `rails` meta-gem onto the boot framework the railties
// binding already installed (registerRailties, which must run first). In Ruby the
// `rails` gem ships almost no code of its own: it is the meta-gem that ties the
// framework's components together and vends the top-level Rails module singleton
// accessors (Rails.application / env / root / logger / cache / configuration /
// autoloaders / groups / public_path / backtrace_cleaner / error), the
// Rails::VERSION constant, and Rails::EnvironmentInquirer (the Rails.env type).
//
// This binding does NOT re-create the Rails module or its Railtie/Engine/
// Application/StringInquirer classes — railties already registered those and the
// "rails" provided feature. It extends the existing Rails module in place with
// the singleton accessors, adds Rails::VERSION and Rails::EnvironmentInquirer, and
// wires `require "rails/all"` to pull in every shipped framework component.
//
// The application-state accessors (root / public_path / configuration /
// autoloaders / cache) delegate to the meta-gem, which resolves them against the
// railties Application registered through rails.SetApplication — the App seam
// bridged here by railsAppAdapter. Rails.application itself keeps returning the
// Ruby Application wrapper (a *RailsAppVal) that Ruby set.
func (vm *VM) registerRails() {
	// registerRailties (which runs first) already created the Rails module and its
	// Railtie/Engine/Application/StringInquirer classes; this binding extends that
	// module in place rather than recreating it.
	mod := vm.consts["Rails"].(*RClass)

	def := func(name string, fn func(*VM, object.Value, []object.Value, *Proc) object.Value) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// Rails.application / Rails.application= — the getter returns the Ruby
	// Application wrapper Ruby registered; the setter records it and threads the
	// underlying railties Application into the meta-gem's App seam so the
	// delegating accessors resolve against it. Assigning nil clears the app.
	def("application", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.railsApp == nil {
			return object.NilV
		}
		return vm.railsApp
	})
	def("application=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if object.IsNil(args[0]) {
			vm.railsApp = nil
			rails.SetApplication(nil)
			return object.NilV
		}
		av, ok := args[0].(*RailsAppVal)
		if !ok {
			raise("TypeError", "wrong argument type %s (expected a Rails::Application)", vm.classOf(args[0]).name)
		}
		vm.railsApp = av
		rails.SetApplication(railsAppAdapter{app: av.app})
		return args[0]
	})

	// Rails.env / Rails.env= — the current environment as an EnvironmentInquirer
	// (resolved from RAILS_ENV, then RACK_ENV, then "development", by the meta-gem).
	def("env", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RailsEnvVal{e: rails.Env(), cls: vm.consts["Rails::EnvironmentInquirer"].(*RClass)}
	})
	def("env=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		e := rails.SetEnv(railsGroupStr(rackArg(args)))
		return &RailsEnvVal{e: e, cls: vm.consts["Rails::EnvironmentInquirer"].(*RClass)}
	})

	// Rails.root / Rails.public_path — filesystem paths delegated to the bound
	// application (nil before an application is registered, as in Ruby).
	def("root", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.railsApp == nil {
			return object.NilV
		}
		return railsStr(rails.Root())
	})
	def("public_path", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.railsApp == nil {
			return object.NilV
		}
		return railsStr(rails.PublicPath())
	})

	// Rails.configuration — the application's configuration object, resolved
	// through the meta-gem's App seam and re-wrapped as the Ruby configuration; nil
	// before an application is registered (as in Ruby).
	def("configuration", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.railsApp == nil {
			return object.NilV
		}
		appCfg := rails.Configuration().(*railties.ApplicationConfiguration)
		return &RailsConfigVal{
			app: appCfg,
			eng: appCfg.EngineConfiguration,
			cfg: appCfg.EngineConfiguration.Configuration,
			cls: vm.consts["Rails::Railtie::Configuration"].(*RClass),
		}
	})

	// Rails.autoloaders / Rails.logger / Rails.cache — opaque any-typed slots the
	// meta-gem stores; a Ruby value set through the setter round-trips, otherwise
	// nil. Cache falls back to the application's store (railties vends none, so
	// nil) when no explicit store was set.
	def("autoloaders", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsAny(rails.Autoloaders())
	})
	def("logger", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsAny(rails.Logger())
	})
	def("logger=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		rails.SetLogger(rackArg(args))
		return rackArg(args)
	})
	def("cache", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsAny(rails.Cache())
	})
	def("cache=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		rails.SetCache(rackArg(args))
		return rackArg(args)
	})

	// Rails.groups(*extra) — the ordered, de-duplicated group list for the current
	// environment (default, env, extras, RAILS_GROUPS), as Ruby Strings.
	def("groups", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		extra := make([]string, len(args))
		for i, a := range args {
			extra[i] = railsGroupStr(a)
		}
		return thorStrArray(rails.Groups(extra...))
	})

	// Rails.version / Rails.gem_version — the targeted Rails release as a String.
	def("version", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsStr(rails.Version())
	})
	def("gem_version", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsStr(rails.GemVersion())
	})

	// Rails.backtrace_cleaner / Rails.error — the shared ActiveSupport singletons
	// (BacktraceCleaner / ErrorReporter). Those concrete objects are owned by
	// go-ruby-activesupport and not yet surfaced as Ruby classes, so the accessors
	// return nil until that binding lands.
	def("backtrace_cleaner", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NilV
	})
	def("error", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NilV
	})

	vm.registerRailsVersion(mod)
	vm.registerRailsEnvironmentInquirer(mod)

	// require "rails/all" pulls in every shipped framework component. The
	// featureHooks map is already created by registerPrime (which runs first).
	vm.featureHooks["rails/all"] = vm.installRailsAll
}

// registerRailsVersion installs Rails::VERSION, the module mirroring Ruby's
// Rails::VERSION with its STRING / MAJOR / MINOR / TINY / PRE constants.
func (vm *VM) registerRailsVersion(mod *RClass) {
	ver := newClass("Rails::VERSION", nil)
	ver.isModule = true
	ver.consts["STRING"] = object.NewString(rails.VERSION.STRING())
	ver.consts["MAJOR"] = object.IntValue(int64(rails.VERSION.Major))
	ver.consts["MINOR"] = object.IntValue(int64(rails.VERSION.Minor))
	ver.consts["TINY"] = object.IntValue(int64(rails.VERSION.Tiny))
	ver.consts["PRE"] = railsPre(rails.VERSION.Pre)
	mod.consts["VERSION"] = ver
	vm.consts["Rails::VERSION"] = ver
}

// registerRailsEnvironmentInquirer installs Rails::EnvironmentInquirer, the
// Rails::StringInquirer subclass Rails.env returns: the generic `name?` predicate
// (via method_missing) plus the environment-specific `local?` (development/test),
// and the String coercions (to_str / ==).
func (vm *VM) registerRailsEnvironmentInquirer(mod *RClass) {
	super := vm.consts["Rails::StringInquirer"].(*RClass)
	c := newClass("Rails::EnvironmentInquirer", super)
	mod.consts["EnvironmentInquirer"] = c
	vm.consts["Rails::EnvironmentInquirer"] = c

	envOf := func(self object.Value) *RailsEnvVal { return self.(*RailsEnvVal) }

	c.define("to_str", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsStr(envOf(self).e.String())
	})
	c.define("==", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(envOf(self).e.String() == railsGroupStr(rackArg(args)))
	})
	c.define("local?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(envOf(self).e.Local())
	})
	c.define("method_missing", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := railsMissingName(args)
		if n := len(name); n > 0 && name[n-1] == '?' {
			return object.Bool(envOf(self).e.Is(name[:n-1]))
		}
		raise("NoMethodError", "undefined method '%s' for an instance of Rails::EnvironmentInquirer", name)
		return object.NilV
	})
}

// installRailsAll is the rails/all feature hook: `require "rails/all"` loads the
// whole framework, so it triggers a require of each shipped (Available) component
// whose feature the VM already provides. Components without a provided feature yet
// (their bindings not landed) are skipped, so the aggregate tracks the catalog's
// Available set as component bindings arrive.
func (vm *VM) installRailsAll() {
	for _, c := range rails.AvailableComponents() {
		feat := railsComponentFeature(c.Name)
		if providedFeatures[feat] {
			vm.doRequire(feat, false)
		}
	}
}
