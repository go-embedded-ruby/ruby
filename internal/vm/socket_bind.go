// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"os"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the TLS half of rbgo's socket transport: it installs the real
// OpenSSL::SSL::SSLSocket (openssl.go builds the surrounding OpenSSL::SSL module
// and error tree but leaves the socket to us) over Go's crypto/tls, wrapping a
// TCPSocket's live net.Conn. Both directions are real: #connect drives the
// client handshake (the common `https` path), #accept drives the server
// handshake, and OpenSSL::SSL::SSLServer wraps a TCPServer + SSLContext so
// accept returns handshaked server SSLSockets. The certificate / verification
// surface is fleshed out too: SSLContext#cert / #key / #ca_file feed real
// crypto/tls material (server certs, client RootCAs, mutual-TLS client certs),
// VERIFY_PEER performs Go's chain + hostname verification, and #peer_cert
// returns the handshaked peer's OpenSSL::X509::Certificate. The client handshake
// sends SNI: #connect sets tls.Config.ServerName from #hostname= (defaulting to
// the peer host), so a name-based virtual host is selected — crypto/tls omits
// SNI only for a bare IP literal, as the TLS spec requires. Server-side SNI
// selection callbacks and session-resumption tuning remain deferred.

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

	// Upgrade the OpenSSL::X509::Certificate shell to a real PEM-carrying cert
	// (parse on construction, #to_pem / #subject readers); #peer_cert returns one.
	certCls := vm.augmentX509Cert()

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
		c := tls.Client(s.conn, clientTLSConfig(vm, s))
		// The handshake blocks on network I/O and, for an in-VM peer (a Ruby
		// SSLServer in another Thread), must run concurrently with it — so release
		// the GVL for its duration, as MRI's C SSL_connect does.
		var err error
		vm.threadBlock(func() { err = c.Handshake() })
		if err != nil {
			raise("OpenSSL::SSL::SSLError", "SSL_connect returned=1 errno=0 state=error: %s", err.Error())
		}
		s.tls = c
		s.r = bufio.NewReader(c)
		return self
	})
	sslSock.define("connect_nonblock", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.send(self, "connect", nil, nil)
	})

	// #accept performs the *server* TLS handshake over the wrapped connection,
	// presenting the SSLContext's certificate / key and (when VERIFY_PEER +
	// ca_file are set) requiring & verifying a client certificate. It is the
	// server dual of #connect: SSLSocket.new(accepted_tcp, ctx).accept.
	sslSock.define("accept", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return sslServerHandshake(vm, asSSLSocket(self))
	})
	sslSock.define("accept_nonblock", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.send(self, "accept", nil, nil)
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
	// peer_cert returns the handshaked peer's leaf certificate as an
	// OpenSSL::X509::Certificate, or nil before the handshake / when the peer
	// presented none (an anonymous client to a non-VERIFY_PEER server).
	sslSock.define("peer_cert", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asSSLSocket(self)
		if s.tls == nil {
			return object.NilV
		}
		st := s.tls.ConnectionState()
		if len(st.PeerCertificates) == 0 {
			return object.NilV
		}
		return newX509Cert(certCls, st.PeerCertificates[0])
	})

	// Share the connected-stream surface (read/gets/readpartial/write/print/puts/
	// recv/send/<</flush/close/closed?/eof?/setsockopt) with TCPSocket.
	installStreamIO(sslSock, func(v object.Value) streamIO { return asSSLSocket(v) })

	vm.registerSSLServer(ssl, sslSock)
}

// augmentSSLContext adds the configuration accessors the TLS handshake and
// common client setup read to OpenSSL::SSL::SSLContext (whose #new / #set_params
// shell is defined in openssl.go). Each is a simple ivar accessor; verify_mode
// is the one #connect / #accept consults, and cert / key / ca_file feed real
// crypto/tls material (server certificate, client trust roots, mutual-TLS client
// certificate — see serverTLSConfig / clientTLSConfig).
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

// clientTLSConfig builds the crypto/tls client config for #connect from the
// wrapped SSLContext. VERIFY_NONE (the bare-context default) skips verification;
// VERIFY_PEER turns on Go's chain + hostname verification, using the ca_file as
// the trust anchor when one is set (else the system roots). A cert+key pair on
// the context is presented as a client certificate for mutual TLS.
func clientTLSConfig(vm *VM, s *sslSocket) *tls.Config {
	cfg := &tls.Config{ServerName: s.hostname}
	if sslVerifyMode(s.ctx) == 0 {
		cfg.InsecureSkipVerify = true
	} else if pool := caPool(s.ctx); pool != nil {
		cfg.RootCAs = pool
	}
	if cert := clientCert(s.ctx); cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	return cfg
}

