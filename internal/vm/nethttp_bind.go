// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"

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
			setIvar(inst, "@started", object.Bool(false))
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
			setIvar(self, "@started", object.Bool(false))
			return res
		}
		return self
	})
	http.define("finish", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		setIvar(self, "@started", object.Bool(false))
		return object.NilV
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
// conveniences take no context). Returns the Net::HTTPResponse.
func (vm *VM) nethttpExecURL(u *url.URL, method string, body []byte, hdr [][2]string) object.Value {
	scheme, port := nethttpSchemePort(u)
	return vm.nethttpDo(scheme, u.Hostname(), port, u.Host, method, u.RequestURI(), body, hdr, 0)
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
// address / port / use_ssl / verify_mode.
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
	return vm.nethttpDo(scheme, host, portStr, hostHdr, method, path, body, hdr, verify)
}

// nethttpDo is the codec↔socket seam: build the request bytes with the net-http
// codec, write them to a connected (optionally TLS) socket, read the whole
// response, parse it, and build the Ruby Net::HTTPResponse.
func (vm *VM) nethttpDo(scheme, host, port, hostHdr, method, path string, body []byte, hdr [][2]string, verifyMode int64) object.Value {
	req, err := nethttp.NewRequest(method, path, hostHdr, hdr)
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	if body != nil {
		req.SetBody(body)
	}
	// One connection per request: ask the server to close so the read drains to
	// EOF (persistent connections are a follow-up).
	req.Set("Connection", "close")
	reqBytes, err := req.Bytes("1.1")
	if err != nil {
		raise("Net::HTTPError", "%s", err.Error())
	}
	raw, err := vm.nethttpRoundTrip(scheme, host, port, reqBytes, verifyMode)
	if err != nil {
		raise("SocketError", "%s", err.Error())
	}
	// A HEAD (and any request whose response body is not permitted) still receives
	// the server's Content-Length / Transfer-Encoding headers but no body bytes;
	// strip the framing so the status-based codec does not try to read a body, and
	// report a nil body as MRI's response_body_permitted? false path does.
	noBody := !req.ResponseBodyPermitted()
	if noBody {
		raw = trimResponseToHeaders(raw)
	}
	resp, err := nethttp.ParseResponse(raw)
	if err != nil {
		raise("Net::HTTPBadResponse", "%s", err.Error())
	}
	respVal := vm.nethttpBuildResponse(resp)
	if noBody {
		setIvar(respVal, "@body", object.NilV)
	}
	return respVal
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

// nethttpRoundTrip opens the connection through rbgo's socket transport (a
// tcpSocket, wrapped in a TLS sslSocket for https), writes the request bytes and
// reads the full response to EOF. It reuses the transport's connected-stream
// types and streamIO surface so the bytes travel the same path a Ruby
// TCPSocket / OpenSSL::SSL::SSLSocket would.
func (vm *VM) nethttpRoundTrip(scheme, host, port string, reqBytes []byte, verifyMode int64) ([]byte, error) {
	stream, err := nethttpDial(scheme, host, port, verifyMode)
	if err != nil {
		return nil, err
	}
	defer stream.closeConn()
	return httpExchange(stream, reqBytes)
}

// nethttpDial opens the connection through rbgo's socket transport (a tcpSocket,
// wrapped in a TLS sslSocket for https) and returns it as a streamIO. verify_mode
// VERIFY_NONE (0) — the default — skips certificate verification, matching the
// SSLSocket transport's bare-context default; VERIFY_PEER turns on Go's chain +
// hostname checks.
func nethttpDial(scheme, host, port string, verifyMode int64) (streamIO, error) {
	conn, err := net.Dial("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}
	tcp := newTCPSocket(nil, conn)
	if scheme != "https" {
		return tcp, nil
	}
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

// httpExchange writes the request bytes to a connected stream and reads the whole
// response to EOF. It is the transport-agnostic write/read half of the round trip
// (split from the dial so both I/O-error arms are exercisable).
func httpExchange(stream streamIO, reqBytes []byte) ([]byte, error) {
	if _, err := stream.writer().Write(reqBytes); err != nil {
		return nil, err
	}
	return io.ReadAll(stream.reader())
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

// nethttpNewInstance builds a Net::HTTP instance from (address[, port]),
// defaulting the port to 80 and use_ssl to false.
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
	return o
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
