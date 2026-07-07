// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	nethttp "github.com/go-ruby-net-http/net-http"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file makes Net::HTTP actually perform requests. nethttp.go builds the
// loadable shell (the class tree, the header surface, the HTTPResponse status
// subclasses) and stubs every networking method with NotImplementedError;
// registerNetHTTPTransport (run right after registerNetHTTP) replaces those stubs
// with real ones that sit on top of the socket transport (socket.go /
// socket_bind.go):
//
//	NewRequest → Request.Bytes  →  write to a TCPSocket (or a TLS SSLSocket for
//	https)  →  read the response bytes off the socket  →  ParseResponse  →  a
//	Net::HTTPResponse (code / message / body / headers).
//
// The request/response wire codec is github.com/go-ruby-net-http/net-http (the
// MRI-byte-exact HTTP/1.1 message codec); the byte I/O is rbgo's own socket
// transport. Every connection is one-shot (Connection: close), so the read
// drains to EOF: keep-alive / persistent connections, redirect following (MRI's
// Net::HTTP never auto-follows anyway), proxies, streaming bodies and read
// timeouts are flagged follow-ups. TLS defaults to VERIFY_NONE (the transport's
// SSLSocket default) so the common client path works out of the box; a
// verify_mode= of OpenSSL::SSL::VERIFY_PEER turns on Go's chain + hostname
// verification. Full certificate-store verification wiring is a follow-up.

// registerNetHTTPTransport wires the real networking surface onto the Net::HTTP
// shell. It runs after registerNetHTTP (the shell) and after registerSocket (the
// transport it dials through), both guaranteed by the call order in builtins.go.
func (vm *VM) registerNetHTTPTransport() {
	netMod := vm.consts["Net"].(*RClass)
	http := netMod.consts["HTTP"].(*RClass)

	// Publish the Net error classes at the top level so raise() (which resolves an
	// exception by its string key in vm.consts) produces the real Net::HTTPError /
	// Net::HTTPBadResponse class a `rescue Net::HTTPBadResponse` can catch.
	vm.consts["Net::HTTPError"] = netMod.consts["HTTPError"]
	vm.consts["Net::HTTPBadResponse"] = netMod.consts["HTTPBadResponse"]
	// Publish the timeout classes too, so a raise("Net::ReadTimeout", ...) from the
	// transport resolves the real class a `rescue Net::ReadTimeout` catches.
	vm.consts["Net::OpenTimeout"] = netMod.consts["OpenTimeout"]
	vm.consts["Net::ReadTimeout"] = netMod.consts["ReadTimeout"]
	vm.consts["Net::WriteTimeout"] = netMod.consts["WriteTimeout"]

	vm.registerNetHTTPClassMethods(http)
	vm.registerNetHTTPInstanceMethods(http)
	vm.registerNetHTTPRequestClasses(http)
	vm.augmentNetHTTPResponse(netMod.consts["HTTPResponse"].(*RClass))
}

// registerNetHTTPClassMethods installs the class-level conveniences
// Net::HTTP.get / get_response / post / post_form / start, replacing the
// NotImplementedError stubs from nethttp.go.
func (vm *VM) registerNetHTTPClassMethods(http *RClass) {
	sm := func(name string, fn NativeFn) {
		http.smethods[name] = &Method{name: name, owner: http, native: fn}
	}

	// Net::HTTP.get(uri) / get(host, path[, port]) → the response body String.
	sm("get", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		u := vm.nethttpGetURI(args)
		resp := vm.nethttpExecURL(u, "GET", nil, nil)
		if b := getIvar(resp, "@body"); !object.IsNil(b) {
			return b
		}
		return object.NewString("")
	})

	// Net::HTTP.get_response(uri) / (host, path[, port]) → a Net::HTTPResponse.
	sm("get_response", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		u := vm.nethttpGetURI(args)
		return vm.nethttpExecURL(u, "GET", nil, nil)
	})

	// Net::HTTP.post(uri, data[, header]) → a Net::HTTPResponse.
	sm("post", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
		}
		u := vm.nethttpParseURI(args[0])
		body := argBytes(vm, args[1])
		return vm.nethttpExecURL(u, "POST", body, vm.headersFromArg(args, 2))
	})

	// Net::HTTP.post_form(uri, params) → POST an x-www-form-urlencoded body.
	sm("post_form", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		u := vm.nethttpParseURI(args[0])
		form := vm.hashToPairs(args[1])
		body := []byte(nethttp.EncodeWWWForm(form))
		hdr := [][2]string{{"content-type", "application/x-www-form-urlencoded"}}
		return vm.nethttpExecURL(u, "POST", body, hdr)
	})

	// Net::HTTP.start(host[, port]) [{ |http| ... }] → yields a started instance
	// and returns the block's value (or the instance when no block is given).
	sm("start", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		inst := vm.nethttpNewInstance(http, args)
		setIvar(inst, "@started", object.Bool(true))
		if blk != nil {
			res := vm.callBlock(blk, []object.Value{inst})
			vm.nethttpFinish(inst)
			return res
		}
		return inst
	})
}

