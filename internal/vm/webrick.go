// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	webrick "github.com/go-ruby-webrick/webrick"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerWEBrick installs the WEBrick module (require "webrick"): the pure-Go
// (CGO=0) port of Ruby's WEBrick HTTP server core, reimplemented by
// github.com/go-ruby-webrick/webrick on top of go-ruby-net-http. The library
// owns the deterministic request-parse / response-build / status-table / mount
// dispatch; it does no socket I/O itself (the TCP accept loop is a host seam).
// This file (with webrick_bind.go) is the thin shell mapping that surface onto
// rbgo classes:
//
//	WEBrick::HTTPServer.new(Port: 80)      — the mount registry + #service dispatch
//	  #mount_proc("/p") { |req, res| … }   — a proc mounted at a path
//	  #mount("/p", servlet)                — an AbstractServlet subclass / instance
//	  #service(req, res)                   — route + run the handler, fill res
//	WEBrick::HTTPRequest.new; #parse(str)  — parse a raw request byte string
//	WEBrick::HTTPResponse.new              — status / headers / body / #to_s bytes
//	WEBrick::HTTPServlet::AbstractServlet  — the do_<METHOD> servlet base class
//	WEBrick::HTTPStatus                    — reason_phrase, the category predicates
//	                                         and the Status exception family
//
// The request-handler seam — a mount_proc block or a servlet's do_<METHOD>
// method — is the injectable Ruby code the deterministic engine leaves open.
// Because rbgo drives #service on the VM goroutine while holding the GVL, the
// handler runs INLINE (vm.callBlock / vm.send) with no extra goroutine and no
// listener, so there is nothing to leak; a Ruby handler that raises a
// WEBrick::HTTPStatus is recovered and turned into the matching error response,
// exactly like WEBrick::HTTPServer#run rescues HTTPStatus::Status (see
// webrick_bind.go).
func (vm *VM) registerWEBrick() {
	mod := newClass("WEBrick", nil)
	mod.isModule = true
	vm.consts["WEBrick"] = mod

	// WEBrick::VERSION — the WEBrick release this port mirrors.
	mod.consts["VERSION"] = object.NewString(webrick.VERSION)

	mk := func(name string, super *RClass) *RClass {
		full := "WEBrick::" + name
		cls := newClass(full, super)
		mod.consts[name] = cls
		vm.consts[full] = cls
		return cls
	}

	vm.registerWEBrickServer(mk("HTTPServer", vm.cObject))
	vm.registerWEBrickRequest(mk("HTTPRequest", vm.cObject))
	vm.registerWEBrickResponse(mk("HTTPResponse", vm.cObject))
	vm.registerWEBrickServlet(mod)
	vm.registerWEBrickStatus(mod)
}

// webrickSMethod installs a class ("singleton") method on cls.
func webrickSMethod(cls *RClass, name string, fn NativeFn) {
	cls.smethods[name] = &Method{name: name, owner: cls, native: fn}
}

// registerWEBrickServer installs WEBrick::HTTPServer: HTTPServer.new(config = {})
// and the mount/dispatch surface. The config Hash honours :Port, :ServerName and
// :ServerSoftware (the deterministic keys the codec reads); the networking keys
// (:BindAddress, SSL, the callbacks) are host seams and are ignored. #start /
// #shutdown / #stop are the host accept-loop seam and are no-ops here — a real
// rbgo host drives #service from its own accept loop.
func (vm *VM) registerWEBrickServer(cls *RClass) {
	webrickSMethod(cls, "new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		cfg := webrickConfig(webrickFirstOpt(args))
		return &WEBrickServer{srv: webrick.NewHTTPServer(cfg), cfg: cfg}
	})

	self := func(v object.Value) *WEBrickServer { return v.(*WEBrickServer) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// #mount_proc(dir) { |req, res| … } — mounts the captured block at dir, run
	// inline as the request handler (WEBrick::HTTPServer#mount_proc).
	d("mount_proc", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if blk == nil {
			raise("ArgumentError", "HTTPServer#mount_proc requires a block")
		}
		self(v).srv.MountProc(webrickStr(args[0]), vm.webrickProcHandler(blk))
		return object.NilV
	})

	// #mount(dir, servlet, *options) — mounts an AbstractServlet subclass (a class,
	// instantiated per request with the options) or a servlet instance at dir; its
	// do_<METHOD> methods handle requests (WEBrick::HTTPServer#mount).
	d("mount", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2+)", len(args))
		}
		self(v).srv.Mount(webrickStr(args[0]), vm.webrickServlet(args[1], args[2:]))
		return object.NilV
	})

	unmount := func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		self(v).srv.Unmount(webrickStr(args[0]))
		return object.NilV
	}
	d("unmount", unmount)
	d("umount", unmount)

	// #service(req, res) — route req to its mounted handler and fill res, wiring
	// the request context first (WEBrick::HTTPServer#service, driven by #run). A
	// handler that raises a WEBrick::HTTPStatus is rendered into the error
	// response; an unmatched path yields the default 404 page.
	d("service", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		vm.webrickService(self(v), webrickReq(args[0]), webrickRes(args[1]))
		return args[1]
	})

	// #config — the frozen config Hash the server was built with.
	d("config", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return webrickConfigHash(self(v).cfg)
	})

	// #start / #shutdown / #stop — the host accept-loop seam; no-ops here (there
	// is no listener), returning nil like a clean shutdown.
	noop := func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return object.NilV }
	d("start", noop)
	d("run", noop)
	d("shutdown", noop)
	d("stop", noop)
}

