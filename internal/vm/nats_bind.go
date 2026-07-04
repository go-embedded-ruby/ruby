// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"net"
	"time"

	nats "github.com/go-ruby-nats/nats"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the binding between rbgo's Ruby object graph (object.Value) and
// the interpreter-independent github.com/go-ruby-nats/nats library — a pure-Go
// (CGO=0) port of the Ruby nats client's surface over the official nats.go
// transport. The library owns the whole messaging client (connection, pub/sub,
// request/reply, the drain/close lifecycle and the error tree); this file wraps
// each library object as a Ruby object reporting the matching NATS::* class (see
// nats.go for the class + method registration) and converts values across the
// boundary.
//
// Threading model: the library delivers subscription messages to a callback from
// a nats.go dispatcher goroutine. rbgo runs bytecode under an emulated GVL (one
// Ruby thread at a time, see thread.go), so natsDeliver acquires the GVL and
// installs a fresh execution context before entering the Ruby block — every
// delivery is thereby serialized onto the VM, exactly like puma's Rack seam. So a
// delivery can actually run, every blocking client call (request/flush/drain)
// releases the GVL through vm.threadBlock while it waits, which is both how a
// responder answers a request in the same VM and how a test deterministically
// waits (via a Thread::Queue) for an async delivery without a fixed sleep.

// NATSX is the single Ruby wrapper for every value the nats library hands across
// the boundary: a connection (NATS::Client, aliased NATS::Connection), a
// subscription (NATS::Subscription) or a message (NATS::Msg). cls is the Ruby
// class it reports (see classOf); exactly one of conn/sub/msg is set, selected by
// cls, and the registered methods (see nats.go) operate on that held value.
type NATSX struct {
	cls  *RClass
	vm   *VM
	conn *nats.Client
	sub  *nats.Subscription
	msg  *nats.Msg
}

func (x *NATSX) ToS() string     { return "#<" + x.cls.name + ">" }
func (x *NATSX) Inspect() string { return "#<" + x.cls.name + ">" }
func (x *NATSX) Truthy() bool    { return true }

// newNATSClient wraps a connected *nats.Client as a NATS::Client.
func (vm *VM) newNATSClient(c *nats.Client) *NATSX {
	return &NATSX{cls: vm.consts["NATS::Client"].(*RClass), vm: vm, conn: c}
}

// newNATSSub wraps a *nats.Subscription as a NATS::Subscription.
func (vm *VM) newNATSSub(s *nats.Subscription) *NATSX {
	return &NATSX{cls: vm.consts["NATS::Subscription"].(*RClass), vm: vm, sub: s}
}

// newNATSMsg wraps a *nats.Msg as a NATS::Msg.
func (vm *VM) newNATSMsg(m *nats.Msg) *NATSX {
	return &NATSX{cls: vm.consts["NATS::Msg"].(*RClass), vm: vm, msg: m}
}

// natsDeliver runs a subscriber block for one delivered message. It mirrors
// puma's Rack seam: called from a nats.go dispatcher goroutine, it acquires the
// GVL and installs a fresh thread context so the block runs serialized onto the
// VM, then restores the previous thread and releases the GVL on return. A raise
// in the block is an asynchronous error that must not crash the dispatcher, so it
// is recovered and dropped — the connection stays up and later messages still
// deliver.
func (vm *VM) natsDeliver(blk *Proc, m *nats.Msg) {
	vm.gvl.Lock()
	prev := vm.currentThread
	prev.saveCtx(vm)
	thr := &RThread{status: "run", done: make(chan struct{}), locals: map[object.Value]object.Value{}, parked: true}
	thr.restoreCtx(vm)
	defer func() {
		_ = recover()
		prev.restoreCtx(vm)
		vm.gvl.Unlock()
	}()
	vm.callBlock(blk, []object.Value{vm.newNATSMsg(m)})
}

