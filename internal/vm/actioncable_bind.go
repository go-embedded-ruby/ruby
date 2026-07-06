// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	actioncable "github.com/go-ruby-actioncable/actioncable"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the four go-ruby-actioncable seams and the ActionCable Ruby
// method surface. The wire protocol lives entirely in the library; rbgo only
// stores the frames it emits (Transport), runs the Ruby channel bodies
// (ChannelAction), maps a subscription to its Ruby channel class
// (ChannelFactory) and defaults the pub-sub backend to the in-process async
// Adapter — so every byte on the wire is the library's, unchanged.

// registerActionCableModule installs the ActionCable.server singleton — an
// ActionCable::Server over a fresh in-process async adapter, memoized on the VM.
func (vm *VM) registerActionCableModule(mod, server *RClass) {
	mod.smethods["server"] = &Method{name: "server", owner: mod, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.acServer == nil {
			vm.acServer = vm.acNewServer(server)
		}
		return vm.acServer
	}}
}

// acNewServer builds an ActionCable::Server object over a fresh async adapter.
func (vm *VM) acNewServer(cls *RClass) object.Value {
	obj := &RObject{class: cls, ivars: map[string]object.Value{}}
	setIvar(obj, "@__ac", acHandle{actioncable.NewServer(actioncable.NewAsyncAdapter())})
	return obj
}

// registerActionCableServer installs ActionCable::Server (.new / #broadcast /
// #remote_connections) and its RemoteConnections/RemoteConnection helpers.
func (vm *VM) registerActionCableServer(server, remoteConns, remoteConn *RClass) {
	server.smethods["new"] = &Method{name: "new", owner: server, native: func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.acNewServer(self.(*RClass))
	}}

	// #broadcast(broadcasting, data) — ActionCable.server.broadcast: JSON-encode
	// data and fan it out to every stream subscribed to broadcasting.
	server.define("broadcast", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if err := acServerOf(self).Broadcast(acKey(acArg(args, 0)), acToGo(acArg(args, 1))); err != nil {
			raise("ActionCable::Error", "%s", err.Error())
		}
		return object.NilV
	})

	// #remote_connections — the entry point for disconnecting connections by
	// their identified-by values from anywhere.
	server.define("remote_connections", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		obj := &RObject{class: remoteConns, ivars: map[string]object.Value{}}
		setIvar(obj, "@__ac", acHandle{acServerOf(self).RemoteConnections()})
		return obj
	})

	// remote_connections.where(current_user: user) selects the matching
	// connection(s) by their identified-by values.
	remoteConns.define("where", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		rc := getIvar(self, "@__ac").(acHandle).v.(*actioncable.RemoteConnections)
		ids := map[string]any{}
		if h, ok := acArg(args, 0).(*object.Hash); ok {
			for _, k := range h.Keys {
				val, _ := h.Get(k)
				ids[acKey(k)] = acToGo(val)
			}
		}
		obj := &RObject{class: remoteConn, ivars: map[string]object.Value{}}
		setIvar(obj, "@__ac", acHandle{rc.Where(ids)})
		return obj
	})

	// remote_connection.disconnect(reconnect: true) asks the matching
	// connection(s) to close, telling the client whether to reconnect. The
	// disconnect travels the connection's internal pub-sub channel.
	remoteConn.define("disconnect", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		rc := getIvar(self, "@__ac").(acHandle).v.(*actioncable.RemoteConnection)
		// Publishing a boolean-only frame on the internal channel can never fail
		// to encode, so the returned error is unreachable from Ruby.
		_ = rc.Disconnect(acReconnect(args))
		return object.NilV
	})
}

// acReconnect reads the reconnect flag from an options Hash argument
// (reconnect: false), defaulting to true as Rails does. It scans all arguments
// so a leading reason String (on #disconnect) does not hide the options Hash.
func acReconnect(args []object.Value) bool {
	for _, a := range args {
		if h, ok := a.(*object.Hash); ok {
			if v, ok := h.Get(object.Symbol("reconnect")); ok {
				return v.Truthy()
			}
		}
	}
	return true
}

