// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	nats "github.com/go-ruby-nats/nats"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerNATS installs the NATS module (require "nats"): the messaging client
// modelled on the Ruby nats / nats-pure gem, reimplemented in pure Go (CGO=0) by
// github.com/go-ruby-nats/nats over the official nats.go transport. The library
// owns the whole client — connection, publish/subscribe, request/reply, the
// drain/close lifecycle and the error tree — and this file is the thin shell that
// maps that surface onto rbgo classes:
//
//	NATS.connect(url = nil, **opts) { |nc| }  — open a connection
//	NATS::Client (aka NATS::Connection)        — #publish / #subscribe { |msg| } /
//	                                             #request / #flush / #drain /
//	                                             #close / #closed?
//	NATS::Subscription                         — #unsubscribe / #drain / #subject
//	NATS::Msg                                  — #subject / #data / #reply /
//	                                             #headers / #respond
//	NATS::Error (< StandardError) / NATS::Timeout / NATS::IO::* — the exception tree
//
// The message-delivery seam — running a subscriber block for a message arriving
// on a nats.go dispatcher goroutine — lives in nats_bind.go, which serializes
// every delivery onto the single-threaded VM under the GVL.
func (vm *VM) registerNATS() {
	mod := newClass("NATS", nil)
	mod.isModule = true
	vm.consts["NATS"] = mod

	vm.registerNATSErrors(mod)

	cClient := vm.natsClass(mod, "Client", "NATS::Client")
	// NATS::Connection is the historical name for the connection class; alias it to
	// the same class so both `is_a?(NATS::Client)` and `is_a?(NATS::Connection)`
	// hold, as they do in the gem.
	mod.consts["Connection"] = cClient
	vm.consts["NATS::Connection"] = cClient
	cSub := vm.natsClass(mod, "Subscription", "NATS::Subscription")
	cMsg := vm.natsClass(mod, "Msg", "NATS::Msg")

	mod.smethods["connect"] = &Method{name: "connect", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			return vm.natsConnect(args, blk)
		}}

	vm.registerNATSClient(cClient)
	vm.registerNATSSubscription(cSub)
	vm.registerNATSMsg(cMsg)
}

// natsClass creates a NATS::* class under cObject, records it flat (for classOf /
// raise) and nests it under the NATS module by its simple name.
func (vm *VM) natsClass(mod *RClass, simple, qualified string) *RClass {
	c := newClass(qualified, vm.cObject)
	vm.consts[qualified] = c
	mod.consts[simple] = c
	return c
}

// registerNATSErrors installs the NATS exception tree, mirroring the gem: the root
// NATS::Error < StandardError, NATS::Timeout beneath it, and the NATS::IO::*
// transport errors (NoRespondersError, ConnectionClosedError, BadSubject, …) each
// beneath NATS::Error. Every class is registered both scoped (under its module)
// and flat in vm.consts so raise can find it by qualified name (see natsErrClass).
func (vm *VM) registerNATSErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)

	base := newClass("NATS::Error", std)
	mod.consts["Error"] = base
	vm.consts["NATS::Error"] = base

	timeout := newClass("NATS::Timeout", base)
	mod.consts["Timeout"] = timeout
	vm.consts["NATS::Timeout"] = timeout

	io := newClass("NATS::IO", nil)
	io.isModule = true
	mod.consts["IO"] = io
	vm.consts["NATS::IO"] = io
	for _, name := range []string{
		"NoRespondersError", "ConnectionClosedError", "ConnectionDrainingError",
		"ConnectionReconnectingError", "BadSubscription", "BadSubject",
		"NoServersError", "MaxPayloadError",
	} {
		cls := newClass("NATS::IO::"+name, base)
		io.consts[name] = cls
		vm.consts["NATS::IO::"+name] = cls
	}
}

// natsConnect opens a connection, mirroring NATS.connect(url = nil, **opts): an
// optional leading URL String and keyword options (servers:/name:/user:/
// password:/token:/max_reconnect_attempts:/reconnect_time_wait:/connect_timeout:)
// configure the connection, and a block is yielded the connected client (as the
// gem's connect block is). The connected NATS::Client is returned.
func (vm *VM) natsConnect(args []object.Value, blk *Proc) object.Value {
	opts := natsConnectOptions(args)
	c, err := nats.Connect(opts...)
	if err != nil {
		raiseNATSConnect(err)
	}
	client := vm.newNATSClient(c)
	if blk != nil {
		vm.callBlock(blk, []object.Value{client})
	}
	return client
}

