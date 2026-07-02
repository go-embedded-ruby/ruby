// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"testing"
)

// TestGrapeConstants covers the Grape module, its Router / Validator / Formatter
// classes and the Grape::Exceptions tree (require "grape").
func TestGrapeConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "grape"; p Grape.is_a?(Module)`, "true\n"},
		{`p require "grape"`, "true\n"},
		{`require "grape"; p require "grape"`, "false\n"},
		{`require "grape"; p Grape::Exceptions::ValidationErrors < StandardError`, "true\n"},
		{`require "grape"; p Grape::Router.new.class`, "Grape::Router\n"},
		{`require "grape"; p Grape::Formatter.new.class`, "Grape::Formatter\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestGrapeRouter covers route declaration (every verb), #match's ok / 404 / 405
// decisions, path-param capture, and the Route / Match accessors.
func TestGrapeRouter(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "grape"
r = Grape::Router.new
r.get("/users/:id", "h")
m = r.match("GET", "/users/42")
p m.status
p m.ok?
p m.params
p m.handler
p m.route.http_method
p m.route.pattern`, ":ok\ntrue\n{\"id\" => \"42\"}\n\"h\"\n\"GET\"\n\"/users/:id\"\n"},
		// 405: path matches a route but not the method; Allowed carries the verbs.
		{`require "grape"
r = Grape::Router.new
r.get("/x", "h")
m = r.match("POST", "/x")
p m.method_not_allowed?
p m.allowed
p m.route
p m.handler`, "true\n[\"GET\"]\nnil\nnil\n"},
		// 404: no path matches.
		{`require "grape"
r = Grape::Router.new
r.get("/x", "h")
m = r.match("GET", "/nope")
p m.not_found?`, "true\n"},
		// The other verbs declare routes too.
		{`require "grape"
r = Grape::Router.new
r.post("/a", "p"); r.put("/a", "u"); r.patch("/a", "pa"); r.delete("/a", "d"); r.head("/a", "h")
p r.match("PUT", "/a").handler
p r.match("DELETE", "/a").handler`, "\"u\"\n\"d\"\n"},
		// A block form supplies the handler (a Proc).
		{`require "grape"
r = Grape::Router.new
r.get("/b") { 1 }
p r.match("GET", "/b").handler.is_a?(Proc)`, "true\n"},
		// A route declared with no handler reports nil.
		{`require "grape"
r = Grape::Router.new
r.get("/c")
p r.match("GET", "/c").route.handler`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestGrapeValidator covers the params DSL (requires/optional with type / values
// / default / regexp), coercion, and the ValidationErrors raise.
func TestGrapeValidator(t *testing.T) {
	cases := []struct{ src, want string }{
		// Integer coercion + optional passthrough.
		{`require "grape"
v = Grape::Validator.new { requires :id, type: Integer; optional :q, type: String }
p v.validate({"id" => "7", "q" => "hi"})`, "{\"id\" => 7, \"q\" => \"hi\"}\n"},
		// A missing required param raises ValidationErrors with Grape's message.
		{`require "grape"
v = Grape::Validator.new { requires :id, type: Integer }
begin; v.validate({}); rescue Grape::Exceptions::ValidationErrors => e; p e.message; end`, "\"id is missing\"\n"},
		// values: constrains the allowed set (an out-of-set value is invalid).
		{`require "grape"
v = Grape::Validator.new { requires :c, type: String, values: ["a", "b"] }
begin; v.validate({"c" => "z"}); rescue Grape::Exceptions::ValidationErrors; p :invalid; end`, ":invalid\n"},
		// default: supplies a value when the param is absent.
		{`require "grape"
v = Grape::Validator.new { optional :n, type: Integer, default: 5 }
p v.validate({})`, "{\"n\" => 5}\n"},
		// regexp: constrains a String via the rbgo regexp engine.
		{`require "grape"
v = Grape::Validator.new { requires :code, type: String, regexp: /\A[A-Z]+\z/ }
p v.validate({"code" => "ABC"})
begin; v.validate({"code" => "ab1"}); rescue Grape::Exceptions::ValidationErrors; p :bad; end`, "{\"code\" => \"ABC\"}\n:bad\n"},
		// allow_blank: false rejects an empty string.
		{`require "grape"
v = Grape::Validator.new { requires :s, type: String, allow_blank: false }
begin; v.validate({"s" => ""}); rescue Grape::Exceptions::ValidationErrors; p :blank; end`, ":blank\n"},
		// An empty params scope validates anything (no declarations).
		{`require "grape"
v = Grape::Validator.new
p v.validate({"x" => "1"})`, "{}\n"},
		// validate with no argument raises ArgumentError; a non-Hash raises TypeError.
		{`require "grape"
v = Grape::Validator.new
begin; v.validate; rescue ArgumentError; p :argerr; end
begin; v.validate("nope"); rescue TypeError; p :typeerr; end`, ":argerr\n:typeerr\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestGrapeFormatter covers #format / #json / #txt / #xml and the module-level
// Grape.mime_for / Grape.default_status helpers.
func TestGrapeFormatter(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "grape"; body, mime = Grape::Formatter.new.format("json", {"a" => 1}); p body; p mime`, "\"{\\\"a\\\":1}\"\n\"application/json\"\n"},
		{`require "grape"; p Grape::Formatter.new.json({"a" => [1, 2]})`, "\"{\\\"a\\\":[1,2]}\"\n"},
		{`require "grape"; p Grape::Formatter.new.txt("hi")`, "\"hi\"\n"},
		{`require "grape"; p Grape::Formatter.new.xml({"root" => "v"}).is_a?(String)`, "true\n"},
		{`require "grape"; p Grape.mime_for("xml")`, "\"application/xml\"\n"},
		{`require "grape"; p Grape.mime_for("bogus")`, "nil\n"},
		{`require "grape"; p Grape.default_status("POST")`, "201\n"},
		{`require "grape"; p Grape.default_status("GET")`, "200\n"},
		// An unknown format raises Grape::Exceptions::Base.
		{`require "grape"; begin; Grape::Formatter.new.format("bogus", 1); rescue Grape::Exceptions::Base; p :fmterr; end`, ":fmterr\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestGrapeArgErrors covers the argument-arity error arms of the router / helpers.
func TestGrapeArgErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "grape"; begin; Grape::Router.new.get; rescue ArgumentError; p :a; end`, ":a\n"},
		{`require "grape"; begin; Grape::Router.new.match("GET"); rescue ArgumentError; p :a; end`, ":a\n"},
		{`require "grape"; begin; Grape::Formatter.new.format("json"); rescue ArgumentError; p :a; end`, ":a\n"},
		{`require "grape"; begin; Grape.mime_for; rescue ArgumentError; p :a; end`, ":a\n"},
		{`require "grape"; begin; Grape.default_status; rescue ArgumentError; p :a; end`, ":a\n"},
		{`require "grape"; begin; Grape::Validator.new { requires }; rescue ArgumentError; p :a; end`, ":a\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
