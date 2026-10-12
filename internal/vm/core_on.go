// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build rbgo_core

package vm

// coreBuild is true on the rbgo_core build: bootstrap skips every gem
// registration guarded by `if !coreBuild`, and the linker drops the gems'
// Go backends with them. See core.go for what core is.
const coreBuild = true

// applyCoreBuild makes every require of a gem this build left out raise
// LoadError. It runs once, after the last registration.
func (vm *VM) applyCoreBuild() { vm.hookOutOfCore() }
