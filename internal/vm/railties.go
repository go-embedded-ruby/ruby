// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	railties "github.com/go-ruby-railties/railties"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerRailties installs the Rails boot framework (require "rails" /
// "rails/railtie" / "rails/engine" / "rails/application"): Rails::Railtie, its
// subclass Rails::Engine and Engine's subclass Rails::Application, plus the
// supporting Rails::Railtie::Configuration, Rails::Paths::Root / ::Path,
// Rails::Engine::RouteSet, Rails::Initializable::Initializer and
// Rails::StringInquirer. The initializer/boot machinery (the topologically-sorted
// bootstrap → railtie → finisher chain, the paths DSL, the lazy load-hooks) lives
// in github.com/go-ruby-railties/railties; this file is the class + method
// wiring (see railties_bind.go for the wrapper types and the three seams).
//
// The Rails module is created here so the classes can nest under it; the
// top-level Rails.application / Rails.root / Rails.env accessors are intentionally
// left for a later `rails` meta-gem binding to layer on, so the two do not
// conflict.
func (vm *VM) registerRailties() {
	mod := newClass("Rails", nil)
	mod.isModule = true
	vm.consts["Rails"] = mod

	cRailtie := vm.railsClass(mod, "Railtie", "Rails::Railtie", vm.cObject)
	cEngine := vm.railsClass(mod, "Engine", "Rails::Engine", cRailtie)
	cApplication := vm.railsClass(mod, "Application", "Rails::Application", cEngine)

	// Supporting classes. Configuration nests under Railtie; the paths classes
	// under a Rails::Paths module; RouteSet under Engine; the Initializer under a
	// Rails::Initializable module; StringInquirer directly under Rails.
	cConfig := newClass("Rails::Railtie::Configuration", vm.cObject)
	cRailtie.consts["Configuration"] = cConfig
	vm.consts["Rails::Railtie::Configuration"] = cConfig

	paths := newClass("Rails::Paths", nil)
	paths.isModule = true
	mod.consts["Paths"] = paths
	vm.consts["Rails::Paths"] = paths
	cPathsRoot := newClass("Rails::Paths::Root", vm.cObject)
	paths.consts["Root"] = cPathsRoot
	vm.consts["Rails::Paths::Root"] = cPathsRoot
	cPath := newClass("Rails::Paths::Path", vm.cObject)
	paths.consts["Path"] = cPath
	vm.consts["Rails::Paths::Path"] = cPath

	cRouteSet := newClass("Rails::Engine::RouteSet", vm.cObject)
	cEngine.consts["RouteSet"] = cRouteSet
	vm.consts["Rails::Engine::RouteSet"] = cRouteSet

	initable := newClass("Rails::Initializable", nil)
	initable.isModule = true
	mod.consts["Initializable"] = initable
	vm.consts["Rails::Initializable"] = initable
	cInit := newClass("Rails::Initializable::Initializer", vm.cObject)
	initable.consts["Initializer"] = cInit
	vm.consts["Rails::Initializable::Initializer"] = cInit

	cInquirer := newClass("Rails::StringInquirer", vm.cObject)
	mod.consts["StringInquirer"] = cInquirer
	vm.consts["Rails::StringInquirer"] = cInquirer

	vm.registerRailtieClass(cRailtie)
	vm.registerRailsEngineClass(cEngine)
	vm.registerRailsApplicationClass(cApplication)
	vm.registerRailsConfig(cConfig)
	vm.registerRailsPathsRoot(cPathsRoot)
	vm.registerRailsPath(cPath)
	vm.registerRailsRouteSet(cRouteSet)
	vm.registerRailsInitializer(cInit)
	vm.registerRailsStringInquirer(cInquirer)
}

// railsClass creates a Rails::* class under super, records it flat (for classOf)
// and nests it under the Rails namespace by its simple name.
func (vm *VM) railsClass(mod *RClass, simple, qualified string, super *RClass) *RClass {
	c := newClass(qualified, super)
	vm.consts[qualified] = c
	mod.consts[simple] = c
	return c
}

