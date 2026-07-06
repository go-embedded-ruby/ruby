// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import (
	"bufio"
	"io"
	"net"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the UNIX-domain half of rbgo's socket transport: a real
// UNIXSocket (connected AF_UNIX stream) and UNIXServer (listening) backed by
// Go's net "unix" network. They share the connected-stream IO surface
// (read/gets/readpartial/write/print/puts/…, plus recv/send) with TCPSocket via
// streamIO, so only construction and the #path accessor are UNIX-specific.
//
// AF_UNIX is unsupported / unreliable on Windows; the whole surface is compiled
// only on non-Windows platforms (see the build tag above). The Windows build
// installs stub classes whose constructors raise a clean, rescuable error rather
// than compiling this net-"unix" code path (socket_unix_windows.go).

// unixSocket is a connected AF_UNIX stream socket. It mirrors tcpSocket (a live
// net.Conn + a buffered reader) with its own class identity and the peer path.
type unixSocket struct {
	cls    *RClass
	conn   net.Conn
	r      *bufio.Reader
	path   string
	closed bool
}

func (s *unixSocket) ToS() string        { return "#<UNIXSocket>" }
func (s *unixSocket) Inspect() string    { return "#<UNIXSocket>" }
func (s *unixSocket) Truthy() bool       { return true }
func (s *unixSocket) rubyClass() *RClass { return s.cls }

// streamIO implementation (shared read/write surface with tcpSocket).
func (s *unixSocket) reader() *bufio.Reader { return s.r }
func (s *unixSocket) writer() io.Writer     { return s.conn }
func (s *unixSocket) markClosed()           { s.closed = true }
func (s *unixSocket) isClosed() bool        { return s.closed }
func (s *unixSocket) closeConn() error      { return s.conn.Close() }

// unixServer is a listening AF_UNIX socket whose #accept returns a connected
// unixSocket.
type unixServer struct {
	cls    *RClass
	ln     net.Listener
	path   string
	closed bool
}

func (s *unixServer) ToS() string        { return "#<UNIXServer>" }
func (s *unixServer) Inspect() string    { return "#<UNIXServer>" }
func (s *unixServer) Truthy() bool       { return true }
func (s *unixServer) rubyClass() *RClass { return s.cls }

// registerUnixSockets installs UNIXSocket and UNIXServer under BasicSocket,
// matching MRI's UNIXSocket < BasicSocket and UNIXServer < UNIXSocket ancestry.
// It is called from registerSocket; the Windows build supplies a stub with the
// same signature (socket_unix_windows.go).
func (vm *VM) registerUnixSockets(basic *RClass) {
	sock := newClass("UNIXSocket", basic)
	vm.consts["UNIXSocket"] = sock
	srv := newClass("UNIXServer", sock)
	vm.consts["UNIXServer"] = srv

	// UNIXSocket.new(path) connects to a listening AF_UNIX socket at path.
	newSock := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		path := strArg(args[0])
		conn, err := net.Dial("unix", path)
		if err != nil {
			raise("SocketError", "connect: %s", err.Error())
		}
		return newUnixSocket(sock, conn, path)
	}
	sock.smethods["new"] = &Method{name: "new", owner: sock, native: newSock}
	sock.smethods["open"] = &Method{name: "open", owner: sock, native: newSock}

	installStreamIO(sock, func(v object.Value) streamIO { return asUnixSocket(v) })
	sock.define("path", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(asUnixSocket(self).path)
	})
	sock.define("addr", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArray(object.NewString("AF_UNIX"), object.NewString(asUnixSocket(self).path))
	})

	// UNIXServer.new(path) binds + listens on an AF_UNIX socket at path.
	newSrv := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 1 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
		}
		path := strArg(args[0])
		ln, err := net.Listen("unix", path)
		if err != nil {
			raise("SocketError", "bind: %s", err.Error())
		}
		return &unixServer{cls: srv, ln: ln, path: path}
	}
	srv.smethods["new"] = &Method{name: "new", owner: srv, native: newSrv}
	srv.smethods["open"] = &Method{name: "open", owner: srv, native: newSrv}

	srv.define("accept", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asUnixServer(self)
		conn, err := s.ln.Accept()
		if err != nil {
			raise("IOError", "accept: %s", err.Error())
		}
		// The accepted peer is a plain connected UNIXSocket (UNIXServer's super).
		return newUnixSocket(s.cls.super, conn, s.path)
	})
	srv.define("path", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(asUnixServer(self).path)
	})
	srv.define("addr", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArray(object.NewString("AF_UNIX"), object.NewString(asUnixServer(self).path))
	})
	srv.define("listen", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		asUnixServer(self)
		return object.IntValue(0)
	})
	srv.define("close", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asUnixServer(self)
		if !s.closed {
			s.closed = true
			s.ln.Close()
		}
		return object.NilV
	})
	srv.define("closed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(asUnixServer(self).closed)
	})
}

// newUnixSocket wraps a live AF_UNIX net.Conn as a UNIXSocket instance of cls.
func newUnixSocket(cls *RClass, conn net.Conn, path string) *unixSocket {
	return &unixSocket{cls: cls, conn: conn, r: bufio.NewReader(conn), path: path}
}

// asUnixSocket narrows a receiver to *unixSocket, raising TypeError otherwise.
func asUnixSocket(v object.Value) *unixSocket {
	if s, ok := v.(*unixSocket); ok {
		return s
	}
	raise("TypeError", "not a UNIXSocket")
	return nil
}

// asUnixServer narrows a receiver to *unixServer, raising TypeError otherwise.
func asUnixServer(v object.Value) *unixServer {
	if s, ok := v.(*unixServer); ok {
		return s
	}
	raise("TypeError", "not a UNIXServer")
	return nil
}
