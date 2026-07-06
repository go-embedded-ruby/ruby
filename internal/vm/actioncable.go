// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	actioncable "github.com/go-ruby-actioncable/actioncable"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file (with actioncable_bind.go) binds github.com/go-ruby-actioncable —
// a pure-Go (CGO=0), byte-faithful reimplementation of the core of Rails'
// Action Cable — into rbgo (require "action_cable"):
//
//	class ChatChannel < ActionCable::Channel::Base
//	  def subscribed;   stream_from "room_#{params['room']}"; end
//	  def unsubscribed; end
//	  def receive(data); transmit({ "echo" => data }); end
//	  def speak(data);   ActionCable.server.broadcast("room_1", data); end
//	end
//
//	server = ActionCable::Server.new
//	conn   = ActionCable::Connection::Base.new(server)
//	conn.connect                       # => transmits the welcome frame
//	conn.dispatch(subscribe_frame)     # => confirm_subscription frame
//	server.broadcast("room_1", data)   # => fans out a message frame
//	conn.transmissions                 # => the raw JSON frames sent to the client
//
// The library owns the WebSocket sub-protocol (welcome/ping/subscribe/confirm/
// reject/unsubscribe/message/disconnect envelopes, encoded byte-for-byte as
// Rails' actioncable-v1-json coder does — see protocol.go), the per-connection
// subscription registry, the channel lifecycle, stream subscriptions and the
// pub-sub fan-out. rbgo supplies the four seams the library leaves injectable:
//
//   - Transport (the WebSocket write): each ActionCable::Connection::Base
//     captures every frame the library emits into a Ruby Array, read back by
//     #transmissions. The bytes are stored verbatim, so the byte-exact wire
//     protocol is preserved end to end — rbgo never re-encodes a frame.
//   - ChannelAction (the Ruby method bodies): the library invokes it as
//     action(channel, action, data); rbgo dispatches the matching Ruby method
//     (subscribed/unsubscribed/receive/custom) on the channel instance, inline
//     on the calling VM goroutine under the GVL (the async adapter fans out
//     synchronously, so nothing runs off-thread).
//   - ChannelFactory (the class dispatch): a subscription identifier's "channel"
//     param names a Ruby channel class; rbgo resolves it to an
//     ActionCable::Channel::Base subclass and instantiates it, or declines the
//     subscription when no such class exists.
//   - Adapter (the pub-sub backend): defaults to the in-process async adapter,
//     so no external Redis is needed and fan-out stays deterministic.

// acHandle wraps a native go-ruby-actioncable value (*Server / *Connection /
// *Channel / *RemoteConnections / *RemoteConnection) stashed in a Ruby object's
// instance variable. It is never user-visible: it exists only so a Ruby
// Server/Connection/Channel object can reach the library object backing it.
type acHandle struct{ v any }

func (acHandle) ToS() string     { return "#<ActionCable::Handle>" }
func (acHandle) Inspect() string { return "#<ActionCable::Handle>" }
func (acHandle) Truthy() bool    { return true }

// registerActionCable installs the ActionCable module surface (require
// "action_cable"): the ActionCable.server singleton, ActionCable::Server and its
// remote-connection helpers, the ActionCable::Connection::Base driver and the
// ActionCable::Channel::Base subscription superclass. The channel-action /
// transport / factory seams and the byte-exact wire protocol are wired in
// actioncable_bind.go.
func (vm *VM) registerActionCable() {
	mod := newClass("ActionCable", nil)
	mod.isModule = true
	vm.consts["ActionCable"] = mod

	// ActionCable::Error < StandardError — the base error the binding raises when
	// a frame fails to dispatch/encode, so application code can rescue it.
	std := vm.consts["StandardError"].(*RClass)
	acErr := newClass("ActionCable::Error", std)
	mod.consts["Error"] = acErr
	vm.consts["ActionCable::Error"] = acErr

	server := newClass("ActionCable::Server", vm.cObject)
	mod.consts["Server"] = server
	vm.consts["ActionCable::Server"] = server
	// Rails also exposes ActionCable::Server::Base; alias it to the same class.
	server.consts["Base"] = server
	vm.consts["ActionCable::Server::Base"] = server

	remoteConns := newClass("ActionCable::RemoteConnections", vm.cObject)
	mod.consts["RemoteConnections"] = remoteConns
	vm.consts["ActionCable::RemoteConnections"] = remoteConns
	remoteConn := newClass("ActionCable::RemoteConnection", vm.cObject)
	mod.consts["RemoteConnection"] = remoteConn
	vm.consts["ActionCable::RemoteConnection"] = remoteConn

	connMod := newClass("ActionCable::Connection", nil)
	connMod.isModule = true
	mod.consts["Connection"] = connMod
	vm.consts["ActionCable::Connection"] = connMod
	connBase := newClass("ActionCable::Connection::Base", vm.cObject)
	connMod.consts["Base"] = connBase
	vm.consts["ActionCable::Connection::Base"] = connBase

	chMod := newClass("ActionCable::Channel", nil)
	chMod.isModule = true
	mod.consts["Channel"] = chMod
	vm.consts["ActionCable::Channel"] = chMod
	chBase := newClass("ActionCable::Channel::Base", vm.cObject)
	chMod.consts["Base"] = chBase
	vm.consts["ActionCable::Channel::Base"] = chBase
	vm.cACChannelBase = chBase

	vm.registerActionCableModule(mod, server)
	vm.registerActionCableServer(server, remoteConns, remoteConn)
	vm.registerActionCableConnection(connBase)
	vm.registerActionCableChannel(chBase)
}

// acArg returns args[i] or nil when i is out of range, so a native method reads
// its arguments without an per-call arity branch.
func acArg(args []object.Value, i int) object.Value {
	if i < len(args) {
		return args[i]
	}
	return object.NilV
}

// acKey coerces a String / Symbol (or anything else) to its string form — used
// for broadcasting names, identifiers and Hash keys.
func acKey(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

// acToGo maps a Ruby value into the plain Go shape go-ruby-actioncable encodes
// as the JSON payload of a broadcast / transmitted message. It mirrors the Ruby
// value model into JSON-native Go types (nil/bool/int64/float64/string, []any,
// map[string]any); anything else falls back to its string form.
func acToGo(v object.Value) any {
	switch n := v.(type) {
	case nil, object.Nil:
		return nil
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int64(n)
	case object.Float:
		return float64(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	case *object.Array:
		out := make([]any, len(n.Elems))
		for i, e := range n.Elems {
			out[i] = acToGo(e)
		}
		return out
	case *object.Hash:
		m := make(map[string]any, len(n.Keys))
		for _, k := range n.Keys {
			val, _ := n.Get(k)
			m[acKey(k)] = acToGo(val)
		}
		return m
	}
	return v.ToS()
}

// acServerOf returns the *actioncable.Server backing an ActionCable::Server
// object (from its @__ac handle).
func acServerOf(self object.Value) *actioncable.Server {
	return getIvar(self, "@__ac").(acHandle).v.(*actioncable.Server)
}

// acConnOf returns the *actioncable.Connection backing an
// ActionCable::Connection::Base object.
func acConnOf(self object.Value) *actioncable.Connection {
	return getIvar(self, "@__ac").(acHandle).v.(*actioncable.Connection)
}

// acChannelOf returns the *actioncable.Channel backing an
// ActionCable::Channel::Base subclass instance.
func acChannelOf(self object.Value) *actioncable.Channel {
	return getIvar(self, "@__ac").(acHandle).v.(*actioncable.Channel)
}