// natsConnectOptions reads NATS.connect's arguments into library options: a
// leading URL String becomes a server, and the trailing keyword Hash's keys map
// to the gem-shaped Option constructors.
func natsConnectOptions(args []object.Value) []nats.Option {
	var opts []nats.Option
	for _, a := range args {
		if s, ok := a.(*object.String); ok {
			opts = append(opts, nats.Servers(s.Str()))
		}
	}
	kw, ok := natsKwHash(args)
	if !ok {
		return opts
	}
	if v, ok := kw.Get(object.Symbol("servers")); ok {
		opts = append(opts, nats.Servers(natsServers(v)...))
	}
	if v, ok := kw.Get(object.Symbol("name")); ok {
		opts = append(opts, nats.Name(natsStr(v)))
	}
	if u, ok := kw.Get(object.Symbol("user")); ok {
		var pass object.Value = object.NilV
		if p, ok := kw.Get(object.Symbol("password")); ok {
			pass = p
		}
		opts = append(opts, nats.UserInfo(natsStr(u), natsStr(pass)))
	}
	if v, ok := kw.Get(object.Symbol("token")); ok {
		opts = append(opts, nats.Token(natsStr(v)))
	}
	if v, ok := kw.Get(object.Symbol("max_reconnect_attempts")); ok {
		opts = append(opts, nats.MaxReconnects(natsInt(v)))
	}
	if v, ok := kw.Get(object.Symbol("reconnect_time_wait")); ok {
		opts = append(opts, nats.ReconnectWait(natsSeconds(v)))
	}
	if v, ok := kw.Get(object.Symbol("connect_timeout")); ok {
		opts = append(opts, nats.Timeout(natsSeconds(v)))
	}
	return opts
}

// natsKwHash returns the call's trailing keyword Hash (the last argument when it
// is a Hash) and whether one was present.
func natsKwHash(args []object.Value) (*object.Hash, bool) {
	if len(args) == 0 {
		return nil, false
	}
	h, ok := args[len(args)-1].(*object.Hash)
	return h, ok
}

// natsServers coerces a servers: option — a single String URL or an Array of
// them — into a slice of URL strings.
func natsServers(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = natsStr(e)
		}
		return out
	}
	return []string{natsStr(v)}
}

// natsInt coerces a Ruby Integer option to an int (0 for a non-Integer).
func natsInt(v object.Value) int {
	if n, ok := v.(object.Integer); ok {
		return int(n)
	}
	return 0
}

// registerNATSClient installs the NATS::Client surface: publish, subscribe (with
// an optional queue: group), request/reply, flush, drain, close and closed?.
func (vm *VM) registerNATSClient(c *RClass) {
	connOf := func(self object.Value) *nats.Client { return self.(*NATSX).conn }

	// #publish(subject, data = "", reply = nil) — also accepts reply: as a keyword.
	c.define("publish", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..3)")
		}
		subject := natsStr(args[0])
		var data []byte
		if len(args) >= 2 {
			data = natsBytes(args[1])
		}
		reply := natsReply(args)
		var err error
		if reply != "" {
			err = connOf(self).PublishRequest(subject, reply, data)
		} else {
			err = connOf(self).Publish(subject, data)
		}
		if err != nil {
			raiseNATS(err)
		}
		return object.NilV
	})

	// #publish_msg(msg) — publish a pre-built NATS::Msg (carrying reply / headers).
	c.define("publish_msg", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		m, ok := args[0].(*NATSX)
		if !ok || m.msg == nil {
			raise("TypeError", "expected a NATS::Msg, got %s", args[0].Inspect())
		}
		if err := connOf(self).PublishMsg(m.msg); err != nil {
			raiseNATS(err)
		}
		return object.NilV
	})

	// #subscribe(subject, queue: nil) { |msg| } — deliver each matching message to
	// the block, serialized onto the VM (see natsDeliver); returns a Subscription.
	c.define("subscribe", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if blk == nil {
			raise("ArgumentError", "subscribe requires a block")
		}
		subject := natsStr(args[0])
		cb := func(m *nats.Msg) { vm.natsDeliver(blk, m) }
		var sub *nats.Subscription
		var err error
		if q := natsKwarg(args, "queue"); q != nil {
			sub, err = connOf(self).QueueSubscribe(subject, natsStr(q), cb)
		} else {
			sub, err = connOf(self).Subscribe(subject, cb)
		}
		if err != nil {
			raiseNATS(err)
		}
		return vm.newNATSSub(sub)
	})

	// #request(subject, data = "", timeout: 0.5) — publish and wait for one reply,
	// returning the reply NATS::Msg. The wait releases the GVL so a responder in
	// the same VM can answer (see natsDeliver).
	c.define("request", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		subject := natsStr(args[0])
		var data []byte
		if len(args) >= 2 {
			if _, ok := args[1].(*object.Hash); !ok {
				data = natsBytes(args[1])
			}
		}
		timeout := 500 * time.Millisecond
		if t := natsKwarg(args, "timeout"); t != nil {
			timeout = natsSeconds(t)
		}
		var rep *nats.Msg
		var err error
		vm.threadBlock(func() { rep, err = connOf(self).Request(subject, data, timeout) })
		if err != nil {
			raiseNATS(err)
		}
		return vm.newNATSMsg(rep)
	})

	// #flush(timeout = nil) — wait for the server to process buffered messages.
	c.define("flush", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var err error
		if len(args) > 0 && !object.IsNil(args[0]) {
			d := natsSeconds(args[0])
			vm.threadBlock(func() { err = connOf(self).FlushTimeout(d) })
		} else {
			vm.threadBlock(func() { err = connOf(self).Flush() })
		}
		if err != nil {
			raiseNATS(err)
		}
		return object.NilV
	})

	// #drain — unsubscribe, flush and close; #close — close immediately.
	c.define("drain", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		var err error
		vm.threadBlock(func() { err = connOf(self).Drain() })
		if err != nil {
			raiseNATS(err)
		}
		return object.NilV
	})
	c.define("close", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		connOf(self).Close()
		return object.NilV
	})
	c.define("closed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(connOf(self).IsClosed())
	})
}

