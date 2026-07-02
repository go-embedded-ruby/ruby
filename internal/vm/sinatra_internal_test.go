// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	rack "github.com/go-ruby-rack/rack"
	sinatra "github.com/go-ruby-sinatra/sinatra"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestSinatraWrapperInspect covers ToS / Inspect / Truthy of the Sinatra value
// wrappers.
func TestSinatraWrapperInspect(t *testing.T) {
	checks := []interface {
		ToS() string
		Inspect() string
		Truthy() bool
	}{
		&SinatraCtx{},
		&SinatraSettings{},
	}
	wantToS := []string{"#<Sinatra::Base request>", "#<Sinatra::Base settings>"}
	for i, c := range checks {
		if c.ToS() != wantToS[i] || c.Inspect() != wantToS[i] || !c.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c, c.ToS(), c.Inspect(), c.Truthy())
		}
	}
}

// TestSinatraConstants covers the module/class surface and require features.
func TestSinatraConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "sinatra/base"`, "true\n"},
		{`p require "sinatra"`, "true\n"},
		{`require "sinatra/base"; p require "sinatra/base"`, "false\n"},
		{`require "sinatra/base"; p Sinatra.is_a?(Module)`, "true\n"},
		{`require "sinatra/base"; p Sinatra::Base.class`, "Class\n"},
		{`require "sinatra/base"; p Sinatra::NotFound < StandardError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSinatraStage3 is the run-conformance headline: a classic Sinatra::Base app
// answered through its Rack #call adapter must yield [200, headers, ["hi amy"]].
func TestSinatraStage3(t *testing.T) {
	src := `require "sinatra/base"
class App < Sinatra::Base
  get "/hi" do
    "hi #{params['n']}"
  end
end
s, h, b = App.new.call("PATH_INFO"=>"/hi","QUERY_STRING"=>"n=amy","REQUEST_METHOD"=>"GET")
puts s
puts b.join
puts h.class`
	if got := eval(t, src); got != "200\nhi amy\nHash\n" {
		t.Errorf("stage3 got=%q", got)
	}
}

// TestSinatraVerbs covers every verb of the class DSL (get also serves head) via
// the class-method #call form.
func TestSinatraVerbs(t *testing.T) {
	base := `require "sinatra/base"
class V < Sinatra::Base
  get("/g"){ "g" }
  post("/p"){ "p" }
  put("/u"){ "u" }
  delete("/d"){ "d" }
  patch("/a"){ "a" }
  options("/o"){ "o" }
  head("/h"){ "h" }
end
`
	cases := []struct{ method, path, want string }{
		{"GET", "/g", "g"},
		{"POST", "/p", "p"},
		{"PUT", "/u", "u"},
		{"DELETE", "/d", "d"},
		{"PATCH", "/a", "a"},
		{"OPTIONS", "/o", "o"},
		{"HEAD", "/h", "h"}, // CallTuple returns the body; a Rack server strips it for HEAD
	}
	for _, c := range cases {
		src := base + `s, _, b = V.call("REQUEST_METHOD"=>"` + c.method + `","PATH_INFO"=>"` + c.path + `"); puts s; puts b.join`
		want := "200\n" + c.want + "\n"
		if got := eval(t, src); got != want {
			t.Errorf("%s %s got=%q want=%q", c.method, c.path, got, want)
		}
	}
}

// TestSinatraHelpers covers filters, halt, redirect, status, content_type, body,
// headers, request/response/session/settings and not_found/error handlers.
func TestSinatraHelpers(t *testing.T) {
	cases := []struct{ src, want string }{
		// before filter mutates params; route reads it.
		{`require "sinatra/base"
class A < Sinatra::Base
  before { @seen = "y" }
  get("/x"){ "b#{params['q']}" }
  after("/x"){ response }
end
_, _, b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x","QUERY_STRING"=>"q=1")
puts b.join`, "b1\n"},
		// status helper (reader + setter) and body helper.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ status 201; body "made"; status }
end
s,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts s; puts b.join`, "201\nmade\n"},
		// halt with status + body.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ halt 403, "no" }
end
s,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts s; puts b.join`, "403\nno\n"},
		// redirect sets status 303 + an absolute Location (resolved against the
		// request base URL, as Sinatra does).
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ redirect "/y", 303 }
end
s,h,_ = A.new.call("rack.url_scheme"=>"http","HTTP_HOST"=>"ex","REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts s; puts h["location"]`, "303\nhttp://ex/y\n"},
		// content_type :json.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ content_type :json; "{}" }
end
_,h,_ = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts h["content-type"]`, "application/json\n"},
		// content_type with an explicit charset: keyword.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ content_type "text/plain", charset: "iso-8859-1"; "x" }
end
_,h,_ = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts h["content-type"]`, "text/plain;charset=iso-8859-1\n"},
		// body reader form returns the buffered body.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ body "buf"; body.join }
end
_,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts b.join`, "buf\n"},
		// headers helper sets response headers.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ headers "X-A" => "1"; "ok" }
end
_,h,_ = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts h["x-a"]`, "1\n"}, // rack Headers are lower-cased
		// request accessor.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ request.request_method }