// registerWEBrickRequest installs WEBrick::HTTPRequest: HTTPRequest.new(config =
// nil) builds an unparsed request, #parse(str) parses a raw request byte string
// into it, and the accessors expose the parsed request the way a WEBrick servlet
// reads it (request_method / path / query / cookies-free subset / keep_alive?).
func (vm *VM) registerWEBrickRequest(cls *RClass) {
	webrickSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return &WEBrickRequest{cfg: webrickConfig(webrickFirstOpt(args))}
	})

	self := func(v object.Value) *WEBrickRequest { return v.(*WEBrickRequest) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// #parse(str) — parse the complete request byte stream (String) into self,
	// returning self; a malformed request raises the matching WEBrick::HTTPStatus.
	d("parse", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		r := self(v)
		req, st := webrick.ParseRequest([]byte(webrickStr(args[0])), r.cfg)
		if st != nil {
			webrickRaiseStatus(st)
		}
		r.req = req
		return v
	})

	req := func(v object.Value) *webrick.Request {
		r := self(v)
		if r.req == nil {
			raise("RuntimeError", "WEBrick::HTTPRequest has not been parsed yet")
		}
		return r.req
	}
	str := func(name string, fn func(*webrick.Request) string) {
		d(name, func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NewString(fn(req(v)))
		})
	}
	str("request_method", func(r *webrick.Request) string { return r.RequestMethod })
	str("unparsed_uri", func(r *webrick.Request) string { return r.UnparsedURI })
	str("path", func(r *webrick.Request) string { return r.Path })
	str("script_name", func(r *webrick.Request) string { return r.ScriptName })
	str("path_info", func(r *webrick.Request) string { return r.PathInfo })
	str("query_string", func(r *webrick.Request) string { return r.QueryString })
	str("http_version", func(r *webrick.Request) string { return r.HTTPVersion.String() })
	str("host", func(r *webrick.Request) string { return r.Host() })
	str("content_type", func(r *webrick.Request) string { return r.ContentType() })
	str("body", func(r *webrick.Request) string { return string(r.Body) })

	d("port", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(req(v).Port()))
	})
	d("keep_alive?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(req(v).KeepAlive())
	})
	d("content_length", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		n, _ := req(v).ContentLength()
		return object.IntValue(int64(n))
	})
	// #[](name) — a request header value, nil when absent (HTTPRequest#[]).
	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if s, ok := req(v).Header(webrickStr(args[0])); ok {
			return object.NewString(s)
		}
		return object.NilV
	})
	// #query — the parsed query as a Hash of first values (HTTPRequest#query).
	d("query", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		q := req(v).Query()
		h := object.NewHash()
		for _, k := range q.Order {
			val, _ := q.Get(k)
			h.Set(object.NewString(k), object.NewString(val))
		}
		return h
	})
	// #each { |name, value| … } — iterate the request headers (HTTPRequest#each).
	d("each", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		req(v).EachHeader(func(name, value string) {
			vm.callBlock(blk, []object.Value{object.NewString(name), object.NewString(value)})
		})
		return v
	})
}