// registerNetHTTPInstanceMethods installs Net::HTTP.new plus the instance surface
// (configuration accessors, start/finish, the verb helpers and #request).
func (vm *VM) registerNetHTTPInstanceMethods(http *RClass) {
	http.smethods["new"] = &Method{name: "new", owner: http,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.nethttpNewInstance(http, args)
		}}

	http.define("address", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@address")
	})
	http.define("port", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@port")
	})
	http.define("use_ssl=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		setIvar(self, "@use_ssl", object.Bool(args[0].Truthy()))
		return args[0]
	})
	http.define("use_ssl?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(getIvar(self, "@use_ssl").Truthy())
	})
	http.define("verify_mode=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		setIvar(self, "@verify_mode", args[0])
		return args[0]
	})
	http.define("verify_mode", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@verify_mode")
	})
	http.define("started?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(getIvar(self, "@started").Truthy())
	})
	http.define("start", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		setIvar(self, "@started", object.Bool(true))
		if blk != nil {
			res := vm.callBlock(blk, []object.Value{self})
			vm.nethttpFinish(self)
			return res
		}
		return self
	})
	http.define("finish", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.nethttpFinish(self)
		return object.NilV
	})

	// --- timeout accessors (open/read/write); values are seconds (Integer/Float),
	// nil disables the deadline. MRI defaults each to 60. -----------------------
	for _, name := range []string{"open_timeout", "read_timeout", "write_timeout"} {
		ivar := "@" + name
		http.define(name, func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return getIvar(self, ivar)
		})
		http.define(name+"=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			setIvar(self, ivar, args[0])
			return args[0]
		})
	}

	// --- proxy accessors --------------------------------------------------------
	// Each reflects the *effective* proxy: an explicitly-configured one, or — when
	// the instance was built with :ENV (MRI's default) — the environment proxy
	// resolved against the instance's current scheme (@use_ssl) and target host.
	http.define("proxy?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		addr, _, _, _ := nethttpEffectiveProxy(self)
		return object.Bool(!object.IsNil(addr))
	})
	http.define("proxy_address", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		addr, _, _, _ := nethttpEffectiveProxy(self)
		return addr
	})
	http.define("proxy_port", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		_, port, _, _ := nethttpEffectiveProxy(self)
		return port
	})
	http.define("proxy_user", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		_, _, user, _ := nethttpEffectiveProxy(self)
		return user
	})
	http.define("proxy_pass", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		_, _, _, pass := nethttpEffectiveProxy(self)
		return pass
	})

	// Instance verb helpers: get/head/delete/options take (path[, header]); the
	// body-carrying verbs post/put/patch take (path, data[, header]).
	for _, verb := range []string{"get", "head", "post", "put", "delete", "patch", "options"} {
		method := strings.ToUpper(verb)
		hasBody := method == "POST" || method == "PUT" || method == "PATCH"
		http.define(verb, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			path := "/"
			if len(args) > 0 && !object.IsNil(args[0]) {
				path = strArg(args[0])
			}
			var body []byte
			hdrIdx := 1
			if hasBody {
				if len(args) > 1 && !object.IsNil(args[1]) {
					body = argBytes(vm, args[1])
				}
				hdrIdx = 2
			}
			return vm.nethttpExecInstance(self, method, path, body, vm.headersFromArg(args, hdrIdx))
		})
	}

	// #request(req[, body]) executes a Net::HTTP::Get/Post/... request object and
	// yields the response to a block if given, returning it either way.
	http.define("request", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		req := args[0]
		method := strArg(vm.send(req, "method", nil, nil))
		path := strArg(getIvar(req, "@path"))
		var body []byte
		if b := getIvar(req, "@body"); !object.IsNil(b) {
			body = argBytes(vm, b)
		} else if len(args) > 1 && !object.IsNil(args[1]) {
			body = argBytes(vm, args[1])
		}
		hdr := vm.hashToPairs(getIvar(req, "@header"))
		resp := vm.nethttpExecInstance(self, method, path, body, hdr)
		if blk != nil {
			vm.callBlock(blk, []object.Value{resp})
		}
		return resp
	})
}

// registerNetHTTPRequestClasses re-defines each Net::HTTP::<Verb> request class
// so #new records the request-target path and the class exposes #method / #path /
// #body / #body= (the shell's #new only seeded @header).
func (vm *VM) registerNetHTTPRequestClasses(http *RClass) {
	for _, verb := range []string{"Get", "Head", "Post", "Put", "Delete", "Patch", "Options"} {
		rc := http.consts[verb].(*RClass)
		method := strings.ToUpper(verb)
		rc.smethods["new"] = &Method{name: "new", owner: rc,
			native: func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
				o := &RObject{class: self.(*RClass), ivars: map[string]object.Value{}}
				o.ivars["@header"] = object.NewHash()
				path := "/"
				if len(args) > 0 && !object.IsNil(args[0]) {
					path = strArg(args[0])
				}
				o.ivars["@path"] = object.NewString(path)
				return o
			}}
		rc.define("method", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NewString(method)
		})
		rc.define("path", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return getIvar(self, "@path")
		})
		rc.define("body", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return getIvar(self, "@body")
		})
		rc.define("body=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			setIvar(self, "@body", args[0])
			return args[0]
		})
		// set_form_data(hash) seeds an x-www-form-urlencoded body + Content-Type.
		rc.define("set_form_data", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			form := vm.hashToPairs(args[0])
			setIvar(self, "@body", object.NewString(nethttp.EncodeWWWForm(form)))
			hashOf := getIvar(self, "@header").(*object.Hash)
			hashOf.Set(object.NewString("content-type"), object.NewString("application/x-www-form-urlencoded"))
			return args[0]
		})
	}
}

// augmentNetHTTPResponse adds the response-side accessors the parsed response
// carries beyond the shell's code/message/body: http_version, read_body, and
// each_header over the @header map.
func (vm *VM) augmentNetHTTPResponse(resp *RClass) {
	resp.define("http_version", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@http_version")
	})
	resp.define("read_body", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@body")
	})
	resp.define("each_header", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		h, ok := getIvar(self, "@header").(*object.Hash)
		if ok && blk != nil {
			for _, k := range h.Keys {
				v, _ := h.Get(k)
				vm.callBlock(blk, []object.Value{k, v})
			}
		}
		return self
	})
	resp.define("content_type", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h, ok := getIvar(self, "@header").(*object.Hash)
		if !ok {
			return object.NilV
		}
		if v, ok := h.Get(object.NewString("content-type")); ok {
			return v
		}
		return object.NilV
	})
}