// registerActionCableConnection installs ActionCable::Connection::Base: the
// driver a host wraps a WebSocket around. .new(server) builds a live
// *actioncable.Connection wiring the Transport seam (frames captured into a Ruby
// Array) and the ChannelFactory seam (identifier -> Ruby channel class).
func (vm *VM) registerActionCableConnection(base *RClass) {
	base.smethods["new"] = &Method{name: "new", owner: base, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		srv := acServerOf(acArg(args, 0))
		obj := &RObject{class: self.(*RClass), ivars: map[string]object.Value{}}
		// Transport seam: every frame the library emits is appended verbatim to
		// this Ruby Array (no re-encoding), read back by #transmissions.
		frames := object.NewArray()
		setIvar(obj, "@__tx", frames)
		setIvar(obj, "@__channels", object.NewHash())
		transport := func(payload []byte) {
			frames.Elems = append(frames.Elems, object.NewString(string(payload)))
		}
		// ChannelFactory seam: resolve a subscription's channel class and build it.
		factory := func(goconn *actioncable.Connection, identifier string, params map[string]any) (*actioncable.Channel, bool) {
			return vm.acBuildChannel(obj, goconn, identifier, params)
		}
		setIvar(obj, "@__ac", acHandle{actioncable.NewConnection(srv, transport, factory)})
		return obj
	}}

	base.define("connect", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		acConnOf(self).Connect()
		return object.NilV
	})

	// #dispatch(raw) routes one client-to-server frame; a malformed frame or an
	// unknown command/subscription surfaces as an ActionCable::Error.
	base.define("dispatch", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if err := acConnOf(self).Dispatch([]byte(acKey(acArg(args, 0)))); err != nil {
			raise("ActionCable::Error", "%s", err.Error())
		}
		return object.NilV
	})

	base.define("beat", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		epoch := int64(0)
		if n, ok := acArg(args, 0).(object.Integer); ok {
			epoch = int64(n)
		}
		acConnOf(self).Beat(epoch)
		return object.NilV
	})

	base.define("advance", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		acConnOf(self).Advance(acDuration(acArg(args, 0)))
		return object.NilV
	})

	base.define("identified_by", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		acConnOf(self).IdentifiedBy(acKey(acArg(args, 0)), acToGo(acArg(args, 1)))
		return object.NilV
	})

	base.define("disconnect", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		reason := actioncable.DisconnectServerRestart
		if s, ok := acArg(args, 0).(*object.String); ok {
			reason = s.Str()
		}
		acConnOf(self).Disconnect(reason, acReconnect(args))
		return object.NilV
	})

	base.define("transmissions", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@__tx")
	})

	base.define("subscriptions", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(acConnOf(self).Subscriptions()))
	})

	base.define("subscription", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		id := acKey(acArg(args, 0))
		if _, ok := acConnOf(self).Subscription(id); !ok {
			return object.NilV
		}
		ch, _ := getIvar(self, "@__channels").(*object.Hash).Get(object.NewString(id))
		return ch
	})

	base.define("closed?", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(acConnOf(self).Closed())
	})
}

// acBuildChannel is the ChannelFactory seam: it resolves a subscription
// identifier's "channel" param to an ActionCable::Channel::Base subclass, and if
// found instantiates that Ruby class, links it to a fresh *actioncable.Channel
// whose Action seam runs the channel's Ruby method bodies, and records it on the
// connection. It returns (nil, false) — declining the subscription — when no
// matching channel class exists, mirroring Rails' "Subscription class not found".
func (vm *VM) acBuildChannel(connObj *RObject, goconn *actioncable.Connection, identifier string, params map[string]any) (*actioncable.Channel, bool) {
	name, _ := params["channel"].(string)
	cls, ok := vm.consts[name].(*RClass)
	if !ok || !vm.acIsChannelClass(cls) {
		return nil, false
	}
	inst := &RObject{class: cls, ivars: map[string]object.Value{}}
	setIvar(inst, "@__params", rackFromGo(params))
	// ChannelAction seam: run the matching Ruby method on this channel instance,
	// inline on the calling VM goroutine (the async adapter fans out synchronously
	// under the GVL). The channel's own StreamFrom/Transmit/Reject are reached
	// through the @__ac handle set below.
	ch := actioncable.NewChannel(goconn, actioncable.ChannelName(name), identifier, params, func(_, action string, data any) any {
		vm.acRunAction(inst, action, data)
		return nil
	})
	setIvar(inst, "@__ac", acHandle{ch})
	getIvar(connObj, "@__channels").(*object.Hash).Set(object.NewString(identifier), inst)
	return ch, true
}

