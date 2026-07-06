// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"io"
	"net"
	"strconv"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is rbgo's native socket transport (require "socket"): a real
// TCPSocket / TCPServer backed by Go's net package, plus the BasicSocket /
// IPSocket / Socket ancestry MRI exposes. It is the foundational network layer
// the higher-level clients want — Net::HTTP, and the redis/pg IO seam — all sit
// on top of a stream socket, which rbgo previously lacked (clients injected an
// IO-like duck instead). The TLS layer that wraps a TCPSocket lives in
// socket_bind.go (OpenSSL::SSL::SSLSocket over crypto/tls).
//
// Ancestry note: MRI's chain is TCPSocket < IPSocket < BasicSocket < IO <
// Object. rbgo roots BasicSocket at Object rather than IO on purpose: IO's
// instance methods assume an *IOObj receiver and would panic on a socket value,
// and the socket surface below re-implements the IO methods that matter
// (read/gets/write/puts/...) directly. UDPSocket (socket_udp.go) and
// UNIXSocket/UNIXServer (socket_unix.go) extend this ancestry; raw Socket.new
// and full Addrinfo are deferred follow-ups.

// tcpSocket is a connected stream socket (TCPSocket): a live net.Conn plus a
// buffered reader so gets / readpartial / eof? can peek without losing bytes.
type tcpSocket struct {
	cls    *RClass
	conn   net.Conn
	r      *bufio.Reader
	closed bool
}

func (s *tcpSocket) ToS() string     { return "#<TCPSocket>" }
func (s *tcpSocket) Inspect() string { return "#<TCPSocket>" }
func (s *tcpSocket) Truthy() bool    { return true }

// tcpServer is a listening socket (TCPServer): a net.Listener whose #accept
// returns a connected tcpSocket.
type tcpServer struct {
	cls    *RClass
	ln     net.Listener
	closed bool
}

func (s *tcpServer) ToS() string     { return "#<TCPServer>" }
func (s *tcpServer) Inspect() string { return "#<TCPServer>" }
func (s *tcpServer) Truthy() bool    { return true }

// registerSocket installs the socket transport (require "socket"): the
// TCPSocket / TCPServer classes over Go net, the BasicSocket / IPSocket / Socket
// ancestry, the Socket address-family / type constants, and SocketError. It also
// upgrades the OpenSSL::SSL::SSLSocket shell to a real crypto/tls transport (see
// registerSSLTransport), so it must run after registerOpenSSL.
func (vm *VM) registerSocket() {
	std := vm.consts["StandardError"].(*RClass)

	// SocketError < StandardError is what a name-resolution / connect failure
	// rescues as, matching MRI.
	sockErr := newClass("SocketError", std)
	vm.consts["SocketError"] = sockErr

	// The MRI ancestry, rooted at Object (see the note above).
	basic := newClass("BasicSocket", vm.cObject)
	vm.consts["BasicSocket"] = basic
	ip := newClass("IPSocket", basic)
	vm.consts["IPSocket"] = ip
	tcp := newClass("TCPSocket", ip)
	vm.consts["TCPSocket"] = tcp
	// MRI: TCPServer < TCPSocket.
	srv := newClass("TCPServer", tcp)
	vm.consts["TCPServer"] = srv

	vm.registerTCPSocket(tcp)
	vm.registerTCPServer(srv)
	vm.registerUDPSocket(ip)
	vm.registerUnixSockets(basic)
	vm.registerSocketClass(basic)

	// Upgrade the OpenSSL::SSL TLS shell to a real transport (socket_bind.go).
	vm.registerSSLTransport()
}

// registerTCPSocket installs TCPSocket.new plus the connected-socket instance
// surface (read/gets/readpartial/write/print/puts/<</flush/close/closed?/eof?/
// setsockopt/peeraddr/local_address), all over the live net.Conn.
func (vm *VM) registerTCPSocket(tcp *RClass) {
	// TCPSocket.new(host, port) / TCPSocket.open(...) dials a TCP connection.
	// port accepts an Integer or a String (numeric or service name), as MRI does.
	newFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		host := strArg(args[0])
		port := portString(args[1])
		conn, err := net.Dial("tcp", net.JoinHostPort(host, port))
		if err != nil {
			raise("SocketError", "getaddrinfo: %s", err.Error())
		}
		return newTCPSocket(tcp, conn)
	}
	tcp.smethods["new"] = &Method{name: "new", owner: tcp, native: newFn}
	tcp.smethods["open"] = &Method{name: "open", owner: tcp, native: newFn}

	defineSocketIO(tcp, func(v object.Value) *tcpSocket { return asTCPSocket(v) })

	tcp.define("peeraddr", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return addrTuple(asTCPSocket(self).conn.RemoteAddr())
	})
	tcp.define("local_address", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return addrTuple(asTCPSocket(self).conn.LocalAddr())
	})
	tcp.define("addr", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return addrTuple(asTCPSocket(self).conn.LocalAddr())
	})
}