// --- request execution ------------------------------------------------------

// nethttpExecURL runs a one-shot request against a parsed URL, resolving the
// scheme / host / port and defaulting TLS verification off (the class-level
// conveniences take no context). It routes through the same transport as instance
// requests, as a non-persistent (Connection: close), un-proxied, deadline-less
// transfer. Returns the Net::HTTPResponse.
func (vm *VM) nethttpExecURL(u *url.URL, method string, body []byte, hdr [][2]string) object.Value {
	scheme, port := nethttpSchemePort(u)
	host := u.Hostname()
	cfg := &nethttpXfer{
		scheme: scheme, host: host, port: port, hostHdr: u.Host,
		dialHost: host, dialPort: port,
	}
	return vm.nethttpDoXfer(cfg, method, u.RequestURI(), body, hdr)
}

// nethttpSchemePort resolves a URL's scheme (defaulting to http) and port
// (defaulting to the scheme's well-known port when the authority omits one).
func nethttpSchemePort(u *url.URL) (scheme, port string) {
	scheme = u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	port = u.Port()
	if port == "" {
		port = "80"
		if scheme == "https" {
			port = "443"
		}
	}
	return scheme, port
}

// nethttpExecInstance runs a request using a Net::HTTP instance's configured
// address / port / use_ssl / verify_mode / timeouts / proxy. Within a start block
// (@started) it keeps one connection alive across requests; otherwise it opens
// and closes a connection per request.
func (vm *VM) nethttpExecInstance(inst object.Value, method, path string, body []byte, hdr [][2]string) object.Value {
	host := strArg(getIvar(inst, "@address"))
	port := intArg(getIvar(inst, "@port"))
	scheme := "http"
	if getIvar(inst, "@use_ssl").Truthy() {
		scheme = "https"
	}
	portStr := strconv.FormatInt(port, 10)
	hostHdr := host
	if (scheme == "http" && port != 80) || (scheme == "https" && port != 443) {
		hostHdr = net.JoinHostPort(host, portStr)
	}
	var verify int64
	if v, ok := getIvar(inst, "@verify_mode").(object.Integer); ok {
		verify = int64(v)
	}
	cfg := &nethttpXfer{
		scheme: scheme, host: host, port: portStr, hostHdr: hostHdr,
		verifyMode: verify,
		openTO:     nethttpDuration(getIvar(inst, "@open_timeout")),
		readTO:     nethttpDuration(getIvar(inst, "@read_timeout")),
		writeTO:    nethttpDuration(getIvar(inst, "@write_timeout")),
	}
	cfg.dialHost, cfg.dialPort = host, portStr
	if pa, pp, pu, ppw := nethttpEffectiveProxy(inst); !object.IsNil(pa) {
		cfg.proxied = true
		cfg.dialHost = strArg(pa)
		cfg.dialPort = portString(pp)
		cfg.connectTunnel = scheme == "https"
		if !object.IsNil(pu) {
			cfg.proxyAuth = basicAuthHeader(strArg(pu), strArg(ppw))
		}
	}
	if getIvar(inst, "@started").Truthy() {
		cfg.inst = inst
	}
	return vm.nethttpDoXfer(cfg, method, path, body, hdr)
}

// nethttpXfer carries the resolved per-request transport configuration for an
// instance request: the final target (scheme/host/port/hostHdr), the TLS verify
// mode, the actual dial endpoint (dialHost/dialPort — the proxy when proxied),
// the proxy shape, the socket deadlines and, when set, the started instance whose
// connection is reused across requests (keep-alive).
type nethttpXfer struct {
	scheme, host, port, hostHdr string
	verifyMode                  int64
	dialHost, dialPort          string
	proxied                     bool   // plain-http proxy: absolute-form request-line
	connectTunnel               bool   // https-via-proxy: CONNECT then TLS through the tunnel
	proxyAuth                   string // Proxy-Authorization value, or "" for none
	openTO, readTO, writeTO     time.Duration
	inst                        object.Value // non-nil ⇒ persistent (keep-alive) on this instance
}

// nethttpDoXfer runs one instance request over the configured transport. Within a
// start block it reuses (or opens and caches) a persistent connection and frames
// exactly one response so the next request reads at the right offset; outside a
// start block it opens a fresh connection, reads one response and closes.
func (vm *VM) nethttpDoXfer(cfg *nethttpXfer, method, path string, body []byte, hdr [][2]string) object.Value {
	reqBytes, noBody := vm.nethttpBuildRequest(cfg, method, path, body, hdr, cfg.inst == nil)

	persistent := cfg.inst != nil
	// Reuse a cached connection once; if the write/read fails (e.g. the server
	// dropped an idle keep-alive), redial and retry a single time.
	var raw []byte
	var err error
	var keepAlive bool
	reused := false
	if persistent {
		if s := nethttpGetConn(cfg.inst); s != nil {
			reused = true
			raw, keepAlive, _, err = vm.nethttpExchangeFramed(cfg, s, reqBytes, noBody)
			if err != nil {
				nethttpDropConn(cfg.inst)
				reused = false
			}
		}
	}
	if !reused {
		stream, derr := vm.nethttpDialXfer(cfg)
		if derr != nil {
			vm.raiseTransportErr(derr, "open")
		}
		var phase string
		raw, keepAlive, phase, err = vm.nethttpExchangeFramed(cfg, stream, reqBytes, noBody)
		if err != nil {
			stream.closeConn()
			vm.raiseTransportErr(err, phase)
		}
		if persistent {
			nethttpSetConn(cfg.inst, stream)
		} else {
			stream.closeConn()
		}
	}
	// A server "Connection: close" (or an unframed body read to EOF) means the
	// cached connection is spent; drop it so the next request redials.
	if persistent && !keepAlive {
		nethttpDropConn(cfg.inst)
	}

	// A no-body response (HEAD etc.) still carries Content-Length / Transfer-Encoding
	// headers but no body bytes: strip that framing so ParseResponse frames an empty
	// body instead of trying to read one, mirroring the one-shot path.
	if noBody {
		raw = trimResponseToHeaders(raw)
	}
	resp, perr := nethttp.ParseResponse(raw)
	if perr != nil {
		raise("Net::HTTPBadResponse", "%s", perr.Error())
	}
	respVal := vm.nethttpBuildResponse(resp)
	if noBody {
		setIvar(respVal, "@body", object.NilV)
	}
	return respVal
}

