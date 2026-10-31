// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build rbgo_core

// The rbgo_core build's side of gems.go: every group of gem registrations is
// empty, because the files those registrations live in are not compiled (they
// carry `//go:build !rbgo_core`). What a program sees instead is a LoadError on
// require, installed by applyCoreBuild (core_on.go) once bootstrap is done.

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The gem seams of arith.go and digest_bind.go (gems_classof.go): no gem
// value exists on the core build, so each answers "not mine".
func gemArith(op bytecode.Op, a, b object.Value) (object.Value, bool) {
	_, _, _ = op, a, b
	return nil, false
}
func gemDispatchesArith(a object.Value) bool        { _ = a; return false }
func gemNegate(v object.Value) (object.Value, bool) { _ = v; return nil, false }
func gemValueEqual(a, b object.Value) (bool, bool)  { _, _ = a, b; return false, false }
func gemHasCustomEq(v object.Value) bool            { _ = v; return false }

// gemState holds no gem state on the core build (gems_state.go).
type gemState struct{}

// classOfGem: no gem value type exists on the core build (gems_classof.go).
func (vm *VM) classOfGem(v object.Value) (*RClass, bool) { _, _ = vm, v; return nil, false }

// ivarStoreOfGem: no gem value carries instance variables on the core build.
func ivarStoreOfGem(self object.Value) (ivarStore, bool) { _ = self; return ivarStore{}, false }

// The binding seams bindings_native.go / bindings_wasm.go fill on the default
// build: none of their value types can be constructed on the core build.
func (vm *VM) classOfExtBinding(v object.Value) (*RClass, bool) { _, _ = vm, v; return nil, false }
func valueEqualExtBinding(a, b object.Value) (bool, bool)       { _, _ = a, b; return false, false }
func hasCustomEqExtBinding(v object.Value) bool                 { _ = v; return false }

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
