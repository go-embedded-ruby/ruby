// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"net"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the datagram half of rbgo's socket transport: a real UDPSocket
// (UDPSocket < IPSocket < BasicSocket) backed by Go's *net.UDPConn. Unlike the
// connected-stream TCPSocket/SSLSocket surface (socket.go / socket_bind.go),
// datagram sockets do not share the streamIO read/gets/readpartial machinery —
// their unit is a whole message, so they expose send / recv / recvfrom directly.
//
// MRI semantics modelled for the common cases: UDPSocket.new binds an ephemeral
// local port immediately (so #recvfrom works without an explicit #bind);
// #connect records a default peer used by a destination-less #send; #send with
// an explicit host/port datagram-addresses a single message; #recvfrom yields
// the payload alongside the sender's [family, port, host, ip] tuple.

// udpSocket is a datagram socket: a live *net.UDPConn plus an optional connected
// peer recorded by #connect (used when #send is called without a destination).
type udpSocket struct {
	cls    *RClass
	net    string       // "udp" / "udp4" / "udp6", fixed at construction
	conn   *net.UDPConn // the bound datagram socket
	remote *net.UDPAddr // the #connect default peer, nil until connected
	closed bool
}

func (s *udpSocket) ToS() string     { return "#<UDPSocket>" }
func (s *udpSocket) Inspect() string { return "#<UDPSocket>" }
func (s *udpSocket) Truthy() bool    { return true }

// udpListen is the net.ListenUDP seam UDPSocket.new binds through. It is a
// package var so a test can inject a bind failure (the ephemeral bind in #new
// never fails on a healthy host, so its error arm has no natural trigger).
var udpListen = net.ListenUDP

// registerUDPSocket installs the UDPSocket class + its datagram surface under
// IPSocket, matching MRI's UDPSocket < IPSocket < BasicSocket ancestry.
func (vm *VM) registerUDPSocket(ip *RClass) {
	udp := newClass("UDPSocket", ip)
	vm.consts["UDPSocket"] = udp

	// UDPSocket.new([address_family]) binds an ephemeral local datagram socket.
	// The optional address family (Socket::AF_INET / AF_INET6) selects the
	// network; omitting it uses an unspecified ("udp") socket.
	newFn := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		network := udpNetwork(args)
		conn, err := udpListen(network, &net.UDPAddr{})
		if err != nil {
			raise("SocketError", "udp: %s", err.Error())
		}
		return &udpSocket{cls: udp, net: network, conn: conn}
	}
	udp.smethods["new"] = &Method{name: "new", owner: udp, native: newFn}
	udp.smethods["open"] = &Method{name: "open", owner: udp, native: newFn}

	// #bind(host, port) rebinds the socket to a specific local address. The
	// ephemeral socket opened by #new is released first; because nothing has been
	// sent yet this is transparent, matching a fresh UDPSocket that binds.
	udp.define("bind", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asUDPSocket(self)
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		addr, err := net.ResolveUDPAddr(s.net, net.JoinHostPort(strArg(args[0]), portString(args[1])))
		if err != nil {
			raise("SocketError", "bind: %s", err.Error())
		}
		conn, err := net.ListenUDP(s.net, addr)
		if err != nil {
			raise("SocketError", "bind: %s", err.Error())
		}
		s.conn.Close()
		s.conn = conn
		return object.IntValue(0)
	})

	// #connect(host, port) records a default peer for destination-less #send;
	// it performs no packets (datagram "connect" is purely a stored address).
	udp.define("connect", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asUDPSocket(self)
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		addr, err := net.ResolveUDPAddr(s.net, net.JoinHostPort(strArg(args[0]), portString(args[1])))
		if err != nil {
			raise("SocketError", "connect: %s", err.Error())
		}
		s.remote = addr
		return object.IntValue(0)
	})

	// #send(mesg, flags[, host, port]) sends one datagram. With host+port it is
	// addressed explicitly; otherwise it goes to the #connect peer. flags is
	// accepted and ignored (rbgo has no MSG_* semantics to honour). Returns the
	// number of bytes written, as MRI does.
	udp.define("send", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asUDPSocket(self)
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..4)", len(args))
		}
		msg := argBytes(vm, args[0])
		var dest *net.UDPAddr
		switch {
		case len(args) >= 4:
			addr, err := net.ResolveUDPAddr(s.net, net.JoinHostPort(strArg(args[2]), portString(args[3])))
			if err != nil {
				raise("SocketError", "send: %s", err.Error())
			}
			dest = addr
		case s.remote != nil:
			dest = s.remote
		default:
			raise("SocketError", "send: destination address required (call #connect or pass host, port)")
		}
		n, err := s.conn.WriteToUDP(msg, dest)
		if err != nil {
			raise("SocketError", "send: %s", err.Error())
		}
		return object.IntValue(int64(n))
	})

	// #recvfrom(maxlen) blocks for one datagram and returns [message, sender],
	// where sender is the MRI 4-tuple [family, port, host, ip].
	udp.define("recvfrom", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asUDPSocket(self)
		buf := make([]byte, udpRecvLen(args))
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			raise("SocketError", "recvfrom: %s", err.Error())
		}
		return object.NewArray(
			object.NewStringBytesEnc(buf[:n], "ASCII-8BIT"),
			addrTuple(addr),
		)
	})

	// #recv(maxlen[, flags]) blocks for one datagram and returns just its payload.
	udp.define("recv", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asUDPSocket(self)
		buf := make([]byte, udpRecvLen(args))
		n, _, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			raise("SocketError", "recv: %s", err.Error())
		}
		return object.NewStringBytesEnc(buf[:n], "ASCII-8BIT")
	})

	// #addr / #local_address report the bound local address as MRI's 4-tuple,
	// letting a caller discover the ephemeral port #new chose.
	udp.define("addr", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return addrTuple(asUDPSocket(self).conn.LocalAddr())
	})
	udp.define("local_address", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return addrTuple(asUDPSocket(self).conn.LocalAddr())
	})

	udp.define("close", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asUDPSocket(self)
		if !s.closed {
			s.closed = true
			s.conn.Close()
		}
		return object.NilV
	})
	udp.define("closed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(asUDPSocket(self).closed)
	})
}

// udpNetwork selects the Go UDP network from an optional address-family
// argument: Socket::AF_INET -> "udp4", AF_INET6 -> "udp6", otherwise (or when
// omitted) the unspecified "udp". A non-integer family raises TypeError.
func udpNetwork(args []object.Value) string {
	if len(args) == 0 || object.IsNil(args[0]) {
		return "udp"
	}
	switch intArg(args[0]) {
	case 2: // AF_INET
		return "udp4"
	case 10, 30: // AF_INET6 (Linux / BSD numbering both accepted)
		return "udp6"
	default:
		return "udp"
	}
}

// udpRecvLen resolves the maxlen argument of #recv / #recvfrom, defaulting to a
// 64 KiB buffer (a full UDP datagram) when none is given.
func udpRecvLen(args []object.Value) int {
	if len(args) == 0 || object.IsNil(args[0]) {
		return 65536
	}
	n := int(intArg(args[0]))
	if n <= 0 {
		raise("ArgumentError", "negative length %d given", n)
	}
	return n
}

// asUDPSocket narrows a receiver to *udpSocket, raising TypeError otherwise so a
// mis-typed self surfaces as a Ruby error rather than a Go panic.
func asUDPSocket(v object.Value) *udpSocket {
	if s, ok := v.(*udpSocket); ok {
		return s
	}
	raise("TypeError", "not a UDPSocket")
	return nil
}