// nethttpBuildRequest builds the request bytes for an instance transfer. For a
// plain-http proxy the request-target is the absolute URI (the proxy routes on
// it); otherwise it is the origin-form path. close asks the server to close the
// connection (used for one-shot, non-persistent requests). It returns the bytes
// and whether the response carries no body (HEAD etc.).
func (vm *VM) nethttpBuildRequest(cfg *nethttpXfer, method, path string, body []byte, hdr [][2]string, close bool) ([]byte, bool) {
	target := path
	if cfg.proxied && !cfg.connectTunnel {
		target = cfg.scheme + "://" + cfg.hostHdr + path
	}
	req, err := nethttp.NewRequest(method, target, cfg.hostHdr, hdr)
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	if body != nil {
		req.SetBody(body)
	}
	if cfg.proxied && !cfg.connectTunnel && cfg.proxyAuth != "" {
		req.Set("Proxy-Authorization", cfg.proxyAuth)
	}
	if close {
		req.Set("Connection", "close")
	}
	reqBytes, err := req.Bytes("1.1")
	if err != nil {
		raise("Net::HTTPError", "%s", err.Error())
	}
	return reqBytes, !req.ResponseBodyPermitted()
}

// nethttpExchangeFramed writes the request (under the write deadline) and reads
// exactly one framed response (under the read deadline) off the stream, leaving
// any following bytes buffered for the next keep-alive request. keepAlive reports
// whether the connection may be reused (false ⇒ the server asked to close or the
// body was unframed and read to EOF). phase names which half failed ("write" /
// "read") so a deadline timeout maps to Net::WriteTimeout vs Net::ReadTimeout.
func (vm *VM) nethttpExchangeFramed(cfg *nethttpXfer, s streamIO, reqBytes []byte, noBody bool) (raw []byte, keepAlive bool, phase string, err error) {
	nethttpSetDeadline(s, cfg.writeTO)
	if _, werr := s.writer().Write(reqBytes); werr != nil {
		return nil, false, "write", werr
	}
	nethttpSetDeadline(s, cfg.readTO)
	raw, keepAlive, err = nethttpReadResponse(s.reader(), noBody)
	return raw, keepAlive, "read", err
}

// nethttpDialXfer opens the transport for a transfer: a direct dial, a plain-http
// proxy dial (a bare TCP socket to the proxy — the absolute-URI request-line does
// the routing), or an https-via-proxy CONNECT tunnel (dial proxy, CONNECT, then
// TLS through the tunnel to the real host).
func (vm *VM) nethttpDialXfer(cfg *nethttpXfer) (streamIO, error) {
	if !cfg.proxied {
		return nethttpDialTimeout(cfg.scheme, cfg.host, cfg.dialHost, cfg.dialPort, cfg.verifyMode, cfg.openTO)
	}
	if !cfg.connectTunnel {
		// Plain http through a proxy: a bare TCP socket to the proxy.
		conn, err := nethttpRawDial(cfg.dialHost, cfg.dialPort, cfg.openTO)
		if err != nil {
			return nil, err
		}
		return newTCPSocket(nil, conn), nil
	}
	// https through a proxy: CONNECT then TLS to the real host over the tunnel.
	conn, err := nethttpRawDial(cfg.dialHost, cfg.dialPort, cfg.openTO)
	if err != nil {
		return nil, err
	}
	// CONNECT always names an explicit host:port authority (hostHdr omits the port
	// for a default-port target, which CONNECT does not permit).
	if err := nethttpProxyConnect(conn, net.JoinHostPort(cfg.host, cfg.port), cfg.proxyAuth); err != nil {
		conn.Close()
		return nil, err
	}
	return nethttpTLSWrap(conn, cfg.host, cfg.verifyMode)
}

// nethttpProxyConnect sends a CONNECT request for target through conn and reads
// the proxy's response, erroring unless it is a 2xx. It reads only up to the
// response's header terminator (byte at a time) so no tunnelled TLS bytes are
// consumed.
func nethttpProxyConnect(conn net.Conn, target, proxyAuth string) error {
	var b strings.Builder
	b.WriteString("CONNECT " + target + " HTTP/1.1\r\n")
	b.WriteString("Host: " + target + "\r\n")
	if proxyAuth != "" {
		b.WriteString("Proxy-Authorization: " + proxyAuth + "\r\n")
	}
	b.WriteString("\r\n")
	if _, err := conn.Write([]byte(b.String())); err != nil {
		return err
	}
	// Read the response headers a byte at a time up to CRLFCRLF.
	var resp []byte
	buf := make([]byte, 1)
	for !bytes.HasSuffix(resp, []byte("\r\n\r\n")) {
		n, err := conn.Read(buf)
		if n > 0 {
			resp = append(resp, buf[0])
		}
		if err != nil {
			return err
		}
	}
	line := string(resp)
	if i := strings.Index(line, "\r\n"); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "2") {
		return fmt.Errorf("proxy CONNECT failed: %s", strings.TrimSpace(line))
	}
	return nil
}