// registerTCPServer installs TCPServer.new + accept/close/addr/local_address/
// listen/closed? over a net.Listener.
func (vm *VM) registerTCPServer(srv *RClass) {
	// TCPServer.new([host,] port): bind + listen. One argument is the port (all
	// interfaces); two are host then port.
	newFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var host, port string
		switch len(args) {
		case 1:
			port = portString(args[0])
		case 2:
			host = strArg(args[0])
			port = portString(args[1])
		default:
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(host, port))
		if err != nil {
			raise("SocketError", "bind: %s", err.Error())
		}
		return &tcpServer{cls: srv, ln: ln}
	}
	srv.smethods["new"] = &Method{name: "new", owner: srv, native: newFn}
	srv.smethods["open"] = &Method{name: "open", owner: srv, native: newFn}

	srv.define("accept", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asTCPServer(self)
		conn, err := s.ln.Accept()
		if err != nil {
			raise("IOError", "accept: %s", err.Error())
		}
		// The accepted peer is a plain connected TCPSocket (TCPServer's super).
		return newTCPSocket(s.cls.super, conn)
	})
	srv.define("addr", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return addrTuple(asTCPServer(self).ln.Addr())
	})
	srv.define("local_address", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return addrTuple(asTCPServer(self).ln.Addr())
	})
	// listen(backlog) is a no-op: net.Listen already established the backlog.
	srv.define("listen", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		asTCPServer(self)
		return object.IntValue(0)
	})
	srv.define("close", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asTCPServer(self)
		if !s.closed {
			s.closed = true
			s.ln.Close()
		}
		return object.NilV
	})
	srv.define("closed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(asTCPServer(self).closed)
	})
}

// registerSocketClass installs the Socket class + the address-family / socket-
// type constants scripts commonly reference (AF_INET, SOCK_STREAM, ...). Raw
// Socket.new / connect / bind is a deferred follow-up (the typed TCPSocket /
// UDPSocket / UNIXSocket classes cover the common cases): Socket.new raises
// NotImplementedError so the gap is loud and rescuable.
func (vm *VM) registerSocketClass(basic *RClass) {
	sock := newClass("Socket", basic)
	vm.consts["Socket"] = sock
	for k, v := range map[string]int64{
		"AF_INET": 2, "AF_INET6": 30, "AF_UNIX": 1, "PF_INET": 2, "PF_INET6": 30,
		"SOCK_STREAM": 1, "SOCK_DGRAM": 2,
		"IPPROTO_TCP": 6, "IPPROTO_UDP": 17,
		"SOL_SOCKET": 0xffff, "SO_REUSEADDR": 4,
	} {
		sock.consts[k] = object.IntValue(v)
	}
	sock.smethods["new"] = &Method{name: "new", owner: sock,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "raw Socket.new is not yet supported (use TCPSocket/TCPServer/UDPSocket/UNIXSocket; raw Socket is a follow-up)")
		}}
}

// defineSocketIO installs the shared connected-stream IO surface on cls. get
// extracts the underlying tcpSocket from the receiver; SSLSocket reuses the same
// method shapes over its own extractor (see socket_bind.go), so the read/write
// helpers live on streamIO rather than being duplicated.
func defineSocketIO(cls *RClass, get func(object.Value) *tcpSocket) {
	io := func(self object.Value) streamIO { return get(self) }
	installStreamIO(cls, io)
}

// streamIO abstracts the two connected-stream transports (a raw tcpSocket and a
// TLS sslSocket) so read/gets/write/puts share one implementation.
type streamIO interface {
	reader() *bufio.Reader
	writer() io.Writer
	markClosed()
	isClosed() bool
	closeConn() error
}

func (s *tcpSocket) reader() *bufio.Reader { return s.r }
func (s *tcpSocket) writer() io.Writer     { return s.conn }
func (s *tcpSocket) markClosed()           { s.closed = true }
func (s *tcpSocket) isClosed() bool        { return s.closed }
func (s *tcpSocket) closeConn() error      { return s.conn.Close() }

