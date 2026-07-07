// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	capistrano "github.com/go-ruby-capistrano/capistrano"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerCapistrano records the require "capistrano" feature hook. Like MRI's
// Capistrano (whose DSL only appears once the gem is loaded), it installs nothing
// eagerly: the hook (run once by doRequire on the first `require "capistrano"` /
// "capistrano/all" / "capistrano/setup" / "capistrano/deploy") builds the
// Capistrano module, its Server / Session / Task / TestBackend classes and error
// tree, creates the per-VM DSL facade over its in-process command backend, and
// installs the top-level DSL methods.
//
// Installing lazily is deliberate: Capistrano's task graph reuses Rake's
// task/namespace/desc, which rbgo already exposes globally (wired to Rake.
// application). Defining those at startup would clobber the always-on Rake DSL
// every program sees; installing them only when capistrano is required scopes the
// override to programs that actually deploy, and matches MRI (requiring
// capistrano is what re-points the DSL). registerRake runs first so the Rake
// surface exists to override. The featureHooks map is created by registerPrime
// (which runs earlier), so this only records the hook.
func (vm *VM) registerCapistrano() {
	vm.featureHooks["capistrano"] = vm.installCapistrano
	vm.featureHooks["capistrano/all"] = vm.installCapistrano
	vm.featureHooks["capistrano/setup"] = vm.installCapistrano
	vm.featureHooks["capistrano/deploy"] = vm.installCapistrano
}

// installCapistrano is the body run on the first require of any capistrano
// feature. It is idempotent (guarded by capApp) so requiring several capistrano
// features installs the DSL exactly once. It creates the DSL facade wired to the
// library's in-process FakeBackend — so execute/capture/test are recorded and
// never reach a real host, keeping every run hermetic — then registers the
// Capistrano classes, the error tree and the top-level DSL.
func (vm *VM) installCapistrano() {
	if vm.capApp != nil {
		return
	}
	vm.capApp = capistrano.NewApplication()
	vm.capBackend = capistrano.NewFakeBackend()
	vm.capApp.SetBackend(vm.capBackend)

	mod := newClass("Capistrano", vm.cObject)
	vm.consts["Capistrano"] = mod

	vm.registerCapistranoErrors(mod)

	cServer := vm.capistranoClass(mod, "Server", "Capistrano::Server")
	cSession := vm.capistranoClass(mod, "Session", "Capistrano::Session")
	cTask := vm.capistranoClass(mod, "Task", "Capistrano::Task")
	cBackend := vm.capistranoClass(mod, "TestBackend", "Capistrano::TestBackend")

	vm.registerCapistranoModule(mod)
	vm.registerCapistranoServer(cServer)
	vm.registerCapistranoSession(cSession)
	vm.registerCapistranoTask(cTask)
	vm.registerCapistranoBackend(cBackend)
	vm.registerCapistranoDSL()
}

// capistranoClass creates a Capistrano::* class under cObject, records it flat
// (for classOf) and nests it under the Capistrano namespace by its simple name.
func (vm *VM) capistranoClass(mod *RClass, simple, qualified string) *RClass {
	c := newClass(qualified, vm.cObject)
	vm.consts[qualified] = c
	mod.consts[simple] = c
	return c
}

// registerCapistranoErrors installs the Capistrano exception tree mirroring the
// gem: Capistrano::Error < StandardError, with TaskNotFoundError /
// NoMatchingServersError / CommandError beneath it. Each Go error the library
// raises maps to one of these by concrete type (see capRaise).
func (vm *VM) registerCapistranoErrors(mod *RClass) {
	defs := []struct{ qualified, parent string }{
		{"Capistrano::Error", "StandardError"},
		{"Capistrano::TaskNotFoundError", "Capistrano::Error"},
		{"Capistrano::NoMatchingServersError", "Capistrano::Error"},
		{"Capistrano::CommandError", "Capistrano::Error"},
	}
	for _, d := range defs {
		parent := vm.consts[d.parent].(*RClass)
		cls := newClass(d.qualified, parent)
		vm.consts[d.qualified] = cls
		mod.consts[d.qualified[len("Capistrano::"):]] = cls
	}
}