// registerRailtieCommon installs the surface shared by Railtie, Engine and
// Application (the whole family): name/railtie_name, config, the initializer DSL
// and the rake_tasks/console/generators/runner/server hooks plus their run_*
// drivers, and the initializers reader. Each is defined on the given class so
// subclasses inherit it; the bodies reach the right library object through
// railtieOf regardless of which family member self is.
func (vm *VM) registerRailtieCommon(c *RClass) {
	c.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsStr(railtieOf(self).Name())
	})
	c.define("railtie_name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsStr(railtieOf(self).RailtieName())
	})
	c.define("config", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.railsConfigFor(self)
	})
	c.define("initializer", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.railtieRegisterInitializer(self, args, blk)
	})

	// Each run closure discards the library's error return: a hook body is a Ruby
	// block, whose only failure mode is raising — which unwinds as a Ruby exception
	// (a Go panic), never a returned error.
	hooks := []struct {
		name string
		reg  func(*railties.Railtie, railties.Hook)
		run  func(*railties.Railtie)
	}{
		{"rake_tasks", func(r *railties.Railtie, h railties.Hook) { r.RakeTasks(h) }, func(r *railties.Railtie) { _ = r.RunRakeTasks(nil) }},
		{"console", func(r *railties.Railtie, h railties.Hook) { r.Console(h) }, func(r *railties.Railtie) { _ = r.RunConsole(nil) }},
		{"generators", func(r *railties.Railtie, h railties.Hook) { r.Generators(h) }, func(r *railties.Railtie) { _ = r.RunGenerators(nil) }},
		{"runner", func(r *railties.Railtie, h railties.Hook) { r.Runner(h) }, func(r *railties.Railtie) { _ = r.RunRunner(nil) }},
		{"server", func(r *railties.Railtie, h railties.Hook) { r.Server(h) }, func(r *railties.Railtie) { _ = r.RunServer(nil) }},
	}
	for _, h := range hooks {
		reg := h.reg
		run := h.run
		c.define(h.name, func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
			return vm.railtieHook(self, blk, reg)
		})
		c.define("run_"+h.name, func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			run(railtieOf(self))
			return object.NilV
		})
	}

	c.define("initializers", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.railsInitializerArray(railtieOf(self).Initializers())
	})
}

// railsInitializerArray wraps a library Collection as a Ruby Array of
// Rails::Initializable::Initializer handles.
func (vm *VM) railsInitializerArray(c railties.Collection) object.Value {
	cls := vm.consts["Rails::Initializable::Initializer"].(*RClass)
	elems := make([]object.Value, len(c))
	for i, in := range c {
		elems[i] = &RailsInitializerVal{in: in, cls: cls}
	}
	return object.NewArrayFromSlice(elems)
}

// registerRailtieClass installs Rails::Railtie: the shared family surface plus a
// `.new(name = class_name)` constructor.
func (vm *VM) registerRailtieClass(c *RClass) {
	vm.registerRailtieCommon(c)
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			cls := self.(*RClass)
			return &RailtieVal{rt: railties.NewRailtie(railsName(self, args, 0)), cls: cls}
		}}
}

// registerRailsEngineClass installs Rails::Engine: the shared railtie surface
// (inherited from Rails::Railtie) plus the paths DSL, the route set, namespace
// isolation and a `.new(name, root = "")` constructor. The engine accessors it
// installs also serve Rails::Application (an Engine), so engOf resolves an
// Application self too.
func (vm *VM) registerRailsEngineClass(c *RClass) {
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			cls := self.(*RClass)
			return &EngineVal{eng: railties.NewEngine(railsName(self, args, 0), railsRootArg(args, 1)), cls: cls}
		}}

	engOf := func(self object.Value) *railties.Engine {
		if e, ok := self.(*EngineVal); ok {
			return e.eng
		}
		return self.(*RailsAppVal).app.Engine
	}
	vm.defineRailsEngineMethods(c, engOf)
}

// defineRailsEngineMethods installs the Engine-level instance methods (shared by
// Rails::Application, which is an Engine), resolving the underlying *Engine via
// engOf.
func (vm *VM) defineRailsEngineMethods(c *RClass, engOf func(object.Value) *railties.Engine) {
	c.define("paths", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RailsPathsVal{root: engOf(self).Paths(), cls: vm.consts["Rails::Paths::Root"].(*RClass)}
	})
	c.define("routes", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RailsRouteSetVal{rs: engOf(self).Routes(), cls: vm.consts["Rails::Engine::RouteSet"].(*RClass)}
	})
	c.define("isolate_namespace", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		engOf(self).IsolateNamespace(railsModuleName(args[0]))
		return self
	})
	c.define("isolated?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(engOf(self).Isolated())
	})
	c.define("namespace", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsStr(engOf(self).Namespace())
	})
	c.define("engine_name", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsStr(engOf(self).EngineName())
	})
}