// nethttpFinish closes a started instance's cached keep-alive connection (if any)
// and marks it un-started. It backs both Net::HTTP#finish and the start-block
// teardown.
func (vm *VM) nethttpFinish(inst object.Value) {
	nethttpDropConn(inst)
	setIvar(inst, "@started", object.Bool(false))
}

// nethttpGetConn returns the instance's cached persistent connection, or nil.
func nethttpGetConn(inst object.Value) streamIO {
	if s, ok := getIvar(inst, "@conn").(streamIO); ok {
		return s
	}
	return nil
}

// nethttpSetConn caches a persistent connection on the instance.
func nethttpSetConn(inst object.Value, s streamIO) {
	setIvar(inst, "@conn", s.(object.Value))
}

// nethttpDropConn closes and clears the instance's cached connection.
func nethttpDropConn(inst object.Value) {
	if s := nethttpGetConn(inst); s != nil {
		s.closeConn()
	}
	setIvar(inst, "@conn", object.NilV)
}

// nethttpDuration converts a Ruby timeout (Integer/Float seconds) to a Duration;
// a nil or non-positive value yields 0 (no deadline).
func nethttpDuration(v object.Value) time.Duration {
	var secs float64
	switch n := v.(type) {
	case object.Integer:
		secs = float64(n)
	case object.Float:
		secs = float64(n)
	default:
		return 0
	}
	if secs <= 0 {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

// nethttpSetDeadline applies a relative deadline to a stream's underlying socket
// (0 clears it). It is a no-op for a stream with no net.Conn (e.g. a test fake).
func nethttpSetDeadline(s streamIO, d time.Duration) {
	conn := nethttpNetConn(s)
	if conn == nil {
		return
	}
	if d <= 0 {
		conn.SetDeadline(time.Time{})
		return
	}
	conn.SetDeadline(time.Now().Add(d))
}

// nethttpNetConn extracts the underlying net.Conn from the two real stream
// transports (tcpSocket / sslSocket); anything else (a test fake) yields nil.
func nethttpNetConn(s streamIO) net.Conn {
	switch t := s.(type) {
	case *tcpSocket:
		return t.conn
	case *sslSocket:
		return t.conn
	default:
		return nil
	}
}

// raiseTransportErr maps a transport error to the MRI exception: an i/o timeout
// during open/read/write raises Net::OpenTimeout / Net::ReadTimeout /
// Net::WriteTimeout; anything else raises SocketError.
func (vm *VM) raiseTransportErr(err error, phase string) {
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		switch phase {
		case "open":
			raise("Net::OpenTimeout", "execution expired")
		case "write":
			raise("Net::WriteTimeout", "execution expired")
		default:
			raise("Net::ReadTimeout", "execution expired")
		}
	}
	raise("SocketError", "%s", err.Error())
}

// basicAuthHeader builds an HTTP Basic credential value for Proxy-Authorization.
func basicAuthHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// nethttpRawDial opens a raw TCP connection with an optional dial timeout
// (open_timeout); 0 means no bound.
func nethttpRawDial(host, port string, openTO time.Duration) (net.Conn, error) {
	addr := net.JoinHostPort(host, port)
	if openTO > 0 {
		return net.DialTimeout("tcp", addr, openTO)
	}
	return net.Dial("tcp", addr)
}

// nethttpDialTimeout is nethttpDial with an open_timeout on the TCP dial and the
// TLS handshake, returning the connected stream (raw for http, TLS for https).
func nethttpDialTimeout(scheme, host, dialHost, dialPort string, verifyMode int64, openTO time.Duration) (streamIO, error) {
	conn, err := nethttpRawDial(dialHost, dialPort, openTO)
	if err != nil {
		return nil, err
	}
	if scheme != "https" {
		return newTCPSocket(nil, conn), nil
	}
	if openTO > 0 {
		conn.SetDeadline(time.Now().Add(openTO))
	}
	s, err := nethttpTLSWrap(conn, host, verifyMode)
	if openTO > 0 && err == nil {
		conn.SetDeadline(time.Time{})
	}
	return s, err
}

// nethttpTLSWrap performs the TLS handshake over an already-connected socket and
// returns the sslSocket stream. verifyMode 0 (VERIFY_NONE) skips verification.
func nethttpTLSWrap(conn net.Conn, host string, verifyMode int64) (streamIO, error) {
	tcp := newTCPSocket(nil, conn)
	cfg := &tls.Config{ServerName: host}
	if verifyMode == 0 {
		cfg.InsecureSkipVerify = true
	}
	c := tls.Client(conn, cfg)
	if err := c.Handshake(); err != nil {
		conn.Close()
		return nil, err
	}
	return &sslSocket{tcp: tcp, ctx: object.NilV, conn: conn, tls: c, r: bufio.NewReader(c), hostname: host}, nil
}

// nethttpReadResponse reads exactly one HTTP/1.1 response off r — the status line,
// the header block, and the body framed by Transfer-Encoding: chunked or
// Content-Length (or read to EOF when unframed) — returning the raw bytes to hand
// to nethttp.ParseResponse. keepAlive reports whether the connection may be reused
// (false when the server sent Connection: close or the body was read to EOF).
// noBody (HEAD and other body-forbidden responses) stops after the header block.
func nethttpReadResponse(r *bufio.Reader, noBody bool) (raw []byte, keepAlive bool, err error) {
	var buf bytes.Buffer
	status, err := r.ReadString('\n')
	if err != nil {
		return nil, false, err
	}
	buf.WriteString(status)
	headers := map[string]string{}
	for {
		line, lerr := r.ReadString('\n')
		if lerr != nil {
			return nil, false, lerr
		}
		buf.WriteString(line)
		if strings.TrimRight(line, "\r\n") == "" {
			break
		}
		if i := strings.IndexByte(line, ':'); i >= 0 {
			k := strings.ToLower(strings.TrimSpace(line[:i]))
			v := strings.TrimSpace(line[i+1:])
			if prev, ok := headers[k]; ok {
				headers[k] = prev + ", " + v
			} else {
				headers[k] = v
			}
		}
	}
	keepAlive = !strings.EqualFold(headers["connection"], "close")
	if noBody {
		return buf.Bytes(), keepAlive, nil
	}
	if te, ok := headers["transfer-encoding"]; ok && nethttpIsChunked(te) {
		if cerr := nethttpCopyChunked(r, &buf); cerr != nil {
			return nil, false, cerr
		}
		return buf.Bytes(), keepAlive, nil
	}
	if cl, ok := headers["content-length"]; ok {
		n, cerr := strconv.Atoi(strings.TrimSpace(cl))
		if cerr != nil || n < 0 {
			return nil, false, fmt.Errorf("wrong Content-Length: %q", cl)
		}
		if _, cerr := io.CopyN(&buf, r, int64(n)); cerr != nil {
			return nil, false, cerr
		}
		return buf.Bytes(), keepAlive, nil
	}
	// No framing: read to EOF. The connection is spent afterwards.
	rest, rerr := io.ReadAll(r)
	if rerr != nil {
		return nil, false, rerr
	}
	buf.Write(rest)
	return buf.Bytes(), false, nil
}

// nethttpCopyChunked copies a chunked body (raw, including the chunk framing) from
// r into buf until the zero-size chunk, then consumes the trailer up to a blank
// line — the same framing nethttp.ParseResponse later decodes.
func nethttpCopyChunked(r *bufio.Reader, buf *bytes.Buffer) error {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		buf.WriteString(line)
		hexlen := nethttpFirstHexRun(line)
		if hexlen == "" {
			return fmt.Errorf("wrong chunk size line: %q", line)
		}
		n, err := strconv.ParseInt(hexlen, 16, 64)
		if err != nil {
			return fmt.Errorf("wrong chunk size line: %q", line)
		}
		if n == 0 {
			break
		}
		if _, err := io.CopyN(buf, r, n); err != nil {
			return err
		}
		if _, err := io.CopyN(buf, r, 2); err != nil { // trailing CRLF
			return err
		}
	}
	for { // trailer lines until a blank one (or EOF)
		line, err := r.ReadString('\n')
		buf.WriteString(line)
		if err != nil || strings.TrimRight(line, "\r\n") == "" {
			break
		}
	}
	return nil
}

