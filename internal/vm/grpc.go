// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	libgrpc "github.com/go-ruby-grpc/grpc"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerGRPC installs the GRPC module (require "grpc"): the pure-Go, CGO-free
// reimplementation of Ruby's grpc gem surface, backed by
// github.com/go-ruby-grpc/grpc (an MRI-faithful API layer over the canonical
// google.golang.org/grpc and google.golang.org/protobuf runtimes). The library
// owns the whole gRPC stack (HTTP/2, status propagation, streaming) and treats
// the network as an injected seam; this binding wires one process-wide in-memory
// transport (bufconn) shared by every server and stub, so a require "grpc"
// program runs a real gRPC HTTP/2 session end-to-end without ever binding a TCP
// port. The Ruby surface it maps onto:
//
//	GRPC::RpcServer        — new / add_http2_port / handle / run /
//	                         run_till_terminated / stop / running?
//	GRPC::ClientStub       — new(host, creds) / request_response /
//	                         client_streamer / server_streamer / bidi_streamer /
//	                         close
//	GRPC::Service          — new(name){ |s| s.rpc(name, type){ handler } } — the
//	                         service definition passed to RpcServer#handle
//	GRPC::ActiveCall       — remote_send / remote_read / each_remote_read /
//	                         metadata / deadline (the object a streaming handler
//	                         drives)
//	GRPC::Status           — the code/details/metadata a BadStatus#to_status
//	                         returns
//	GRPC::Core::StatusCodes — the canonical status-code constants (OK … )
//	GRPC::Core::CallError   — a low-level call-machinery error
//	GRPC::Error < StandardError, GRPC::BadStatus < GRPC::Error and the per-code
//	                         subclasses (GRPC::InvalidArgument, GRPC::NotFound, …)
//
// The server invokes Ruby handlers from gRPC goroutines, so each handler is
// serialized onto the single-threaded VM under the emulated GVL (see grpcServe
// in grpc_bind.go, mirroring puma's Rack seam), and every blocking client call
// releases the GVL (via threadBlock) so an in-process server handler can run —
// which is what lets a single require "grpc" program act as both peers.
func (vm *VM) registerGRPC() {
	mod := newClass("GRPC", nil)
	mod.isModule = true
	vm.consts["GRPC"] = mod

	vm.registerGRPCCore(mod)
	vm.registerGRPCErrors(mod)

	mk := func(name string, super *RClass) *RClass {
		full := "GRPC::" + name
		cls := newClass(full, super)
		mod.consts[name] = cls
		vm.consts[full] = cls
		return cls
	}
	vm.registerGRPCStatus(mk("Status", vm.cObject))
	vm.registerGRPCService(mk("Service", vm.cObject))
	vm.registerGRPCActiveCall(mk("ActiveCall", vm.cObject))

	// One in-memory transport per VM, shared by every server and stub the program
	// creates, so servers and stubs wire together over bufconn with no OS port.
	tr := libgrpc.NewMemTransport()
	vm.registerGRPCServer(mk("RpcServer", vm.cObject), tr)
	vm.registerGRPCClientStub(mk("ClientStub", vm.cObject), tr)
}

// grpcCode pairs a status code with its gem spellings: the SCREAMING_SNAKE_CASE
// GRPC::Core::StatusCodes constant name and the CamelCase GRPC:: exception-class
// name.
type grpcCode struct {
	screaming string
	camel     string
	code      libgrpc.StatusCode
}

