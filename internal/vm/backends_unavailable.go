// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// wasmUnavailableFeatures is the roster of require feature names whose gem
// backends link heavy network/OS Go libraries — Kafka (twmb/franz-go), NATS
// (nats.go), MongoDB (mongo-driver), MySQL (go-sql-driver), gRPC
// (google.golang.org/grpc), Arrow and Parquet (apache/arrow-go), OpenStack
// (gophercloud), and Sidekiq/Resque (redis/go-redis) — that cannot function
// under GOOS=js GOARCH=wasm, where there are no TCP sockets and no cgo. On the
// wasm build those bindings are compiled out (their files carry the
// //go:build !(js && wasm) tag) and a require of any of these raises a LoadError;
// on every native build the gems are present, so this table is only the
// documentation that the stub-registration test asserts against.
//
// It is deliberately declared in a shared (build-tag-free) file so a native test
// can read it and prove exactly which features degrade, and so the shared
// raiseWasmUnavailable below — which produces the LoadError on wasm — is itself
// natively testable.
var wasmUnavailableFeatures = []string{
	"grpc",
	"nats",
	"kafka",
	"mysql2", "mysql",
	"mongo", "bson",
	"parquet",
	"arrow",
	"openstack",
	"sidekiq",
	"resque",
}

// raiseWasmUnavailable raises the LoadError a wasm build produces when a program
// requires one of the network/OS backends compiled out of that build. It clears
// the just-set loaded marker first (require.go marks the feature loaded before
// invoking its hook), so a retried require re-raises rather than silently
// returning false. The message mirrors MRI's "cannot load such file" LoadError,
// annotated with the reason. It is only wired to a require hook on wasm (see
// backends_wasm.go), but lives in a shared file so a native test can assert its
// class and message directly.
func (vm *VM) raiseWasmUnavailable(feature string) object.Value {
	delete(vm.loaded, "feature:"+feature)
	return raise("LoadError", "cannot load such file -- %s (not available in the js/wasm build)", feature)
}
