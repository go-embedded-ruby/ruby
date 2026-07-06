// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"crypto/tls"
	"io"
	"net"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the TLS half of rbgo's socket transport: it installs the real
// OpenSSL::SSL::SSLSocket (openssl.go builds the surrounding OpenSSL::SSL module
// and error tree but leaves the socket to us) as a client-side TLS transport
// over Go's crypto/tls, wrapping a TCPSocket's live net.Conn. Server-side TLS,
// client-certificate auth, and the
// full certificate / verification surface (peer_cert, SSLContext#cert wiring,
// hostname verification callbacks) are deferred follow-ups; the common `https`
// client path — SSLSocket.new(tcp).connect then read/write — is real.

// sslSocket is a TLS stream over a wrapped TCPSocket. It shares the connected-
// stream IO surface (read/gets/write/puts/...) with tcpSocket via streamIO, so
// only the handshake and the crypto/tls plumbing are TLS-specific.
type sslSocket struct {
	cls      *RClass
	tcp      object.Value // the wrapped TCPSocket, kept reachable and returned by #io
	ctx      object.Value // the OpenSSL::SSL::SSLContext (nil-Ruby if none given)
	conn     net.Conn     // the raw underlying connection (the TCPSocket's net.Conn)
	tls      *tls.Conn    // set on #connect
	r        *bufio.Reader
	hostname string // SNI / verification name; defaults to the peer host
	closed   bool
}

func (s *sslSocket) ToS() string     { return "#<OpenSSL::SSL::SSLSocket>" }
func (s *sslSocket) Inspect() string { return "#<OpenSSL::SSL::SSLSocket>" }
func (s *sslSocket) Truthy() bool    { return true }

// streamIO implementation: reader/writer are only valid after a successful
// #connect, so both raise SSLError when the handshake has not run yet.
func (s *sslSocket) reader() *bufio.Reader {
	if s.tls == nil {
		raise("OpenSSL::SSL::SSLError", "SSL session is not started yet")
	}
	return s.r
}

func (s *sslSocket) writer() io.Writer {
	if s.tls == nil {
		raise("OpenSSL::SSL::SSLError", "SSL session is not started yet")
	}
	return s.tls
}

func (s *sslSocket) markClosed()    { s.closed = true }
func (s *sslSocket) isClosed() bool { return s.closed }

// closeConn closes the TLS session if the handshake ran, else the raw
// connection, so closing an un-connected SSLSocket still releases the socket.
func (s *sslSocket) closeConn() error {
	if s.tls != nil {
		return s.tls.Close()
	}
	return s.conn.Close()
}

