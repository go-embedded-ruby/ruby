// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build rbgo_core

// The rbgo_core build's side of gems.go: every group of gem registrations is
// empty, because the files those registrations live in are not compiled (they
// carry `//go:build !rbgo_core`). What a program sees instead is a LoadError on
// require, installed by applyCoreBuild (core_on.go) once bootstrap is done.

package vm

func (vm *VM) registerImageGem()            { _ = vm }
func (vm *VM) registerNetworkGems()         { _ = vm }
func (vm *VM) registerTimecopGem()          { _ = vm }
func (vm *VM) registerThirdPartyGems()      { _ = vm }
func (vm *VM) registerGetoptLongGem()       { _ = vm }
func (vm *VM) registerToolingGems()         { _ = vm }
func (vm *VM) registerOptparseURIGems()     { _ = vm }
func (vm *VM) registerREXMLGem()            { _ = vm }
func (vm *VM) registerArrowShellwordsGems() { _ = vm }
func (vm *VM) registerZlibGem()             { _ = vm }
func (vm *VM) registerServerGems()          { _ = vm }
func (vm *VM) registerConcurrentGem()       { _ = vm }
func (vm *VM) registerRailsSupportGems()    { _ = vm }
func (vm *VM) registerIPAddrGem()           { _ = vm }
func (vm *VM) registerPStoreGem()           { _ = vm }
func (vm *VM) includeMySQLEnumerableGem()   { _ = vm }
