// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"net"
	"strconv"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the raw, low-level socket half of rbgo's socket transport: the
// generic Socket.new(domain, type[, protocol]) the typed TCPSocket / UDPSocket /
// UNIXSocket specializations previously stood in for (socket.go left Socket.new
// a NotImplementedError stub). A raw Socket is the MRI BSD-socket surface —
// bind / listen / accept / connect / send / recv / recvfrom / getsockname /
// getpeername / setsockopt / getsockopt / close — over Go's net package, chosen
// per (domain, type): net.Listen / net.Dial for a stream, net.ListenPacket for a
// datagram, across AF_INET / AF_INET6 / AF_UNIX.
//
// A raw Socket is a small state machine: fresh after #new, then bind (a stream
// binds+listens through net.Listen so an ephemeral port is assigned immediately,
// as POSIX bind does; a datagram binds through net.ListenPacket), or connect
// (net.Dial, both stream and datagram), reaching exactly one live transport —
// a net.Listener (bound stream), a net.Conn (connected), or a net.PacketConn
// (bound datagram). The typed sockets keep their own dedicated implementations;
// only Socket.new routes here.
//
// The AF_INET / AF_INET6 handling is here; the per-domain address dispatch
// (rawSocketNetwork + resolveAddr / resolveNetAddr / packAddr / addrinfoOf) is
// platform-split so every AF_UNIX branch lives in socket_raw_unix.go: on Windows
// (socket_raw_windows.go) AF_UNIX is rejected at Socket.new, matching the
// UNIXSocket transport, and the INET-only build stays fully covered.

// rawSocket is a generic BSD socket. At most one of ln / conn / pconn is live at
// a time, selected by the bind / connect / accept transition the script drove.
type rawSocket struct {
	cls    *RClass
	domain int // AF_INET / AF_INET6 / AF_UNIX
	typ    int // SOCK_STREAM / SOCK_DGRAM
	proto  int

	ln    net.Listener   // a bound+listening stream socket (after #bind)
	conn  net.Conn       // a connected socket (after #connect or #accept)
	r     *bufio.Reader  // buffered reader over conn, for #recv / #recvfrom
	pconn net.PacketConn // a bound datagram socket (after #bind)

	opts   map[[2]int64]object.Value // best-effort setsockopt / getsockopt store
	closed bool
}

func (s *rawSocket) ToS() string     { return "#<Socket>" }
func (s *rawSocket) Inspect() string { return "#<Socket>" }
func (s *rawSocket) Truthy() bool    { return true }

// rawListenPacket is the net.ListenPacket seam a datagram Socket binds an
// ephemeral send socket through (for a #send with an explicit destination on an
// otherwise-unbound datagram socket). It is a package var so a test can inject a
// bind failure, which has no natural trigger on a healthy host.
var rawListenPacket = net.ListenPacket

// rawINETNetwork maps an AF_INET / AF_INET6 (domain, type) pair to the Go net
// network string, reporting ok=false for anything else. It is the INET core of
// the platform-split rawSocketNetwork (socket_raw_unix.go / socket_raw_windows.go
// add the AF_UNIX handling — real on non-Windows, rejected on Windows).
func rawINETNetwork(domain, typ int) (string, bool) {
	switch domain {
	case afINET:
		switch typ {
		case sockStream:
			return "tcp4", true
		case sockDgram:
			return "udp4", true
		}
	case afINET6:
		switch typ {
		case sockStream:
			return "tcp6", true
		case sockDgram:
			return "udp6", true
		}
	}
	return "", false
}

// rawStProto reports the socket-type / protocol pair an Addrinfo carries for a
// raw socket's SOCK_ type.
func rawStProto(typ int) (int, int) {
	if typ == sockDgram {
		return sockDgram, ipprotoUDP
	}
	return sockStream, ipprotoTCP
}