// grpcCodes is the canonical gRPC status table, matching the gem one-to-one.
var grpcCodes = []grpcCode{
	{"OK", "Ok", libgrpc.OK},
	{"CANCELLED", "Cancelled", libgrpc.Cancelled},
	{"UNKNOWN", "Unknown", libgrpc.Unknown},
	{"INVALID_ARGUMENT", "InvalidArgument", libgrpc.InvalidArgument},
	{"DEADLINE_EXCEEDED", "DeadlineExceeded", libgrpc.DeadlineExceeded},
	{"NOT_FOUND", "NotFound", libgrpc.NotFound},
	{"ALREADY_EXISTS", "AlreadyExists", libgrpc.AlreadyExists},
	{"PERMISSION_DENIED", "PermissionDenied", libgrpc.PermissionDenied},
	{"RESOURCE_EXHAUSTED", "ResourceExhausted", libgrpc.ResourceExhausted},
	{"FAILED_PRECONDITION", "FailedPrecondition", libgrpc.FailedPrecondition},
	{"ABORTED", "Aborted", libgrpc.Aborted},
	{"OUT_OF_RANGE", "OutOfRange", libgrpc.OutOfRange},
	{"UNIMPLEMENTED", "Unimplemented", libgrpc.Unimplemented},
	{"INTERNAL", "Internal", libgrpc.Internal},
	{"UNAVAILABLE", "Unavailable", libgrpc.Unavailable},
	{"DATA_LOSS", "DataLoss", libgrpc.DataLoss},
	{"UNAUTHENTICATED", "Unauthenticated", libgrpc.Unauthenticated},
}

// registerGRPCCore installs GRPC::Core: the StatusCodes constant module and the
// Core::CallError class.
func (vm *VM) registerGRPCCore(mod *RClass) {
	core := newClass("GRPC::Core", nil)
	core.isModule = true
	mod.consts["Core"] = core
	vm.consts["GRPC::Core"] = core

	codes := newClass("GRPC::Core::StatusCodes", nil)
	codes.isModule = true
	core.consts["StatusCodes"] = codes
	vm.consts["GRPC::Core::StatusCodes"] = codes
	for _, c := range grpcCodes {
		codes.consts[c.screaming] = object.IntValue(int64(c.code))
	}
}

// registerGRPCErrors installs the GRPC exception tree: GRPC::Error <
// StandardError, GRPC::BadStatus < GRPC::Error carrying #code/#details/#metadata,
// and one BadStatus subclass per status code (GRPC::InvalidArgument, … ) so a
// failed call raises the class the gem raises and rescues by GRPC::BadStatus.
// GRPC::Core::CallError < GRPC::Error is the low-level call-machinery error.
func (vm *VM) registerGRPCErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)

	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}

	base := reg("Error", "GRPC::Error", std)
	bad := reg("BadStatus", "GRPC::BadStatus", base)

	// GRPC::BadStatus.new(code, details = "", metadata = {}) — the gem's explicit
	// constructor; the code is the first argument.
	bad.smethods["new"] = &Method{name: "new", owner: bad,
		native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			cls := self.(*RClass)
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..3)")
			}
			code := libgrpc.StatusCode(grpcInt(args[0]))
			return grpcNewBadStatus(cls, code, grpcArgAt(args, 1), grpcArgAt(args, 2))
		}}

	vm.registerGRPCBadStatusMethods(bad)

	// One exception class per code, each carrying its fixed code so
	// GRPC::InvalidArgument.new(details) needs no code argument, mirroring the gem.
	core := mod.consts["Core"].(*RClass)
	for _, c := range grpcCodes {
		code := c.code
		sub := newClass("GRPC::"+c.camel, bad)
		mod.consts[c.camel] = sub
		vm.consts["GRPC::"+c.camel] = sub
		sub.smethods["new"] = &Method{name: "new", owner: sub,
			native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
				return grpcNewBadStatus(self.(*RClass), code, grpcArgAt(args, 0), grpcArgAt(args, 1))
			}}
	}

	callErr := newClass("GRPC::Core::CallError", base)
	core.consts["CallError"] = callErr
	vm.consts["GRPC::Core::CallError"] = callErr
	callErr.smethods["new"] = &Method{name: "new", owner: callErr,
		native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			o := &RObject{class: self.(*RClass), ivars: map[string]object.Value{}}
			o.ivars["@message"] = object.NewString(grpcArgAt(args, 0).ToS())
			return o
		}}
}

// registerGRPCBadStatusMethods installs the readers shared by every BadStatus
// (and its subclasses): #code, #details, #metadata, #message and #to_status.
func (vm *VM) registerGRPCBadStatusMethods(bad *RClass) {
	bad.define("code", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@code")
	})
	bad.define("details", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@details")
	})
	bad.define("metadata", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@metadata")
	})
	bad.define("message", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@message")
	})
	bad.define("to_status", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &GRPCStatus{
			code:     getIvar(self, "@code"),
			details:  getIvar(self, "@details"),
			metadata: getIvar(self, "@metadata"),
		}
	})
}

