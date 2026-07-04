// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"io"
	"strconv"
	"time"

	gotime "github.com/go-composites/time/src"
	libgrpc "github.com/go-ruby-grpc/grpc"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the seam between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-grpc/grpc library. The library owns
// the whole gRPC stack over google.golang.org/grpc; this binding supplies the
// Ruby handlers as a host seam and converts messages at the boundary. Messages
// cross the boundary as opaque bytes: the binding applies the Ruby marshal /
// unmarshal procs (a String codec by default) while it holds the GVL, and hands
// the library an identity codec, so no Ruby is ever run off the VM thread.
//
// Threading model (mirrors puma.go's Rack seam). rbgo runs bytecode under an
// emulated GVL (one Ruby thread at a time, see thread.go). The gRPC server
// invokes handlers from its own goroutines, so grpcServe acquires the GVL and
// installs a fresh execution context before entering Ruby — every handler is
// thereby serialized onto the VM. Conversely every blocking client call releases
// the GVL (via threadBlock) for the duration of the round trip, so that an
// in-process server handler can acquire it and run: that is what lets a single
// require "grpc" program act as both the server and the client over one bufconn.

// GRPCServer is the Ruby wrapper around a go-ruby-grpc RpcServer bound to the
// VM's shared in-memory transport.
type GRPCServer struct {
	srv *libgrpc.RpcServer
}

func (s *GRPCServer) ToS() string     { return "#<GRPC::RpcServer>" }
func (s *GRPCServer) Inspect() string { return "#<GRPC::RpcServer>" }
func (s *GRPCServer) Truthy() bool    { return true }

// GRPCStub is the Ruby wrapper around a go-ruby-grpc ClientStub dialed over the
// VM's shared in-memory transport.
type GRPCStub struct {
	lib *libgrpc.ClientStub
}

func (s *GRPCStub) ToS() string     { return "#<GRPC::ClientStub>" }
func (s *GRPCStub) Inspect() string { return "#<GRPC::ClientStub>" }
func (s *GRPCStub) Truthy() bool    { return true }

// GRPCService is the Ruby-defined service passed to RpcServer#handle: a name, its
// RPCs and the (optional) message codec procs (a String codec by default).
type GRPCService struct {
	name               string
	methods            []grpcMethodDef
	marshal, unmarshal object.Value
}

func (s *GRPCService) ToS() string     { return "#<GRPC::Service " + s.name + ">" }
func (s *GRPCService) Inspect() string { return s.ToS() }
func (s *GRPCService) Truthy() bool    { return true }

// grpcMethodDef is one RPC recorded on a GRPCService: its wire name, cardinality
// and the Ruby handler block.
type grpcMethodDef struct {
	name  string
	mtype libgrpc.MethodType
	blk   *Proc
}

// grpcCall is the subset of the library's *ActiveCall the binding drives. Naming
// it as an interface (which *libgrpc.ActiveCall satisfies) keeps the wrapper's
// transport-error paths unit-testable with an injected fake stream.
type grpcCall interface {
	Send(any) error
	Read() (any, error)
	Metadata() libgrpc.Metadata
	Deadline() (time.Time, bool)
}

// GRPCActiveCall is the Ruby wrapper around a library ActiveCall, the object a
// streaming handler drives. It carries the service codec procs so remote_send /
// remote_read speak in Ruby messages while the library moves bytes.
type GRPCActiveCall struct {
	call               grpcCall
	marshal, unmarshal object.Value
}

func (c *GRPCActiveCall) ToS() string     { return "#<GRPC::ActiveCall>" }
func (c *GRPCActiveCall) Inspect() string { return "#<GRPC::ActiveCall>" }
func (c *GRPCActiveCall) Truthy() bool    { return true }

// send marshals msg with the service codec and writes it to the call, raising
// the matching GRPC error on a transport failure.
func (c *GRPCActiveCall) send(vm *VM, msg object.Value) {
	b := vm.grpcMarshal(c.marshal, msg)
	if err := c.call.Send(b); err != nil {
		vm.raiseGRPCError(err)
	}
}

// read receives the next message and unmarshals it with the service codec,
// returning nil at end of stream (so a Ruby loop stops on nil) and raising on a
// transport error.
func (c *GRPCActiveCall) read(vm *VM) object.Value {
	msg, err := c.call.Read()
	if err == io.EOF {
		return object.NilV
	}
	if err != nil {
		vm.raiseGRPCError(err)
	}
	return vm.grpcUnmarshal(c.unmarshal, msg.([]byte))
}