// installStreamIO defines the connected-stream methods (read/gets/readpartial/
// write/print/puts/<</flush/close/closed?/eof?/setsockopt) on cls, resolving the
// receiver to a streamIO via get.
func installStreamIO(cls *RClass, get func(object.Value) streamIO) {
	cls.define("read", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return streamRead(get(self), args)
	})
	cls.define("gets", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return streamGets(get(self), args)
	})
	cls.define("readpartial", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return streamReadpartial(get(self), args)
	})
	cls.define("write", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return streamWrite(vm, get(self), args)
	})
	cls.define("print", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s := get(self)
		for _, a := range args {
			s.writer().Write(argBytes(vm, a))
		}
		return object.NilV
	})
	cls.define("puts", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		streamPuts(vm, get(self), args)
		return object.NilV
	})
	cls.define("<<", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		get(self).writer().Write(argBytes(vm, args[0]))
		return self
	})
	cls.define("flush", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		get(self)
		return self
	})
	cls.define("sync", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		get(self)
		return object.Bool(true)
	})
	cls.define("sync=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		get(self)
		return args[0]
	})
	cls.define("close", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := get(self)
		if !s.isClosed() {
			s.markClosed()
			s.closeConn()
		}
		return object.NilV
	})
	cls.define("closed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(get(self).isClosed())
	})
	cls.define("eof?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		_, err := get(self).reader().Peek(1)
		return object.Bool(err == io.EOF)
	})
	cls.define("eof", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		_, err := get(self).reader().Peek(1)
		return object.Bool(err == io.EOF)
	})
	// setsockopt is accepted and ignored (MRI returns 0).
	cls.define("setsockopt", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		get(self)
		return object.IntValue(0)
	})
	// recv(maxlen[, flags]) reads up to maxlen bytes from the stream, returning
	// whatever is available (an empty ASCII-8BIT String at end of stream), the
	// BasicSocket#recv datagram-less semantics for a connected stream. flags is
	// accepted and ignored.
	cls.define("recv", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return streamRecv(get(self), args)
	})
	// send(mesg[, flags[, dest]]) writes mesg to the stream and returns the byte
	// count; flags and dest are accepted and ignored for a connected stream.
	cls.define("send", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..3)")
		}
		b := argBytes(vm, args[0])
		get(self).writer().Write(b)
		return object.IntValue(int64(len(b)))
	})
}

// streamRecv implements #recv(maxlen): read up to maxlen bytes, blocking only
// when the buffer is empty, and returning an empty ASCII-8BIT String at end of
// stream (BasicSocket#recv semantics) rather than raising.
func streamRecv(s streamIO, args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	n := int(intArg(args[0]))
	if n < 0 {
		raise("ArgumentError", "negative length %d given", n)
	}
	if n == 0 {
		return object.NewStringBytesEnc(nil, "ASCII-8BIT")
	}
	buf := make([]byte, n)
	k, _ := s.reader().Read(buf)
	return object.NewStringBytesEnc(buf[:k], "ASCII-8BIT")
}

// streamRead implements #read: no argument reads to EOF (returning a possibly
// empty ASCII-8BIT String); an integer length reads up to that many bytes,
// returning nil at EOF with nothing read, exactly like MRI's IO#read.
func streamRead(s streamIO, args []object.Value) object.Value {
	if len(args) == 0 || object.IsNil(args[0]) {
		data, _ := io.ReadAll(s.reader())
		return object.NewStringBytesEnc(data, "ASCII-8BIT")
	}
	n := int(intArg(args[0]))
	if n < 0 {
		raise("ArgumentError", "negative length %d given", n)
	}
	if n == 0 {
		return object.NewStringBytesEnc(nil, "ASCII-8BIT")
	}
	buf := make([]byte, n)
	k, err := io.ReadFull(s.reader(), buf)
	if k == 0 && err == io.EOF {
		return object.NilV
	}
	return object.NewStringBytesEnc(buf[:k], "ASCII-8BIT")
}

// streamGets implements #gets: read up to and including the separator (default
// "\n"), returning nil at EOF with nothing buffered.
func streamGets(s streamIO, args []object.Value) object.Value {
	sep := "\n"
	if len(args) > 0 && !object.IsNil(args[0]) {
		sep = strArg(args[0])
	}
	data, err := readUntil(s.reader(), sep)
	if len(data) == 0 && err == io.EOF {
		return object.NilV
	}
	return object.NewString(string(data))
}

