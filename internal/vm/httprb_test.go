// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// httprbServer starts an in-process httptest server (loopback only, no external
// network) that the HTTP (http.rb) binding drives through its default net/http
// transport. /status/NNN replies with that status and a JSON body; /text replies
// with a plain-text body (so Response#parse hits the unknown-MIME error); every
// other path echoes the request as JSON, so the response accessors have real data.
func httprbServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasPrefix(r.URL.Path, "/status/"):
			code, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/status/"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			fmt.Fprintf(w, `{"error":"status %d"}`, code)
		case r.URL.Path == "/text":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "plain body")
		default:
			out := map[string]any{
				"method": r.Method,
				"path":   r.URL.Path,
				"query":  r.URL.RawQuery,
				"auth":   r.Header.Get("Authorization"),
				"accept": r.Header.Get("Accept"),
				"ctype":  r.Header.Get("Content-Type"),
				"xtest":  r.Header.Get("X-Test"),
				"body":   string(body),
				"n":      7,
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Reply", "hi")
			enc, _ := json.Marshal(out)
			w.Write(enc)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// httprbRun evaluates an http.rb script with the server URL bound to BASE.
func httprbRun(t *testing.T, srv *httptest.Server, script string) string {
	t.Helper()
	return eval(t, `require "http"`+"\n"+fmt.Sprintf("BASE = %q\n", srv.URL)+script)
}

// TestHTTPrbFeature covers the require probe and the module/class/error tree.
func TestHTTPrbFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "http"`, "true\n"},
		{`require "http"; p require "http"`, "false\n"},
		{`require "http"; p HTTP.is_a?(Module)`, "true\n"},
		{`require "http"; p HTTP::Client.is_a?(Class)`, "true\n"},
		{`require "http"; p HTTP::Response.is_a?(Class)`, "true\n"},
		{`require "http"; p HTTP::Response::Status.is_a?(Class)`, "true\n"},
		{`require "http"; p HTTP::Error < StandardError`, "true\n"},
		{`require "http"; p HTTP::ConnectionError < HTTP::Error`, "true\n"},
		{`require "http"; p HTTP::RequestError < HTTP::Error`, "true\n"},
		{`require "http"; p HTTP::ResponseError < HTTP::Error`, "true\n"},
		{`require "http"; p HTTP::TimeoutError < HTTP::Error`, "true\n"},
		{`require "http"; p HTTP::HeaderError < HTTP::Error`, "true\n"},
		{`require "http"; p HTTP::StateError < HTTP::ResponseError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestHTTPrbGetAndResponse covers a GET, the Response accessors and the Status
// value predicates.
func TestHTTPrbGetAndResponse(t *testing.T) {
	srv := httprbServer(t)
	got := httprbRun(t, srv, `
r = HTTP.get(BASE + "/hello")
p r.class.name
p r.status.class.name
p r.status.code
p r.status.success?
p r.status.to_i
p r.status.reason
p r.status.client_error?
p r.status.server_error?
p r.status.redirect?
p r.status.informational?
p r.code
p r.content_type
p r.reason
p r.version.class.name
p r.uri.start_with?("http")
p r.status.to_s
p r.body.include?("GET")
p r.headers["X-Reply"]
p r.parse["method"]
p r.parse("json")["n"]
p r.to_s.include?("GET")
`)
	want := strings.Join([]string{
		`"HTTP::Response"`,
		`"HTTP::Response::Status"`,
		`200`,
		`true`,
		`200`,
		`"OK"`,
		`false`,
		`false`,
		`false`,
		`false`,
		`200`,
		`"application/json"`,
		`"OK"`,
		`"String"`,
		`true`,
		`"200 OK"`,
		`true`,
		`"hi"`,
		`"GET"`,
		`7`,
		`true`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestHTTPrbChainable covers the chainable DSL: headers/header/auth/basic_auth/
// accept/timeout/follow, threaded into the request the echo server reports.
func TestHTTPrbChainable(t *testing.T) {
	srv := httprbServer(t)
	got := httprbRun(t, srv, `
c = HTTP.headers("X-Test" => "y").accept(:json).timeout(5).follow
r = c.get(BASE + "/a")
p r.parse["xtest"]
p r.parse["accept"]

r2 = HTTP.auth("Bearer tok").get(BASE + "/b")
p r2.parse["auth"]

r3 = HTTP.basic_auth(user: "u", pass: "p").get(BASE + "/c")
p r3.parse["auth"].start_with?("Basic ")

r4 = HTTP.header("X-Test", "z").get(BASE + "/d")
p r4.parse["xtest"]

p HTTP.accept(:json).get(BASE + "/e").parse["accept"]
p HTTP.timeout(5).get(BASE + "/f").code
p HTTP.follow.get(BASE + "/g").code
`)
	want := strings.Join([]string{
		`"y"`,
		`"application/json"`,
		`"Bearer tok"`,
		`true`,
		`"z"`,
		`"application/json"`,
		`200`,
		`200`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestHTTPrbBodies covers the verb body/params encodings: json:, form:, params:
// and body:, plus every verb.
func TestHTTPrbBodies(t *testing.T) {
	srv := httprbServer(t)
	got := httprbRun(t, srv, `
p HTTP.post(BASE + "/j", json: {"name" => "gadget", "count" => 3}).parse["body"]
p HTTP.post(BASE + "/j", json: {}).parse["ctype"].start_with?("application/json")
p HTTP.post(BASE + "/f", form: {"a" => "1", "b" => "2"}).parse["body"]
p HTTP.get(BASE + "/g", params: {"q" => "go"}).parse["query"]
p HTTP.post(BASE + "/r", body: "raw text").parse["body"]
p HTTP.put(BASE + "/x").parse["method"]
p HTTP.delete(BASE + "/x").parse["method"]
p HTTP.patch(BASE + "/x").parse["method"]
p HTTP.head(BASE + "/x").code
`)
	want := strings.Join([]string{
		`"{\"count\":3,\"name\":\"gadget\"}"`,
		`true`,
		`"a=1&b=2"`,
		`"q=go"`,
		`"raw text"`,
		`"PUT"`,
		`"DELETE"`,
		`"PATCH"`,
		`200`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestHTTPrbClientVerbsAndBody covers the client-level verbs with a body and the
// client chainable header path (a client reused across calls).
func TestHTTPrbClientVerbsAndBody(t *testing.T) {
	srv := httprbServer(t)
	got := httprbRun(t, srv, `
c = HTTP.headers("X-Test" => "keep")
p c.class.name
p c.post(BASE + "/p", json: {"k" => "v"}).parse["xtest"]
p c.get(BASE + "/g").parse["xtest"]
`)
	want := strings.Join([]string{
		`"HTTP::Client"`,
		`"keep"`,
		`"keep"`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestHTTPrbErrors covers the raised-error paths: a parse error on a non-JSON body
// (HTTP::Error with a nil #response) and a transport connection error (a fresh
// bind to a closed port), both surfacing as HTTP::Error subclasses.
func TestHTTPrbErrors(t *testing.T) {
	srv := httprbServer(t)
	got := httprbRun(t, srv, `
begin
  HTTP.get(BASE + "/text").parse
rescue HTTP::Error => e
  p e.is_a?(HTTP::Error)
  p e.response
end
begin
  HTTP.get("http://127.0.0.1:1/x")
rescue HTTP::Error => e
  p e.is_a?(HTTP::Error)
  p e.response
end
`)
	want := strings.Join([]string{
		`true`,
		`nil`,
		`true`,
		`nil`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}