// railsModuleName coerces an isolate_namespace argument (a class/module or a
// String/Symbol) to its qualified name.
func railsModuleName(v object.Value) string {
	switch x := v.(type) {
	case *RClass:
		return x.name
	case object.Symbol:
		return string(x)
	}
	return strArg(v)
}

// registerRailsApplicationClass installs Rails::Application: on top of the shared
// railtie surface (inherited from Rails::Railtie) and the engine surface
// (inherited from Rails::Engine, whose accessors already handle an Application
// self) it adds the railtie collection, the boot (initialize!/initialized?/
// initializers), the lazy load-hooks (on_load/run_load_hooks) and env.
func (vm *VM) registerRailsApplicationClass(c *RClass) {
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			cls := self.(*RClass)
			app := railties.NewApplication(railsName(self, args, 0), railsRootArg(args, 1))
			app.RunInitializer = vm.railtieInitSeam()
			return &RailsAppVal{app: app, cls: cls}
		}}

	appOf := func(self object.Value) *railties.Application { return self.(*RailsAppVal).app }

	c.define("add_railtie", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		rt := railtieOf(args[0])
		if rt == nil {
			raise("TypeError", "wrong argument type %s (expected a Rails::Railtie)", vm.classOf(args[0]).name)
		}
		appOf(self).AddRailtie(rt)
		return self
	})
	c.define("railties", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		cls := vm.consts["Rails::Railtie"].(*RClass)
		rts := appOf(self).Railties()
		elems := make([]object.Value, len(rts))
		for i, r := range rts {
			elems[i] = &RailtieVal{rt: r, cls: cls}
		}
		return object.NewArrayFromSlice(elems)
	})
	// initializers overrides the inherited railtie reader (which lists only a
	// railtie's own initializers) with the fully-assembled boot chain — bootstrap,
	// each registered railtie, the app's own, then the finishers — as
	// Application#initializers does.
	c.define("initializers", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.railsInitializerArray(appOf(self).Initializers())
	})
	c.define("initialize!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.railsInitializeBang(self, args)
	})
	c.define("initialized?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(appOf(self).Initialized())
	})
	c.define("on_load", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		name := railsGroupStr(args[0])
		s := self
		// The hook's error return is discarded: its body is a Ruby block, which can
		// only fail by raising (a Go panic that unwinds as a Ruby exception).
		_ = appOf(self).OnLoad(name, func(_ any) error {
			if blk != nil {
				vm.callBlockSelf(blk, s, nil)
			}
			return nil
		})
		return self
	})
	c.define("run_load_hooks", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		name := railsGroupStr(args[0])
		var base any = self
		if len(args) > 1 {
			base = args[1]
		}
		// See on_load: a fired hook can only fail by raising, so the error is discarded.
		_ = appOf(self).RunLoadHooks(name, base)
		return self
	})
	c.define("env", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &StringInquirerVal{s: appOf(self).Env(), cls: vm.consts["Rails::StringInquirer"].(*RClass)}
	})
}