// registerCapistranoModule installs the Capistrano module singleton surface:
// Capistrano.backend returns the per-VM recording backend (so a test can script
// command output and read the recorded command log).
func (vm *VM) registerCapistranoModule(mod *RClass) {
	mod.smethods["backend"] = &Method{name: "backend", owner: mod,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &CapBackendVal{b: vm.capBackend}
		}}
}

// registerCapistranoServer installs Capistrano::Server: a deploy target's
// connection triple, its roles and its per-server property bag. The readers
// mirror Capistrano::Configuration::Server's public surface.
func (vm *VM) registerCapistranoServer(c *RClass) {
	srvOf := func(self object.Value) *capistrano.Server { return self.(*CapServerVal).s }

	// to_s / inspect fall through to Object#to_s / #inspect, which call the value's
	// Go ToS / Inspect (both render the canonical "[user@]host[:port]" form).
	c.define("hostname", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(srvOf(self).Host)
	})
	c.define("user", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if u := srvOf(self).User; u != "" {
			return object.NewString(u)
		}
		return object.NilV
	})
	c.define("port", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if p := srvOf(self).Port; p != 0 {
			return object.Integer(int64(p))
		}
		return object.NilV
	})
	c.define("roles", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		rs := srvOf(self).Roles()
		elems := make([]object.Value, len(rs))
		for i, r := range rs {
			elems[i] = object.SymVal(r)
		}
		return object.NewArrayFromSlice(elems)
	})
	c.define("has_role?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Bool(srvOf(self).HasRole(capName(args[0])))
	})
	c.define("primary?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(srvOf(self).IsPrimary())
	})
	c.define("no_release?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(srvOf(self).NoRelease())
	})
	c.define("fetch", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return capPropValue(srvOf(self).Fetch(capName(args[0])))
	})
	c.define("set", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		srvOf(self).Set(capName(args[0]), capProp(args[1]))
		return self
	})
}

// registerCapistranoSession installs Capistrano::Session: the per-host execution
// context an on-block runs as `self`. execute raises Capistrano::CommandError on
// a non-zero exit, test returns a boolean, capture returns the stripped stdout
// (and raises on a non-zero exit); upload! / download! transfer a file. A
// transport failure (the backend could not run the command) raises
// Capistrano::Error. Every command flows through the recording backend.
func (vm *VM) registerCapistranoSession(c *RClass) {
	sessOf := func(self object.Value) *capistrano.Session { return self.(*CapSessionVal).s }

	c.define("execute", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		if err := sessOf(self).Execute(capName(args[0]), capStringArgs(args[1:])...); err != nil {
			return capRaise(err)
		}
		return object.True
	})
	c.define("test", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		ok, err := sessOf(self).Test(capName(args[0]), capStringArgs(args[1:])...)
		if err != nil {
			return capRaise(err)
		}
		return object.Bool(ok)
	})
	c.define("capture", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		out, err := sessOf(self).Capture(capName(args[0]), capStringArgs(args[1:])...)
		if err != nil {
			return capRaise(err)
		}
		return object.NewString(out)
	})
	c.define("upload!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		if err := sessOf(self).Upload(capName(args[0]), capName(args[1])); err != nil {
			return capRaise(err)
		}
		return object.True
	})
	c.define("download!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		if err := sessOf(self).Download(capName(args[0]), capName(args[1])); err != nil {
			return capRaise(err)
		}
		return object.True
	})
	c.define("host", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return wrapServer(sessOf(self).Host())
	})
}