// GRPCStatus is the Ruby wrapper around the code/details/metadata triple
// BadStatus#to_status returns (the gem's Struct::Status).
type GRPCStatus struct {
	code, details, metadata object.Value
}

func (s *GRPCStatus) ToS() string     { return "#<GRPC::Status>" }
func (s *GRPCStatus) Inspect() string { return "#<GRPC::Status>" }
func (s *GRPCStatus) Truthy() bool    { return true }

// registerGRPCServer installs GRPC::RpcServer over the VM's shared transport:
// new (optional keyword options, accepted for surface fidelity),
// add_http2_port(addr, creds), handle(service), run / run_till_terminated (start
// serving in the background and return, so the same thread can then drive a
// client), running? and stop (a graceful, leak-free teardown).
func (vm *VM) registerGRPCServer(cls *RClass, tr *libgrpc.MemTransport) {
	self := func(v object.Value) *GRPCServer { return v.(*GRPCServer) }

	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &GRPCServer{srv: libgrpc.NewRpcServer(libgrpc.WithTransport(tr))}
		}}

	cls.define("add_http2_port", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		creds := ":this_port_is_insecure"
		if len(args) >= 2 {
			creds = args[1].ToS()
		}
		return object.NewString(self(v).srv.AddHTTP2Port(args[0].ToS(), creds))
	})
	cls.define("handle", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		svc, ok := grpcArgAt(args, 0).(*GRPCService)
		if !ok {
			raise("TypeError", "handle expects a GRPC::Service, got %s", grpcArgAt(args, 0).Inspect())
		}
		self(v).srv.Handle(vm.grpcBuildService(svc))
		return v
	})
	run := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self(v)
		go func() { _ = s.srv.Run() }()
		grpcWaitRunning(s.srv)
		return v
	}
	cls.define("run", run)
	cls.define("run_till_terminated", run)
	cls.define("running?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).srv.Running())
	})
	cls.define("stop", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).srv.Stop()
		return v
	})
}