// registerWEBrickResponse installs WEBrick::HTTPResponse: HTTPResponse.new(config
// = nil) plus the status / header / body accessors a servlet fills in, and #to_s
// which runs setup_header + send_header + send_body and returns the wire bytes.
func (vm *VM) registerWEBrickResponse(cls *RClass) {
	webrickSMethod(cls, "new", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		cfg := webrickConfig(webrickFirstOpt(args))
		return &WEBrickResponse{res: webrick.NewResponse(cfg)}
	})

	self := func(v object.Value) *webrick.Response { return v.(*WEBrickResponse).res }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// #request=(req) — wire the request method / version / host into the response
	// (the assignments HTTPServer#run makes before servicing).
	d("request=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		self(v).SetRequest(webrickReq(args[0]).req)
		return args[0]
	})

	d("status", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Status))
	})
	d("status=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetStatus(webrickInt(webrickArg(args)))
		return args[0]
	})
	d("reason_phrase", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ReasonPhrase)
	})
	d("status_line", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).StatusLine())
	})
	d("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		if s, ok := self(v).Get(webrickStr(args[0])); ok {
			return object.NewString(s)
		}
		return object.NilV
	})
	d("[]=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).Set(webrickStr(args[0]), webrickStr(args[1]))
		return args[1]
	})
	d("content_type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if s, ok := self(v).Get("content-type"); ok {
			return object.NewString(s)
		}
		return object.NilV
	})
	d("content_type=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetContentType(webrickStr(webrickArg(args)))
		return args[0]
	})
	d("content_length=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetContentLength(webrickInt(webrickArg(args)))
		return args[0]
	})
	d("body", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(string(self(v).Body))
	})
	d("body=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).Body = []byte(webrickStr(webrickArg(args)))
		return args[0]
	})
	d("chunked?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Chunked())
	})
	d("chunked=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetChunked(webrickArg(args).Truthy())
		return args[0]
	})
	d("keep_alive?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).KeepAlive())
	})
	// #set_redirect(status, url) — set the redirect body / Location / status from a
	// WEBrick::HTTPStatus redirect class (HTTPResponse#set_redirect; deterministic,
	// it does not raise here — the response is left ready to send).
	d("set_redirect", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		code := vm.webrickStatusCode(args[0])
		res := self(v)
		res.SetStatus(code)
		res.SetRedirect(webrick.NewStatus(code, ""), webrickStr(args[1]))
		return object.NilV
	})
	// #set_error(err) — render err (a WEBrick::HTTPStatus, else 500) as the default
	// error page (HTTPResponse#set_error).
	d("set_error", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetError(vm.webrickErrorStatus(webrickArg(args)))
		return object.NilV
	})
	// #to_s — the response wire bytes (setup_header + send_header + send_body).
	d("to_s", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		b, err := self(v).Bytes()
		if err != nil {
			raise("WEBrick::HTTPResponse::InvalidHeader", "%s", err.Error())
		}
		return object.NewString(string(b))
	})
}

// registerWEBrickServlet installs WEBrick::HTTPServlet and its AbstractServlet
// base class. A servlet is written as a subclass defining do_<METHOD> methods and
// mounted with HTTPServer#mount; the do_<METHOD> dispatch (HEAD -> GET, the
// OPTIONS Allow list, MethodNotAllowed for an unhandled verb) is the library's,
// run over the subclass's methods via the seam in webrick_bind.go.
func (vm *VM) registerWEBrickServlet(mod *RClass) {
	hs := newClass("WEBrick::HTTPServlet", nil)
	hs.isModule = true
	mod.consts["HTTPServlet"] = hs
	vm.consts["WEBrick::HTTPServlet"] = hs

	abs := newClass("WEBrick::HTTPServlet::AbstractServlet", vm.cObject)
	hs.consts["AbstractServlet"] = abs
	vm.consts["WEBrick::HTTPServlet::AbstractServlet"] = abs
}

// registerWEBrickStatus installs WEBrick::HTTPStatus: the reason_phrase lookup,
// the category predicates (info? / success? / … / error?), and the Status
// exception family (Status < StandardError, the category classes, and the named
// leaf classes servlets raise, each carrying its code). A raised status is
// recovered in webrick_bind.go and rendered as the matching error response.
func (vm *VM) registerWEBrickStatus(mod *RClass) {
	hs := newClass("WEBrick::HTTPStatus", nil)
	hs.isModule = true
	mod.consts["HTTPStatus"] = hs
	vm.consts["WEBrick::HTTPStatus"] = hs

	// Module functions: reason_phrase(code) and the range predicates.
	sm := func(name string, fn NativeFn) { hs.smethods[name] = &Method{name: name, owner: hs, native: fn} }
	sm("reason_phrase", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(webrick.ReasonPhrase(webrickInt(webrickArg(args))))
	})
	pred := func(name string, fn func(int) bool) {
		sm(name, func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.Bool(fn(webrickInt(webrickArg(args))))
		})
	}
	pred("info?", webrick.IsInfo)
	pred("success?", webrick.IsSuccess)
	pred("redirect?", webrick.IsRedirect)
	pred("error?", webrick.IsError)
	pred("client_error?", webrick.IsClientError)
	pred("server_error?", webrick.IsServerError)

	std := vm.consts["StandardError"].(*RClass)
	mk := func(name string, super *RClass) *RClass {
		cls := newClass("WEBrick::HTTPStatus::"+name, super)
		hs.consts[name] = cls
		vm.consts["WEBrick::HTTPStatus::"+name] = cls
		return cls
	}

	// The exception hierarchy: Status < StandardError, the five category classes,
	// then the named leaf classes (each < its category) that carry a code.
	status := mk("Status", std)
	cats := map[string]*RClass{
		"Info":     mk("Info", status),
		"Success":  mk("Success", status),
		"Redirect": mk("Redirect", status),
		"Error":    mk("Error", status),
	}
	cats["ClientError"] = mk("ClientError", cats["Error"])
	cats["ServerError"] = mk("ServerError", cats["Error"])
	for _, def := range webrickStatusDefs {
		mk(def.Name, cats[def.Cat])
	}

	// Status#code / #to_i / #reason_phrase read the code stamped on the raised
	// class, resolved through the class hierarchy so a subclass still answers.
	code := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(vm.webrickCodeOf(v)))
	}
	status.define("code", code)
	status.define("to_i", code)
	status.define("reason_phrase", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(webrick.ReasonPhrase(vm.webrickCodeOf(v)))
	})
}
