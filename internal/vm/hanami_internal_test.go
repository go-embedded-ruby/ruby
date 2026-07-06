// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	hanami "github.com/go-ruby-hanami/hanami"
	rack "github.com/go-ruby-rack/rack"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// hanamiRun runs src on a fresh VM and returns its trimmed stdout, failing on a
// parse/compile/run error.
func hanamiRun(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	if _, err := New(&buf).Run(iseq); err != nil {
		t.Fatalf("run: %v", err)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// hanamiRunErr runs src on a fresh VM and returns the uncaught error's class, or
// "" if it ran clean.
func hanamiRunErr(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, rerr := New(io.Discard).Run(iseq)
	if rerr == nil {
		return ""
	}
	if i := strings.Index(rerr.Error(), ": "); i > 0 {
		return rerr.Error()[:i]
	}
	return rerr.Error()
}

// hanamiReqResp builds a real *hanami.Request/*hanami.Response over env by running
// a trivial action that captures them, so the Ruby accessor natives can be driven
// directly against genuine library objects.
func hanamiReqResp(env rack.Env) (*hanami.Request, *hanami.Response) {
	var gotReq *hanami.Request
	var gotResp *hanami.Response
	a := hanami.NewAction("probe", func(_ string, req *hanami.Request, resp *hanami.Response) error {
		gotReq, gotResp = req, resp
		return nil
	})
	a.Call(env)
	return gotReq, gotResp
}

// TestHanamiValueTypes covers the value wrappers' string/truthy surface and
// classOf reporting.
func TestHanamiValueTypes(t *testing.T) {
	vm := New(io.Discard)
	router := &HanamiRouter{cls: vm.cHanamiRouter}
	req := &HanamiReq{cls: vm.cHanamiRequest}
	resp := &HanamiResp{cls: vm.cHanamiResponse}
	flash := &HanamiFlash{cls: vm.cHanamiFlash}
	for _, c := range []interface {
		ToS() string
		Inspect() string
		Truthy() bool
	}{router, req, resp, flash} {
		if c.ToS() != c.Inspect() || !c.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c, c.ToS(), c.Inspect(), c.Truthy())
		}
	}
	if vm.classOf(router) != vm.cHanamiRouter || vm.classOf(req) != vm.cHanamiRequest ||
		vm.classOf(resp) != vm.cHanamiResponse || vm.classOf(flash) != vm.cHanamiFlash {
		t.Fatal("classOf mismatch for Hanami wrappers")
	}
}