// nethttpIsChunked reports whether a Transfer-Encoding field requests chunked
// framing (case-insensitive substring, as MRI's chunked? does).
func nethttpIsChunked(te string) bool {
	return strings.Contains(strings.ToLower(te), "chunked")
}

// nethttpFirstHexRun returns the first maximal run of hex digits in a chunk-size
// line (the codec's firstHexRun), "" when there is none.
func nethttpFirstHexRun(line string) string {
	start := -1
	for i := 0; i < len(line); i++ {
		c := line[i]
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if isHex {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			return line[start:i]
		}
	}
	if start >= 0 {
		return line[start:]
	}
	return ""
}

// trimResponseToHeaders keeps only the status line and header block of a raw
// response, dropping the body and any Content-Length / Transfer-Encoding fields,
// so ParseResponse frames an empty body (used for HEAD, whose response carries
// those headers but no body bytes).
func trimResponseToHeaders(raw []byte) []byte {
	i := bytes.Index(raw, []byte("\r\n\r\n"))
	if i < 0 {
		return raw
	}
	var kept [][]byte
	for _, ln := range bytes.Split(raw[:i], []byte("\r\n")) {
		low := bytes.ToLower(ln)
		if bytes.HasPrefix(low, []byte("content-length:")) || bytes.HasPrefix(low, []byte("transfer-encoding:")) {
			continue
		}
		kept = append(kept, ln)
	}
	return append(bytes.Join(kept, []byte("\r\n")), []byte("\r\n\r\n")...)
}

// nethttpBuildResponse turns a parsed *nethttp.Response into a Ruby
// Net::HTTPResponse of the subclass MRI would have instantiated (e.g.
// Net::HTTPOK), carrying @code / @message / @body / @header / @http_version.
func (vm *VM) nethttpBuildResponse(resp *nethttp.Response) object.Value {
	netMod := vm.consts["Net"].(*RClass)
	o := &RObject{class: vm.nethttpResponseClass(netMod, resp.Class(), resp.Category()),
		ivars: map[string]object.Value{}}
	o.ivars["@code"] = object.NewString(resp.Code())
	o.ivars["@message"] = object.NewString(resp.Message())
	if b := resp.Body(); b != nil {
		o.ivars["@body"] = object.NewStringBytes(b)
	} else {
		o.ivars["@body"] = object.NilV
	}
	o.ivars["@http_version"] = object.NewString(resp.HTTPVersion())
	h := object.NewHash()
	resp.EachHeader(func(k, v string) {
		// EachHeader already emits downcased keys joined per field, matching the
		// downcased-key convention Net::HTTPHeader#[] looks up under.
		h.Set(object.NewString(k), object.NewString(v))
	})
	o.ivars["@header"] = h
	return o
}