// sslServerHandshake drives the server side of the TLS handshake for #accept,
// wrapping the SSLSocket's raw connection as a tls.Server with the context's
// certificate material and recording the session on success. The handshake runs
// with the GVL released (it blocks on I/O and must run concurrently with the
// peer client, which may be another in-VM Thread).
func sslServerHandshake(vm *VM, s *sslSocket) object.Value {
	c := tls.Server(s.conn, serverTLSConfig(s.ctx))
	var err error
	vm.threadBlock(func() { err = c.Handshake() })
	if err != nil {
		raise("OpenSSL::SSL::SSLError", "SSL_accept returned=1 errno=0 state=error: %s", err.Error())
	}
	s.tls = c
	s.r = bufio.NewReader(c)
	return s
}

// serverTLSConfig builds the crypto/tls server config from an SSLContext: the
// certificate + key are required (a missing / malformed pair raises SSLError),
// and a VERIFY_PEER context with a ca_file requires & verifies a client
// certificate (mutual TLS).
func serverTLSConfig(ctx object.Value) *tls.Config {
	cert, err := tls.X509KeyPair(pemBytes(getIvar(ctx, "@cert")), pemBytes(getIvar(ctx, "@key")))
	if err != nil {
		raise("OpenSSL::SSL::SSLError", "SSL server context: %s", err.Error())
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	if sslVerifyMode(ctx) != 0 {
		if pool := caPool(ctx); pool != nil {
			cfg.ClientCAs = pool
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}
	return cfg
}

// clientCert resolves the SSLContext's cert + key into a tls.Certificate for
// mutual-TLS client authentication, or nil when either is absent (the common
// no-client-cert case). A present-but-malformed pair raises SSLError.
func clientCert(ctx object.Value) *tls.Certificate {
	c, k := getIvar(ctx, "@cert"), getIvar(ctx, "@key")
	if object.IsNil(c) || object.IsNil(k) {
		return nil
	}
	cert, err := tls.X509KeyPair(pemBytes(c), pemBytes(k))
	if err != nil {
		raise("OpenSSL::SSL::SSLError", "SSL client certificate: %s", err.Error())
	}
	return &cert
}

// pemBytes resolves a cert / key SSLContext value to its PEM bytes: a String is
// its own PEM, an OpenSSL::X509::Certificate / PKey object yields its stored
// @pem, and nil yields nil (letting the X509KeyPair caller report the gap).
func pemBytes(v object.Value) []byte {
	if object.IsNil(v) {
		return nil
	}
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	if p, ok := getIvar(v, "@pem").(*object.String); ok {
		return p.Bytes()
	}
	raise("OpenSSL::SSL::SSLError", "cannot read PEM from %s", v.Inspect())
	return nil
}

// caPool builds an x509.CertPool from the SSLContext's ca_file (a path to a PEM
// bundle), or nil when no ca_file is set. A missing / certless file raises
// SSLError so a misconfigured trust store fails loudly.
func caPool(ctx object.Value) *x509.CertPool {
	v := getIvar(ctx, "@ca_file")
	if object.IsNil(v) {
		return nil
	}
	data, err := os.ReadFile(strArg(v))
	if err != nil {
		raise("OpenSSL::SSL::SSLError", "ca_file: %s", err.Error())
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		raise("OpenSSL::SSL::SSLError", "ca_file: no certificates found in %s", strArg(v))
	}
	return pool
}

// newX509Cert wraps a parsed *x509.Certificate as an OpenSSL::X509::Certificate
// object, carrying its PEM encoding (@pem) and subject DN (@subject) for the
// #to_pem / #subject readers.
func newX509Cert(cls *RClass, c *x509.Certificate) *RObject {
	o := &RObject{class: cls, ivars: map[string]object.Value{}}
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	o.ivars["@pem"] = object.NewStringBytesEnc(pemBlock, "ASCII-8BIT")
	o.ivars["@subject"] = object.NewString(c.Subject.String())
	return o
}

// augmentX509Cert upgrades the OpenSSL::X509::Certificate shell (openssl.go
// registers it with a NotImplementedError .new) to a real PEM-carrying object:
// .new(pem) parses the certificate and stores its PEM + subject, and #to_pem /
// #to_s / #subject read them back. It returns the class so #peer_cert can mint
// instances. The qualified CertificateError is published top-level so a parse
// failure is rescuable by name.
func (vm *VM) augmentX509Cert() *RClass {
	x509ns := vm.consts["OpenSSL"].(*RClass).consts["X509"].(*RClass)
	cls := x509ns.consts["Certificate"].(*RClass)
	vm.consts["OpenSSL::X509::CertificateError"] = x509ns.consts["CertificateError"]

	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			o := &RObject{class: cls, ivars: map[string]object.Value{}}
			if len(args) > 0 && !object.IsNil(args[0]) {
				pemStr := strArg(args[0])
				block, _ := pem.Decode([]byte(pemStr))
				if block == nil {
					raise("OpenSSL::X509::CertificateError", "not enough data")
				}
				c, err := x509.ParseCertificate(block.Bytes)
				if err != nil {
					raise("OpenSSL::X509::CertificateError", "%s", err.Error())
				}
				o.ivars["@pem"] = object.NewString(pemStr)
				o.ivars["@subject"] = object.NewString(c.Subject.String())
			}
			return o
		}}
	cls.define("to_pem", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@pem")
	})
	cls.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@pem")
	})
	cls.define("subject", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@subject")
	})
	return cls
}

