// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !wasm && !plan9 && !rbgo_core

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// classOfMsgBinding is the native half of the classOf seam for the NATS, Kafka and
// Parquet bindings, whose Go drivers (nats.go, franz-go, apache/thrift) do not
// build for GOOS=plan9. It handles their value types exactly as the inline cases
// of classOfExtBinding did before they were split out; the plan9 half
// (backends_plan9.go) always reports false, since those types can never be
// constructed there (their register functions are graceful stubs).
func (vm *VM) classOfMsgBinding(v object.Value) (*RClass, bool) {
	switch x := v.(type) {
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
	case *ParquetReader:
		return vm.consts["Parquet::ArrowFileReader"].(*RClass), true
	case *ParquetWriter:
		return vm.consts["Parquet::ArrowFileWriter"].(*RClass), true
	}
	return nil, false
}
