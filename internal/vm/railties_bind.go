// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"strconv"

	railties "github.com/go-ruby-railties/railties"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the value-model + seam half of the railties binding (require
// "rails"); railties.go holds the class + method registration. The
// interpreter-independent boot framework — the railtie/engine/application object
// graph, the initializer Collection and its topological sort, the paths DSL, the
// lazy load-hooks and the StringInquirer — lives in
// github.com/go-ruby-railties/railties (Go package `rails`, imported aliased as
// `railties`). rbgo wraps each library object as a Ruby object reporting the
// matching Rails::* class and threads the three seams the library exposes:
//
//   - RunInitializer (rails.InitializerSeam): the deferred body of every
//     body-less initializer. Every Ruby `initializer "name" do … end` block is
//     recorded body-less in the library and its block stashed here, so the seam
//     runs it INLINE on the VM goroutine when Application#initialize! drives the
//     topologically-sorted chain (bootstrap → railties → app → finisher).
//   - Hook (rails.Hook): the rake_tasks/console/generators/runner/server
//     extension bodies — each wraps the captured Ruby block, run inline by the
//     matching run_* method.
//   - RouteBlock (rails.RouteBlock): the routes.draw DSL replay — the block is
//     recorded and replayed by RouteSet#run (concrete route matching is deferred
//     to a later actionpack binding).

// The wrapper types. Each holds a pointer into the library and carries the Ruby
// class it reports (so a subclass of Rails::Application/Engine/Railtie keeps its
// own identity); the methods registered in railties.go operate on the held value.

// RailtieVal wraps a *railties.Railtie (Rails::Railtie).
type RailtieVal struct {
	rt  *railties.Railtie
	cls *RClass
}

// EngineVal wraps a *railties.Engine (Rails::Engine).
type EngineVal struct {
	eng *railties.Engine
	cls *RClass
}

// RailsAppVal wraps a *railties.Application (Rails::Application).
type RailsAppVal struct {
	app *railties.Application
	cls *RClass
}

// RailsConfigVal wraps a railtie/engine/application configuration: cfg is always
// the dynamic key/value bag (config.foo = x); eng and app are the richer typed
// views, non-nil only when the owner is an Engine (paths/root) or Application
// (load_defaults/eager_load) respectively.
type RailsConfigVal struct {
	cfg *railties.Configuration
	eng *railties.EngineConfiguration
	app *railties.ApplicationConfiguration
	cls *RClass
}

// RailsPathsVal wraps a *railties.PathsRoot (Rails::Paths::Root).
type RailsPathsVal struct {
	root *railties.PathsRoot
	cls  *RClass
}

// RailsPathVal wraps a *railties.Path (Rails::Paths::Path).
type RailsPathVal struct {
	p   *railties.Path
	cls *RClass
}

// RailsRouteSetVal wraps a *railties.RouteSet (Rails::Engine::RouteSet).
type RailsRouteSetVal struct {
	rs  *railties.RouteSet
	cls *RClass
}

// RailsInitializerVal wraps a *railties.Initializer (Rails::Initializable::Initializer).
type RailsInitializerVal struct {
	in  *railties.Initializer
	cls *RClass
}

// StringInquirerVal wraps a railties.StringInquirer (Rails::StringInquirer): the
// value Rails.env returns so `env.production?` reads naturally.
type StringInquirerVal struct {
	s   railties.StringInquirer
	cls *RClass
}

func (v *RailtieVal) ToS() string           { return "#<Rails::Railtie:" + v.rt.Name() + ">" }
func (v *RailtieVal) Inspect() string       { return v.ToS() }
func (v *RailtieVal) Truthy() bool          { return true }
func (v *EngineVal) ToS() string            { return "#<Rails::Engine:" + v.eng.Name() + ">" }
func (v *EngineVal) Inspect() string        { return v.ToS() }
func (v *EngineVal) Truthy() bool           { return true }
func (v *RailsAppVal) ToS() string          { return "#<Rails::Application:" + v.app.Name() + ">" }
func (v *RailsAppVal) Inspect() string      { return v.ToS() }
func (v *RailsAppVal) Truthy() bool         { return true }
func (v *RailsConfigVal) ToS() string       { return "#<Rails::Railtie::Configuration>" }
func (v *RailsConfigVal) Inspect() string   { return v.ToS() }
func (v *RailsConfigVal) Truthy() bool      { return true }
func (v *RailsPathsVal) ToS() string        { return "#<Rails::Paths::Root>" }
func (v *RailsPathsVal) Inspect() string    { return v.ToS() }
func (v *RailsPathsVal) Truthy() bool       { return true }
func (v *RailsPathVal) ToS() string         { return "#<Rails::Paths::Path>" }
func (v *RailsPathVal) Inspect() string     { return v.ToS() }
func (v *RailsPathVal) Truthy() bool        { return true }
func (v *RailsRouteSetVal) ToS() string     { return "#<Rails::Engine::RouteSet>" }
func (v *RailsRouteSetVal) Inspect() string { return v.ToS() }
func (v *RailsRouteSetVal) Truthy() bool    { return true }
func (v *RailsInitializerVal) ToS() string {
	return "#<Rails::Initializable::Initializer:" + v.in.Name + ">"
}
func (v *RailsInitializerVal) Inspect() string { return v.ToS() }
func (v *RailsInitializerVal) Truthy() bool    { return true }
func (v *StringInquirerVal) ToS() string       { return v.s.String() }
func (v *StringInquirerVal) Inspect() string   { return "\"" + v.s.String() + "\"" }
func (v *StringInquirerVal) Truthy() bool      { return true }

// railtieOf returns the underlying *railties.Railtie of any railtie-family value
// (Railtie/Engine/Application), reaching through the library's embedding so the
// shared Railtie surface (config, initializer, hooks) works on all three.
func railtieOf(self object.Value) *railties.Railtie {
	switch s := self.(type) {
	case *RailtieVal:
		return s.rt
	case *EngineVal:
		return s.eng.Railtie
	case *RailsAppVal:
		return s.app.Engine.Railtie
	}
	return nil
}

// railtieSeamKey returns the Go object an initializer registered on self is bound
// to when the boot chain runs — the same object the library threads back to the
// InitializerSeam as ctx. For a standalone Railtie/Engine that is the *Railtie;
// for an Application's own initializers the library binds to the *Application, so
// the key is the app itself.
func railtieSeamKey(self object.Value) any {
	if av, ok := self.(*RailsAppVal); ok {
		return any(av.app)
	}
	return any(railtieOf(self))
}

// railtieSeam holds the deferred Ruby initializer blocks for one railtie-family
// object, plus the Ruby self they run against (the railtie/engine/app instance).
type railtieSeam struct {
	self   object.Value
	blocks map[string]*Proc
}

// railtieSeamFor returns the seam record for key, creating it (bound to self) on
// first use.
func (vm *VM) railtieSeamFor(key any, self object.Value) *railtieSeam {
	if vm.railtieSeams == nil {
		vm.railtieSeams = map[any]*railtieSeam{}
	}
	rs, ok := vm.railtieSeams[key]
	if !ok {
		rs = &railtieSeam{self: self, blocks: map[string]*Proc{}}
		vm.railtieSeams[key] = rs
	}
	return rs
}

// railtieInitSeam is the InitializerSeam wired into every Application: when the
// boot chain reaches a body-less initializer it looks up the Ruby block recorded
// for (ctx, name) and runs it INLINE on the VM goroutine with the railtie/engine/
// app as self. Bootstrap and finisher initializers carry no recorded block, so
// they are no-ops (their real bodies are deferred, exactly as in the library).
func (vm *VM) railtieInitSeam() railties.InitializerSeam {
	return func(name string, ctx any) error {
		if rs, ok := vm.railtieSeams[ctx]; ok {
			if blk := rs.blocks[name]; blk != nil {
				vm.callBlockSelf(blk, rs.self, nil)
			}
		}
		return nil
	}
}

// railtieRegisterInitializer implements the shared `initializer name, opts, &block`
// class/instance method: it registers a body-less initializer in the library
// (preserving the before/after/group ordering constraints) and stashes the block
// for the RunInitializer seam. It returns self for chaining.
func (vm *VM) railtieRegisterInitializer(self object.Value, args []object.Value, blk *Proc) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	name := strArg(args[0])
	railtieOf(self).Initializer(name, railsInitOpts(thorOptHash(args, 1)), nil)
	if blk != nil {
		vm.railtieSeamFor(railtieSeamKey(self), self).blocks[name] = blk
	}
	return self
}