// registerRailsConfig installs Rails::Railtie::Configuration: the dynamic
// key/value bag (config.foo = x via method_missing, plus #[] / #[]=) and the
// typed Engine/Application accessors (paths/root/load_defaults/eager_load), each
// guarded to the config levels that own it.
func (vm *VM) registerRailsConfig(c *RClass) {
	cfgOf := func(self object.Value) *RailsConfigVal { return self.(*RailsConfigVal) }

	get := func(cv *RailsConfigVal, key string) object.Value {
		if raw, ok := cv.cfg.Get(key); ok {
			return raw.(object.Value)
		}
		return object.NilV
	}
	c.define("[]", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return get(cfgOf(self), railsGroupStr(rackArg(args)))
	})
	c.define("[]=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		cfgOf(self).cfg.Set(railsGroupStr(args[0]), args[1])
		return args[1]
	})
	c.define("method_missing", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cv := cfgOf(self)
		name := railsMissingName(args)
		if n := len(name); n > 0 && name[n-1] == '=' {
			if len(args) < 2 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			cv.cfg.Set(name[:n-1], args[1])
			return args[1]
		}
		return get(cv, name)
	})

	c.define("paths", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		cv := cfgOf(self)
		if cv.eng == nil {
			raise("NoMethodError", "undefined method 'paths' for a railtie configuration")
		}
		return &RailsPathsVal{root: cv.eng.Paths(), cls: vm.consts["Rails::Paths::Root"].(*RClass)}
	})
	c.define("root", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		cv := cfgOf(self)
		if cv.eng == nil {
			raise("NoMethodError", "undefined method 'root' for a railtie configuration")
		}
		return railsStr(cv.eng.Root())
	})
	c.define("root=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cv := cfgOf(self)
		if cv.eng == nil {
			raise("NoMethodError", "undefined method 'root=' for a railtie configuration")
		}
		cv.eng.SetRoot(strArg(rackArg(args)))
		return rackArg(args)
	})
	c.define("load_defaults", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cv := cfgOf(self)
		if cv.app == nil {
			raise("NoMethodError", "undefined method 'load_defaults' for a non-application configuration")
		}
		if err := cv.app.LoadDefaults(railsVersionStr(rackArg(args))); err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return object.NilV
	})
	c.define("loaded_defaults", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		cv := cfgOf(self)
		if cv.app == nil {
			raise("NoMethodError", "undefined method 'loaded_defaults' for a non-application configuration")
		}
		return railsStr(cv.app.LoadedDefaults())
	})
	c.define("eager_load", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		cv := cfgOf(self)
		if cv.app == nil {
			raise("NoMethodError", "undefined method 'eager_load' for a non-application configuration")
		}
		return object.Bool(cv.app.EagerLoad)
	})
	c.define("eager_load=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cv := cfgOf(self)
		if cv.app == nil {
			raise("NoMethodError", "undefined method 'eager_load=' for a non-application configuration")
		}
		cv.app.EagerLoad = rackArg(args).Truthy()
		return rackArg(args)
	})
}

// registerRailsPathsRoot installs Rails::Paths::Root: the paths DSL that maps
// labels to Path entries and aggregates the expanded paths by classification.
func (vm *VM) registerRailsPathsRoot(c *RClass) {
	rootOf := func(self object.Value) *railties.PathsRoot { return self.(*RailsPathsVal).root }

	c.define("add", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		p := rootOf(self).Add(strArg(args[0]), railsPathOpts(thorOptHash(args, 1)))
		return &RailsPathVal{p: p, cls: vm.consts["Rails::Paths::Path"].(*RClass)}
	})
	c.define("[]", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		p := rootOf(self).Get(strArg(rackArg(args)))
		if p == nil {
			return object.NilV
		}
		return &RailsPathVal{p: p, cls: vm.consts["Rails::Paths::Path"].(*RClass)}
	})
	c.define("root", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsStr(rootOf(self).Root())
	})
	c.define("root=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		rootOf(self).SetRoot(strArg(rackArg(args)))
		return rackArg(args)
	})
	c.define("keys", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return thorStrArray(rootOf(self).Keys())
	})
	c.define("eager_load", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return thorStrArray(rootOf(self).EagerLoad())
	})
	c.define("autoload_paths", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return thorStrArray(rootOf(self).AutoloadPaths())
	})
	c.define("autoload_once", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return thorStrArray(rootOf(self).AutoloadOnce())
	})
	c.define("load_paths", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return thorStrArray(rootOf(self).LoadPaths())
	})
}