// nethttpResponseClass resolves the parsed response's subclass name to a
// registered Net:: class, falling back to its category then Net::HTTPResponse for
// a status code with no dedicated class.
func (vm *VM) nethttpResponseClass(netMod *RClass, class, category string) *RClass {
	if c, ok := netMod.consts[class].(*RClass); ok {
		return c
	}
	if c, ok := netMod.consts[category].(*RClass); ok {
		return c
	}
	return netMod.consts["HTTPResponse"].(*RClass)
}

// --- argument helpers -------------------------------------------------------

// nethttpNewInstance builds a Net::HTTP instance from
// (address[, port[, p_addr[, p_port[, p_user[, p_pass]]]]]), defaulting the port
// to 80, use_ssl to false and each timeout to 60s (MRI's defaults). The p_addr
// argument selects the proxy policy, mirroring MRI:
//   - absent, or the :ENV symbol (MRI's default) → resolve the proxy from the
//     environment at request time (http_proxy/https_proxy, honoring no_proxy);
//   - nil (or false) → no proxy, even if the environment sets one;
//   - an explicit host String → that fixed proxy (with p_port/p_user/p_pass).
//
// Environment resolution is deferred to nethttpEffectiveProxy because it depends
// on the instance's scheme (@use_ssl, settable after .new) and target host.
func (vm *VM) nethttpNewInstance(http *RClass, args []object.Value) object.Value {
	o := &RObject{class: http, ivars: map[string]object.Value{}}
	host := ""
	if len(args) > 0 && !object.IsNil(args[0]) {
		host = strArg(args[0])
	}
	o.ivars["@address"] = object.NewString(host)
	port := int64(80)
	if len(args) > 1 && !object.IsNil(args[1]) {
		port = intArg(args[1])
	}
	o.ivars["@port"] = object.IntValue(port)
	o.ivars["@use_ssl"] = object.Bool(false)
	o.ivars["@header"] = object.NewHash()
	// MRI defaults: 60s open/read/write timeouts.
	o.ivars["@open_timeout"] = object.IntValue(60)
	o.ivars["@read_timeout"] = object.IntValue(60)
	o.ivars["@write_timeout"] = object.IntValue(60)
	// Proxy (p_addr, p_port, p_user, p_pass). An explicit host String pins a fixed
	// proxy; the :ENV symbol (or an omitted p_addr) selects environment resolution
	// via @proxy_from_env; nil/false disables the proxy outright.
	o.ivars["@proxy_address"] = object.NilV
	o.ivars["@proxy_port"] = object.NilV
	o.ivars["@proxy_user"] = object.NilV
	o.ivars["@proxy_pass"] = object.NilV
	o.ivars["@proxy_from_env"] = object.Bool(len(args) <= 2 || nethttpIsENV(args[2]))
	if len(args) > 2 {
		if ps, ok := args[2].(*object.String); ok && ps.Str() != "" {
			o.ivars["@proxy_address"] = args[2]
			pp := int64(80)
			if len(args) > 3 && !object.IsNil(args[3]) {
				pp = intArg(args[3])
			}
			o.ivars["@proxy_port"] = object.IntValue(pp)
			if len(args) > 4 && !object.IsNil(args[4]) {
				o.ivars["@proxy_user"] = args[4]
			}
			if len(args) > 5 && !object.IsNil(args[5]) {
				o.ivars["@proxy_pass"] = args[5]
			}
		}
	}
	return o
}

// nethttpIsENV reports whether a p_addr argument is MRI's :ENV symbol, which
// selects environment-based proxy resolution.
func nethttpIsENV(v object.Value) bool {
	s, ok := v.(object.Symbol)
	return ok && string(s) == "ENV"
}

// nethttpEffectiveProxy resolves the proxy that applies to a Net::HTTP instance
// right now, returning (address, port, user, pass) as Ruby values (each NilV when
// absent). An explicitly-configured proxy (@proxy_address set) wins unchanged;
// otherwise, when the instance opted into :ENV (@proxy_from_env), the environment
// is consulted against the instance's current scheme and target host. A direct
// (no-proxy) result yields four NilV values.
func nethttpEffectiveProxy(inst object.Value) (addr, port, user, pass object.Value) {
	if pa := getIvar(inst, "@proxy_address"); !object.IsNil(pa) {
		return pa, getIvar(inst, "@proxy_port"), getIvar(inst, "@proxy_user"), getIvar(inst, "@proxy_pass")
	}
	if getIvar(inst, "@proxy_from_env").Truthy() {
		host := strArg(getIvar(inst, "@address"))
		reqPort := intArg(getIvar(inst, "@port"))
		https := getIvar(inst, "@use_ssl").Truthy()
		if a, p, u, pw, ok := nethttpResolveEnvProxy(host, reqPort, https); ok {
			user, pass = object.NilV, object.NilV
			if u != "" {
				user = object.NewString(u)
			}
			if pw != "" {
				pass = object.NewString(pw)
			}
			return object.NewString(a), object.IntValue(p), user, pass
		}
	}
	return object.NilV, object.NilV, object.NilV, object.NilV
}

