// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	webrick "github.com/go-ruby-webrick/webrick"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the request-handler seam between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-webrick/webrick core. The library
// owns the deterministic request parse, the response build, the status table and
// the longest-prefix mount dispatch; rbgo supplies the two things the engine
// leaves injectable — a mount_proc block and a servlet's do_<METHOD> methods —
// and converts values at the boundary (a Ruby request/response wrapper each way).
//
// Threading model: unlike a threaded server (puma), the webrick core does no
// socket I/O and starts no goroutines — HTTPServer#service is driven on the VM
// goroutine while it holds the GVL, so the handler runs INLINE (vm.callBlock /
// vm.send) with no extra goroutine to leak and no GVL juggling. A handler that
// raises a WEBrick::HTTPStatus unwinds as a Go panic through the library's Go
// dispatch back to webrickService, which recovers it and renders the matching
// error response, exactly as WEBrick::HTTPServer#run rescues HTTPStatus::Status.

// WEBrickServer is the Ruby wrapper around a webrick.HTTPServer (the mount
// registry + #service dispatch) plus the config it was built with.
type WEBrickServer struct {
	srv *webrick.HTTPServer
	cfg *webrick.Config
}

func (s *WEBrickServer) ToS() string     { return "#<WEBrick::HTTPServer>" }
func (s *WEBrickServer) Inspect() string { return "#<WEBrick::HTTPServer>" }
func (s *WEBrickServer) Truthy() bool    { return true }

// WEBrickRequest is the Ruby wrapper around a webrick.Request. req is nil until
// #parse runs (or the request originates from the service seam); cfg is the
// config #parse parses against.
type WEBrickRequest struct {
	req *webrick.Request
	cfg *webrick.Config
}

func (r *WEBrickRequest) ToS() string     { return "#<WEBrick::HTTPRequest>" }
func (r *WEBrickRequest) Inspect() string { return "#<WEBrick::HTTPRequest>" }
func (r *WEBrickRequest) Truthy() bool    { return true }

// WEBrickResponse is the Ruby wrapper around a webrick.Response — the mutable
// status/headers/body a servlet fills in, whose #to_s yields the wire bytes.
type WEBrickResponse struct {
	res *webrick.Response
}

func (r *WEBrickResponse) ToS() string     { return "#<WEBrick::HTTPResponse>" }
func (r *WEBrickResponse) Inspect() string { return "#<WEBrick::HTTPResponse>" }
func (r *WEBrickResponse) Truthy() bool    { return true }

