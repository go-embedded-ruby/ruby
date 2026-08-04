// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build js && wasm

// This file is the GOOS=js GOARCH=wasm counterpart of the gem backends whose Go
// libraries do build for wasm but cannot function in a browser because they open
// real TCP sockets or use OS facilities unavailable there: Kafka (twmb/franz-go),
// NATS (nats.go), MongoDB (mongo-driver), MySQL (go-sql-driver), gRPC
// (google.golang.org/grpc), Arrow and Parquet (apache/arrow-go), OpenStack
// (gophercloud) and Sidekiq/Resque (redis/go-redis). Their real register
// functions and value bindings live in files carried by the //go:build !(js &&
// wasm) tag, so linking them into a browser build only bloats it (23 MB Arrow,
// 7 MB go-redis, 6.7 MB franz-go, …) for code that can never run.
//
// The degradation strategy here differs from the graceful-module stubs in
// bindings_wasm.go (SQLite3/Bolt/Bleve/Etcd, whose require still succeeds): these
// backends are truly unloadable, so each stub installs a require hook that raises
// a clean LoadError (see raiseWasmUnavailable) — `require "kafka"` fails exactly
// as a missing gem would, instead of silently pretending to load a module whose
// operations would all fail. The stubs keep builtins.go and vm.go compiling and
// byte-for-byte identical on the native build (where the real register functions
// are linked instead).

package vm

// The register* stubs replace the native gem registrations called from
// builtins.go; each marks its require feature name(s) unavailable.
func (vm *VM) registerGRPC()      { vm.hookWasmUnavailable("grpc") }
func (vm *VM) registerNATS()      { vm.hookWasmUnavailable("nats") }
func (vm *VM) registerKafka()     { vm.hookWasmUnavailable("kafka") }
func (vm *VM) registerMySQL()     { vm.hookWasmUnavailable("mysql2", "mysql") }
func (vm *VM) registerMongo()     { vm.hookWasmUnavailable("mongo", "bson") }
func (vm *VM) registerParquet()   { vm.hookWasmUnavailable("parquet") }
func (vm *VM) registerArrow()     { vm.hookWasmUnavailable("arrow") }
func (vm *VM) registerOpenStack() { vm.hookWasmUnavailable("openstack") }
func (vm *VM) registerSidekiq()   { vm.hookWasmUnavailable("sidekiq") }
func (vm *VM) registerResque()    { vm.hookWasmUnavailable("resque") }

// includeMySQLEnumerable is the wasm stub of the native post-prelude step that
// mixes Enumerable into Mysql2::Result; there is no Mysql2::Result on wasm.
func (vm *VM) includeMySQLEnumerable() {}

// hookWasmUnavailable installs, for each require feature name, a first-require
// hook that raises the LoadError for a compiled-out backend. require.go consults
// featureHooks after marking a provided feature loaded, so the hook fires on the
// first `require "<name>"`; raiseWasmUnavailable clears the marker so a retry
// re-raises. featureHooks is created by registerPrime before these run, but the
// nil-guard keeps the stubs order-independent.
func (vm *VM) hookWasmUnavailable(features ...string) {
	if vm.featureHooks == nil {
		vm.featureHooks = map[string]func(){}
	}
	for _, f := range features {
		f := f
		vm.featureHooks[f] = func() { vm.raiseWasmUnavailable(f) }
	}
}