// acIsChannelClass reports whether cls is ActionCable::Channel::Base or a
// subclass of it.
func (vm *VM) acIsChannelClass(cls *RClass) bool {
	for c := cls; c != nil; c = c.super {
		if c == vm.cACChannelBase {
			return true
		}
	}
	return false
}

// acRunAction dispatches one channel action to its Ruby method: subscribed /
// unsubscribed take no argument; receive and custom actions take the decoded
// payload. An action with no matching method is a no-op (Rails logs and ignores).
func (vm *VM) acRunAction(inst *RObject, action string, data any) {
	if vm.findMethod(inst, action) == nil {
		return
	}
	switch action {
	case "subscribed", "unsubscribed":
		vm.send(inst, action, nil, nil)
	default:
		vm.send(inst, action, []object.Value{rackFromGo(data)}, nil)
	}
}

// registerActionCableChannel installs the ActionCable::Channel::Base instance
// surface a subscription runs against (stream_from/stream_for/transmit/reject/
// broadcast_to/periodically/params/identifier).
func (vm *VM) registerActionCableChannel(base *RClass) {
	// #stream_from(broadcasting) — subscribe so every value broadcast there is
	// decoded and transmitted to this client. The async adapter's Subscribe never
	// errors, so the returned error is unreachable here.
	base.define("stream_from", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_ = acChannelOf(self).StreamFrom(acKey(acArg(args, 0)))
		return object.NilV
	})

	// #stream_for(model) — subscribe to the broadcasting derived from model.
	base.define("stream_for", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_ = acChannelOf(self).StreamFor(acToGo(acArg(args, 0)))
		return object.NilV
	})

	// #transmit(data) — send data to this subscription's client.
	base.define("transmit", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if err := acChannelOf(self).Transmit(acToGo(acArg(args, 0))); err != nil {
			raise("ActionCable::Error", "%s", err.Error())
		}
		return object.NilV
	})

	// #broadcast_to(model, data) — broadcast to the broadcasting for model.
	base.define("broadcast_to", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if err := acChannelOf(self).BroadcastTo(acToGo(acArg(args, 0)), acToGo(acArg(args, 1))); err != nil {
			raise("ActionCable::Error", "%s", err.Error())
		}
		return object.NilV
	})

	// #reject — reject the subscription from within the subscribed hook.
	base.define("reject", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		acChannelOf(self).Reject()
		return object.NilV
	})

	// #periodically(interval) { ... } or #periodically(:method, every: interval)
	// registers a timer on the connection's deterministic scheduler, run inline
	// when the host advances the clock.
	base.define("periodically", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		ch := acChannelOf(self)
		if blk != nil {
			ch.Periodically(acDuration(acArg(args, 0)), func() { vm.callBlockSelf(blk, self, nil) })
			return object.NilV
		}
		name := acKey(acArg(args, 0))
		ch.Periodically(acEvery(args), func() { vm.send(self, name, nil, nil) })
		return object.NilV
	})

	base.define("params", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@__params")
	})

	base.define("identifier", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(acChannelOf(self).Identifier)
	})
}

// acDuration coerces a numeric seconds value into a time.Duration.
func acDuration(v object.Value) time.Duration {
	switch n := v.(type) {
	case object.Integer:
		return time.Duration(int64(n)) * time.Second
	case object.Float:
		return time.Duration(float64(n) * float64(time.Second))
	}
	return 0
}

// acEvery reads the every: interval from a trailing options Hash of
// periodically(:method, every: seconds); a missing every yields an inert timer.
func acEvery(args []object.Value) time.Duration {
	for _, a := range args {
		if h, ok := a.(*object.Hash); ok {
			if v, ok := h.Get(object.Symbol("every")); ok {
				return acDuration(v)
			}
		}
	}
	return 0
}
