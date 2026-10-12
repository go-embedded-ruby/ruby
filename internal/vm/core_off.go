// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

// coreBuild is false on the default build, so every `if !coreBuild { … }` in
// bootstrap runs exactly the registrations it always ran, in the same order.
// See core.go for the rbgo_core build.
const coreBuild = false

// applyCoreBuild has nothing to do on the default build: every gem is there.
func (vm *VM) applyCoreBuild() { _ = vm }
