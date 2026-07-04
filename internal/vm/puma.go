// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	libpuma "github.com/go-ruby-puma/puma"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerPuma installs the Puma module (require "puma"): the threaded Rack web
// server puma gem, reimplemented in pure Go (CGO=0) by
// github.com/go-ruby-puma/puma on top of net/http. The library owns the server
// machinery (listeners, the bounded thread pool, the HTTP<->Rack env/response
// translation) and treats the Rack application as an injectable host seam; this
// file is the thin shell mapping that surface onto rbgo classes:
//
//	Puma::Server.new(app)     — the threaded server; #run/#start bind a listener
//	                            and serve, #stop/#halt tear it down, #port/#url
//	                            report the bound address
//	Puma::ThreadPool          — the server's Rack-call pool (#spawned/#backlog/…)
//	Puma::Configuration/::DSL — the puma config-block surface (bind/port/threads/…)
//	Puma::Const               — the VERSION / server-string constants
//	Puma::Error (< StandardError) / Puma::HttpParserError — the exception tree
//
// The Rack seam — turning an incoming HTTP request into a Ruby Rack `env` Hash,
// invoking the app's #call and translating the returned [status, headers, body]
// triple back into the HTTP response — lives in puma_bind.go. Because rbgo runs
// bytecode under an emulated GVL (one Ruby thread at a time, see thread.go), that
// seam serializes every request onto the VM by acquiring the GVL, so the app is
// only ever entered by one goroutine at a time regardless of the pool's width.
func (vm *VM) registerPuma() {
	mod := newClass("Puma", nil)
	mod.isModule = true
	vm.consts["Puma"] = mod

	// Puma::VERSION and the Puma::Const version/server-string constants, mirroring
	// the gem's Puma::Const::PUMA_VERSION / PUMA_SERVER_STRING.
	mod.consts["VERSION"] = object.NewString(libpuma.Version)
	constMod := newClass("Puma::Const", nil)
	constMod.isModule = true
	mod.consts["Const"] = constMod
	vm.consts["Puma::Const"] = constMod
	constMod.consts["VERSION"] = object.NewString(libpuma.Version)
	constMod.consts["PUMA_VERSION"] = object.NewString(libpuma.Version)
	constMod.consts["PUMA_SERVER_STRING"] = object.NewString(libpuma.ServerSoftware)

	// Puma.version — the module-level reader the gem exposes as Puma::Const::VERSION.
	mod.smethods["version"] = &Method{name: "version", owner: mod,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NewString(libpuma.Version)
		}}

	vm.registerPumaErrors(mod)

	mk := func(name string, super *RClass) *RClass {
		full := "Puma::" + name
		cls := newClass(full, super)
		mod.consts[name] = cls
		vm.consts[full] = cls
		return cls
	}
	vm.registerPumaServer(mk("Server", vm.cObject))
	vm.registerPumaThreadPool(mk("ThreadPool", vm.cObject))
	vm.registerPumaConfiguration(mk("Configuration", vm.cObject), mk("DSL", vm.cObject))
}

// registerPumaErrors installs the Puma exception tree: Puma::Error <
// StandardError and Puma::HttpParserError < Puma::Error, the class a failed bind
// / listener setup raises. Each is registered both scoped (under Puma) and flat
// in vm.consts so raise can find it by its qualified name.
func (vm *VM) registerPumaErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)

	base := newClass("Puma::Error", std)
	mod.consts["Error"] = base
	vm.consts["Puma::Error"] = base

	hpe := newClass("Puma::HttpParserError", base)
	mod.consts["HttpParserError"] = hpe
	vm.consts["Puma::HttpParserError"] = hpe
}

// pumaSMethod installs a class ("singleton") method on a class.
func pumaSMethod(cls *RClass, name string, fn NativeFn) {
	cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
}

// registerPumaServer installs Puma::Server: Server.new(app, options=nil) and the
// #run/#start (bind + serve), #stop/#halt, #running?, address-reporting and
// #thread_pool surface. options is either a Hash ({min_threads:, max_threads:,
// workers:, environment:}) or a Puma::Configuration.
func (vm *VM) registerPumaServer(cls *RClass) {
	pumaSMethod(cls, "new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		opts := libpuma.DefaultOptions()
		if len(args) >= 2 {
			opts = pumaOptions(args[1])
		}
		return newPumaServer(vm, args[0], opts)
	})

	self := func(v object.Value) *PumaServer { return v.(*PumaServer) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// #run / #start(host = "127.0.0.1", port = 0) — bind a TCP listener (port 0
	// selects an ephemeral port) and start serving; returns self, mirroring
	// Puma::Server#run returning a handle rather than blocking.
	run := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return self(v).run(args)
	}
	d("run", run)
	d("start", run)

	d("stop", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).srv.Stop()
		return v
	})
	d("halt", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).srv.Halt()
		return v
	})
	d("running?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).srv.Running())
	})
	d("port", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).port()))
	})
	d("host", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).host())
	})
	d("address", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).address())
	})
	d("url", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).url())
	})
	d("thread_pool", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &PumaThreadPool{tp: self(v).srv.ThreadPool()}
	})
	d("app", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).app
	})
}

// registerPumaThreadPool installs Puma::ThreadPool: the read/trim surface over
// the server's Rack-call pool (Server#thread_pool). #spawned/#backlog report the
// live pool, #trim/#shutdown drive it, mirroring Puma::ThreadPool.
func (vm *VM) registerPumaThreadPool(cls *RClass) {
	self := func(v object.Value) *PumaThreadPool { return v.(*PumaThreadPool) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("spawned", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).tp.Spawned()))
	})
	d("backlog", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).tp.Backlog()))
	})
	d("trim", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		force := len(args) > 0 && args[0].Truthy()
		self(v).tp.Trim(force)
		return v
	})
	d("shutdown", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).tp.Shutdown()
		return v
	})
}

// registerPumaConfiguration installs Puma::Configuration and Puma::DSL:
// Configuration.new { |c| c.bind …; c.port …; c.threads … } evaluates the config
// block against a Puma::DSL (each method records an option) and #options reads
// the accumulated options back as a Hash. A Configuration is also accepted as
// Puma::Server.new's options argument.
func (vm *VM) registerPumaConfiguration(cfgCls, dslCls *RClass) {
	pumaSMethod(cfgCls, "new", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			return &PumaConfiguration{cfg: libpuma.NewConfiguration()}
		}
		cfg := libpuma.NewConfiguration(func(d *libpuma.DSL) {
			vm.callBlock(blk, []object.Value{&PumaDSL{d: d}})
		})
		return &PumaConfiguration{cfg: cfg}
	})

	cfgCls.define("options", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return pumaOptionsHash(v.(*PumaConfiguration).cfg.Options())
	})

	self := func(v object.Value) *PumaDSL { return v.(*PumaDSL) }
	d := func(name string, fn NativeFn) { dslCls.define(name, fn) }

	d("bind", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).d.Bind(rackStr(pumaFirst(args, "bind")))
		return object.NilV
	})
	d("port", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		if len(args) >= 2 {
			self(v).d.Port(pumaInt(args[0]), rackStr(args[1]))
		} else {
			self(v).d.Port(pumaInt(args[0]))
		}
		return object.NilV
	})
	d("threads", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).d.Threads(pumaInt(args[0]), pumaInt(args[1]))
		return object.NilV
	})
	d("workers", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).d.Workers(pumaInt(pumaFirst(args, "workers")))
		return object.NilV
	})
	d("environment", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).d.Environment(rackStr(pumaFirst(args, "environment")))
		return object.NilV
	})
}