// registerGRPCStatus installs GRPC::Status, the value BadStatus#to_status
// returns: the read-only code/details/metadata triple, mirroring the gem's
// Struct::Status.
func (vm *VM) registerGRPCStatus(cls *RClass) {
	cls.define("code", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*GRPCStatus).code
	})
	cls.define("details", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*GRPCStatus).details
	})
	cls.define("metadata", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*GRPCStatus).metadata
	})
}

// grpcType maps an RPC-cardinality symbol/string to the library's MethodType.
func grpcType(v object.Value) libgrpc.MethodType {
	switch grpcName(v) {
	case "request_response":
		return libgrpc.Unary
	case "client_streamer":
		return libgrpc.ClientStream
	case "server_streamer":
		return libgrpc.ServerStream
	case "bidi_streamer":
		return libgrpc.BidiStream
	}
	raise("ArgumentError", "unknown rpc type %s", v.Inspect())
	return 0
}

// registerGRPCService installs GRPC::Service: Service.new(name) { |s| … } builds
// a service definition, s.rpc(name, type) { handler } (and the four cardinality
// shorthands) records one RPC, and s.marshal= / s.unmarshal= override the
// message codec (the default is a String codec, so String messages need no
// procs). The result is passed to GRPC::RpcServer#handle.
func (vm *VM) registerGRPCService(cls *RClass) {
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			svc := &GRPCService{name: args[0].ToS(), marshal: object.NilV, unmarshal: object.NilV}
			if blk != nil {
				vm.callBlock(blk, []object.Value{svc})
			}
			return svc
		}}

	cls.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*GRPCService).name)
	})
	cls.define("marshal=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*GRPCService).marshal = args[0]
		return args[0]
	})
	cls.define("unmarshal=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*GRPCService).unmarshal = args[0]
		return args[0]
	})
	cls.define("rpc", func(_ *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		if blk == nil {
			raise("ArgumentError", "rpc requires a handler block")
		}
		svc := self.(*GRPCService)
		svc.methods = append(svc.methods, grpcMethodDef{
			name:  args[0].ToS(),
			mtype: grpcType(args[1]),
			blk:   blk,
		})
		return self
	})
	// The four cardinality shorthands: s.request_response("Name") { … } etc.
	for _, t := range []string{"request_response", "client_streamer", "server_streamer", "bidi_streamer"} {
		typ := t
		cls.define(typ, func(_ *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			if blk == nil {
				raise("ArgumentError", "%s requires a handler block", typ)
			}
			svc := self.(*GRPCService)
			svc.methods = append(svc.methods, grpcMethodDef{
				name:  args[0].ToS(),
				mtype: grpcType(object.NewString(typ)),
				blk:   blk,
			})
			return self
		})
	}
}

// registerGRPCActiveCall installs GRPC::ActiveCall, the object a streaming
// handler drives: #remote_send emits a response, #remote_read reads the next
// request (nil at end of stream), #each_remote_read iterates the request stream,
// and #metadata / #deadline expose the call's request metadata and deadline.
func (vm *VM) registerGRPCActiveCall(cls *RClass) {
	self := func(v object.Value) *GRPCActiveCall { return v.(*GRPCActiveCall) }

	cls.define("remote_send", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).send(vm, grpcArgAt(args, 0))
		return object.NilV
	})
	cls.define("remote_read", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return self(v).read(vm)
	})
	cls.define("each_remote_read", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "each_remote_read requires a block")
		}
		c := self(v)
		for {
			msg := c.read(vm)
			if object.IsNil(msg) {
				return v
			}
			vm.callBlock(blk, []object.Value{msg})
		}
	})
	cls.define("metadata", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return grpcMetadataToHash(self(v).call.Metadata())
	})
	cls.define("deadline", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		t, ok := self(v).call.Deadline()
		if !ok {
			return object.NilV
		}
		return vm.grpcTimeValue(t)
	})
}