// webrickMethods is the fixed set of HTTP methods a servlet may handle via a
// do_<METHOD> method, in the order they are probed when wiring a servlet.
var webrickMethods = []string{"GET", "HEAD", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"}

// webrickProcHandler adapts a mount_proc Ruby block into a webrick.HandlerFunc:
// the library calls it (on the VM goroutine, inline) with the per-request Go
// Request/Response, which are wrapped as Ruby WEBrick::HTTPRequest /
// HTTPResponse (thin pointer holders, so the block's mutations land on the same
// response) and yielded to the block. The block's return value is ignored — a
// WEBrick handler communicates through res — so the handler reports nil.
func (vm *VM) webrickProcHandler(blk *Proc) webrick.HandlerFunc {
	return func(req *webrick.Request, res *webrick.Response) *webrick.Status {
		vm.callBlock(blk, []object.Value{&WEBrickRequest{req: req}, &WEBrickResponse{res: res}})
		return nil
	}
}

// webrickRubyServlet is the webrick.Servlet backing HTTPServer#mount: it holds
// either a fixed servlet instance or a servlet class instantiated per request
// (with the mount options), and dispatches by building a library AbstractServlet
// whose handlers call the instance's do_<METHOD> methods — so the HEAD->GET
// aliasing, the OPTIONS Allow list and MethodNotAllowed all come from the
// library, over the Ruby methods.
type webrickRubyServlet struct {
	vm   *VM
	inst object.Value   // a pre-built servlet instance, or nil
	cls  *RClass        // a servlet class instantiated per request, when inst is nil
	opts []object.Value // the mount options passed to the class's #new
}

// Service dispatches req to the Ruby servlet's do_<METHOD> handler through a
// library AbstractServlet (see the type comment).
func (s *webrickRubyServlet) Service(req *webrick.Request, res *webrick.Response) *webrick.Status {
	inst := s.inst
	if inst == nil {
		inst = s.vm.send(s.cls, "new", s.opts, nil)
	}
	abs := webrick.NewAbstractServlet()
	for _, m := range webrickMethods {
		if !s.vm.respondsTo(inst, "do_"+m) {
			continue
		}
		mm := m
		abs.Handle(m, func(rq *webrick.Request, rs *webrick.Response) *webrick.Status {
			s.vm.send(inst, "do_"+mm, []object.Value{&WEBrickRequest{req: rq}, &WEBrickResponse{res: rs}}, nil)
			return nil
		})
	}
	return abs.Service(req, res)
}

// webrickServlet resolves HTTPServer#mount's servlet argument into a
// webrick.Servlet: a Class is instantiated per request (WEBrick's get_instance
// model, with the mount options as #new args), and any other value is used as a
// pre-built servlet instance.
func (vm *VM) webrickServlet(v object.Value, opts []object.Value) webrick.Servlet {
	if cls, ok := v.(*RClass); ok {
		return &webrickRubyServlet{vm: vm, cls: cls, opts: append([]object.Value(nil), opts...)}
	}
	return &webrickRubyServlet{vm: vm, inst: v}
}

// webrickService is the Go port of HTTPServer#service driven by #run: it wires
// the request context into the response, routes the request to its servlet and,
// on a raised (returned or panicked) WEBrick::HTTPStatus, renders the matching
// error response. A non-status raise from a handler propagates untouched.
func (vm *VM) webrickService(s *WEBrickServer, req *WEBrickRequest, res *WEBrickResponse) {
	defer func() {
		if r := recover(); r != nil {
			if st, ok := vm.webrickStatusFromPanic(r); ok {
				res.res.SetError(st)
				return
			}
			panic(r)
		}
	}()
	if req.req == nil {
		raise("RuntimeError", "WEBrick::HTTPRequest has not been parsed yet")
	}
	res.res.SetRequest(req.req)
	if st := s.srv.Service(req.req, res.res); st != nil {
		res.res.SetError(st)
	}
}

// webrickStatusDefs is the named WEBrick::HTTPStatus leaf exceptions this binding
// registers, each with its code and its category class (the exception's parent).
var webrickStatusDefs = []struct {
	Name string
	Code int
	Cat  string
}{
	{"OK", 200, "Success"},
	{"MovedPermanently", 301, "Redirect"},
	{"Found", 302, "Redirect"},
	{"NotModified", 304, "Redirect"},
	{"TemporaryRedirect", 307, "Redirect"},
	{"BadRequest", 400, "ClientError"},
	{"Forbidden", 403, "ClientError"},
	{"NotFound", 404, "ClientError"},
	{"MethodNotAllowed", 405, "ClientError"},
	{"LengthRequired", 411, "ClientError"},
	{"RequestEntityTooLarge", 413, "ClientError"},
	{"InternalServerError", 500, "ServerError"},
	{"NotImplemented", 501, "ServerError"},
}

// webrickCodeByClassName maps a registered status exception's fully-qualified
// class name to its code; webrickClassNameByCode is the inverse. Both are built
// once from webrickStatusDefs.
var (
	webrickCodeByClassName = map[string]int{}
	webrickClassNameByCode = map[int]string{}
)

func init() {
	for _, d := range webrickStatusDefs {
		name := "WEBrick::HTTPStatus::" + d.Name
		webrickCodeByClassName[name] = d.Code
		webrickClassNameByCode[d.Code] = name
	}
}

// webrickCodeFromValue resolves the status code carried by a WEBrick::HTTPStatus
// class or instance, walking the ancestor chain so a subclass still resolves. ok
// is false when the value is not (a subclass of) a coded status.
func (vm *VM) webrickCodeFromValue(v object.Value) (int, bool) {
	cls, ok := v.(*RClass)
	if !ok {
		cls = vm.classOf(v)
	}
	for _, a := range vm.ancestors(cls) {
		if code, ok := webrickCodeByClassName[a.name]; ok {
			return code, true
		}
	}
	return 0, false
}

// webrickCodeOf returns the code of a status exception instance (Status#code),
// or 0 for a bare Status/category with no code of its own.
func (vm *VM) webrickCodeOf(v object.Value) int {
	code, _ := vm.webrickCodeFromValue(v)
	return code
}

// webrickStatusCode returns the code of the status class/instance passed to
// #set_redirect, raising ArgumentError when it is not a WEBrick::HTTPStatus.
func (vm *VM) webrickStatusCode(v object.Value) int {
	code, ok := vm.webrickCodeFromValue(v)
	if !ok {
		raise("ArgumentError", "expected a WEBrick::HTTPStatus, got %s", v.Inspect())
	}
	return code
}

// webrickErrorStatus maps #set_error's argument onto the error the library's
// SetError renders: a coded WEBrick::HTTPStatus becomes that status, anything
// else a plain error (which SetError turns into a 500 page).
func (vm *VM) webrickErrorStatus(v object.Value) error {
	if code, ok := vm.webrickCodeFromValue(v); ok {
		if st := webrick.NewStatus(code, ""); st != nil {
			return st
		}
	}
	return errors.New(v.ToS())
}

// webrickStatusFromPanic maps a recovered panic to the status a handler raised: a
// Ruby exception whose class (or an ancestor) is a coded WEBrick::HTTPStatus
// yields that status carrying the raise's message. Any other panic is not a
// status (ok=false) and is re-raised by the caller.
func (vm *VM) webrickStatusFromPanic(r any) (*webrick.Status, bool) {
	re, ok := r.(RubyError)
	if !ok {
		return nil, false
	}
	if re.Obj != nil {
		if code, ok := vm.webrickCodeFromValue(re.Obj); ok {
			return webrick.NewStatus(code, re.Message), true
		}
		return nil, false
	}
	if code, ok := webrickCodeByClassName[re.Class]; ok {
		return webrick.NewStatus(code, re.Message), true
	}
	return nil, false
}

// webrickRaiseStatus raises the Ruby exception matching a *webrick.Status a parse
// returned: the mapped WEBrick::HTTPStatus class when the code is one this
// binding registers, else a plain RuntimeError carrying the status message.
func webrickRaiseStatus(st *webrick.Status) {
	if name, ok := webrickClassNameByCode[st.Code]; ok {
		raise(name, "%s", st.Error())
	}
	raise("RuntimeError", "%s", st.Error())
}

// webrickConfig builds a *webrick.Config from an optional Ruby config Hash,
// honouring the deterministic keys the codec reads (:Port, :ServerName,
// :ServerSoftware); the networking keys are host seams and are ignored.
func webrickConfig(v object.Value) *webrick.Config {
	cfg := webrick.DefaultConfig()
	h, ok := v.(*object.Hash)
	if !ok {
		return cfg
	}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		switch webrickKey(k) {
		case "Port":
			cfg.Port = webrickInt(val)
		case "ServerName":
			cfg.ServerName = webrickStr(val)
		case "ServerSoftware":
			cfg.ServerSoftware = webrickStr(val)
		}
	}
	return cfg
}