// natsReply returns the reply subject a publish should carry: a reply: keyword, or
// a third positional argument, or "" when neither is given.
func natsReply(args []object.Value) string {
	if r := natsKwarg(args, "reply"); r != nil {
		return natsStr(r)
	}
	if len(args) >= 3 {
		if _, ok := args[2].(*object.Hash); !ok {
			return natsStr(args[2])
		}
	}
	return ""
}

// registerNATSSubscription installs the NATS::Subscription surface: unsubscribe
// (optionally auto-unsubscribe after N messages), drain, and the subject / queue
// readers.
func (vm *VM) registerNATSSubscription(c *RClass) {
	subOf := func(self object.Value) *nats.Subscription { return self.(*NATSX).sub }

	c.define("unsubscribe", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var err error
		if len(args) > 0 && !object.IsNil(args[0]) {
			err = subOf(self).AutoUnsubscribe(natsInt(args[0]))
		} else {
			err = subOf(self).Unsubscribe()
		}
		if err != nil {
			raiseNATS(err)
		}
		return object.NilV
	})
	c.define("drain", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := subOf(self).Drain(); err != nil {
			raiseNATS(err)
		}
		return object.NilV
	})
	c.define("subject", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(subOf(self).Subject)
	})
	c.define("queue", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(subOf(self).Queue)
	})
}

// registerNATSMsg installs the NATS::Msg surface: the read accessors
// (subject/data/reply/headers), Msg.new for building a message to publish, and
// #respond, which answers a request-reply message over its reply subject.
func (vm *VM) registerNATSMsg(c *RClass) {
	msgOf := func(self object.Value) *nats.Msg { return self.(*NATSX).msg }

	// NATS::Msg.new(subject: "", data: "", reply: "", header: {}) — build a message
	// for #publish_msg / #respond_msg.
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			m := &nats.Msg{}
			if v := natsKwarg(args, "subject"); v != nil {
				m.Subject = natsStr(v)
			}
			if v := natsKwarg(args, "data"); v != nil {
				m.Data = natsBytes(v)
			}
			if v := natsKwarg(args, "reply"); v != nil {
				m.Reply = natsStr(v)
			}
			if v := natsKwarg(args, "header"); v != nil {
				m.Header = natsBuildHeader(v)
			}
			return vm.newNATSMsg(m)
		}}

	c.define("subject", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(msgOf(self).Subject)
	})
	c.define("data", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewStringBytes(msgOf(self).Data)
	})
	c.define("reply", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(msgOf(self).Reply)
	})
	c.define("headers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return natsHeaderHash(msgOf(self).Headers())
	})

	// #respond(data) — publish data to the message's reply subject.
	c.define("respond", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var data []byte
		if len(args) > 0 {
			data = natsBytes(args[0])
		}
		if err := msgOf(self).Respond(data); err != nil {
			raiseNATS(err)
		}
		return object.NilV
	})
}
