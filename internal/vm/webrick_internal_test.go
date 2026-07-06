// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"io"
	"math/big"
	"strings"
	"testing"

	webrick "github.com/go-ruby-webrick/webrick"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// webrickRun runs src on a fresh VM and returns its trimmed stdout, failing on a
// parse/compile/run error.
func webrickRun(t *testing.T, src string) string {
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
		t.Fatalf("run: %v\n%s", err, buf.String())
	}
	return strings.TrimRight(buf.String(), "\n")
}

// webrickRunErr runs src on a fresh VM and returns the uncaught error's class, or
// "" if it ran clean.
func webrickRunErr(t *testing.T, src string) string {
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

// TestWEBrickValueTypes covers the wrappers' string/truthy surface and classOf.
func TestWEBrickValueTypes(t *testing.T) {
	vm := New(io.Discard)
	srv := &WEBrickServer{}
	req := &WEBrickRequest{}
	res := &WEBrickResponse{}
	for _, c := range []interface {
		ToS() string
		Inspect() string
		Truthy() bool
	}{srv, req, res} {
		if c.ToS() != c.Inspect() || !c.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c, c.ToS(), c.Inspect(), c.Truthy())
		}
	}
	if vm.classOf(srv) != vm.consts["WEBrick::HTTPServer"] ||
		vm.classOf(req) != vm.consts["WEBrick::HTTPRequest"] ||
		vm.classOf(res) != vm.consts["WEBrick::HTTPResponse"] {
		t.Fatal("classOf mismatch for WEBrick wrappers")
	}
}