// railtieHook implements a shared extension-point method (rake_tasks/console/
// generators/runner/server): it registers a library Hook that runs the captured
// Ruby block inline, and returns self.
func (vm *VM) railtieHook(self object.Value, blk *Proc, reg func(*railties.Railtie, railties.Hook)) object.Value {
	s := self
	reg(railtieOf(self), func(_ any, _ ...any) error {
		if blk != nil {
			vm.callBlockSelf(blk, s, nil)
		}
		return nil
	})
	return self
}

// railsConfigFor returns the configuration handle for self, exposing the richest
// typed view the owner supports (dynamic bag for a Railtie, +paths/root for an
// Engine, +load_defaults/eager_load for an Application).
func (vm *VM) railsConfigFor(self object.Value) object.Value {
	v := &RailsConfigVal{cls: vm.consts["Rails::Railtie::Configuration"].(*RClass)}
	switch s := self.(type) {
	case *RailtieVal:
		v.cfg = s.rt.Config()
	case *EngineVal:
		v.eng = s.eng.Config()
		v.cfg = v.eng.Configuration
	case *RailsAppVal:
		v.app = s.app.Config()
		v.eng = v.app.EngineConfiguration
		v.cfg = v.eng.Configuration
	}
	return v
}

// railsInitializeBang implements Application#initialize!: it boots the app,
// mapping the library's guards to Ruby exceptions (a re-boot to RuntimeError, an
// initializer cycle to RuntimeError). A Ruby exception raised from an initializer
// block unwinds through the boot as usual.
func (vm *VM) railsInitializeBang(self object.Value, args []object.Value) object.Value {
	av := self.(*RailsAppVal)
	var group []string
	if len(args) > 0 && !object.IsNil(args[0]) {
		group = append(group, railsGroupStr(args[0]))
	}
	if err := av.app.Initialize(group...); err != nil {
		if errors.Is(err, railties.ErrAlreadyInitialized) {
			raise("RuntimeError", "Application has been already initialized.")
		}
		raise("RuntimeError", "%s", err.Error())
	}
	return self
}

