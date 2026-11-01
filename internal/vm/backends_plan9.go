// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build plan9 && !rbgo_core

// This file is the GOOS=plan9 counterpart of the gem backends whose Go drivers do
// not compile there: NATS (nats-io/nats.go uses syscall.EPIPE and ECONNRESET),
// Kafka (twmb/franz-go uses syscall.ECONNRESET) and Parquet (apache/arrow-go's
// parquet package pulls apache/thrift, which uses syscall.Sockaddr, Recvfrom and
// EAGAIN). Plan 9's syscall package has no errno numbers and no socket-address
// API, so those packages are compiled out (their files carry !plan9) and the
// bindings degrade the way the store gems do on targets without modernc sqlite
// (bindings_store_stub.go): `require "nats"` still succeeds, the module and its
// exception class exist so a `rescue NATS::Error` loads, and the first real
// operation raises NotImplementedError "<gem> is not supported on plan9/<arch>".

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// classOfMsgBinding is the plan9 half of the classOf seam (bindings_msg.go): the
// NATS, Kafka and Parquet value types do not exist here.
func (vm *VM) classOfMsgBinding(v object.Value) (*RClass, bool) { _, _ = vm, v; return nil, false }

// registerNATS installs a graceful NATS module: NATS::Error, and the NATS.connect
// factory plus NATS::Client.new, all raising.
func (vm *VM) registerNATS() {
	mod := vm.stubBindingModule("NATS", "NATS::Error")
	stubSMethods(mod, "nats", "connect")
	client := vm.stubBindingClass(mod, "NATS::Client")
	stubSMethods(client, "nats", "new")
}

// registerKafka installs a graceful Kafka module: Kafka::Error, and the Kafka.new
// client factory, which raises.
func (vm *VM) registerKafka() {
	mod := vm.stubBindingModule("Kafka", "Kafka::Error")
	stubSMethods(mod, "kafka", "new")
}

// registerParquet installs a graceful Parquet module: Parquet::Error, the
// Parquet.load / .save convenience methods and the ArrowFileReader/ArrowFileWriter
// classes whose .new/.open raise.
func (vm *VM) registerParquet() {
	mod := vm.stubBindingModule("Parquet", "Parquet::Error")
	stubSMethods(mod, "parquet", "load", "save")
	for _, n := range []string{"Parquet::ArrowFileReader", "Parquet::ArrowFileWriter"} {
		stubSMethods(vm.stubBindingClass(mod, n), "parquet", "new", "open")
	}
}