// grpcWaitRunning blocks until the server has bound its listener, so a client
// call issued right after run does not race the listener registration. It gives
// up after a bounded spin (the server never becomes running only if a bind
// failed, which run itself does not surface asynchronously).
func grpcWaitRunning(srv *libgrpc.RpcServer) {
	for i := 0; i < 400; i++ {
		if srv.Running() {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// grpcBuildService converts a Ruby GRPCService into the library's Service. Every
// method is given identity byte codecs; the real (un)marshalling happens in the
// handler under the GVL (grpcServe), so the library never runs Ruby off-thread.
func (vm *VM) grpcBuildService(svc *GRPCService) libgrpc.Service {
	out := libgrpc.Service{Name: svc.name}
	for _, m := range svc.methods {
		out.Methods = append(out.Methods, libgrpc.Method{
			Name:                m.name,
			Type:                m.mtype,
			RequestUnmarshal:    grpcIdentityUnmarshal,
			ResponseMarshal:     grpcIdentityMarshal,
			UnaryHandler:        vm.grpcUnaryHandler(svc, m.blk),
			ClientStreamHandler: vm.grpcClientStreamHandler(svc, m.blk),
			ServerStreamHandler: vm.grpcServerStreamHandler(svc, m.blk),
			BidiStreamHandler:   vm.grpcBidiStreamHandler(svc, m.blk),
		})
	}
	return out
}

// grpcUnaryHandler builds the Go unary handler: it unmarshals the request,
// invokes the Ruby block with (req, call) under the GVL and marshals the result.
func (vm *VM) grpcUnaryHandler(svc *GRPCService, blk *Proc) func(any, *libgrpc.ActiveCall) (any, error) {
	return func(reqAny any, call *libgrpc.ActiveCall) (any, error) {
		var out []byte
		err := vm.grpcServe(func() {
			req := vm.grpcUnmarshal(svc.unmarshal, reqAny.([]byte))
			ac := &GRPCActiveCall{call: call, marshal: svc.marshal, unmarshal: svc.unmarshal}
			resp := vm.callBlock(blk, []object.Value{req, ac})
			out = vm.grpcMarshal(svc.marshal, resp)
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

// grpcClientStreamHandler builds the Go client-stream handler: the Ruby block
// reads the request stream off the call and returns the single response.
func (vm *VM) grpcClientStreamHandler(svc *GRPCService, blk *Proc) func(*libgrpc.ActiveCall) (any, error) {
	return func(call *libgrpc.ActiveCall) (any, error) {
		var out []byte
		err := vm.grpcServe(func() {
			ac := &GRPCActiveCall{call: call, marshal: svc.marshal, unmarshal: svc.unmarshal}
			resp := vm.callBlock(blk, []object.Value{ac})
			out = vm.grpcMarshal(svc.marshal, resp)
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

// grpcServerStreamHandler builds the Go server-stream handler: the Ruby block
// receives the single request and emits responses with call.remote_send.
func (vm *VM) grpcServerStreamHandler(svc *GRPCService, blk *Proc) func(any, *libgrpc.ActiveCall) error {
	return func(reqAny any, call *libgrpc.ActiveCall) error {
		return vm.grpcServe(func() {
			req := vm.grpcUnmarshal(svc.unmarshal, reqAny.([]byte))
			ac := &GRPCActiveCall{call: call, marshal: svc.marshal, unmarshal: svc.unmarshal}
			vm.callBlock(blk, []object.Value{req, ac})
		})
	}
}

// grpcBidiStreamHandler builds the Go bidi handler: the Ruby block reads and
// emits over the same call.
func (vm *VM) grpcBidiStreamHandler(svc *GRPCService, blk *Proc) func(*libgrpc.ActiveCall) error {
	return func(call *libgrpc.ActiveCall) error {
		return vm.grpcServe(func() {
			ac := &GRPCActiveCall{call: call, marshal: svc.marshal, unmarshal: svc.unmarshal}
			vm.callBlock(blk, []object.Value{ac})
		})
	}
}

// grpcServe runs fn (the Ruby handler body) on the VM under the GVL, from a gRPC
// server goroutine, serializing it onto the single-threaded VM exactly as
// pumaServe does. A Ruby raise inside fn is recovered and mapped to the error the
// gRPC runtime carries to the client: a GRPC::BadStatus keeps its code, any other
// raise becomes an UNKNOWN status.
func (vm *VM) grpcServe(fn func()) (retErr error) {
	vm.gvl.Lock()
	prev := vm.currentThread
	prev.saveCtx(vm)
	thr := &RThread{status: "run", done: make(chan struct{}), locals: map[object.Value]object.Value{}, parked: true}
	thr.restoreCtx(vm)
	defer func() {
		r := recover()
		prev.restoreCtx(vm)
		vm.gvl.Unlock()
		if r != nil {
			retErr = grpcErrorFromRecover(r)
		}
	}()
	fn()
	return nil
}

// grpcErrorFromRecover maps a recovered Ruby raise to the error a handler
// returns to the gRPC runtime. A GRPC::BadStatus (or subclass) keeps its exact
// status code and details; any other raise becomes a plain error, which the
// library reports as an UNKNOWN status.
func grpcErrorFromRecover(r any) error {
	re, ok := r.(RubyError)
	if !ok {
		panic(r) // not a Ruby raise (a real Go panic): propagate.
	}
	if code, ok := grpcBadStatusCode(re.Obj); ok {
		return libgrpc.NewBadStatus(code, grpcDetailsOf(re.Obj), nil)
	}
	return errors.New(re.Message)
}

// grpcBadStatusCode returns the status code carried by a raised GRPC::BadStatus
// exception object, and whether the object is one.
func grpcBadStatusCode(obj object.Value) (libgrpc.StatusCode, bool) {
	o, ok := obj.(*RObject)
	if !ok {
		return 0, false
	}
	c, ok := o.ivars["@code"]
	if !ok {
		return 0, false
	}
	n, ok := c.(object.Integer)
	if !ok {
		return 0, false
	}
	return libgrpc.StatusCode(n), true
}

// grpcDetailsOf reads the @details string of a raised BadStatus, or "" when
// absent.
func grpcDetailsOf(obj object.Value) string {
	if o, ok := obj.(*RObject); ok {
		if d, ok := o.ivars["@details"]; ok {
			return d.ToS()
		}
	}
	return ""
}

// registerGRPCClientStub installs GRPC::ClientStub over the VM's shared
// transport: new(host, creds, timeout:) dials, request_response / client_streamer
// / server_streamer / bidi_streamer issue the four call shapes (each releasing
// the GVL for the round trip), and close releases the connection.
func (vm *VM) registerGRPCClientStub(cls *RClass, tr *libgrpc.MemTransport) {
	self := func(v object.Value) *GRPCStub { return v.(*GRPCStub) }

	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			host := args[0].ToS()
			creds := ":this_channel_is_insecure"
			if len(args) >= 2 {
				creds = args[1].ToS()
			}
			opts := []libgrpc.StubOption{libgrpc.WithStubTransport(tr)}
			if d, ok := grpcTimeoutOpt(args); ok {
				opts = append(opts, libgrpc.WithTimeout(d))
			}
			lib, err := libgrpc.NewClientStub(host, creds, opts...)
			if err != nil {
				vm.raiseGRPCError(err)
			}
			return &GRPCStub{lib: lib}
		}}

	cls.define("request_response", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		method, req, opts := vm.grpcCallArgs(args, true)
		reqBytes := vm.grpcMarshal(opts.marshal, req)
		var reply any
		var callErr error
		vm.threadBlock(func() {
			reply, callErr = self(v).lib.RequestResponse(method, reqBytes, opts.call)
		})
		if callErr != nil {
			vm.raiseGRPCError(callErr)
		}
		return vm.grpcUnmarshal(opts.unmarshal, reply.([]byte))
	})
	cls.define("client_streamer", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		method, reqs, opts := vm.grpcStreamArgs(args)
		payload := vm.grpcMarshalEach(opts.marshal, reqs)
		var reply any
		var callErr error
		vm.threadBlock(func() {
			reply, callErr = self(v).lib.ClientStreamer(method, payload, opts.call)
		})
		if callErr != nil {
			vm.raiseGRPCError(callErr)
		}
		return vm.grpcUnmarshal(opts.unmarshal, reply.([]byte))
	})
	cls.define("server_streamer", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		method, req, opts := vm.grpcCallArgs(args, true)
		reqBytes := vm.grpcMarshal(opts.marshal, req)
		var replies []any
		var callErr error
		vm.threadBlock(func() {
			replies, callErr = self(v).lib.ServerStreamer(method, reqBytes, opts.call)
		})
		if callErr != nil {
			vm.raiseGRPCError(callErr)
		}
		return vm.grpcUnmarshalEach(opts.unmarshal, replies)
	})
	cls.define("bidi_streamer", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		method, reqs, opts := vm.grpcStreamArgs(args)
		payload := vm.grpcMarshalEach(opts.marshal, reqs)
		var replies []any
		var callErr error
		vm.threadBlock(func() {
			replies, callErr = self(v).lib.BidiStreamer(method, payload, opts.call)
		})
		if callErr != nil {
			vm.raiseGRPCError(callErr)
		}
		return vm.grpcUnmarshalEach(opts.unmarshal, replies)
	})
	cls.define("close", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		_ = self(v).lib.Close()
		return object.NilV
	})
}

// grpcCallOpts collects a client call's resolved codec procs and the library
// CallOptions (identity codecs plus metadata and deadline).
type grpcCallOpts struct {
	marshal, unmarshal object.Value
	call               libgrpc.CallOptions
}

// grpcCallArgs parses a unary/server-stream call: (method, req, opts_hash?),
// returning the method, the request and the resolved options.
func (vm *VM) grpcCallArgs(args []object.Value, needReq bool) (string, object.Value, grpcCallOpts) {
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
	}
	return args[0].ToS(), args[1], vm.grpcResolveOpts(grpcHashAt(args, 2))
}

// grpcStreamArgs parses a client/bidi-stream call: (method, requests_array,
// opts_hash?), returning the method, the request elements and the options.
func (vm *VM) grpcStreamArgs(args []object.Value) (string, []object.Value, grpcCallOpts) {
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
	}
	arr, ok := args[1].(*object.Array)
	if !ok {
		raise("TypeError", "streaming requests must be an Array, got %s", args[1].Inspect())
	}
	return args[0].ToS(), arr.Elems, vm.grpcResolveOpts(grpcHashAt(args, 2))
}

// grpcResolveOpts turns a per-call options Hash (marshal:/unmarshal:/metadata:/
// deadline:) into the codec procs and the library CallOptions carrying identity
// byte codecs, the metadata and the deadline.
func (vm *VM) grpcResolveOpts(h *object.Hash) grpcCallOpts {
	opts := grpcCallOpts{marshal: object.NilV, unmarshal: object.NilV}
	opts.call = libgrpc.CallOptions{Marshal: grpcIdentityMarshal, Unmarshal: grpcIdentityUnmarshal}
	if h == nil {
		return opts
	}
	if v, ok := h.Get(object.Symbol("marshal")); ok {
		opts.marshal = v
	}
	if v, ok := h.Get(object.Symbol("unmarshal")); ok {
		opts.unmarshal = v
	}
	if v, ok := h.Get(object.Symbol("metadata")); ok {
		if md, ok := v.(*object.Hash); ok {
			opts.call.Metadata = grpcHashToMetadata(md)
		}
	}
	if v, ok := h.Get(object.Symbol("deadline")); ok && !object.IsNil(v) {
		opts.call.Deadline = time.Now().Add(grpcDurationOf(v))
	}
	return opts
}

// grpcTimeoutOpt reads the trailing timeout: keyword (seconds) of ClientStub.new.
func grpcTimeoutOpt(args []object.Value) (time.Duration, bool) {
	h := grpcHashAt(args, 2)
	if h == nil {
		return 0, false
	}
	if v, ok := h.Get(object.Symbol("timeout")); ok && !object.IsNil(v) {
		return grpcDurationOf(v), true
	}
	return 0, false
}

// grpcMarshal serializes a Ruby message to bytes: with the given proc when set,
// else via the default String codec (the message must be a String).
func (vm *VM) grpcMarshal(proc, v object.Value) []byte {
	out := v
	if !object.IsNil(proc) {
		out = vm.send(proc, "call", []object.Value{v}, nil)
	}
	s, ok := out.(*object.String)
	if !ok {
		raise("TypeError", "grpc: marshal must yield a String, got %s", out.Inspect())
	}
	return s.Bytes()
}

// grpcUnmarshal deserializes bytes into a Ruby message: via the proc when set,
// else as a String (the default codec).
func (vm *VM) grpcUnmarshal(proc object.Value, b []byte) object.Value {
	s := object.NewStringBytes(append([]byte(nil), b...))
	if object.IsNil(proc) {
		return s
	}
	return vm.send(proc, "call", []object.Value{s}, nil)
}

// grpcMarshalEach marshals every element of a request slice to bytes.
func (vm *VM) grpcMarshalEach(proc object.Value, msgs []object.Value) []any {
	out := make([]any, len(msgs))
	for i, m := range msgs {
		out[i] = vm.grpcMarshal(proc, m)
	}
	return out
}

// grpcUnmarshalEach unmarshals every response byte slice into a Ruby Array.
func (vm *VM) grpcUnmarshalEach(proc object.Value, msgs []any) object.Value {
	out := make([]object.Value, len(msgs))
	for i, m := range msgs {
		out[i] = vm.grpcUnmarshal(proc, m.([]byte))
	}
	return object.NewArrayFromSlice(out)
}

// grpcIdentityMarshal is the byte-identity codec handed to the library: the
// binding has already produced the wire bytes under the GVL.
func grpcIdentityMarshal(v any) ([]byte, error) { return v.([]byte), nil }

// grpcIdentityUnmarshal is the byte-identity decode handed to the library.
func grpcIdentityUnmarshal(b []byte) (any, error) { return b, nil }

// raiseGRPCError re-raises a library error as its matching Ruby exception: a
// *BadStatus becomes the per-code GRPC:: subclass (carrying code/details/
// metadata), a *CallError becomes GRPC::Core::CallError, and anything else a
// bare GRPC::Error.
func (vm *VM) raiseGRPCError(err error) {
	var bs *libgrpc.BadStatus
	if errors.As(err, &bs) {
		cls := vm.grpcErrorClass(bs.Code)
		exc := grpcNewBadStatus(cls, bs.Code, object.NewString(bs.Details), grpcMetadataToHash(bs.Metadata))
		panic(vm.excError(exc))
	}
	var ce *libgrpc.CallError
	if errors.As(err, &ce) {
		exc := &RObject{class: vm.consts["GRPC::Core::CallError"].(*RClass), ivars: map[string]object.Value{}}
		exc.ivars["@message"] = object.NewString(ce.Message)
		panic(vm.excError(exc))
	}
	exc := &RObject{class: vm.consts["GRPC::Error"].(*RClass), ivars: map[string]object.Value{}}
	exc.ivars["@message"] = object.NewString(err.Error())
	panic(vm.excError(exc))
}

// grpcErrorClass returns the GRPC:: exception class for a status code, falling
// back to GRPC::BadStatus for an unknown numeric code.
func (vm *VM) grpcErrorClass(code libgrpc.StatusCode) *RClass {
	for _, c := range grpcCodes {
		if c.code == code {
			return vm.consts["GRPC::"+c.camel].(*RClass)
		}
	}
	return vm.consts["GRPC::BadStatus"].(*RClass)
}

// grpcNewBadStatus builds a BadStatus exception object of class cls carrying the
// code, details and metadata, plus the gem's "<code>:<details>" #message.
func grpcNewBadStatus(cls *RClass, code libgrpc.StatusCode, details, metadata object.Value) object.Value {
	o := &RObject{class: cls, ivars: map[string]object.Value{}}
	o.ivars["@code"] = object.IntValue(int64(code))
	o.ivars["@details"] = grpcOrString(details, "")
	o.ivars["@metadata"] = grpcOrHash(metadata)
	o.ivars["@message"] = object.NewString(strconv.FormatInt(int64(code), 10) + ":" + o.ivars["@details"].ToS())
	return o
}

// grpcBuildService-side helpers ------------------------------------------------

// grpcMetadataToHash renders library Metadata as a Ruby Hash of String to String.
func grpcMetadataToHash(md libgrpc.Metadata) *object.Hash {
	h := object.NewHash()
	for _, k := range md.Keys() {
		h.Set(object.NewString(k), object.NewString(md[k]))
	}
	return h
}

// grpcHashToMetadata reads a Ruby Hash into library Metadata (String to String).
func grpcHashToMetadata(h *object.Hash) libgrpc.Metadata {
	md := libgrpc.Metadata{}
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		md[k.ToS()] = v.ToS()
	}
	return md
}

// grpcTimeValue wraps a Go instant as a Ruby Time (whole seconds).
func (vm *VM) grpcTimeValue(t time.Time) object.Value {
	return &Time{t: gotime.FromUnix(t.Unix())}
}

// grpcDurationOf reads a Ruby Integer/Float number of seconds as a Duration.
func grpcDurationOf(v object.Value) time.Duration {
	switch n := v.(type) {
	case object.Integer:
		return time.Duration(int64(n)) * time.Second
	case object.Float:
		return time.Duration(float64(n) * float64(time.Second))
	}
	raise("TypeError", "grpc: seconds must be a number, got %s", v.Inspect())
	return 0
}

// grpcInt coerces a Ruby Integer argument to an int (for a status code).
func grpcInt(v object.Value) int {
	if n, ok := v.(object.Integer); ok {
		return int(n)
	}
	raise("TypeError", "expected an Integer, got %s", v.Inspect())
	return 0
}

// grpcName coerces a Symbol or String to its Go string.
func grpcName(v object.Value) string {
	if s, ok := v.(object.Symbol); ok {
		return string(s)
	}
	return v.ToS()
}

// grpcArgAt returns args[i] or nil (a Ruby nil) when the argument was omitted.
func grpcArgAt(args []object.Value, i int) object.Value {
	if i < len(args) {
		return args[i]
	}
	return object.NilV
}

// grpcHashAt returns args[i] as a Hash, or nil when it is absent or not a Hash
// (so a trailing options Hash is optional).
func grpcHashAt(args []object.Value, i int) *object.Hash {
	if i < len(args) {
		if h, ok := args[i].(*object.Hash); ok {
			return h
		}
	}
	return nil
}

// grpcOrString returns v when it is a non-nil value, else a String of def.
func grpcOrString(v object.Value, def string) object.Value {
	if object.IsNil(v) {
		return object.NewString(def)
	}
	return object.NewString(v.ToS())
}

// grpcOrHash returns v when it is a Hash, else a fresh empty Hash.
func grpcOrHash(v object.Value) object.Value {
	if h, ok := v.(*object.Hash); ok {
		return h
	}
	return object.NewHash()
}