// nethttpResolveEnvProxy resolves the environment proxy for a request to
// host:reqPort over http (https=false) or https (https=true), mirroring MRI's
// URI::Generic#find_proxy. It reads https_proxy/HTTPS_PROXY for TLS requests and
// http_proxy/HTTP_PROXY otherwise, honors no_proxy/NO_PROXY, and returns the proxy
// endpoint (address/port) plus any userinfo credentials. ok is false when no proxy
// applies (a direct connection).
func nethttpResolveEnvProxy(host string, reqPort int64, https bool) (addr string, port int64, user, pass string, ok bool) {
	raw := nethttpProxyEnvValue(https)
	if raw == "" {
		return "", 0, "", "", false
	}
	if nethttpNoProxyBypass(host, reqPort, nethttpEnvAny("no_proxy", "NO_PROXY")) {
		return "", 0, "", "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		// A bare "host:port" (no scheme) is a common form MRI's URI.parse would
		// reject as relative; retry it as an http:// URL before giving up.
		u, err = url.Parse("http://" + raw)
		if err != nil || u.Host == "" {
			return "", 0, "", "", false
		}
	}
	addr = u.Hostname()
	port = 80
	if u.Scheme == "https" {
		port = 443
	}
	if ps := u.Port(); ps != "" {
		if n, e := strconv.ParseInt(ps, 10, 64); e == nil {
			port = n
		}
	}
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	return addr, port, user, pass, true
}

// nethttpProxyEnvValue returns the proxy URL string from the environment for the
// request scheme. For https it prefers https_proxy then HTTPS_PROXY. For http it
// normally prefers http_proxy then HTTP_PROXY, but when REQUEST_METHOD is set (a
// CGI context) it deliberately ignores the uppercase HTTP_PROXY — which in CGI is
// attacker-controlled via the Proxy request header — and honors only the lowercase
// http_proxy, matching MRI's find_proxy CGI-safety rule.
func nethttpProxyEnvValue(https bool) string {
	if https {
		return nethttpEnvAny("https_proxy", "HTTPS_PROXY")
	}
	if os.Getenv("REQUEST_METHOD") != "" {
		return os.Getenv("http_proxy")
	}
	return nethttpEnvAny("http_proxy", "HTTP_PROXY")
}

// nethttpEnvAny returns the first non-empty value among the named environment
// variables, in order.
func nethttpEnvAny(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// nethttpNoProxyBypass reports whether host:reqPort matches the no_proxy list and
// should therefore connect directly. Each comma/space-separated entry is a host,
// a domain suffix (leading '.', or a bare name that also matches its subdomains),
// or a CIDR/IP (matched when host is an IP literal), optionally suffixed with
// ":port" to restrict it to that port. This mirrors MRI's URI::Generic.use_proxy?
// (inverted: use_proxy? false ⇒ bypass true).
func nethttpNoProxyBypass(host string, reqPort int64, noProxy string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || noProxy == "" {
		return false
	}
	for _, ent := range strings.FieldsFunc(noProxy, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		ehost := ent
		// An optional trailing ":port" restricts the entry to that request port.
		// (A CIDR mask like "/24" or an IPv6 literal has no bare numeric tail here.)
		if i := strings.LastIndex(ent, ":"); i >= 0 {
			if n, e := strconv.ParseInt(ent[i+1:], 10, 64); e == nil {
				if n != reqPort {
					continue
				}
				ehost = ent[:i]
			}
		}
		el := strings.ToLower(ehost)
		if strings.HasPrefix(el, ".") {
			if strings.HasSuffix(host, el) {
				return true
			}
		} else if el != "" {
			if host == el || strings.HasSuffix("."+host, "."+el) {
				return true
			}
		}
		// A CIDR or IP entry matches when the request host is an IP literal.
		if ip := net.ParseIP(host); ip != nil {
			if _, cidr, e := net.ParseCIDR(ehost); e == nil {
				if cidr.Contains(ip) {
					return true
				}
			} else if eip := net.ParseIP(ehost); eip != nil && eip.Equal(ip) {
				return true
			}
		}
	}
	return false
}

// nethttpGetURI resolves the argument forms of Net::HTTP.get / get_response: a
// single URI (String or URI object), or (host, path[, port]).
func (vm *VM) nethttpGetURI(args []object.Value) *url.URL {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..3)")
	}
	if len(args) == 1 {
		return vm.nethttpParseURI(args[0])
	}
	host := strArg(args[0])
	path := strArg(args[1])
	authority := host
	if len(args) > 2 && !object.IsNil(args[2]) {
		authority = net.JoinHostPort(host, portString(args[2]))
	}
	u, err := url.Parse("http://" + authority + path)
	if err != nil {
		raise("ArgumentError", "invalid URI: %s", host+path)
	}
	return u
}

// nethttpParseURI parses a URI argument (a String, or any object via #to_s, e.g.
// a URI::HTTP) into a *url.URL, raising ArgumentError on a malformed or
// authority-less value.
func (vm *VM) nethttpParseURI(v object.Value) *url.URL {
	var s string
	if str, ok := v.(*object.String); ok {
		s = str.Str()
	} else {
		s = strArg(vm.send(v, "to_s", nil, nil))
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		raise("ArgumentError", "invalid URI: %s", s)
	}
	return u
}

// headersFromArg reads an optional trailing initheader Hash at args[idx] into the
// ordered pairs the codec wants; a missing or non-Hash argument yields nil.
func (vm *VM) headersFromArg(args []object.Value, idx int) [][2]string {
	if len(args) <= idx || object.IsNil(args[idx]) {
		return nil
	}
	return vm.hashToPairs(args[idx])
}

// hashToPairs flattens a Ruby Hash into ordered [key, value] string pairs (both
// stringified via #to_s), preserving insertion order; a non-Hash yields nil.
func (vm *VM) hashToPairs(v object.Value) [][2]string {
	h, ok := v.(*object.Hash)
	if !ok {
		return nil
	}
	out := make([][2]string, 0, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		out = append(out, [2]string{strArg(k), string(argBytes(vm, val))})
	}
	return out
}
