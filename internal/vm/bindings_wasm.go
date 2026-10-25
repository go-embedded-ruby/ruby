// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build wasm && !rbgo_core

// This file is the wasm half of the value seams of the bindings that cannot
// function under GOOS=js / GOOS=wasip1 GOARCH=wasm (bindings_native.go is the
// other half). The store bindings (SQLite3, Sequel, ActiveRecord's adapter,
// Bolt, Bleve, Etcd, Confd) are stubbed by bindings_store_stub.go, which serves
// every target their drivers do not build for, wasm included.

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// classOfExtBinding is the wasm half of the classOf seam: the guarded value types
// can never be constructed on wasm (their constructors are stubs), so no value
// ever needs mapping here.
func (vm *VM) classOfExtBinding(v object.Value) (*RClass, bool) { _, _ = vm, v; return nil, false }

// valueEqualExtBinding is the wasm half of the value-equality seam: the guarded
// value types (Arrow::DataType, …) can never be constructed on wasm, so no value
// ever needs its own equality here.
func valueEqualExtBinding(a, b object.Value) (bool, bool) { _, _ = a, b; return false, false }

// hasCustomEqExtBinding is the wasm half of the custom-== seam: the guarded value
// types (BSON::ObjectId, …) can never be constructed on wasm.
func hasCustomEqExtBinding(v object.Value) bool { _ = v; return false }