// registerCapistranoTask installs Capistrano::Task: a task-graph node's read-only
// metadata — its fully-qualified name, its prerequisites and its description.
func (vm *VM) registerCapistranoTask(c *RClass) {
	taskOf := func(self object.Value) *capistrano.Task { return self.(*CapTaskVal).t }

	// to_s falls through to Object#to_s, which calls the value's Go ToS (the task
	// name); name is the explicit reader.
	c.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(taskOf(self).Name())
	})
	c.define("prerequisites", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		pres := taskOf(self).Prerequisites()
		elems := make([]object.Value, len(pres))
		for i, p := range pres {
			elems[i] = object.NewString(p)
		}
		return object.NewArrayFromSlice(elems)
	})
	c.define("description", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if d := taskOf(self).Description(); d != "" {
			return object.NewString(d)
		}
		return object.NilV
	})
}

// registerCapistranoBackend installs Capistrano::TestBackend: the recording
// command backend the DSL runs against. commands / uploads / downloads read the
// ordered logs; script makes a command return a scripted result (stdout /
// exit_status), and fail_transport / fail_uploads / fail_downloads inject the
// failure paths — everything a hermetic deploy test needs.
func (vm *VM) registerCapistranoBackend(c *RClass) {
	backOf := func(self object.Value) *capistrano.FakeBackend { return self.(*CapBackendVal).b }

	c.define("commands", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return capStrArray(backOf(self).Commands)
	})
	c.define("uploads", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return capStrArray(backOf(self).Uploads)
	})
	c.define("downloads", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return capStrArray(backOf(self).Downloads)
	})
	c.define("script", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		res := capistrano.CommandResult{}
		if len(args) > 1 {
			if h, ok := args[1].(*object.Hash); ok {
				res = capCommandResult(h)
			}
		}
		backOf(self).Script(capName(args[0]), res, nil)
		return self
	})
	c.define("fail_transport", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		backOf(self).FailTransport(capErr(args[0]))
		return self
	})
	c.define("fail_uploads", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		backOf(self).FailUploads(capErr(args[0]))
		return self
	})
	c.define("fail_downloads", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		backOf(self).FailDownloads(capErr(args[0]))
		return self
	})
}

// registerCapistranoDSL installs the top-level Capistrano DSL on the main object
// (Object), the Ruby surface a Capfile / deploy.rb reads: the variable store
// (set / fetch / set?), the server & role registry (role / server / roles /
// release_roles / primary), the task graph (task / namespace / desc / before /
// after / invoke / invoke!) and the execution context (on). These override Rake's
// always-on task/namespace/desc for the duration of this VM, scoped to programs
// that required capistrano (see installCapistrano's doc).
func (vm *VM) registerCapistranoDSL() {
	vm.cObject.define("set", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		var val object.Value = object.NilV
		if len(args) > 1 {
			val = args[1]
		}
		vm.capApp.Set(capName(args[0]), vm.capValue(val, blk))
		return val
	})
	vm.cObject.define("fetch", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		key := capName(args[0])
		var defs []any
		if len(args) > 1 {
			defs = append(defs, vm.capValue(args[1], nil))
		} else if blk != nil {
			defs = append(defs, vm.capValue(object.NilV, blk))
		}
		return capFetched(vm.capApp.Fetch(key, defs...))
	})
	vm.cObject.define("set?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Bool(vm.capApp.IsSet(capName(args[0])))
	})
	vm.cObject.define("role", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2+)", len(args))
		}
		_, props := capOptions(capArg(args, 2))
		vm.capApp.Role(capName(args[0]), capStrList(args[1]), props)
		return object.NilV
	})
	vm.cObject.define("server", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		roles, props := capOptions(capArg(args, 1))
		return &CapServerVal{s: vm.capApp.AddServer(capName(args[0]), roles, props)}
	})
	vm.cObject.define("roles", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return wrapServers(vm.capApp.Roles(capStringArgs(args)...))
	})
	vm.cObject.define("release_roles", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return wrapServers(vm.capApp.ReleaseRoles(capStringArgs(args)...))
	})
	vm.cObject.define("primary", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return wrapServer(vm.capApp.Primary(capName(args[0])))
	})
	vm.cObject.define("desc", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		text := capName(args[0])
		vm.capApp.Desc(text)
		return object.NewString(text)
	})
	vm.cObject.define("task", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		name, deps := capResolveTask(args)
		return &CapTaskVal{t: vm.capApp.Task(name, deps, vm.capBody(blk))}
	})
	vm.cObject.define("namespace", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		vm.capApp.Namespace(capName(args[0]), func() {
			if blk != nil {
				vm.callBlock(blk, nil)
			}
		})
		return object.NilV
	})
	vm.cObject.define("before", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.capHook(args, blk, false)
	})
	vm.cObject.define("after", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.capHook(args, blk, true)
	})
	vm.cObject.define("invoke", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if err := vm.capApp.Invoke(capName(args[0])); err != nil {
			return capRaise(err)
		}
		return object.NilV
	})
	vm.cObject.define("invoke!", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if err := vm.capApp.InvokeBang(capName(args[0])); err != nil {
			return capRaise(err)
		}
		return object.NilV
	})
	vm.cObject.define("on", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		hosts := capHosts(args[0])
		err := vm.capApp.On(hosts, func(s *capistrano.Session) error {
			// The on-block runs with the Session bound as `self`, so execute/test/
			// capture resolve as its methods, and the host Server is passed as the
			// block argument (the `|host|` form). The block runs INLINE under the GVL;
			// a Ruby exception inside unwinds as a Go panic, so this never returns an
			// error — only the empty-hosts NoMatchingServersError does.
			vm.callBlockSelf(blk, &CapSessionVal{s: s}, []object.Value{wrapServer(s.Host())})
			return nil
		})
		if err != nil {
			return capRaise(err)
		}
		return object.NilV
	})
}