// natsErrClass maps a library sentinel error to the Ruby exception class it
// raises, mirroring the gem's NATS::IO::* tree. mapError guarantees the library
// only returns one of these (or an unrecognised transport error), so the lookup
// is a single map probe; an unmatched error falls back to NATS::Error.
var natsErrClass = map[error]string{
	nats.ErrTimeout:                "NATS::Timeout",
	nats.ErrNoResponders:           "NATS::IO::NoRespondersError",
	nats.ErrConnectionClosed:       "NATS::IO::ConnectionClosedError",
	nats.ErrConnectionDraining:     "NATS::IO::ConnectionDrainingError",
	nats.ErrConnectionReconnecting: "NATS::IO::ConnectionReconnectingError",
	nats.ErrBadSubscription:        "NATS::IO::BadSubscription",
	nats.ErrBadSubject:             "NATS::IO::BadSubject",
	nats.ErrNoServers:              "NATS::IO::NoServersError",
	nats.ErrMaxPayload:             "NATS::IO::MaxPayloadError",
}

// raiseNATS re-raises a library error as its matching Ruby exception. A known
// sentinel maps through natsErrClass; anything else raises the root NATS::Error.
func raiseNATS(err error) {
	cls := "NATS::Error"
	for e, name := range natsErrClass {
		if errors.Is(err, e) {
			cls = name
			break
		}
	}
	raise(cls, "%s", err.Error())
}

// raiseNATSConnect re-raises a failed NATS.connect. It maps the known sentinels
// through natsErrClass like raiseNATS, but additionally classifies a bare
// network/dial failure — one carrying no nats sentinel — as
// NATS::IO::NoServersError, i.e. "couldn't reach any server". This keeps connect
// errors OS-independent: on Unix nats.go wraps a refused dial as nats.ErrNoServers,
// but on Windows it surfaces the raw net error (a *net.OpError / net.Error) that
// does not satisfy errors.Is(err, nats.ErrNoServers).
func raiseNATSConnect(err error) {
	for e, name := range natsErrClass {
		if errors.Is(err, e) {
			raise(name, "%s", err.Error())
		}
	}
	var ne net.Error
	if errors.As(err, &ne) {
		raise("NATS::IO::NoServersError", "%s", err.Error())
	}
	raise("NATS::Error", "%s", err.Error())
}

// natsBytes coerces a Ruby payload argument to the raw message bytes: a String
// contributes its bytes verbatim, nil is an empty payload, and any other value is
// stringified (matching the gem coercing a non-String payload with #to_s).
func natsBytes(v object.Value) []byte {
	if v == nil || object.IsNil(v) {
		return nil
	}
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	return []byte(v.ToS())
}

// natsStr coerces a Ruby argument (subject / queue / reply) to a plain string.
func natsStr(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// natsSeconds reads a Ruby duration argument in seconds (Integer or Float) as a
// time.Duration, used for the request/flush timeouts the gem expresses in
// seconds.
func natsSeconds(v object.Value) time.Duration {
	var secs float64
	switch n := v.(type) {
	case object.Integer:
		secs = float64(n)
	case object.Float:
		secs = float64(n)
	default:
		secs = 0
	}
	return time.Duration(secs * float64(time.Second))
}

// natsKwarg returns the value of keyword key in the call's trailing keyword Hash
// (the last argument when it is a Hash), or nil when the key is absent. Keys are
// matched as Symbols, as Ruby keyword arguments arrive.
func natsKwarg(args []object.Value, key string) object.Value {
	if len(args) == 0 {
		return nil
	}
	h, ok := args[len(args)-1].(*object.Hash)
	if !ok {
		return nil
	}
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v
	}
	return nil
}

// natsHeaderHash renders a library message Header as a Ruby Hash of
// String→String (the first value per key), mirroring NATS::Msg#header.
func natsHeaderHash(h nats.Header) object.Value {
	rh := object.NewHash()
	for k, vals := range h {
		if len(vals) > 0 {
			rh.Set(object.NewString(k), object.NewString(vals[0]))
		}
	}
	return rh
}

// natsBuildHeader builds a library Header from a Ruby Hash (String/Symbol keys to
// #to_s values), used when a message is constructed for publish_msg / respond.
func natsBuildHeader(v object.Value) nats.Header {
	h, ok := v.(*object.Hash)
	if !ok {
		return nil
	}
	out := nats.Header{}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		out.Set(natsStr(k), natsStr(val))
	}
	return out
}