// --- argument / value helpers ----------------------------------------------

// railsInitOpts reads the initializer keyword Hash (:before/:after/:group) into
// the library's InitOpts.
func railsInitOpts(h *object.Hash) railties.InitOpts {
	var o railties.InitOpts
	if h != nil {
		if v, ok := thorKw(h, "before"); ok {
			o.Before = strArg(v)
		}
		if v, ok := thorKw(h, "after"); ok {
			o.After = strArg(v)
		}
		if v, ok := thorKw(h, "group"); ok {
			o.Group = railsGroupStr(v)
		}
	}
	return o
}

// railsPathOpts reads the paths.add keyword Hash (:with/:glob/:eager_load/
// :autoload/:autoload_once/:load_path) into the library's PathOpts.
func railsPathOpts(h *object.Hash) railties.PathOpts {
	var o railties.PathOpts
	if h != nil {
		if v, ok := thorKw(h, "with"); ok {
			o.With = thorStrList(v)
		}
		if v, ok := thorKw(h, "glob"); ok {
			o.Glob = strArg(v)
		}
		if v, ok := thorKw(h, "eager_load"); ok {
			o.EagerLoad = v.Truthy()
		}
		if v, ok := thorKw(h, "autoload"); ok {
			o.Autoload = v.Truthy()
		}
		if v, ok := thorKw(h, "autoload_once"); ok {
			o.AutoloadOnce = v.Truthy()
		}
		if v, ok := thorKw(h, "load_path"); ok {
			o.LoadPath = v.Truthy()
		}
	}
	return o
}

// railsGroupStr coerces a group argument (a Symbol like :all or a String) to its
// plain string.
func railsGroupStr(v object.Value) string {
	if s, ok := v.(object.Symbol); ok {
		return string(s)
	}
	return strArg(v)
}

// railsVersionStr coerces a load_defaults version argument (a Float like 7.1, a
// String like "8.0", or anything else via #to_s) to the string key the defaults
// table is keyed by.
func railsVersionStr(v object.Value) string {
	switch x := v.(type) {
	case *object.String:
		return x.Str()
	case object.Float:
		return strconv.FormatFloat(float64(x), 'f', -1, 64)
	}
	return v.ToS()
}

// railsName resolves a railtie/engine/application name: the explicit argument at
// index i, else the class name `.new` was invoked on (so a `class MyApp <
// Rails::Application` picks up "MyApp"), else "".
func railsName(self object.Value, args []object.Value, i int) string {
	if i < len(args) && !object.IsNil(args[i]) {
		return strArg(args[i])
	}
	if c, ok := self.(*RClass); ok && c.name != "" {
		return c.name
	}
	return ""
}

// railsRootArg resolves the optional root argument at index i (defaulting to "").
func railsRootArg(args []object.Value, i int) string {
	if i < len(args) && !object.IsNil(args[i]) {
		return strArg(args[i])
	}
	return ""
}

// railsStr wraps a Go string as a Ruby String.
func railsStr(s string) object.Value { return object.NewString(s) }

// railsMissingName extracts the method name from a method_missing args slice: the
// interpreter always prepends the missing method's name as a Symbol at args[0],
// whose ToS is that plain name.
func railsMissingName(args []object.Value) string {
	return args[0].ToS()
}
