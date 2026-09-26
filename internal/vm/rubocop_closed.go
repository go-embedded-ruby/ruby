// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build rbgo_closed

// This file is the closed-world (`rbgo build --closed`, -tags rbgo_closed)
// counterpart of the RuboCop binding, whose real implementation lives in
// rubocop.go and rubocop_bind.go under the //go:build !rbgo_closed tag.
//
// The degradation strategy is the LoadError one from backends_wasm.go, not the
// graceful-module one from bindings_wasm.go: a linter cannot work without a
// parser, so there is no useful subset of RuboCop to keep in a binary that has
// dropped the front-end. `require "rubocop"` therefore fails exactly as a missing
// gem would, instead of defining a module whose every operation would raise.
//
// The four wrapper TYPES are redeclared here as empty shells. They are not
// vestigial: internal/vm/object_model.go's classOf has a case arm per wrapper
// type, in a file with no build tag, so the type names must resolve for the
// closed build to compile at all. No value of these types can be constructed here
// — their constructors are the RuboCop::Runner / Config surface that this file
// does not install — so the arms are unreachable, and the methods exist only to
// satisfy object.Value. This mirrors arSQLiteAdapter in bindings_wasm.go, a stub
// type that exists to keep shared code compiling and is never reached.

package vm

// RuboCopRunner is the closed-world shell of the RuboCop::Runner wrapper. See the
// file comment: the type must exist for object_model.go's classOf, no value of it
// can be constructed here.
type RuboCopRunner struct{}

func (r *RuboCopRunner) ToS() string     { return "#<RuboCop::Runner>" }
func (r *RuboCopRunner) Inspect() string { return "#<RuboCop::Runner>" }
func (r *RuboCopRunner) Truthy() bool    { return true }

// RuboCopConfig is the closed-world shell of the RuboCop::Config wrapper.
type RuboCopConfig struct{}

func (c *RuboCopConfig) ToS() string     { return "#<RuboCop::Config>" }
func (c *RuboCopConfig) Inspect() string { return "#<RuboCop::Config>" }
func (c *RuboCopConfig) Truthy() bool    { return true }

// RuboCopOffense is the closed-world shell of the RuboCop::Cop::Offense wrapper.
type RuboCopOffense struct{}

func (o *RuboCopOffense) ToS() string     { return "#<RuboCop::Cop::Offense>" }
func (o *RuboCopOffense) Inspect() string { return "#<RuboCop::Cop::Offense>" }
func (o *RuboCopOffense) Truthy() bool    { return true }

// RuboCopLocation is the closed-world shell of the
// RuboCop::Cop::Offense::Location wrapper.
type RuboCopLocation struct{}

func (l *RuboCopLocation) ToS() string     { return "#<RuboCop::Cop::Offense::Location>" }
func (l *RuboCopLocation) Inspect() string { return l.ToS() }
func (l *RuboCopLocation) Truthy() bool    { return true }

// registerRuboCop is the closed-world stub of the native registration
// builtins.go calls unconditionally. It installs no RuboCop constants and instead
// marks the feature unavailable, so builtins.go needs no build tag of its own.
func (vm *VM) registerRuboCop() { vm.hookClosedUnavailable(closedUnavailableFeatures...) }

// hookClosedUnavailable installs, for each require feature name, a first-require
// hook raising the LoadError for a gem compiled out of the closed-world build.
// require.go consults featureHooks after marking a provided feature loaded, so the
// hook fires on the first `require "<name>"`, and raiseClosedUnavailable clears the
// marker so a retry re-raises. The nil-guard keeps this order-independent with
// respect to registerPrime, exactly as hookWasmUnavailable does.
func (vm *VM) hookClosedUnavailable(features ...string) {
	if vm.featureHooks == nil {
		vm.featureHooks = map[string]func(){}
	}
	for _, f := range features {
		f := f
		vm.featureHooks[f] = func() { vm.raiseClosedUnavailable(f) }
	}
}
