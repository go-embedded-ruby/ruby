// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !wasm && !rbgo_core && ((darwin && (amd64 || arm64)) || (freebsd && (386 || amd64 || arm || arm64)) || (linux && (386 || amd64 || arm || arm64 || loong64 || ppc64le || riscv64 || s390x)) || (netbsd && amd64) || (openbsd && (amd64 || arm64)) || windows)

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// The store bindings — SQLite3, Sequel and ActiveRecord's adapter (all three over
// modernc.org/sqlite), Bolt, Bleve and Etcd/Confd — build only where
// modernc.org/sqlite has a port: the targets listed in this file's constraint,
// which are exactly the GOOS_GOARCH pairs of modernc.org/sqlite v1.60.1's lib/
// directory (its pure-Go translation is generated per target, and
// modernc.org/libc under it likewise). Everywhere else — js/wasm and wasip1,
// linux/mips*, linux/ppc64, AIX, Solaris and illumos, DragonFly, NetBSD and
// OpenBSD on their smaller ports — bindings_store_stub.go installs graceful
// modules whose operations raise NotImplementedError. Bolt, Bleve and Etcd
// share the constraint rather than carrying their own: they are one seam, and
// on AIX, the only further target where they would build without SQLite,
// Bleve's mmap-go and Etcd's fileutil do not.

// classOfStoreBinding maps the store bindings' value types to their Ruby class,
// the tail of classOfExtBinding. ok is false for any other value.
func (vm *VM) classOfStoreBinding(v object.Value) (*RClass, bool) {
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
