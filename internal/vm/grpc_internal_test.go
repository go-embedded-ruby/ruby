// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	libgrpc "github.com/go-ruby-grpc/grpc"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// fakeGRPCCall is an injected grpcCall used to drive a GRPCActiveCall's
// transport-error paths deterministically, without a live stream.
type fakeGRPCCall struct {
	readVal any
	readErr error
	sendErr error
	md      libgrpc.Metadata
	dl      time.Time
	hasDL   bool
}

func (f *fakeGRPCCall) Send(any) error              { return f.sendErr }
func (f *fakeGRPCCall) Read() (any, error)          { return f.readVal, f.readErr }
func (f *fakeGRPCCall) Metadata() libgrpc.Metadata  { return f.md }
func (f *fakeGRPCCall) Deadline() (time.Time, bool) { return f.dl, f.hasDL }

// grpcRecover asserts fn raises a Ruby exception of the given class.
func grpcRecover(t *testing.T, wantClass string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected raise %s, got none", wantClass)
		}
		re, ok := r.(RubyError)
		if !ok {
			t.Fatalf("expected a RubyError, got %#v", r)
		}
		if re.Class != wantClass {
			t.Fatalf("raised %s, want %s", re.Class, wantClass)
		}
	}()
	fn()
}

// TestGRPCWrapperStrings covers the object.Value ToS/Inspect/Truthy protocol on
// every GRPC wrapper type.
func TestGRPCWrapperStrings(t *testing.T) {
	for _, w := range []object.Value{
		&GRPCServer{}, &GRPCStub{}, &GRPCService{name: "n"},
		&GRPCActiveCall{}, &GRPCStatus{},
	} {
		if w.ToS() == "" || w.Inspect() == "" || !w.Truthy() {
			t.Errorf("%T: unexpected ToS/Inspect/Truthy", w)
		}
	}
}

// TestGRPCIdentityCodec covers the byte-identity codecs handed to the library.
func TestGRPCIdentityCodec(t *testing.T) {
	b, err := grpcIdentityMarshal([]byte("hi"))
	if err != nil || string(b) != "hi" {
		t.Errorf("grpcIdentityMarshal = %q, %v", b, err)
	}
	v, err := grpcIdentityUnmarshal([]byte("yo"))
	if err != nil || string(v.([]byte)) != "yo" {
		t.Errorf("grpcIdentityUnmarshal = %v, %v", v, err)
	}
}

// TestGRPCActiveCallTransportErrors covers the send / read error and end-of-stream
// paths through an injected fake stream.
func TestGRPCActiveCallTransportErrors(t *testing.T) {
	vm := New(&bytes.Buffer{})

	// A Send transport failure raises the mapped GRPC exception.
	sendFail := &GRPCActiveCall{call: &fakeGRPCCall{sendErr: libgrpc.NewBadStatus(libgrpc.Unavailable, "down", nil)}, marshal: object.NilV, unmarshal: object.NilV}
	grpcRecover(t, "GRPC::Unavailable", func() { sendFail.send(vm, object.NewString("x")) })

	// A successful Send does not raise.
	sendOK := &GRPCActiveCall{call: &fakeGRPCCall{}, marshal: object.NilV, unmarshal: object.NilV}
	sendOK.send(vm, object.NewString("x"))

	// A Read transport failure (non-EOF) raises the mapped GRPC exception.
	readFail := &GRPCActiveCall{call: &fakeGRPCCall{readErr: libgrpc.NewBadStatus(libgrpc.Internal, "boom", nil)}, marshal: object.NilV, unmarshal: object.NilV}
	grpcRecover(t, "GRPC::Internal", func() { readFail.read(vm) })

	// Read at end of stream returns nil.
	readEOF := &GRPCActiveCall{call: &fakeGRPCCall{readErr: io.EOF}, marshal: object.NilV, unmarshal: object.NilV}
	if v := readEOF.read(vm); !object.IsNil(v) {
		t.Errorf("read at EOF = %v, want nil", v)
	}

	// A successful Read unmarshals the message (default String codec).
	readOK := &GRPCActiveCall{call: &fakeGRPCCall{readVal: []byte("hi")}, marshal: object.NilV, unmarshal: object.NilV}
	if v := readOK.read(vm); v.ToS() != "hi" {
		t.Errorf("read = %q, want hi", v.ToS())
	}
}

// TestRaiseGRPCError covers the three error shapes raiseGRPCError maps: a
// BadStatus (per-code subclass), a Core::CallError, and a bare error (GRPC::Error).
func TestRaiseGRPCError(t *testing.T) {
	vm := New(&bytes.Buffer{})
	grpcRecover(t, "GRPC::NotFound", func() {
		vm.raiseGRPCError(libgrpc.NewBadStatus(libgrpc.NotFound, "nf", libgrpc.Metadata{"k": "v"}))
	})
	grpcRecover(t, "GRPC::Core::CallError", func() {
		vm.raiseGRPCError(libgrpc.NewCallError("call-machinery"))
	})
	grpcRecover(t, "GRPC::Error", func() {
		vm.raiseGRPCError(errors.New("plain"))
	})
}