// sslServer is OpenSSL::SSL::SSLServer: a TCPServer paired with an SSLContext
// whose #accept accepts a raw connection then completes the server handshake,
// yielding a ready server-side SSLSocket.
type sslServer struct {
	cls     *RClass
	srv     object.Value // the wrapped TCPServer
	ctx     object.Value // the SSLContext supplying the server certificate
	sslSock *RClass      // the SSLSocket class, to wrap accepted connections
	closed  bool
}

func (s *sslServer) ToS() string     { return "#<OpenSSL::SSL::SSLServer>" }
func (s *sslServer) Inspect() string { return "#<OpenSSL::SSL::SSLServer>" }
func (s *sslServer) Truthy() bool    { return true }

// registerSSLServer installs OpenSSL::SSL::SSLServer.new(tcp_server, ctx) with
// accept / to_io / addr / listen / close / closed?, wrapping accepted TCPSockets
// as handshaked server SSLSockets.
func (vm *VM) registerSSLServer(ssl, sslSock *RClass) {
	srvCls := newClass("OpenSSL::SSL::SSLServer", vm.cObject)
	ssl.consts["SSLServer"] = srvCls

	srvCls.smethods["new"] = &Method{name: "new", owner: srvCls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 2 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
			}
			if _, ok := args[0].(*tcpServer); !ok {
				raise("TypeError", "OpenSSL::SSL::SSLServer.new expects a TCPServer")
			}
			return &sslServer{cls: srvCls, srv: args[0], ctx: args[1], sslSock: sslSock}
		}}

	srvCls.define("accept", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asSSLServer(self)
		conn := asTCPSocket(vm.send(s.srv, "accept", nil, nil))
		host, _, _ := net.SplitHostPort(conn.conn.LocalAddr().String())
		ssl := &sslSocket{cls: s.sslSock, tcp: conn, ctx: s.ctx, conn: conn.conn, hostname: host}
		// Route through SSLSocket#accept so the server handshake path is uniform.
		return vm.send(ssl, "accept", nil, nil)
	})
	srvCls.define("to_io", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return asSSLServer(self).srv
	})
	srvCls.define("addr", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.send(asSSLServer(self).srv, "addr", nil, nil)
	})
	srvCls.define("listen", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.send(asSSLServer(self).srv, "listen", args, nil)
	})
	srvCls.define("close", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		s := asSSLServer(self)
		if !s.closed {
			s.closed = true
			vm.send(s.srv, "close", nil, nil)
		}
		return object.NilV
	})
	srvCls.define("closed?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(asSSLServer(self).closed)
	})
}

// asSSLServer narrows a receiver to *sslServer, raising TypeError otherwise.
func asSSLServer(v object.Value) *sslServer {
	if s, ok := v.(*sslServer); ok {
		return s
	}
	raise("TypeError", "not an OpenSSL::SSL::SSLServer")
	return nil
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