// TestWEBrickServing drives the full request/response/mount/servlet/status
// surface through in-process #service dispatch (no listening socket).
func TestWEBrickServing(t *testing.T) {
	src := `
require "webrick"
srv = WEBrick::HTTPServer.new(Port: 8080, ServerName: "example.com", ServerSoftware: "test/1")
srv.mount_proc("/hello") do |req, res|
  res.status = 200
  res.content_type = "text/plain"
  res.body = "path=#{req.path} m=#{req.request_method} x=#{req.query["x"]} h=#{req["X-Test"]} miss=#{req["X-No"].inspect} v=#{req.http_version} ka=#{req.keep_alive?} host=#{req.host} port=#{req.port} sn=#{req.script_name} pi=#{req.path_info} uri=#{req.unparsed_uri} qs=#{req.query_string}"
end
srv.mount_proc("/echo") do |req, res|
  res["X-Len"] = req.content_length.to_s
  hs = []
  req.each { |k, v| hs << "#{k}=#{v}" }
  res.body = req.body + " ct=" + req.content_type.to_s + " hdrs=" + hs.sort.join(",")
end
srv.mount_proc("/redir") { |req, res| res.set_redirect(WEBrick::HTTPStatus::Found, "/hello") }
srv.mount_proc("/boom") { |req, res| raise WEBrick::HTTPStatus::NotFound, "nope" }
srv.mount_proc("/chunk") { |req, res| res.chunked = true; res.body = "abc" }

class MyServlet < WEBrick::HTTPServlet::AbstractServlet
  def do_GET(req, res); res.body = "sget #{req.path}"; end
  def do_POST(req, res); res.body = "spost"; end
end
srv.mount("/s", MyServlet)
srv.mount("/i", MyServlet.new)

def run(srv, raw)
  req = WEBrick::HTTPRequest.new
  req.parse(raw)
  res = WEBrick::HTTPResponse.new
  srv.service(req, res)
  res
end

r = run(srv, "GET /hello?x=42 HTTP/1.1\r\nHost: h.example:9\r\nX-Test: yo\r\n\r\n")
puts "hello: #{r.status} #{r.reason_phrase} | #{r.body}"
puts "hello line: #{r.status_line.start_with?("HTTP/1.1 200 OK")} ka=#{r.keep_alive?} chunked=#{r.chunked?}"
r = run(srv, "POST /echo HTTP/1.1\r\nHost: h\r\nContent-Type: text/plain\r\nContent-Length: 5\r\n\r\nhello")
puts "echo: #{r.status} | #{r.body} | xlen=#{r["X-Len"]} ctmiss=#{r.content_type.inspect}"
r = run(srv, "GET /redir HTTP/1.1\r\nHost: h\r\n\r\n")
puts "redir: #{r.status} loc=#{r["Location"]} body=#{r.body}"
r = run(srv, "GET /boom HTTP/1.1\r\nHost: h\r\n\r\n")
puts "boom: #{r.status} #{r.reason_phrase}"
r = run(srv, "GET /chunk HTTP/1.1\r\nHost: h\r\n\r\n")
puts "chunk: #{r.status} chunked=#{r.chunked?}"
r = run(srv, "GET /s/x HTTP/1.1\r\nHost: h\r\n\r\n")
puts "sget: #{r.status} #{r.body}"
r = run(srv, "POST /s HTTP/1.1\r\nHost: h\r\nContent-Length: 0\r\n\r\n")
puts "spost: #{r.status} #{r.body}"
r = run(srv, "DELETE /s HTTP/1.1\r\nHost: h\r\n\r\n")
puts "sdel: #{r.status} #{r.reason_phrase}"
r = run(srv, "OPTIONS /hello HTTP/1.1\r\nHost: h\r\n\r\n")
puts "opts: #{r.status} allow=#{r["Allow"]}"
r = run(srv, "GET /nowhere HTTP/1.1\r\nHost: h\r\n\r\n")
puts "nowhere: #{r.status} #{r.reason_phrase}"
r = run(srv, "DELETE /hello HTTP/1.1\r\nHost: h\r\n\r\n")
puts "mna: #{r.status} #{r.reason_phrase}"
r = run(srv, "GET /i/y HTTP/1.1\r\nHost: h\r\n\r\n")
puts "inst: #{r.status} #{r.body}"
puts "toS: #{run(srv, "GET /s/x HTTP/1.1\r\nHost: h\r\n\r\n").to_s.start_with?("HTTP/1.1 200 OK")}"
puts "config: #{srv.config.inspect}"
puts "version: #{WEBrick::VERSION}"
puts "status: rp=#{WEBrick::HTTPStatus.reason_phrase(404)} i=#{WEBrick::HTTPStatus.info?(100)} s=#{WEBrick::HTTPStatus.success?(200)} rd=#{WEBrick::HTTPStatus.redirect?(302)} e=#{WEBrick::HTTPStatus.error?(404)} ce=#{WEBrick::HTTPStatus.client_error?(404)} se=#{WEBrick::HTTPStatus.server_error?(500)}"
begin; raise WEBrick::HTTPStatus::Forbidden, "no"; rescue => e; puts "rescue: #{e.class} code=#{e.code} to_i=#{e.to_i} rp=#{e.reason_phrase}"; end
srv.start; srv.run; srv.shutdown; srv.stop
srv.unmount("/i"); srv.umount("/s")

# set_error with a coded status class and with a plain object.
res = WEBrick::HTTPResponse.new
res.set_error(WEBrick::HTTPStatus::BadRequest)
puts "seterr-coded: #{res.status}"
res2 = WEBrick::HTTPResponse.new
res2.set_error("boom")
puts "seterr-plain: #{res2.status}"

# content_length= / body= / []= round-trips.
res3 = WEBrick::HTTPResponse.new
res3.content_length = 3
res3.body = "xyz"
res3["X-K"] = "v"
res3.content_type = "text/html"
puts "resp: cl=#{res3["Content-Length"]} body=#{res3.body} k=#{res3["X-K"]} ct=#{res3.content_type} miss=#{res3["X-No"].inspect}"
`
	got := webrickRun(t, src)
	want := strings.Join([]string{
		"hello: 200 OK | path=/hello m=GET x=42 h=yo miss=nil v=1.1 ka=true host=h.example port=9 sn=/hello pi= uri=/hello?x=42 qs=x=42",
		"hello line: true ka=true chunked=false",
		"echo: 200 | hello ct=text/plain hdrs=content-length=5,content-type=text/plain,host=h | xlen=5 ctmiss=nil",
		"redir: 302 loc=/hello body=<HTML><A HREF=\"/hello\">/hello</A>.</HTML>",
		"boom: 404 Not Found",
		"chunk: 200 chunked=true",
		"sget: 200 sget /s/x",
		"spost: 200 spost",
		"sdel: 405 Method Not Allowed",
		"opts: 200 allow=GET,HEAD,OPTIONS,POST,PUT",
		"nowhere: 404 Not Found",
		"mna: 405 Method Not Allowed",
		"inst: 200 sget /i/y",
		"toS: true",
		"config: {Port: 8080, ServerName: \"example.com\", ServerSoftware: \"test/1\"}",
		"version: 1.9.2",
		"status: rp=Not Found i=true s=true rd=true e=true ce=true se=true",
		"rescue: WEBrick::HTTPStatus::Forbidden code=403 to_i=403 rp=Forbidden",
		"seterr-coded: 400",
		"seterr-plain: 500",
		"resp: cl=3 body=xyz k=v ct=text/html miss=nil",
	}, "\n")
	if got != want {
		t.Fatalf("webrick serving mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestWEBrickRubyErrors covers the raising branches reachable from Ruby: the
// arity / missing-block / not-parsed / wrong-type / non-status-raise / malformed
// parse / not-a-status-redirect paths.
func TestWEBrickRubyErrors(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`require "webrick"; WEBrick::HTTPServer.new.mount_proc("/x")`, "ArgumentError"}, // no block
		{`require "webrick"; WEBrick::HTTPServer.new.mount("/x")`, "ArgumentError"},      // <2 args
		{`require "webrick"; WEBrick::HTTPRequest.new.parse`, "ArgumentError"},           // parse no arg
		{`require "webrick"; WEBrick::HTTPRequest.new.path`, "RuntimeError"},             // not parsed
		{`require "webrick"; WEBrick::HTTPServer.new.service(1, 2)`, "TypeError"},        // bad req type
		{`require "webrick"
r = WEBrick::HTTPRequest.new; r.parse("GET / HTTP/1.1\r\nHost: h\r\n\r\n")
WEBrick::HTTPServer.new.service(r, 2)`, "TypeError"}, // bad res type
		{`require "webrick"; res = WEBrick::HTTPResponse.new; res.request = 5`, "TypeError"}, // request= wrong type
		{`require "webrick"
srv = WEBrick::HTTPServer.new
srv.service(WEBrick::HTTPRequest.new, WEBrick::HTTPResponse.new)`, "RuntimeError"}, // unparsed req in service
		{`require "webrick"
srv = WEBrick::HTTPServer.new
srv.mount_proc("/x") { |req, res| raise "generic" }
r = WEBrick::HTTPRequest.new; r.parse("GET /x HTTP/1.1\r\nHost: h\r\n\r\n")
srv.service(r, WEBrick::HTTPResponse.new)`, "RuntimeError"}, // non-status raise propagates
		{`require "webrick"; WEBrick::HTTPRequest.new.parse("GET /x HTTP/9\r\n")`, "WEBrick::HTTPStatus::BadRequest"}, // malformed -> mapped status
		{`require "webrick"; WEBrick::HTTPRequest.new.parse("")`, "RuntimeError"},                                     // EOF -> unmapped status
		{`require "webrick"; WEBrick::HTTPResponse.new.set_redirect(42, "/x")`, "ArgumentError"},                      // not a status
		{`require "webrick"; WEBrick::HTTPResponse.new.set_redirect(WEBrick::HTTPStatus::Found)`, "ArgumentError"},    // <2 args
		{`require "webrick"
res = WEBrick::HTTPResponse.new; res["X"] = "a\r\nb"; res.to_s`, "WEBrick::HTTPResponse::InvalidHeader"}, // CRLF header
		{`require "webrick"; WEBrick::HTTPRequest.new.parse("GET / HTTP/1.1\r\nHost: h\r\n\r\n").each`, "LocalJumpError"}, // each no block
		{`require "webrick"; WEBrick::HTTPResponse.new.status = "x"`, "TypeError"},                                        // non-integer status
	}
	for _, c := range cases {
		if got := webrickRunErr(t, c.src); got != c.want {
			t.Errorf("src %q: got %q want %q", c.src, got, c.want)
		}
	}
}

// TestWEBrickArityDirect covers the native-level arity guards that Ruby call
// syntax cannot express (a bare setter with zero args, unmount with none, etc.).
func TestWEBrickArityDirect(t *testing.T) {
	vm := New(io.Discard)
	srv := &WEBrickServer{srv: webrick.NewHTTPServer(webrick.DefaultConfig()), cfg: webrick.DefaultConfig()}
	srvCls := vm.consts["WEBrick::HTTPServer"].(*RClass)
	reqCls := vm.consts["WEBrick::HTTPRequest"].(*RClass)
	resCls := vm.consts["WEBrick::HTTPResponse"].(*RClass)
	res := &WEBrickResponse{res: webrick.NewResponse(webrick.DefaultConfig())}
	rq := &WEBrickRequest{req: mustParse(t)}

	expectRaise := func(cls, method string, m map[string]*Method, self object.Value, args []object.Value) {
		defer func() {
			r := recover()
			re, ok := r.(RubyError)
			if !ok || re.Class != cls {
				t.Errorf("%s expected %s, got %#v", method, cls, r)
			}
		}()
		m[method].native(vm, self, args, nil)
	}
	// Zero-arg guards.
	expectRaise("ArgumentError", "mount_proc", srvCls.methods, srv, nil)
	expectRaise("ArgumentError", "unmount", srvCls.methods, srv, nil)
	expectRaise("ArgumentError", "umount", srvCls.methods, srv, nil)
	expectRaise("ArgumentError", "service", srvCls.methods, srv, nil)
	expectRaise("ArgumentError", "parse", reqCls.methods, rq, nil)
	expectRaise("ArgumentError", "[]", reqCls.methods, rq, nil)
	expectRaise("ArgumentError", "status=", resCls.methods, res, nil)
	expectRaise("ArgumentError", "request=", resCls.methods, res, nil)
	expectRaise("ArgumentError", "[]", resCls.methods, res, nil)
	expectRaise("ArgumentError", "[]=", resCls.methods, res, []object.Value{object.NewString("k")})
	expectRaise("ArgumentError", "set_redirect", resCls.methods, res, nil)
	expectRaise("ArgumentError", "content_type=", resCls.methods, res, nil)
	expectRaise("ArgumentError", "content_length=", resCls.methods, res, nil)
	expectRaise("ArgumentError", "body=", resCls.methods, res, nil)
	expectRaise("ArgumentError", "chunked=", resCls.methods, res, nil)
	expectRaise("ArgumentError", "reason_phrase", vm.consts["WEBrick::HTTPStatus"].(*RClass).smethods, object.NilV, nil)
}

// mustParse builds a parsed request for the direct tests.
func mustParse(t *testing.T) *webrick.Request {
	t.Helper()
	req, st := webrick.ParseRequest([]byte("GET /p?a=1 HTTP/1.1\r\nHost: h\r\n\r\n"), webrick.DefaultConfig())
	if st != nil {
		t.Fatalf("parse: %v", st)
	}
	return req
}

// TestWEBrickHelpers exercises the pure conversion helpers and the panic->status
// mapper directly, including the fall-through branches Ruby cannot reach.
func TestWEBrickHelpers(t *testing.T) {
	vm := New(io.Discard)

	// webrickStr: String, Symbol, other.
	if webrickStr(object.NewString("s")) != "s" || webrickStr(object.Symbol("y")) != "y" || webrickStr(object.IntValue(3)) != "3" {
		t.Fatal("webrickStr")
	}
	// webrickKey: Symbol, String, other.
	if webrickKey(object.Symbol("P")) != "P" || webrickKey(object.NewString("Q")) != "Q" || webrickKey(object.IntValue(5)) != "5" {
		t.Fatal("webrickKey")
	}
	// webrickInt: Integer, Bignum, non-int -> TypeError.
	if webrickInt(object.IntValue(7)) != 7 {
		t.Fatal("webrickInt int")
	}
	if webrickInt(&object.Bignum{I: big.NewInt(11)}) != 11 {
		t.Fatal("webrickInt bignum")
	}
	func() {
		defer func() {
			if re, ok := recover().(RubyError); !ok || re.Class != "TypeError" {
				t.Errorf("webrickInt non-int: %#v", recover())
			}
		}()
		webrickInt(object.NewString("x"))
	}()

	// webrickFirstOpt: empty -> nil, non-empty -> first.
	if !object.IsNil(webrickFirstOpt(nil)) || webrickFirstOpt([]object.Value{object.IntValue(1)}) != object.IntValue(1) {
		t.Fatal("webrickFirstOpt")
	}

	// webrickConfig: non-Hash default, and a Hash mixing Symbol/String/other keys.
	if c := webrickConfig(object.NilV); c.Port != 80 {
		t.Fatal("webrickConfig default")
	}
	h := object.NewHash()
	h.Set(object.Symbol("Port"), object.IntValue(9))
	h.Set(object.NewString("ServerName"), object.NewString("n"))
	h.Set(object.Symbol("ServerSoftware"), object.NewString("sw"))
	h.Set(object.IntValue(1), object.NewString("ignored"))
	c := webrickConfig(h)
	if c.Port != 9 || c.ServerName != "n" || c.ServerSoftware != "sw" {
		t.Fatalf("webrickConfig hash = %+v", c)
	}

	// webrickCodeFromValue: class found, class not found, instance not found.
	nf := vm.consts["WEBrick::HTTPStatus::NotFound"].(*RClass)
	if code, ok := vm.webrickCodeFromValue(nf); !ok || code != 404 {
		t.Fatal("codeFromValue class")
	}
	if _, ok := vm.webrickCodeFromValue(vm.cObject); ok {
		t.Fatal("codeFromValue non-status class")
	}
	if _, ok := vm.webrickCodeFromValue(object.IntValue(1)); ok {
		t.Fatal("codeFromValue non-status value")
	}

	// webrickErrorStatus: coded -> *Status, non-coded -> plain error.
	if st, ok := vm.webrickErrorStatus(nf).(*webrick.Status); !ok || st.Code != 404 {
		t.Fatal("errorStatus coded")
	}
	if _, ok := vm.webrickErrorStatus(object.NewString("x")).(*webrick.Status); ok {
		t.Fatal("errorStatus plain should not be *Status")
	}

	// webrickStatusFromPanic: non-RubyError, Obj-status, Obj-non-status, Obj-nil
	// class-match, Obj-nil no-match.
	if _, ok := vm.webrickStatusFromPanic("not a ruby error"); ok {
		t.Fatal("fromPanic non-RubyError")
	}
	inst := &RObject{class: nf}
	if st, ok := vm.webrickStatusFromPanic(RubyError{Class: "X", Message: "m", Obj: inst}); !ok || st.Code != 404 {
		t.Fatal("fromPanic Obj status")
	}
	if _, ok := vm.webrickStatusFromPanic(RubyError{Class: "X", Obj: object.IntValue(1)}); ok {
		t.Fatal("fromPanic Obj non-status")
	}
	if st, ok := vm.webrickStatusFromPanic(RubyError{Class: "WEBrick::HTTPStatus::Forbidden"}); !ok || st.Code != 403 {
		t.Fatal("fromPanic class-name match")
	}
	if _, ok := vm.webrickStatusFromPanic(RubyError{Class: "Foo"}); ok {
		t.Fatal("fromPanic no match")
	}
}