// TestGRPCErrorClass covers the code->class lookup, including the fallback to
// GRPC::BadStatus for a code outside the canonical table.
func TestGRPCErrorClass(t *testing.T) {
	vm := New(&bytes.Buffer{})
	if got := vm.grpcErrorClass(libgrpc.InvalidArgument); got != vm.consts["GRPC::InvalidArgument"] {
		t.Errorf("grpcErrorClass(InvalidArgument) = %v", got.name)
	}
	if got := vm.grpcErrorClass(libgrpc.StatusCode(999)); got != vm.consts["GRPC::BadStatus"] {
		t.Errorf("grpcErrorClass(999) = %v, want GRPC::BadStatus", got.name)
	}
}

// TestGRPCErrorFromRecover covers the handler-raise -> gRPC-error mapping: a
// raised BadStatus keeps its code, any other Ruby raise becomes a plain error,
// and a genuine (non-Ruby) Go panic is re-propagated.
func TestGRPCErrorFromRecover(t *testing.T) {
	// A raised GRPC::BadStatus maps to a *BadStatus carrying its code and details.
	obj := &RObject{ivars: map[string]object.Value{
		"@code":    object.IntValue(int64(libgrpc.PermissionDenied)),
		"@details": object.NewString("denied"),
	}}
	err := grpcErrorFromRecover(RubyError{Class: "GRPC::PermissionDenied", Message: "7:denied", Obj: obj})
	var bs *libgrpc.BadStatus
	if !errors.As(err, &bs) || bs.Code != libgrpc.PermissionDenied || bs.Details != "denied" {
		t.Fatalf("grpcErrorFromRecover(BadStatus) = %v", err)
	}

	// A non-BadStatus Ruby raise becomes a plain error with the message.
	err = grpcErrorFromRecover(RubyError{Class: "RuntimeError", Message: "oops"})
	if err == nil || err.Error() != "oops" {
		t.Fatalf("grpcErrorFromRecover(RuntimeError) = %v", err)
	}

	// A genuine Go panic (not a Ruby raise) is re-propagated unchanged.
	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("expected re-panic \"boom\", got %v", r)
		}
	}()
	_ = grpcErrorFromRecover("boom")
}

// TestGRPCBadStatusCode covers grpcBadStatusCode and grpcDetailsOf across their
// non-object / missing-ivar / wrong-type branches.
func TestGRPCBadStatusCode(t *testing.T) {
	// Not an RObject.
	if _, ok := grpcBadStatusCode(object.NewString("x")); ok {
		t.Errorf("grpcBadStatusCode(String) reported ok")
	}
	// RObject without @code.
	if _, ok := grpcBadStatusCode(&RObject{ivars: map[string]object.Value{}}); ok {
		t.Errorf("grpcBadStatusCode(no @code) reported ok")
	}
	// RObject with a non-Integer @code.
	if _, ok := grpcBadStatusCode(&RObject{ivars: map[string]object.Value{"@code": object.NewString("x")}}); ok {
		t.Errorf("grpcBadStatusCode(non-Integer @code) reported ok")
	}
	// RObject with a valid @code.
	c, ok := grpcBadStatusCode(&RObject{ivars: map[string]object.Value{"@code": object.IntValue(3)}})
	if !ok || c != libgrpc.InvalidArgument {
		t.Errorf("grpcBadStatusCode(3) = %v, %v", c, ok)
	}

	// grpcDetailsOf: present and absent.
	if d := grpcDetailsOf(&RObject{ivars: map[string]object.Value{"@details": object.NewString("d")}}); d != "d" {
		t.Errorf("grpcDetailsOf(present) = %q", d)
	}
	if d := grpcDetailsOf(object.NewString("x")); d != "" {
		t.Errorf("grpcDetailsOf(non-object) = %q, want empty", d)
	}
}

// TestGRPCConversions covers the metadata <-> Hash conversions, the deadline
// Time wrapper and the default-codec String marshal error.
func TestGRPCConversions(t *testing.T) {
	vm := New(&bytes.Buffer{})

	h := grpcMetadataToHash(libgrpc.Metadata{"a": "1", "b": "2"})
	if v, _ := h.Get(object.NewString("a")); v.ToS() != "1" {
		t.Errorf("grpcMetadataToHash a = %v", v)
	}

	rh := object.NewHash()
	rh.Set(object.NewString("x"), object.NewString("y"))
	if md := grpcHashToMetadata(rh); md["x"] != "y" {
		t.Errorf("grpcHashToMetadata = %v", md)
	}

	if tv := vm.grpcTimeValue(time.Unix(1000, 0)); tv == nil || !tv.Truthy() {
		t.Errorf("grpcTimeValue returned %v", tv)
	}

	// The default codec rejects a non-String message.
	grpcRecover(t, "TypeError", func() { vm.grpcMarshal(object.NilV, object.IntValue(5)) })
}