// streamReadpartial implements #readpartial(n): return whatever is immediately
// available (at least one byte), blocking only when the buffer is empty, and
// raising EOFError at end of stream, matching MRI.
func streamReadpartial(s streamIO, args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	n := int(intArg(args[0]))
	if n < 0 {
		raise("ArgumentError", "negative length %d given", n)
	}
	if n == 0 {
		return object.NewStringBytesEnc(nil, "ASCII-8BIT")
	}
	buf := make([]byte, n)
	k, err := s.reader().Read(buf)
	if k == 0 && err == io.EOF {
		raise("EOFError", "end of file reached")
	}
	return object.NewStringBytesEnc(buf[:k], "ASCII-8BIT")
}

// streamWrite implements #write(*args): write each argument's to_s and return
// the total byte count written.
func streamWrite(vm *VM, s streamIO, args []object.Value) object.Value {
	total := 0
	for _, a := range args {
		b := argBytes(vm, a)
		s.writer().Write(b)
		total += len(b)
	}
	return object.IntValue(int64(total))
}

// streamPuts implements #puts(*args): write each argument's to_s followed by a
// newline (unless it already ends in one); no arguments writes a bare newline.
func streamPuts(vm *VM, s streamIO, args []object.Value) {
	if len(args) == 0 {
		s.writer().Write([]byte("\n"))
		return
	}
	for _, a := range args {
		b := argBytes(vm, a)
		s.writer().Write(b)
		if len(b) == 0 || b[len(b)-1] != '\n' {
			s.writer().Write([]byte("\n"))
		}
	}
}

// newTCPSocket wraps a live net.Conn as a TCPSocket instance of cls.
func newTCPSocket(cls *RClass, conn net.Conn) *tcpSocket {
	return &tcpSocket{cls: cls, conn: conn, r: bufio.NewReader(conn)}
}

// asTCPSocket narrows a receiver to *tcpSocket, raising TypeError otherwise so a
// mis-typed self surfaces as a Ruby error rather than a Go panic.
func asTCPSocket(v object.Value) *tcpSocket {
	if s, ok := v.(*tcpSocket); ok {
		return s
	}
	raise("TypeError", "not a TCPSocket")
	return nil
}

// asTCPServer narrows a receiver to *tcpServer, raising TypeError otherwise.
func asTCPServer(v object.Value) *tcpServer {
	if s, ok := v.(*tcpServer); ok {
		return s
	}
	raise("TypeError", "not a TCPServer")
	return nil
}

// portString renders a port argument (Integer or String) as the string net
// wants. A non-string, non-integer value raises TypeError.
func portString(v object.Value) string {
	switch p := v.(type) {
	case object.Integer:
		return strconv.FormatInt(int64(p), 10)
	case *object.String:
		return p.Str()
	default:
		raise("TypeError", "no implicit conversion of %s into Integer", v.Inspect())
		return ""
	}
}

// readUntil reads from r up to and including the first occurrence of sep. A
// single-byte separator uses the fast bufio path; a multi-byte one scans byte by
// byte. It returns the bytes read so far alongside any terminating error (io.EOF
// when the stream ended before sep was seen).
func readUntil(r *bufio.Reader, sep string) ([]byte, error) {
	if len(sep) == 1 {
		return r.ReadBytes(sep[0])
	}
	if sep == "" {
		// An empty separator means "whole stream" in MRI (paragraph mode is not
		// modelled); read to EOF.
		return io.ReadAll(r)
	}
	var out []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return out, err
		}
		out = append(out, b)
		if len(out) >= len(sep) && string(out[len(out)-len(sep):]) == sep {
			return out, nil
		}
	}
}

// addrTuple renders a net.Addr as MRI's 4-element address array
// [family, port, host, ip]. Reverse DNS is not performed (host == ip), keeping
// address queries hermetic and free of network lookups.
func addrTuple(addr net.Addr) object.Value {
	host, portStr, err := net.SplitHostPort(addr.String())
	if err != nil {
		host, portStr = addr.String(), "0"
	}
	port, _ := strconv.Atoi(portStr)
	family := "AF_INET"
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		family = "AF_INET6"
	}
	return object.NewArray(
		object.NewString(family),
		object.IntValue(int64(port)),
		object.NewString(host),
		object.NewString(host),
	)
}

// argBytes stringifies a Ruby value through #to_s and returns its bytes, the
// common step for the write-family methods (a String is taken directly).
func argBytes(vm *VM, v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	return vm.send(v, "to_s", nil, nil).(*object.String).Bytes()
}
