// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"io"
	"strings"
	"testing"

	rack "github.com/go-ruby-rack/rack"
	roda "github.com/go-ruby-roda/roda"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// rodaRun runs src on a fresh VM and returns its trimmed stdout, failing on a
// parse/compile/run error.
func rodaRun(t *testing.T, src string) string {
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

// rodaRunErr runs src on a fresh VM and returns the uncaught error's class, or ""
// if it ran clean.
func rodaRunErr(t *testing.T, src string) string {
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

// TestRodaValueTypes covers the value wrappers' string/truthy surface.
func TestRodaValueTypes(t *testing.T) {
	vm := New(io.Discard)
	req := &RodaReq{cls: vm.cRodaRequest}
	resp := &RodaResp{cls: vm.cRodaResponse}
	for _, c := range []interface {
		ToS() string
		Inspect() string
		Truthy() bool
	}{req, resp} {
		if c.ToS() != c.Inspect() || !c.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c, c.ToS(), c.Inspect(), c.Truthy())
		}
	}
	if vm.classOf(req) != vm.cRodaRequest || vm.classOf(resp) != vm.cRodaResponse {
		t.Fatal("classOf mismatch for Roda wrappers")
	}
}

// TestRodaRouting drives a Roda app through every matcher/verb/accessor, both as
// a class method (App.call) and via an instance (App.new.call), and through an
// inheriting subclass.
func TestRodaRouting(t *testing.T) {
	src := `
require "roda"
class App < Roda
  route do |r|
    r.root { "home" }
    r.get("about") { "about" }
    r.on "users", Integer do |id|
      r.get { "user #{id}" }
      r.post { "create #{id}" }
    end
    r.is("name", String) { |n| "name=#{n}" }
    r.on "sym", :seg do |s| "sym=#{s}" end
    r.put("edit") { "edited" }
    r.delete("del") { "deleted" }
    r.on "flag", true do "flagged" end
    r.on "never", false do "unreachable" end
    r.get("go") { r.redirect "/there" }
    r.get("go2") { r.redirect "/here", 301 }
    r.get("stop") { r.halt }
    r.get("meth") { r.request_method }
    r.get("path") { r.path }
    r.get("rem") { r.remaining_path }
    r.get("q") { r.params["x"].to_s }
    r.on("caps") { r.on(:a) { r.captures.inspect } }
    r.get("resp") do
      r.response.status = 201
      r.response.write("body-")
      "written"
    end
    r.get("same") { (r.request.equal?(r) ? "same" : "diff") }
    42
  end
end
class Sub < App
end

def show(app, method, path)
  env = { "REQUEST_METHOD" => method, "PATH_INFO" => path, "QUERY_STRING" => "x=9" }
  status, _, body = app.call(env)
  puts "#{method} #{path} -> #{status} #{body.join}"
end

show(App, "GET", "/")
show(App, "GET", "/about")
show(App, "GET", "/users/7")
show(App, "POST", "/users/7")
show(App, "GET", "/name/bob")
show(App, "GET", "/sym/zz")
show(App, "PUT", "/edit")
show(App, "DELETE", "/del")
show(App, "GET", "/flag")
show(App, "GET", "/never")
show(App, "GET", "/go")
show(App, "GET", "/go2")
show(App, "GET", "/stop")
show(App, "GET", "/meth")
show(App, "GET", "/path")
show(App, "GET", "/rem")
show(App, "GET", "/q")
show(App, "GET", "/caps/hi")
show(App, "GET", "/resp")
show(App, "GET", "/same")
show(App, "GET", "/nowhere")
show(App.new, "GET", "/about")
show(Sub, "GET", "/about")

begin
  App.call({ "REQUEST_METHOD" => "GET", "PATH_INFO" => "/q", "QUERY_STRING" => "%ZZ" })
rescue ArgumentError
  puts "bad-query rescued"
end
`
	got := rodaRun(t, src)
	want := strings.Join([]string{
		"GET / -> 200 home",
		"GET /about -> 200 about",
		"GET /users/7 -> 200 user 7",
		"POST /users/7 -> 200 create 7",
		"GET /name/bob -> 200 name=bob",
		"GET /sym/zz -> 200 sym=zz",
		"PUT /edit -> 200 edited",
		"DELETE /del -> 200 deleted",
		"GET /flag -> 200 flagged",
		"GET /never -> 404 ",
		"GET /go -> 302 ",
		"GET /go2 -> 301 ",
		"GET /stop -> 404 ",
		"GET /meth -> 200 GET",
		"GET /path -> 200 /path",
		"GET /rem -> 200 ",
		"GET /q -> 200 9",
		"GET /caps/hi -> 200 [\"hi\"]",
		"GET /resp -> 201 body-written",
		"GET /same -> 200 same",
		"GET /nowhere -> 404 ",
		"GET /about -> 200 about",
		"GET /about -> 200 about",
		"bad-query rescued",
	}, "\n")
	if got != want {
		t.Fatalf("roda routing mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestRodaErrors covers the raising branches: a route with no block, calling an
// app that never declared a route, and the RodaResponse/redirect arity checks.
func TestRodaErrors(t *testing.T) {
	if cls := rodaRunErr(t, "require \"roda\"\nclass X < Roda; end\nX.route\n"); cls != "ArgumentError" {
		t.Fatalf("route without block: got %q", cls)
	}
	if cls := rodaRunErr(t, "require \"roda\"\nclass X < Roda; end\nX.call({})\n"); cls != "Roda::RodaError" {
		t.Fatalf("call without route: got %q", cls)
	}

	vm := New(io.Discard)
	resp := &RodaResp{r: roda.NewResponse(), cls: vm.cRodaResponse}
	req := &RodaReq{r: nil, cls: vm.cRodaRequest}
	cases := []struct {
		cls    *RClass
		method string
		self   object.Value
		args   []object.Value
	}{
		{vm.cRodaResponse, "status=", resp, nil},
		{vm.cRodaResponse, "[]", resp, nil},
		{vm.cRodaResponse, "[]=", resp, []object.Value{object.NewString("k")}},
		{vm.cRodaResponse, "redirect", resp, nil},
		{vm.cRodaRequest, "redirect", req, nil},
	}
	for _, c := range cases {
		func() {
			defer func() {
				r := recover()
				re, ok := r.(RubyError)
				if !ok || re.Class != "ArgumentError" {
					t.Errorf("%s.%s: expected ArgumentError, got %#v", c.cls.name, c.method, r)
				}
			}()
			c.cls.methods[c.method].native(vm, c.self, c.args, nil)
		}()
	}
}

// TestRodaResponseDirect covers the RodaResponse accessors not hit by routing:
// status/[]/headers/body/empty?/finish/redirect(2-arg).
func TestRodaResponseDirect(t *testing.T) {
	vm := New(io.Discard)
	r := roda.NewResponse()
	resp := &RodaResp{r: r, cls: vm.cRodaResponse}
	m := func(name string, args ...object.Value) object.Value {
		return vm.cRodaResponse.methods[name].native(vm, resp, args, nil)
	}
	if m("empty?") != object.Bool(true) {
		t.Fatal("new response should be empty")
	}
	m("status=", object.IntValue(203))
	if int64(m("status").(object.Integer)) != 203 {
		t.Fatal("status roundtrip")
	}
	m("[]=", object.NewString("X-A"), object.NewString("v"))
	if m("[]", object.NewString("X-A")).(*object.String).Str() != "v" {
		t.Fatal("header roundtrip")
	}
	m("write", object.NewString("hi"))
	if m("empty?") != object.Bool(false) {
		t.Fatal("response should be non-empty after write")
	}
	if m("body").(*object.Array).Elems[0].(*object.String).Str() != "hi" {
		t.Fatal("body")
	}
	if _, ok := m("headers").(*object.Hash); !ok {
		t.Fatal("headers hash")
	}
	m("redirect", object.NewString("/x"), object.IntValue(307))
	fin := m("finish").(*object.Array)
	if int64(fin.Elems[0].(object.Integer)) != 307 {
		t.Fatalf("finish status = %v", fin.Elems[0])
	}
}

// TestRodaHelpers exercises the pure conversion helpers directly, including the
// fall-through branches Ruby routing does not reach.
func TestRodaHelpers(t *testing.T) {
	vm := New(io.Discard)

	// rodaStr: String, Symbol, other.
	if rodaStr(object.NewString("s")) != "s" || rodaStr(object.Symbol("y")) != "y" || rodaStr(object.IntValue(3)) != "3" {
		t.Fatal("rodaStr")
	}
	// rodaInt: Integer, Float, other.
	if rodaInt(object.IntValue(5)) != 5 || rodaInt(object.Float(2.9)) != 2 || rodaInt(object.NewString("x")) != 0 {
		t.Fatal("rodaInt")
	}
	// rodaCaptureValue: string, int, int64, bool, default nil.
	if rodaCaptureValue("a").(*object.String).Str() != "a" {
		t.Fatal("cap string")
	}
	if int64(rodaCaptureValue(7).(object.Integer)) != 7 || int64(rodaCaptureValue(int64(8)).(object.Integer)) != 8 {
		t.Fatal("cap int")
	}
	if rodaCaptureValue(true) != object.Bool(true) {
		t.Fatal("cap bool")
	}
	if !object.IsNil(rodaCaptureValue(1.5)) {
		t.Fatal("cap default")
	}
	// rodaBodyResult: String, Array, other(false).
	if h, b := rodaBodyResult(object.NewString("x")); !h || b.(string) != "x" {
		t.Fatal("body string")
	}
	if h, b := rodaBodyResult(object.NewArray(object.NewString("a"), object.IntValue(2))); !h || b.([]string)[1] != "2" {
		t.Fatal("body array")
	}
	if h, _ := rodaBodyResult(object.IntValue(9)); h {
		t.Fatal("body int should be unhandled")
	}
	// rodaMatcher: string, symbol, bool, integer, String/Integer classes, other
	// class, fall-through.
	if vm.rodaMatcher(object.NewString("p")).(string) != "p" {
		t.Fatal("m string")
	}
	if vm.rodaMatcher(object.Symbol("id")).(roda.Sym) != roda.Sym("id") {
		t.Fatal("m sym")
	}
	if vm.rodaMatcher(object.Bool(true)).(bool) != true {
		t.Fatal("m bool")
	}
	if vm.rodaMatcher(object.IntValue(3)).(string) != "3" {
		t.Fatal("m int")
	}
	if _, ok := vm.rodaMatcher(vm.consts["String"]).(roda.StringMatcher); !ok {
		t.Fatal("m String class")
	}
	if _, ok := vm.rodaMatcher(vm.consts["Integer"]).(roda.IntegerMatcher); !ok {
		t.Fatal("m Integer class")
	}
	if vm.rodaMatcher(vm.consts["Array"]).(string) != "Array" {
		t.Fatal("m other class")
	}
	if vm.rodaMatcher(object.Float(1.0)).(string) != "1.0" {
		t.Fatal("m fallthrough")
	}
	// rodaHashMatcher: method-as-array, method-as-string, param, extension, and an
	// ignored key.
	h := object.NewHash()
	h.Set(object.NewString("method"), object.NewArray(object.NewString("GET"), object.NewString("POST")))
	h.Set(object.NewString("param"), object.NewString("id"))
	h.Set(object.NewString("extension"), object.NewString("json"))
	h.Set(object.NewString("ignored"), object.NewString("x"))
	hm := vm.rodaHashMatcher(h)
	if len(hm["method"].([]any)) != 2 || hm["param"] != "id" || hm["extension"] != "json" {
		t.Fatalf("hash matcher (array method) = %#v", hm)
	}
	h2 := object.NewHash()
	h2.Set(object.Symbol("method"), object.NewString("PUT"))
	if vm.rodaHashMatcher(h2)["method"] != "PUT" {
		t.Fatal("hash matcher (string method)")
	}
	// rodaMatcher dispatching to the Hash matcher.
	if _, ok := vm.rodaMatcher(h2).(roda.Hash); !ok {
		t.Fatal("rodaMatcher hash case")
	}
}

// TestRodaRequestDirect drives the RodaRequest redirect (single- and two-arg)
// and halt via the library's route-block seam, so the branches that terminate
// the request with a panic are exercised deterministically.
func TestRodaRequestDirect(t *testing.T) {
	vm := New(io.Discard)
	env := func() rack.Env {
		return rack.Env{"REQUEST_METHOD": "GET", "PATH_INFO": "/"}
	}
	call := func(name string, args ...object.Value) (int, *rack.Headers) {
		app := roda.New(func(rr *roda.RodaRequest) (bool, any) {
			req := &RodaReq{r: rr, cls: vm.cRodaRequest}
			vm.cRodaRequest.methods[name].native(vm, req, args, nil)
			return false, nil
		})
		status, headers, _ := app.Call(env())
		return status, headers
	}
	if st, h := call("redirect", object.NewString("/one")); st != 302 || h.Get("location") != "/one" {
		t.Fatalf("1-arg redirect: %d %q", st, h.Get("location"))
	}
	if st, _ := call("redirect", object.NewString("/two"), object.IntValue(303)); st != 303 {
		t.Fatalf("2-arg redirect: %d", st)
	}
	if st, _ := call("halt"); st != 404 {
		t.Fatalf("halt: %d", st)
	}
}