end
_,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts b.join`, "GET\n"},
		// not_found handler.
		{`require "sinatra/base"
class A < Sinatra::Base
  not_found { "missing" }
end
s,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/nope")
puts s; puts b.join`, "404\nmissing\n"},
		// error handler for a bare status returned by a route.
		{`require "sinatra/base"
class A < Sinatra::Base
  error(500) { "boom" }
  get("/x"){ 500 }
end
s,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts s; puts b.join`, "500\nboom\n"},
		// pass falls through to the next matching route.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ pass }
  get("/x"){ "second" }
end
_,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts b.join`, "second\n"},
		// session helper (nil when no rack.session).
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ session.inspect }
end
_,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts b.join`, "nil\n"},
		// session helper returns the injected rack.session value.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ session }
end
_,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x","rack.session"=>"tok")
puts b.join`, "tok\n"},
		// uri / url helpers.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ [uri("/a"), url("//h/b")].join(" ") }
end
_,_,b = A.new.call("rack.url_scheme"=>"http","HTTP_HOST"=>"ex","REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts b.join`, "http://ex/a //h/b\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSinatraSettings covers set/enable/disable, configure, the settings view
// (#[], respond_to?, method_missing with and without ?), and the default
// content type they drive.
func TestSinatraSettings(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "sinatra/base"
class A < Sinatra::Base
  set :name, "app"
  enable :logging
  disable :sessions
  get("/x"){ "#{settings[:name]} #{settings.logging?} #{settings.sessions?} #{settings.name}" }
end
_,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts b.join`, "app true false app\n"},
		{`require "sinatra/base"
class A < Sinatra::Base
  configure { set :env, "test" }
  get("/x"){ "#{settings.respond_to?(:env)} #{settings.env} #{settings.respond_to?(:nope)} #{settings.nope.inspect}" }
end
_,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts b.join`, "true test false nil\n"},
		// set default_content_type drives the response content type.
		{`require "sinatra/base"
class A < Sinatra::Base
  set "default_content_type", "text/plain"
  get("/x"){ "hi" }
end
_,h,_ = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts h["content-type"]`, "text/plain;charset=utf-8\n"}, // Sinatra appends the default charset to text/*
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSinatraHelpersMethod covers helpers { def … } grafting a method onto the
// request context.
func TestSinatraHelpersMethod(t *testing.T) {
	src := `require "sinatra/base"
class A < Sinatra::Base
  helpers do
    def shout(s); s.upcase; end
  end
  get("/x"){ shout("hi") }
end
_,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts b.join`
	if got := eval(t, src); got != "HI\n" {
		t.Errorf("helpers got=%q", got)
	}
}

// TestSinatraInheritance covers a subclass inheriting a parent's routes.
func TestSinatraInheritance(t *testing.T) {
	src := `require "sinatra/base"
class Parent < Sinatra::Base
  get("/base"){ "from-parent" }
end
class Child < Parent
  get("/own"){ "from-child" }
end
_,_,b1 = Child.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/base")
_,_,b2 = Child.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/own")
puts b1.join; puts b2.join`
	if got := eval(t, src); got != "from-parent\nfrom-child\n" {
		t.Errorf("inheritance got=%q", got)
	}
}

// TestSinatraErrors covers the error arms: an unknown content_type raises, a bad
// env raises, and a route returning an unhandled type is coerced via to_s.
func TestSinatraErrors(t *testing.T) {
	// Unknown media type -> RuntimeError surfaced from #call.
	class, _ := evalErr(t, `require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ content_type :nope; "x" }
end
A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")`)
	if class != "RuntimeError" {
		t.Errorf("unknown content_type class=%q want RuntimeError", class)
	}
	// content_type with no argument.
	class, _ = evalErr(t, `require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ content_type }
end
A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")`)
	if class != "ArgumentError" {
		t.Errorf("content_type no-arg class=%q", class)
	}
	// redirect with no argument.
	class, _ = evalErr(t, `require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ redirect }
end
A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")`)
	if class != "ArgumentError" {
		t.Errorf("redirect no-arg class=%q", class)
	}
	// set with too few arguments.
	class, _ = evalErr(t, `require "sinatra/base"
class A < Sinatra::Base
  set :only
end`)
	if class != "ArgumentError" {
		t.Errorf("set one-arg class=%q", class)
	}
}