// registerRailsPath installs Rails::Paths::Path: one labelled set of relative
// sub-paths plus the autoload / eager-load / load-path classification flags.
func (vm *VM) registerRailsPath(c *RClass) {
	pathOf := func(self object.Value) *railties.Path { return self.(*RailsPathVal).p }

	bang := []struct {
		name string
		fn   func(*railties.Path)
	}{
		{"eager_load!", func(p *railties.Path) { p.EagerLoad() }},
		{"autoload!", func(p *railties.Path) { p.Autoload() }},
		{"autoload_once!", func(p *railties.Path) { p.AutoloadOnce() }},
		{"load_path!", func(p *railties.Path) { p.LoadPath() }},
	}
	for _, b := range bang {
		fn := b.fn
		c.define(b.name, func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			fn(pathOf(self))
			return self
		})
	}

	pred := []struct {
		name string
		fn   func(*railties.Path) bool
	}{
		{"eager_load?", func(p *railties.Path) bool { return p.EagerLoadQ() }},
		{"autoload?", func(p *railties.Path) bool { return p.AutoloadQ() }},
		{"autoload_once?", func(p *railties.Path) bool { return p.AutoloadOnceQ() }},
		{"load_path?", func(p *railties.Path) bool { return p.LoadPathQ() }},
	}
	for _, pd := range pred {
		fn := pd.fn
		c.define(pd.name, func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Bool(fn(pathOf(self)))
		})
	}

	push := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		subs := make([]string, len(args))
		for i, a := range args {
			subs[i] = strArg(a)
		}
		pathOf(self).Push(subs...)
		return self
	}
	c.define("push", push)
	c.define("<<", push)
	c.define("unshift", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		subs := make([]string, len(args))
		for i, a := range args {
			subs[i] = strArg(a)
		}
		pathOf(self).Unshift(subs...)
		return self
	})
	c.define("to_a", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return thorStrArray(pathOf(self).To())
	})
	c.define("expanded", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return thorStrArray(pathOf(self).Expanded())
	})
}

// registerRailsRouteSet installs Rails::Engine::RouteSet: routes.draw records a
// routing block (the RouteBlock seam) and #run replays the recorded blocks (the
// concrete route matching is deferred to a later actionpack binding).
func (vm *VM) registerRailsRouteSet(c *RClass) {
	rsOf := func(self object.Value) *railties.RouteSet { return self.(*RailsRouteSetVal).rs }

	c.define("draw", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		s := self
		rsOf(self).Draw(func(_ *railties.RouteSet) error {
			if blk != nil {
				vm.callBlockSelf(blk, s, nil)
			}
			return nil
		})
		return self
	})
	c.define("run", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		// A recorded routing block can only fail by raising (a Go panic), so Run's
		// error return is discarded.
		_ = rsOf(self).Run()
		return self
	})
	c.define("draws", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(rsOf(self).Draws()))
	})
	c.define("default_scope", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsStr(rsOf(self).DefaultScope())
	})
}

// registerRailsInitializer installs Rails::Initializable::Initializer: the
// read-only view of one registered initializer (name and its before/after/group
// ordering constraints) plus belongs_to? for group membership.
func (vm *VM) registerRailsInitializer(c *RClass) {
	inOf := func(self object.Value) *railties.Initializer { return self.(*RailsInitializerVal).in }

	c.define("name", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsStr(inOf(self).Name)
	})
	c.define("before", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if b := inOf(self).Before; b != "" {
			return railsStr(b)
		}
		return object.NilV
	})
	c.define("after", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if a := inOf(self).After; a != "" {
			return railsStr(a)
		}
		return object.NilV
	})
	c.define("group", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsStr(inOf(self).Group)
	})
	c.define("belongs_to?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(inOf(self).BelongsTo(railsGroupStr(rackArg(args))))
	})
}

// registerRailsStringInquirer installs Rails::StringInquirer: a String-like value
// whose `foo?` predicate reads whether it equals "foo" (the type Rails.env
// returns), plus .new and to_s/to_str.
func (vm *VM) registerRailsStringInquirer(c *RClass) {
	inqOf := func(self object.Value) *StringInquirerVal { return self.(*StringInquirerVal) }

	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			cls := self.(*RClass)
			return &StringInquirerVal{s: railties.StringInquirer(strArg(rackArg(args))), cls: cls}
		}}

	// to_s is intentionally not defined: the default Object#to_s renders the value
	// via its Go ToS (which returns the wrapped string), so `env.to_s` and string
	// interpolation both yield the plain environment name. to_str is defined so the
	// value coerces where a String is required.
	c.define("to_str", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return railsStr(inqOf(self).s.String())
	})
	c.define("==", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(inqOf(self).s.String() == railsGroupStr(rackArg(args)))
	})
	c.define("method_missing", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := railsMissingName(args)
		if n := len(name); n > 0 && name[n-1] == '?' {
			return object.Bool(inqOf(self).s.Is(name[:n-1]))
		}
		raise("NoMethodError", "undefined method '%s' for an instance of Rails::StringInquirer", name)
		return object.NilV
	})
}