// capHook is the shared body of before / after. With a block it first defines the
// hook task from that block (so `before :deploy, :warm_cache do … end` works),
// then wires the relationship; without one it wires two existing task names. A
// missing target task raises Capistrano::TaskNotFoundError.
func (vm *VM) capHook(args []object.Value, blk *Proc, after bool) object.Value {
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
	}
	target := capName(args[0])
	hook := capName(args[1])
	if blk != nil {
		vm.capApp.Task(hook, nil, vm.capBody(blk))
	}
	var err error
	if after {
		err = vm.capApp.After(target, hook)
	} else {
		err = vm.capApp.Before(target, hook)
	}
	if err != nil {
		return capRaise(err)
	}
	return object.NilV
}

// capResolveTask decodes the task DSL argument forms into (name, deps):
//
//	task :deploy do … end        → name deploy, no deps
//	task :deploy => :build       → name deploy, deps [build]   (sole Hash arg)
//	task :deploy, [:build, :log] → name deploy, deps [build log]
func capResolveTask(args []object.Value) (name string, deps []string) {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
	}
	if len(args) == 1 {
		if h, ok := args[0].(*object.Hash); ok {
			key := h.Keys[0]
			val, _ := h.Get(key)
			return capName(key), capStrList(val)
		}
		return capName(args[0]), nil
	}
	for _, a := range args[1:] {
		deps = append(deps, capStrList(a)...)
	}
	return capName(args[0]), deps
}

// capCommandResult builds a scripted CommandResult from a TestBackend#script
// options Hash (stdout / stderr / exit_status), defaulting each field.
func capCommandResult(h *object.Hash) capistrano.CommandResult {
	res := capistrano.CommandResult{}
	if v, ok := h.Get(object.SymVal("stdout")); ok {
		res.Stdout = capName(v)
	}
	if v, ok := h.Get(object.SymVal("stderr")); ok {
		res.Stderr = capName(v)
	}
	if v, ok := h.Get(object.SymVal("exit_status")); ok {
		if n, ok := v.(object.Integer); ok {
			res.ExitStatus = int(n)
		}
	}
	return res
}

// capStrArray wraps a Go string slice as a Ruby Array of String.
func capStrArray(ss []string) object.Value {
	elems := make([]object.Value, len(ss))
	for i, s := range ss {
		elems[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(elems)
}

// capArg returns args[i] or Ruby nil when the position is absent (an optional
// trailing options Hash).
func capArg(args []object.Value, i int) object.Value {
	if i < len(args) {
		return args[i]
	}
	return object.NilV
}