// TestHanamiRouter drives a Hanami app end-to-end: the verb DSL block form, named
// routes with path/URL helpers, path params, the resolver seam (name → action /
// nil), a directly-mounted action, an inheriting action subclass, content
// negotiation + the 406 path, exception handling, halt/redirect_to, a redirect
// route, a scope, a mounted Rack app and the 404 fall-through.
func TestHanamiRouter(t *testing.T) {
	src := `
require "hanami"

class Show < Hanami::Action
  before :mark
  after { |req, resp| resp["X-After"] = "1" }
  default_status 201
  def mark(req, resp); resp["X-Before"] = "1"; end
  def handle(req, resp)
    resp.body = "id=#{req.params["id"]} fmt=#{resp.format} b=#{resp["X-Before"]}"
  end
end
class Show2 < Show; end
class Api < Hanami::Action
  accept "json"
  default_format "json"
  def handle(req, resp); resp.write("api ok"); end
end
class Boom < Hanami::Action
  handle_exception { |err, req, resp| resp.status = 422; resp.body = "caught #{err.message}"; true }
  def handle(req, resp); raise "kaboom"; end
end
class Boom2 < Hanami::Action
  def handle(req, resp); raise "unhandled"; end
end
class Redir < Hanami::Action
  def handle(req, resp); resp.redirect_to("/dest"); end
end
class Redir2 < Hanami::Action
  def handle(req, resp); resp.redirect_to("/dest", 303); end
end
class Halt1 < Hanami::Action
  def handle(req, resp); resp.halt(404); end
end
class Halt2 < Hanami::Action
  def handle(req, resp); resp.halt(503, "down"); end
end

router = Hanami::Router.new(resolver: ->(name) {
  case name
  when "books.show" then Show.new
  when "api.index" then Api.new
  when "symname" then Show.new
  else nil
  end
}, scheme: "https", host: "ex.com") do
  root to: ->(env) { [200, {}, ["home"]] }
  get "/books/:id", to: "books.show", as: "book"
  get "/show2/:id", to: Show2.new
  get "/api", to: "api.index"
  get "/boom", to: Boom.new
  get "/boom2", to: Boom2.new
  get "/redir", to: Redir.new
  get "/redir2", to: Redir2.new
  get "/halt1", to: Halt1.new
  get "/halt2", to: Halt2.new
  get "/sym/:id", to: :symname
  get "/missing", to: "no.such"
  post "/echo", to: ->(env) { [201, {}, ["echo"]] }
  redirect "/old", to: "/new", code: 301
  scope "v1" do
    get "/ping", to: ->(env) { [200, {}, ["pong"]] }
  end
  mount ->(env) { [200, {}, ["mounted #{env["PATH_INFO"]}"]] }, at: "/ext"
end

def call(router, method, path, headers={})
  env = { "REQUEST_METHOD" => method, "PATH_INFO" => path, "QUERY_STRING" => "" }.merge(headers)
  status, _, body = router.call(env)
  puts "#{method} #{path} -> #{status} #{body.join}"
end

call(router, "GET", "/")
call(router, "GET", "/books/7", { "HTTP_ACCEPT" => "text/html" })
call(router, "GET", "/show2/3", { "HTTP_ACCEPT" => "text/html" })
call(router, "GET", "/api", { "HTTP_ACCEPT" => "application/json" })
call(router, "GET", "/api", { "HTTP_ACCEPT" => "text/html" })
call(router, "GET", "/boom")
call(router, "GET", "/boom2")
call(router, "GET", "/redir")
call(router, "GET", "/redir2")
call(router, "GET", "/halt1")
call(router, "GET", "/halt2")
call(router, "GET", "/sym/9", { "HTTP_ACCEPT" => "text/html" })
call(router, "GET", "/missing")
call(router, "POST", "/echo")
call(router, "GET", "/old")
call(router, "GET", "/v1/ping")
call(router, "GET", "/ext/foo")
puts router.path("book", id: "9")
puts router.url("book", id: "9")
`
	got := hanamiRun(t, src)
	want := strings.Join([]string{
		"GET / -> 200 home",
		"GET /books/7 -> 201 id=7 fmt=html b=1",
		"GET /show2/3 -> 201 id=3 fmt=html b=1",
		"GET /api -> 200 api ok",
		"GET /api -> 406 Not Acceptable",
		"GET /boom -> 422 caught kaboom",
		"GET /boom2 -> 500 Internal Server Error",
		"GET /redir -> 302 ",
		"GET /redir2 -> 303 ",
		"GET /halt1 -> 404 Not Found",
		"GET /halt2 -> 503 down",
		"GET /sym/9 -> 201 id=9 fmt=html b=1",
		"GET /missing -> 404 Not Found",
		"POST /echo -> 201 echo",
		"GET /old -> 301 ",
		"GET /v1/ping -> 200 pong",
		"GET /ext/foo -> 200 mounted /foo",
		"/books/9",
		"https://ex.com/books/9",
	}, "\n")
	if got != want {
		t.Fatalf("hanami router mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestHanamiActionSeams covers the params-validation seam (hash / error / passthrough
// results), the session-loading seam (hash / nil results) and the flash store.
func TestHanamiActionSeams(t *testing.T) {
	src := `
require "hanami"
class Val < Hanami::Action
  params_validator { |params| m = params["mode"]; m == "hash" ? params : (m == "err" ? "bad input" : nil) }
  def handle(req, resp)
    resp.body = req.params_valid? ? "ok mode=#{req.params["mode"]}" : "invalid: #{req.params_error}"
  end
end
class Sess < Hanami::Action
  session_loader { |env| env["want"] == "yes" ? { "user" => "alice" } : nil }
  def handle(req, resp); resp.body = "user=#{req.session["user"]}"; end
end
class Fl < Hanami::Action
  def handle(req, resp)
    resp.flash["notice"] = "hello"
    resp.flash.keep("notice")
    resp.format = "txt"
    resp.body = "empty=#{req.flash.empty?} got=#{resp.flash["notice"]} miss=#{resp.flash["x"].inspect} fmt=#{resp.format}"
  end
end

r = Hanami::Router.new do
  get "/val", to: Val.new
  get "/sess", to: Sess.new
  get "/fl", to: Fl.new
end
def call(r, path, qs="", extra={})
  env = { "REQUEST_METHOD" => "GET", "PATH_INFO" => path, "QUERY_STRING" => qs }.merge(extra)
  st, _, body = r.call(env)
  puts "#{path}?#{qs} -> #{st} #{body.join}"
end
call(r, "/val", "mode=hash")
call(r, "/val", "mode=err")
call(r, "/val", "mode=other")
call(r, "/sess", "", { "want" => "yes" })
call(r, "/sess", "", { "want" => "no" })
call(r, "/fl")
st, _, body = Fl.new.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/fl", "QUERY_STRING" => "" })
puts "direct -> #{st} #{body.join}"
`
	got := hanamiRun(t, src)
	want := strings.Join([]string{
		"/val?mode=hash -> 200 ok mode=hash",
		"/val?mode=err -> 200 invalid: bad input",
		"/val?mode=other -> 200 ok mode=other",
		"/sess? -> 200 user=alice",
		"/sess? -> 200 user=",
		"/fl? -> 200 empty=false got=hello miss=nil fmt=txt",
		"direct -> 200 empty=false got=hello miss=nil fmt=txt",
	}, "\n")
	if got != want {
		t.Fatalf("hanami seams mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestHanamiVerbs exercises every verb helper, a route with no `to:` (the default
// 404 endpoint), per-param constraints, a scope with no block, a no-param named
// route, a Ruby Rack app returning a malformed / single-String / non-Array body,
// and a directly-mounted sub-router.
func TestHanamiVerbs(t *testing.T) {
	src := `
require "hanami"
r = Hanami::Router.new
r.get "/g", to: ->(e){ [200, {}, ["g"]] }
r.post "/po", to: ->(e){ [200, {}, ["po"]] }
r.put "/pu", to: ->(e){ [200, {}, ["pu"]] }
r.patch "/pa", to: ->(e){ [200, {}, ["pa"]] }
r.delete "/d", to: ->(e){ [200, {}, ["d"]] }
r.options "/o", to: ->(e){ [200, {}, ["o"]] }
r.trace "/t", to: ->(e){ [200, {}, ["t"]] }
r.link "/l", to: ->(e){ [200, {}, ["l"]] }
r.unlink "/u", to: ->(e){ [200, {}, ["u"]] }
r.get "/noto"
r.get "/n/:id", to: ->(e){ [200, {}, ["n"]] }, constraints: { id: "[0-9]+" }
r.get "/health", to: ->(e){ [200, {}, ["h"]] }, as: "health"
r.get "/bad", to: ->(e){ [200] }
r.get "/single", to: ->(e){ [200, {}, "one"] }
r.get "/nonarr", to: ->(e){ "nope" }
r.scope "empty" do
end
sub = Hanami::Router.new
sub.get "/inner", to: ->(e){ [200, {}, ["inner"]] }
r.mount sub, at: "/sub"

def call(r, method, path)
  st, _, body = r.call({ "REQUEST_METHOD" => method, "PATH_INFO" => path, "QUERY_STRING" => "" })
  puts "#{method} #{path} -> #{st} #{body.join}"
end
call(r, "GET", "/g")
call(r, "POST", "/po")
call(r, "PUT", "/pu")
call(r, "PATCH", "/pa")
call(r, "DELETE", "/d")
call(r, "OPTIONS", "/o")
call(r, "TRACE", "/t")
call(r, "LINK", "/l")
call(r, "UNLINK", "/u")
call(r, "GET", "/noto")
call(r, "GET", "/n/5")
call(r, "GET", "/n/abc")
call(r, "GET", "/bad")
call(r, "GET", "/single")
call(r, "GET", "/nonarr")
call(r, "GET", "/sub/inner")
puts r.path("health")
`
	got := hanamiRun(t, src)
	want := strings.Join([]string{
		"GET /g -> 200 g",
		"POST /po -> 200 po",
		"PUT /pu -> 200 pu",
		"PATCH /pa -> 200 pa",
		"DELETE /d -> 200 d",
		"OPTIONS /o -> 200 o",
		"TRACE /t -> 200 t",
		"LINK /l -> 200 l",
		"UNLINK /u -> 200 u",
		"GET /noto -> 404 Not Found",
		"GET /n/5 -> 200 n",
		"GET /n/abc -> 404 Not Found",
		"GET /bad -> 500 Internal Server Error",
		"GET /single -> 200 one",
		"GET /nonarr -> 500 Internal Server Error",
		"GET /sub/inner -> 200 inner",
		"/health",
	}, "\n")
	if got != want {
		t.Fatalf("hanami verbs mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestHanamiErrors covers the raising branches: the router DSL arity/validation
// checks, the action DSL block requirements, an unsupported endpoint and an
// unknown named route, plus the Request/Response/Flash native arity checks reached
// only by a direct call.
func TestHanamiErrors(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"require \"hanami\"\nHanami::Router.new { redirect }\n", "ArgumentError"},
		{"require \"hanami\"\nHanami::Router.new { mount }\n", "ArgumentError"},
		{"require \"hanami\"\nHanami::Router.new { get \"/x\", to: 42 }\n", "ArgumentError"},
		{"require \"hanami\"\nHanami::Router.new.path(\"nope\")\n", "Hanami::Error"},
		{"require \"hanami\"\nHanami::Router.new.url(\"nope\")\n", "Hanami::Error"},
		{"require \"hanami\"\nclass X < Hanami::Action; handle_exception; end\n", "ArgumentError"},
		{"require \"hanami\"\nclass X < Hanami::Action; params_validator; end\n", "ArgumentError"},
		{"require \"hanami\"\nclass X < Hanami::Action; session_loader; end\n", "ArgumentError"},
	}
	for _, c := range cases {
		if got := hanamiRunErr(t, c.src); got != c.want {
			t.Errorf("src %q: got %q, want %q", c.src, got, c.want)
		}
	}

	vm := New(io.Discard)
	_, resp := hanamiReqResp(rack.Env{"REQUEST_METHOD": "GET", "PATH_INFO": "/"})
	flash := hanami.NewFlash(nil)
	respW := &HanamiResp{r: resp, cls: vm.cHanamiResponse}
	flashW := &HanamiFlash{f: flash, cls: vm.cHanamiFlash}
	arity := []struct {
		cls  *RClass
		name string
		self object.Value
		args []object.Value
	}{
		{vm.cHanamiResponse, "[]=", respW, []object.Value{object.NewString("k")}},
		{vm.cHanamiResponse, "set_cookie", respW, []object.Value{object.NewString("k")}},
		{vm.cHanamiResponse, "redirect_to", respW, nil},
		{vm.cHanamiResponse, "halt", respW, nil},
		{vm.cHanamiFlash, "[]=", flashW, []object.Value{object.NewString("k")}},
	}
	for _, c := range arity {
		func() {
			defer func() {
				r := recover()
				re, ok := r.(RubyError)
				if !ok || re.Class != "ArgumentError" {
					t.Errorf("%s.%s: expected ArgumentError, got %#v", c.cls.name, c.name, r)
				}
			}()
			c.cls.methods[c.name].native(vm, c.self, c.args, nil)
		}()
	}
}

// TestHanamiAccessorsDirect drives every Request/Response/Flash accessor against
// real library objects, covering the readers/setters not reached by routing.
func TestHanamiAccessorsDirect(t *testing.T) {
	vm := New(io.Discard)
	env := rack.Env{
		"REQUEST_METHOD": "POST",
		"PATH_INFO":      "/p",
		"QUERY_STRING":   "a=1",
		"HTTP_ACCEPT":    "application/json",
		"HTTP_COOKIE":    "c=v",
	}
	req, resp := hanamiReqResp(env)
	reqW := &HanamiReq{r: req, cls: vm.cHanamiRequest}
	respW := &HanamiResp{r: resp, cls: vm.cHanamiResponse}

	rq := func(name string, args ...object.Value) object.Value {
		return vm.cHanamiRequest.methods[name].native(vm, reqW, args, nil)
	}
	rp := func(name string, args ...object.Value) object.Value {
		return vm.cHanamiResponse.methods[name].native(vm, respW, args, nil)
	}
	str := func(v object.Value) string { return v.(*object.String).Str() }

	// Request accessors.
	if str(rq("param", object.NewString("a"))) != "1" {
		t.Fatal("req.param")
	}
	if str(rq("[]", object.NewString("a"))) != "1" {
		t.Fatal("req.[]")
	}
	if _, ok := rq("params").(*object.Hash); !ok {
		t.Fatal("req.params")
	}
	if rq("params_valid?") != object.Bool(true) {
		t.Fatal("req.params_valid?")
	}
	if !object.IsNil(rq("params_error")) {
		t.Fatal("req.params_error should be nil with no validator")
	}
	if str(rq("format")) != "json" {
		t.Fatalf("req.format = %q", str(rq("format")))
	}
	if rq("accepts?", object.NewString("json")) != object.Bool(true) {
		t.Fatal("req.accepts?")
	}
	if _, ok := rq("session").(*object.Hash); !ok {
		t.Fatal("req.session")
	}
	if c, ok := rq("cookies").(*object.Hash); !ok {
		t.Fatal("req.cookies")
	} else if v, _ := c.Get(object.NewString("c")); str(v) != "v" {
		t.Fatal("req.cookies value")
	}
	if _, ok := rq("flash").(*HanamiFlash); !ok {
		t.Fatal("req.flash")
	}
	if str(rq("request_method")) != "POST" {
		t.Fatal("req.request_method")
	}
	if str(rq("path")) != "/p" {
		t.Fatal("req.path")
	}

	// Response readers/setters.
	if int64(rp("status").(object.Integer)) != 200 {
		t.Fatal("resp.status default")
	}
	if rp("status=", object.IntValue(302)) != object.IntValue(302) {
		t.Fatal("resp.status= returns arg")
	}
	if int64(rp("status").(object.Integer)) != 302 {
		t.Fatal("resp.status roundtrip")
	}
	if str(rp("body")) != "" {
		t.Fatal("resp.body default empty")
	}
	rp("body=", object.NewString("hi"))
	rp("write", object.NewString("!"))
	if str(rp("body")) != "hi!" {
		t.Fatalf("resp.body = %q", str(rp("body")))
	}
	// negotiate ran during Call, deriving the format from the request's Accept.
	if str(rp("format")) != "json" {
		t.Fatalf("resp.format negotiated = %q", str(rp("format")))
	}
	rp("format=", object.NewString("txt"))
	if str(rp("format")) != "txt" {
		t.Fatal("resp.format roundtrip")
	}
	if !object.IsNil(rp("[]", object.NewString("X-H"))) {
		t.Fatal("resp.[] missing header nil")
	}
	rp("[]=", object.NewString("X-H"), object.NewString("v"))
	if str(rp("[]", object.NewString("X-H"))) != "v" {
		t.Fatal("resp.[] roundtrip")
	}
	if _, ok := rp("headers").(*object.Hash); !ok {
		t.Fatal("resp.headers")
	}
	if _, ok := rp("session").(*object.Hash); !ok {
		t.Fatal("resp.session")
	}
	if !object.IsNil(rp("set_cookie", object.NewString("k"), object.NewString("v"))) {
		t.Fatal("resp.set_cookie")
	}
	if !object.IsNil(rp("delete_cookie", object.NewString("k"))) {
		t.Fatal("resp.delete_cookie")
	}

	// Flash surface.
	fl := rp("flash").(*HanamiFlash)
	fm := func(name string, args ...object.Value) object.Value {
		return vm.cHanamiFlash.methods[name].native(vm, fl, args, nil)
	}
	if fm("empty?") != object.Bool(true) {
		t.Fatal("flash empty? on fresh")
	}
	fm("[]=", object.NewString("k"), object.NewString("v"))
	if str(fm("[]", object.NewString("k"))) != "v" {
		t.Fatal("flash [] roundtrip")
	}
	if !object.IsNil(fm("[]", object.NewString("absent"))) {
		t.Fatal("flash [] missing nil")
	}
	fm("keep", object.NewString("k"))
	if fm("empty?") != object.Bool(false) {
		t.Fatal("flash empty? after set")
	}
}

// TestHanamiHelpers exercises the pure conversion helpers and the seam-adapter
// fall-through branches routing does not reach.
func TestHanamiHelpers(t *testing.T) {
	vm := New(io.Discard)

	// hanamiStr: String, Symbol, other.
	if hanamiStr(object.NewString("s")) != "s" || hanamiStr(object.Symbol("y")) != "y" || hanamiStr(object.IntValue(3)) != "3" {
		t.Fatal("hanamiStr")
	}
	// hanamiInt: Integer, Float, other.
	if hanamiInt(object.IntValue(5)) != 5 || hanamiInt(object.Float(2.9)) != 2 || hanamiInt(object.NewString("x")) != 0 {
		t.Fatal("hanamiInt")
	}
	// hanamiPath: positional String, only-Hash, empty.
	if hanamiPath([]object.Value{object.NewString("/x")}) != "/x" {
		t.Fatal("hanamiPath positional")
	}
	if hanamiPath([]object.Value{object.NewHash()}) != "/" || hanamiPath(nil) != "/" {
		t.Fatal("hanamiPath default")
	}
	// hanamiHelperName: positional, only-Hash, empty.
	if hanamiHelperName([]object.Value{object.NewString("n")}) != "n" {
		t.Fatal("hanamiHelperName positional")
	}
	if hanamiHelperName([]object.Value{object.NewHash()}) != "" || hanamiHelperName(nil) != "" {
		t.Fatal("hanamiHelperName default")
	}
	// hanamiConstraints: non-Hash yields nil.
	if hanamiConstraints(object.NewString("x")) != nil {
		t.Fatal("hanamiConstraints non-hash")
	}
	// hanamiBodyParts: Array, String, other.
	if p := hanamiBodyParts(object.NewArray(object.NewString("a"), object.IntValue(2))); p[0] != "a" || p[1] != "2" {
		t.Fatal("hanamiBodyParts array")
	}
	if p := hanamiBodyParts(object.NewString("s")); len(p) != 1 || p[0] != "s" {
		t.Fatal("hanamiBodyParts string")
	}
	if p := hanamiBodyParts(object.IntValue(9)); p[0] != "9" {
		t.Fatal("hanamiBodyParts other")
	}
	// hanamiRackResponseFrom: short Array and non-Array both yield 500.
	if hanamiRackResponseFrom(object.NewArray(object.IntValue(200))).Status != 500 {
		t.Fatal("hanamiRackResponseFrom short")
	}
	if hanamiRackResponseFrom(object.NewString("x")).Status != 500 {
		t.Fatal("hanamiRackResponseFrom non-array")
	}
	// hanamiRouterOptions: scheme-only and host-only both add a base option.
	sh := object.NewHash()
	sh.Set(object.Symbol("scheme"), object.NewString("https"))
	if len(hanamiRouterOptions(vm, []object.Value{sh})) != 1 {
		t.Fatal("hanamiRouterOptions scheme-only")
	}
	ho := object.NewHash()
	ho.Set(object.Symbol("host"), object.NewString("h"))
	if len(hanamiRouterOptions(vm, []object.Value{ho})) != 1 {
		t.Fatal("hanamiRouterOptions host-only")
	}
	// hanamiIsAction: a class (not an instance) is never an action endpoint.
	if vm.hanamiIsAction(vm.cHanamiAction) {
		t.Fatal("hanamiIsAction class")
	}
	// hanamiErrValue: a non-Ruby Go error becomes a RuntimeError.
	if ev := hanamiErrValue(vm, errors.New("boom")); vm.classOf(ev).name != "RuntimeError" {
		t.Fatalf("hanamiErrValue other = %s", vm.classOf(ev).name)
	}
	// hanamiRubyErr.Error formats class + message.
	if (&hanamiRubyErr{e: RubyError{Class: "X", Message: "m"}}).Error() != "X: m" {
		t.Fatal("hanamiRubyErr.Error")
	}
}
