// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build js && wasm

// This file is the GOOS=js GOARCH=wasm half of the server-gem seam. The gems it
// covers (see serverGemFeatures in servergems.go) are pointless in a browser:
// server/RPC/telemetry (Protobuf, GraphQL, OpenTelemetry), config management and
// deploy (Puppet + ResourceAPI, Hiera, SemanticPuppet, Augeas, Capistrano,
// Bundler), server/site frameworks and mail (Railties, Rails, Hanami, ActiveJob,
// Mail, ActionMailer, Jekyll), the Redis client, and acceptance/unit testing
// (Capybara, Minitest + Spec, Faker). None can do anything useful in a browser
// tab, yet linking their register functions pulls megabytes of Go backend code
// (google.golang.org/protobuf alone is ~3 MB) into every wasm client for code
// that can never run.
//
// registerServerGems is the wasm stub of the native registrar (servergems_
// native.go): it registers none of them, so the linker's dead-code elimination
// drops their whole backends from the browser binary. Because their feature
// names are still listed in require.go's providedFeatures, this stub installs a
// first-require hook for each (via hookWasmUnavailable, shared with the network
// backend stubs) so `require "puppet"` (etc.) raises a clean LoadError — exactly
// as a compiled-out gem should — instead of silently succeeding on a module that
// was never defined. builtins.go and the native build are unchanged: bootstrap()
// calls registerServerGems() on both, only the body differs by build tag.

package vm

// registerServerGems marks every browser-pointless server/ops/testing gem
// unavailable on wasm, one require hook per feature name in serverGemFeatures.
func (vm *VM) registerServerGems() {
	for _, f := range serverGemFeatures {
		vm.hookWasmUnavailable(f)
	}
}