// registerSSLTransport upgrades OpenSSL::SSL::SSLSocket to a real crypto/tls
// transport and extends OpenSSL::SSL::SSLContext with the verify_mode / cert /
// key / ca_file accessors the handshake and common configuration read. It runs
// from registerSocket, after registerOpenSSL has built the OpenSSL::SSL shell.
func (vm *VM) registerSSLTransport() {
	// OpenSSL and its SSL module are always registered before registerSocket runs
	// (see the call order in builtins.go), so these lookups never fail.
	mod := vm.consts["OpenSSL"].(*RClass)
	ssl := mod.consts["SSL"].(*RClass)

	// raise() resolves an exception class by its string key in vm.consts, so the
	// qualified OpenSSL::SSL::SSLError (registered nested in openssl.go) is also
	// published at the top level, letting `raise "OpenSSL::SSL::SSLError"` here be
	// rescued as that class.
	vm.consts["OpenSSL::SSL::SSLError"] = ssl.consts["SSLError"]

	vm.augmentSSLContext(ssl.consts["SSLContext"].(*RClass))

	// Install the real SSLSocket class (openssl.go deliberately leaves the
	// SSLSocket constant to us rather than registering a stub).
	sslSock := newClass("OpenSSL::SSL::SSLSocket", vm.cObject)
	ssl.consts["SSLSocket"] = sslSock

	// OpenSSL::SSL::SSLSocket.new(io [, ctx]) wraps a connected TCPSocket. The
	// handshake is deferred to #connect, matching MRI.
	sslSock.smethods["new"] = &Method{name: "new", owner: sslSock,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			tcp, ok := args[0].(*tcpSocket)
			if !ok {
				raise("TypeError", "OpenSSL::SSL::SSLSocket.new expects a TCPSocket")
			}
			ctx := object.Value(object.NilV)
			if len(args) > 1 && !object.IsNil(args[1]) {
				ctx = args[1]
			}
			host, _, _ := net.SplitHostPort(tcp.conn.RemoteAddr().String())
			return &sslSocket{cls: sslSock, tcp: args[0], ctx: ctx, conn: tcp.conn, hostname: host}
		}}

	// #connect performs the client TLS handshake over the wrapped connection.
	sslSock.define("connect", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asSSLSocket(self)
		cfg := &tls.Config{ServerName: s.hostname}
		// verify_mode nil / VERIFY_NONE (0) means "do not verify", MRI's default
		// for a bare SSLContext; anything else turns on Go's chain + hostname
		// verification.
		if sslVerifyMode(s.ctx) == 0 {
			cfg.InsecureSkipVerify = true
		}
		c := tls.Client(s.conn, cfg)
		if err := c.Handshake(); err != nil {
			raise("OpenSSL::SSL::SSLError", "SSL_connect returned=1 errno=0 state=error: %s", err.Error())
		}
		s.tls = c
		s.r = bufio.NewReader(c)
		return self
	})
	sslSock.define("connect_nonblock", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.send(self, "connect", nil, nil)
	})

	// #io / #to_io returns the wrapped TCPSocket.
	sslSock.define("io", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return asSSLSocket(self).tcp
	})
	sslSock.define("to_io", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return asSSLSocket(self).tcp
	})
	sslSock.define("context", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return asSSLSocket(self).ctx
	})
	sslSock.define("hostname", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(asSSLSocket(self).hostname)
	})
	sslSock.define("hostname=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		asSSLSocket(self).hostname = strArg(args[0])
		return args[0]
	})
	// peer_cert is nil until the certificate surface lands (deferred follow-up).
	sslSock.define("peer_cert", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		asSSLSocket(self)
		return object.NilV
	})

	// Share the connected-stream surface (read/gets/readpartial/write/print/puts/
	// <</flush/close/closed?/eof?/setsockopt) with TCPSocket.
	installStreamIO(sslSock, func(v object.Value) streamIO { return asSSLSocket(v) })
}

// augmentSSLContext adds the configuration accessors the TLS handshake and
// common client setup read to OpenSSL::SSL::SSLContext (whose #new / #set_params
// shell is defined in openssl.go). Each is a simple ivar accessor; verify_mode
// is the one #connect consults. cert / key / ca_file are stored for inspection
// but client-certificate auth is a deferred follow-up.
func (vm *VM) augmentSSLContext(ctx *RClass) {
	accessor := func(name, ivar string) {
		ctx.define(name, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return getIvar(self, ivar)
		})
		ctx.define(name+"=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			setIvar(self, ivar, args[0])
			return args[0]
		})
	}
	accessor("verify_mode", "@verify_mode")
	accessor("cert", "@cert")
	accessor("key", "@key")
	accessor("ca_file", "@ca_file")
	accessor("ca_path", "@ca_path")
	accessor("ciphers", "@ciphers")
	accessor("options", "@options")
	accessor("min_version", "@min_version")
	accessor("max_version", "@max_version")
}

// asSSLSocket narrows a receiver to *sslSocket, raising TypeError otherwise so a
// mis-typed self surfaces as a Ruby error rather than a Go panic.
func asSSLSocket(v object.Value) *sslSocket {
	if s, ok := v.(*sslSocket); ok {
		return s
	}
	raise("TypeError", "not an OpenSSL::SSL::SSLSocket")
	return nil
}

// sslVerifyMode resolves an SSLContext's effective verify_mode as an integer: the
// @verify_mode ivar if set, else the :verify_mode entry of a set_params @params
// hash, else 0 (VERIFY_NONE) — including when no context was given at all.
func sslVerifyMode(ctx object.Value) int64 {
	if object.IsNil(ctx) {
		return 0
	}
	if v := getIvar(ctx, "@verify_mode"); !object.IsNil(v) {
		if i, ok := v.(object.Integer); ok {
			return int64(i)
		}
	}
	if p := getIvar(ctx, "@params"); !object.IsNil(p) {
		if h, ok := p.(*object.Hash); ok {
			if v, ok := h.Get(object.Symbol("verify_mode")); ok {
				if i, ok := v.(object.Integer); ok {
					return int64(i)
				}
			}
		}
	}
	return 0
}