// TestSinatraReturnCoercion covers sinatraResult's arms: an Integer return is a
// status, an Array of strings is the body parts, a non-string is to_s'd, and a
// route whose body is set by a helper (nil return) keeps that body.
func TestSinatraReturnCoercion(t *testing.T) {
	cases := []struct{ src, want string }{
		// Integer return -> status only, empty body.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ 204 }
end
s,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts s; puts b.join.length`, "204\n0\n"},
		// Array of strings -> body parts.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ ["a","b"] }
end
_,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts b.join`, "ab\n"},
		// Non-string, non-array -> to_s.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ 42.5 }
end
_,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts b.join`, "42.5\n"},
		// nil return with body set by helper.
		{`require "sinatra/base"
class A < Sinatra::Base
  get("/x"){ body "set"; nil }
end
_,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts b.join`, "set\n"},
		// empty route block -> nil action, empty body.
		{`require "sinatra/base"
class A < Sinatra::Base
  get "/x"
end
s,_,b = A.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts s; puts b.join.length`, "200\n0\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSinatraClassOf covers the RClass, RObject and default (raise) arms.
func TestSinatraClassOf(t *testing.T) {
	c := newClass("K", nil)
	if sinatraClassOf(c) != c {
		t.Error("RClass arm")
	}
	obj := &RObject{class: c}
	if sinatraClassOf(obj) != c {
		t.Error("RObject arm")
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("default arm: expected a raise")
		}
		if re, ok := r.(RubyError); !ok || re.Class != "TypeError" {
			t.Errorf("default arm: got %#v", r)
		}
	}()
	sinatraClassOf(object.Integer(1))
}

// TestSinatraPatternHelpers covers the pattern/optional-pattern defaults and
// sinatraInt / sinatraVerbName / sinatraSettingValue arms directly.
func TestSinatraPatternHelpers(t *testing.T) {
	if sinatraPattern(nil) != "/" || sinatraPattern([]object.Value{object.NewString("/p")}) != "/p" {
		t.Error("sinatraPattern")
	}
	if sinatraOptPattern(nil) != "" || sinatraOptPattern([]object.Value{object.NewString("/f")}) != "/f" {
		t.Error("sinatraOptPattern")
	}
	if sinatraInt(object.Integer(3), 0) != 3 || sinatraInt(object.NewString("7"), 0) != 7 ||
		sinatraInt(object.NewString("x"), 9) != 9 || sinatraInt(object.NilV, 4) != 4 {
		t.Error("sinatraInt")
	}
	if sinatraVerbName("GET") != "get" {
		t.Error("sinatraVerbName")
	}
	checks := []struct {
		in   object.Value
		want any
	}{
		{object.Bool(true), true},
		{object.Integer(5), 5},
		{object.NewString("s"), "s"},
		{object.Symbol("y"), "y"},
		{object.NilV, ""},
	}
	for _, c := range checks {
		if got := sinatraSettingValue(c.in); got != c.want {
			t.Errorf("sinatraSettingValue(%#v)=%#v want %#v", c.in, got, c.want)
		}
	}
}

// TestSinatraHaltArgs covers the Integer / String / Array / ignored arms.
func TestSinatraHaltArgs(t *testing.T) {
	got := sinatraHaltArgs([]object.Value{
		object.Integer(500),
		object.NewString("body"),
		&object.Array{Elems: []object.Value{object.NewString("a"), object.Integer(2)}},
		object.NilV, // ignored
	})
	if len(got) != 3 {
		t.Fatalf("len=%d want 3 (%#v)", len(got), got)
	}
	if got[0] != 500 || got[1] != "body" {
		t.Errorf("scalar arms got=%#v", got)
	}
	parts, ok := got[2].([]string)
	if !ok || len(parts) != 2 || parts[0] != "a" || parts[1] != "2" {
		t.Errorf("array arm got=%#v", got[2])
	}
}

// TestSinatraStr covers the String / Symbol / default arms.
func TestSinatraStr(t *testing.T) {
	if sinatraStr(object.NewString("x")) != "x" || sinatraStr(object.Symbol("y")) != "y" || sinatraStr(object.Integer(3)) != "3" {
		t.Error("sinatraStr arms")
	}
}

// TestSinatraChainEmpty covers sinatraChain for a class with no recorded def
// (the defs-map lookup miss) — buildSinatraApp still yields a usable 404 app.
func TestSinatraChainEmpty(t *testing.T) {
	src := `require "sinatra/base"
class Empty < Sinatra::Base
end
s,_,_ = Empty.new.call("REQUEST_METHOD"=>"GET","PATH_INFO"=>"/x")
puts s`
	if got := eval(t, src); got != "404\n" {
		t.Errorf("empty app got=%q", got)
	}
}

// TestSinatraParamsHashDirect exercises sinatraParamsHash against a library
// context built directly, covering the []string (splat) value path.
func TestSinatraParamsHashDirect(t *testing.T) {
	app := sinatra.New()
	var captured *object.Hash
	app.Get("/a/*", func(c *sinatra.Context) any {
		captured = sinatraParamsHash(c)
		return "ok"
	})
	app.CallTuple(rack.Env{"REQUEST_METHOD": "GET", "PATH_INFO": "/a/x", "QUERY_STRING": "n=1"})
	if captured == nil {
		t.Fatal("action not invoked")
	}
	if v, ok := captured.Get(object.NewString("n")); !ok || v.(*object.String).Str() != "1" {
		t.Errorf("scalar param missing: %#v", v)
	}
	if v, ok := captured.Get(object.NewString("splat")); !ok {
		t.Errorf("splat param missing")
	} else if _, isArr := v.(*object.Array); !isArr {
		t.Errorf("splat should be an Array, got %#v", v)
	}
}
