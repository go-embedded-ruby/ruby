// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !wasm

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// classOfExtBinding maps the value types of the database / search / KV / network
// bindings that cannot function under GOOS=js GOARCH=wasm — either because their
// Go drivers do not build for wasm (SQLite3, Sequel, Bolt, Bleve, Etcd) or
// because they link heavy network/OS libraries useless in a browser (gRPC, NATS,
// Kafka, MySQL, MongoDB, Parquet, Arrow, Sidekiq/Resque over go-redis,
// OpenStack/gophercloud) — to their Ruby class. It is the native half of the
// classOf seam: on the native build it handles every such value exactly as an
// inline classOf case would; the js/wasm half (bindings_wasm.go) always reports
// false, since those value types can never be constructed there (their register
// functions are graceful stubs). ok is false for any other value, so classOf
// falls through to its main switch.
func (vm *VM) classOfExtBinding(v object.Value) (*RClass, bool) {
	switch x := v.(type) {
	case *GRPCServer:
		return vm.consts["GRPC::RpcServer"].(*RClass), true
	case *GRPCStub:
		return vm.consts["GRPC::ClientStub"].(*RClass), true
	case *GRPCService:
		return vm.consts["GRPC::Service"].(*RClass), true
	case *GRPCActiveCall:
		return vm.consts["GRPC::ActiveCall"].(*RClass), true
	case *GRPCStatus:
		return vm.consts["GRPC::Status"].(*RClass), true
	case *NATSX:
		return x.cls, true
	case *KafkaClient:
		return x.cls, true
	case *KafkaProducer:
		return x.cls, true
	case *KafkaConsumer:
		return x.cls, true
	case *KafkaMessage:
		return x.cls, true
	case *KafkaBatch:
		return x.cls, true
	case *MySQLClient:
		return x.cls, true
	case *MySQLResult:
		return x.cls, true
	case *MySQLStatement:
		return x.cls, true
	case *MongoClient:
		return x.cls, true
	case *MongoDatabase:
		return x.cls, true
	case *MongoCollection:
		return x.cls, true
	case *MongoCursor:
		return x.cls, true
	case *MongoResult:
		return x.cls, true
	case *MongoObjectId:
		return x.cls, true
	case *ParquetReader:
		return vm.consts["Parquet::ArrowFileReader"].(*RClass), true
	case *ParquetWriter:
		return vm.consts["Parquet::ArrowFileWriter"].(*RClass), true
	case *ArrowArray:
		return vm.consts["Arrow::Array"].(*RClass), true
	case *ArrowArrayBuilder:
		return vm.consts["Arrow::ArrayBuilder"].(*RClass), true
	case *ArrowDataType:
		return vm.consts["Arrow::DataType"].(*RClass), true
	case *ArrowField:
		return vm.consts["Arrow::Field"].(*RClass), true
	case *ArrowSchema:
		return vm.consts["Arrow::Schema"].(*RClass), true
	case *ArrowRecordBatch:
		return vm.consts["Arrow::RecordBatch"].(*RClass), true
	case *ArrowTable:
		return vm.consts["Arrow::Table"].(*RClass), true
	case *JobRedis:
		// A Sidekiq.redis / Resque.redis block connection reports the class stamped
		// on it at construction (Sidekiq::RedisConnection or Resque::RedisConnection).
		return x.cls, true
	case *ResqueJob:
		return x.cls, true
	case *ResqueWorker:
		return x.cls, true
	case *OpenStackConnection:
		return vm.consts["OpenStack::Connection"].(*RClass), true
	case *OpenStackCompute:
		return vm.consts["OpenStack::Compute"].(*RClass), true
	case *OpenStackNetwork:
		return vm.consts["OpenStack::Network"].(*RClass), true
	case *OpenStackBlockStorage:
		return vm.consts["OpenStack::BlockStorage"].(*RClass), true
	case *OpenStackObjectStorage:
		return vm.consts["OpenStack::ObjectStorage"].(*RClass), true
	case *OpenStackImage:
		return vm.consts["OpenStack::Image"].(*RClass), true
	case *OpenStackIdentity:
		return vm.consts["OpenStack::Identity"].(*RClass), true
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

// valueEqualExtBinding is the native half of the value-equality seam for the
// js/wasm-incompatible bindings: Arrow's DataType compares through the
// go-ruby-arrow library. It mirrors the inline case valueEqualRec would carry.
// ok is false for any other value, so the shared comparison proceeds. The
// js/wasm half (bindings_wasm.go) always reports false — the type cannot exist
// there.
func valueEqualExtBinding(a, b object.Value) (eq bool, ok bool) {
	if av, isArrow := a.(*ArrowDataType); isArrow {
		bv, isArrowB := b.(*ArrowDataType)
		return isArrowB && av.dt.EqualQ(bv.dt), true
	}
	return false, false
}

// hasCustomEqExtBinding is the native half of the custom-== seam for the
// js/wasm-incompatible bindings: BSON::ObjectId (Mongo) compares by value, so it
// dispatches its own ==. The js/wasm half always reports false — the type cannot
// exist there.
func hasCustomEqExtBinding(v object.Value) bool {
	_, ok := v.(*MongoObjectId)
	return ok
}