// registerRawSocket installs the raw Socket surface: Socket.new plus the BSD
// socket instance methods. It replaces the NotImplementedError stub socket.go
// used before raw sockets existed, and runs from registerSocketClass after the
// Socket constant and its constants are published.
func (vm *VM) registerRawSocket(sock *RClass) {
	// Socket.new(domain, type [, protocol]) creates a raw socket. domain / type
	// accept the Socket:: integer constants or the Symbol / String names
	// (:INET / "SOCK_STREAM"), matching MRI. No OS socket is opened until #bind /
	// #connect selects a transport.
	newFn := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
		}
		domain := familyNumber(args[0])
		typ := socktypeNumber(args[1])
		proto := 0
		if len(args) > 2 && !object.IsNil(args[2]) {
			proto = int(intArg(args[2]))
		}
		if _, ok := rawSocketNetwork(domain, typ); !ok {
			raise("SocketError", "unsupported domain/type combination")
		}
		return &rawSocket{cls: sock, domain: domain, typ: typ, proto: proto, opts: map[[2]int64]object.Value{}}
	}
	sock.smethods["new"] = &Method{name: "new", owner: sock, native: newFn}
	sock.smethods["open"] = &Method{name: "open", owner: sock, native: newFn}

	// #bind(sockaddr) binds the socket to the local address packed in sockaddr. A
	// stream binds+listens (net.Listen assigns an ephemeral port immediately, as
	// POSIX bind does); a datagram binds a packet socket (net.ListenPacket).
	sock.define("bind", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asRawSocket(self)
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		network, _ := rawSocketNetwork(s.domain, s.typ)
		addr := s.resolveAddr(sockaddrBytes(args[0]))
		if s.typ == sockStream {
			ln, err := net.Listen(network, addr)
			if err != nil {
				raise("SocketError", "bind: %s", err.Error())
			}
			s.ln = ln
		} else {
			pc, err := net.ListenPacket(network, addr)
			if err != nil {
				raise("SocketError", "bind: %s", err.Error())
			}
			s.pconn = pc
		}
		return object.IntValue(0)
	})

	// #listen(backlog) is a no-op returning 0: #bind already established the
	// listening socket (net.Listen sets the backlog), matching TCPServer#listen.
	sock.define("listen", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		asRawSocket(self)
		return object.IntValue(0)
	})

	// #accept blocks for a connection and returns [Socket, Addrinfo]: the
	// connected peer socket and the peer's address, as MRI does.
	sock.define("accept", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asRawSocket(self)
		if s.ln == nil {
			raise("IOError", "accept: socket is not listening (call #bind first)")
		}
		conn, err := s.ln.Accept()
		if err != nil {
			raise("IOError", "accept: %s", err.Error())
		}
		child := &rawSocket{cls: s.cls, domain: s.domain, typ: s.typ, proto: s.proto,
			conn: conn, r: bufio.NewReader(conn), opts: map[[2]int64]object.Value{}}
		return object.NewArray(child, s.addrinfoOf(vm.consts["Addrinfo"].(*RClass), conn.RemoteAddr()))
	})

	// #connect(sockaddr) connects to the peer packed in sockaddr (net.Dial, for
	// both stream and datagram sockets).
	sock.define("connect", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asRawSocket(self)
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		network, _ := rawSocketNetwork(s.domain, s.typ)
		conn, err := net.Dial(network, s.resolveAddr(sockaddrBytes(args[0])))
		if err != nil {
			raise("SocketError", "connect: %s", err.Error())
		}
		s.conn = conn
		s.r = bufio.NewReader(conn)
		return object.IntValue(0)
	})

	// #send(mesg, flags [, dest_sockaddr]) sends mesg and returns the byte count.
	// A connected socket writes to its peer (any dest is ignored, as MRI does); an
	// unconnected datagram socket sends to the dest sockaddr (binding an ephemeral
	// send socket on demand). flags is accepted and ignored.
	sock.define("send", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asRawSocket(self)
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..4)", len(args))
		}
		msg := argBytes(vm, args[0])
		if s.conn != nil {
			n, err := s.conn.Write(msg)
			if err != nil {
				raise("SocketError", "send: %s", err.Error())
			}
			return object.IntValue(int64(n))
		}
		if s.typ != sockDgram {
			raise("SocketError", "send: socket is not connected")
		}
		if len(args) < 3 || object.IsNil(args[2]) {
			raise("SocketError", "send: destination address required (connect or pass a sockaddr)")
		}
		s.ensurePconn()
		n, err := s.pconn.WriteTo(msg, s.resolveNetAddr(sockaddrBytes(args[2])))
		if err != nil {
			raise("SocketError", "send: %s", err.Error())
		}
		return object.IntValue(int64(n))
	})

	// #recv(maxlen [, flags]) reads up to maxlen bytes: from the connected peer
	// for a connected socket, or one datagram for a bound datagram socket. flags
	// is accepted and ignored.
	sock.define("recv", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asRawSocket(self)
		buf := make([]byte, rawRecvLen(args))
		switch {
		case s.conn != nil:
			k, _ := s.r.Read(buf)
			return object.NewStringBytesEnc(buf[:k], "ASCII-8BIT")
		case s.pconn != nil:
			k, _, err := s.pconn.ReadFrom(buf)
			if err != nil {
				raise("SocketError", "recv: %s", err.Error())
			}
			return object.NewStringBytesEnc(buf[:k], "ASCII-8BIT")
		default:
			raise("SocketError", "recv: socket is neither connected nor bound")
			return object.NilV
		}
	})

	// #recvfrom(maxlen [, flags]) returns [mesg, sender_addrinfo]: for a bound
	// datagram socket the sender is the datagram's source; for a connected socket
	// it is the connected peer, as MRI does.
	sock.define("recvfrom", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asRawSocket(self)
		buf := make([]byte, rawRecvLen(args))
		aiCls := vm.consts["Addrinfo"].(*RClass)
		switch {
		case s.pconn != nil:
			k, addr, err := s.pconn.ReadFrom(buf)
			if err != nil {
				raise("SocketError", "recvfrom: %s", err.Error())
			}
			return object.NewArray(object.NewStringBytesEnc(buf[:k], "ASCII-8BIT"), s.addrinfoOf(aiCls, addr))
		case s.conn != nil:
			k, _ := s.r.Read(buf)
			return object.NewArray(object.NewStringBytesEnc(buf[:k], "ASCII-8BIT"), s.addrinfoOf(aiCls, s.conn.RemoteAddr()))
		default:
			raise("SocketError", "recvfrom: socket is neither connected nor bound")
			return object.NilV
		}
	})

	// #getsockname returns the packed sockaddr of the socket's local address
	// (whichever transport is live), matching MRI.
	sock.define("getsockname", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asRawSocket(self)
		var a net.Addr
		switch {
		case s.ln != nil:
			a = s.ln.Addr()
		case s.pconn != nil:
			a = s.pconn.LocalAddr()
		case s.conn != nil:
			a = s.conn.LocalAddr()
		default:
			raise("IOError", "getsockname: socket is not bound or connected")
		}
		return object.NewStringBytesEnc(s.packAddr(a), "ASCII-8BIT")
	})

	// #getpeername returns the packed sockaddr of the connected peer, raising when
	// the socket is not connected, as MRI does.
	sock.define("getpeername", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asRawSocket(self)
		if s.conn == nil {
			raise("IOError", "getpeername: socket is not connected")
		}
		return object.NewStringBytesEnc(s.packAddr(s.conn.RemoteAddr()), "ASCII-8BIT")
	})

	// #setsockopt(level, optname, value) records the option (best-effort: Go's net
	// abstracts most socket options) and returns 0, letting #getsockopt read it
	// back. MRI returns 0.
	sock.define("setsockopt", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asRawSocket(self)
		if len(args) < 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 3)", len(args))
		}
		s.opts[[2]int64{intArg(args[0]), intArg(args[1])}] = args[2]
		return object.IntValue(0)
	})

	// #getsockopt(level, optname) returns the value stored by a prior #setsockopt,
	// or Integer 0 when the option was never set (Go's net does not expose the
	// kernel option value).
	sock.define("getsockopt", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := asRawSocket(self)
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		if v, ok := s.opts[[2]int64{intArg(args[0]), intArg(args[1])}]; ok {
			return v
		}
		return object.IntValue(0)
	})

	sock.define("close", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asRawSocket(self)
		if !s.closed {
			s.closed = true
			if s.ln != nil {
				s.ln.Close()
			}
			if s.conn != nil {
				s.conn.Close()
			}
			if s.pconn != nil {
				s.pconn.Close()
			}
		}
		return object.NilV
	})
	sock.define("closed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(asRawSocket(self).closed)
	})
}

