// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !(js && wasm)

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// classOfExtBinding maps the value types of the database / search / KV bindings
// whose drivers do not build for GOOS=js GOARCH=wasm (SQLite3, Sequel, Bolt,
// Bleve, Etcd) to their Ruby class. It is the native half of the classOf seam:
// on the native build it handles every such value exactly as an inline classOf
// case would; the js/wasm half (bindings_wasm.go) always reports false, since
// those value types can never be constructed there (their register functions are
// graceful stubs). ok is false for any other value, so classOf falls through to
// its main switch.
func (vm *VM) classOfExtBinding(v object.Value) (*RClass, bool) {
	switch x := v.(type) {
	case *SQLite3Database:
		return vm.consts["SQLite3::Database"].(*RClass), true
	case *SQLite3Statement:
		return vm.consts["SQLite3::Statement"].(*RClass), true
	case *SequelDBObj:
		return x.cls, true
	case *SequelDatasetObj:
		return x.cls, true
	case *SequelSchemaObj:
		return x.cls, true
	case *BoltDB:
		return x.cls, true
	case *BoltTx:
		return x.cls, true
	case *BoltBucket:
		return x.cls, true
	case *BoltCursor:
		return x.cls, true
	case *BleveIndex:
		return x.cls, true
	case *BleveMapping:
		return x.cls, true
	case *BleveQuery:
		return x.cls, true
	case *BleveSearchResult:
		return x.cls, true
	case *BleveHit:
		return x.cls, true
	case *BleveBatch:
		return x.cls, true
	case *BleveFacet:
		return x.cls, true
	case *EtcdClient:
		return x.cls, true
	case *EtcdKeyValue:
		return x.cls, true
	case *EtcdGetResult:
		return x.cls, true
	case *EtcdPutResult:
		return x.cls, true
	case *EtcdDelResult:
		return x.cls, true
	case *EtcdLease:
		return x.cls, true
	case *EtcdEvent:
		return x.cls, true
	case *EtcdTxn:
		return x.cls, true
	case *EtcdCmp:
		return x.cls, true
	case *EtcdOp:
		return x.cls, true
	case *EtcdTxnResult:
		return x.cls, true
	case *EtcdLock:
		return x.cls, true
	case *EtcdMember:
		return x.cls, true
	case *EtcdStatus:
		return x.cls, true
	}
	return nil, false
}