// webrickConfigHash renders a *webrick.Config as the Hash HTTPServer#config
// returns, keyed by Symbol like a WEBrick config.
func webrickConfigHash(cfg *webrick.Config) *object.Hash {
	h := object.NewHash()
	h.Set(object.Symbol("Port"), object.IntValue(int64(cfg.Port)))
	h.Set(object.Symbol("ServerName"), object.NewString(cfg.ServerName))
	h.Set(object.Symbol("ServerSoftware"), object.NewString(cfg.ServerSoftware))
	return h
}

// webrickFirstOpt returns the optional first argument (a config Hash) or nil.
func webrickFirstOpt(args []object.Value) object.Value {
	if len(args) == 0 {
		return object.NilV
	}
	return args[0]
}

// webrickArg returns the single required argument of a setter, raising
// ArgumentError when it was omitted.
func webrickArg(args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return args[0]
}

// webrickReq coerces a Ruby argument to a *WEBrickRequest, raising TypeError for
// anything else.
func webrickReq(v object.Value) *WEBrickRequest {
	if r, ok := v.(*WEBrickRequest); ok {
		return r
	}
	raise("TypeError", "expected a WEBrick::HTTPRequest, got %s", v.Inspect())
	panic("unreachable")
}

// webrickRes coerces a Ruby argument to a *WEBrickResponse, raising TypeError for
// anything else.
func webrickRes(v object.Value) *WEBrickResponse {
	if r, ok := v.(*WEBrickResponse); ok {
		return r
	}
	raise("TypeError", "expected a WEBrick::HTTPResponse, got %s", v.Inspect())
	panic("unreachable")
}

// webrickKey coerces a Ruby config key (Symbol or String) to its Go string name.
func webrickKey(v object.Value) string {
	switch k := v.(type) {
	case object.Symbol:
		return string(k)
	case *object.String:
		return k.Str()
	}
	return v.ToS()
}

// webrickStr coerces a Ruby argument to its String contents (String or Symbol).
func webrickStr(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

// webrickInt coerces a Ruby Integer argument to an int, raising TypeError for a
// non-Integer (a port / status / length must be an Integer).
func webrickInt(v object.Value) int {
	if n, ok := v.(object.Integer); ok {
		return int(n)
	}
	if n, ok := object.BigOf(v); ok {
		return int(n.Int64())
	}
	raise("TypeError", "expected an Integer, got %s", v.Inspect())
	panic("unreachable")
}