// ensurePconn lazily binds an ephemeral datagram send socket for a #send with an
// explicit destination on an otherwise-unbound datagram socket (the client that
// never called #bind). A socket already bound keeps its packet conn.
func (s *rawSocket) ensurePconn() {
	if s.pconn != nil {
		return
	}
	network, _ := rawSocketNetwork(s.domain, s.typ)
	pc, err := rawListenPacket(network, ":0")
	if err != nil {
		raise("SocketError", "send: %s", err.Error())
	}
	s.pconn = pc
}

// resolveINETAddr turns a packed sockaddr_in / sockaddr_in6 into the host:port
// string net.Listen / net.Dial want for #bind / #connect. It is the INET core the
// platform-split resolveAddr delegates to (the AF_UNIX arm lives in
// socket_raw_unix.go).
func (s *rawSocket) resolveINETAddr(sa []byte) string {
	port, host := unpackSockaddrIn(sa)
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// resolveINETNetAddr turns a packed destination sockaddr into a *net.UDPAddr for a
// datagram #send, building the value directly (no resolver round-trip) so it
// never fails on a numeric address. It is the INET core the platform-split
// resolveNetAddr delegates to.
func (s *rawSocket) resolveINETNetAddr(sa []byte) net.Addr {
	port, host := unpackSockaddrIn(sa)
	return &net.UDPAddr{IP: net.ParseIP(host), Port: port}
}

// packINETAddr renders a net.Addr as a packed sockaddr_in / sockaddr_in6 for
// #getsockname / #getpeername. It is the INET core the platform-split packAddr
// delegates to.
func (s *rawSocket) packINETAddr(a net.Addr) []byte {
	host, portStr, _ := net.SplitHostPort(a.String())
	port, _ := strconv.Atoi(portStr)
	return packSockaddrIn(port, host)
}

// addrinfoINET builds the Addrinfo #accept / #recvfrom report for an AF_INET /
// AF_INET6 peer, taking the family from the socket's domain. It is the INET core
// the platform-split addrinfoOf delegates to.
func (s *rawSocket) addrinfoINET(cls *RClass, a net.Addr, st, proto int) *addrinfo {
	host, portStr, _ := net.SplitHostPort(a.String())
	port, _ := strconv.Atoi(portStr)
	return &addrinfo{cls: cls, afamily: s.domain, ip: host, port: port, socktype: st, protocol: proto}
}

// rawRecvLen resolves the maxlen argument of #recv / #recvfrom, requiring it (a
// raw socket read has no default buffer) and rejecting a negative length, as MRI
// does.
func rawRecvLen(args []object.Value) int {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	n := int(intArg(args[0]))
	if n < 0 {
		raise("ArgumentError", "negative length %d given", n)
	}
	return n
}

// asRawSocket narrows a receiver to *rawSocket, raising TypeError otherwise so a
// mis-typed self surfaces as a Ruby error rather than a Go panic.
func asRawSocket(v object.Value) *rawSocket {
	if s, ok := v.(*rawSocket); ok {
		return s
	}
	raise("TypeError", "not a Socket")
	return nil
}
