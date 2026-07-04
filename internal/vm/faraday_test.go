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

// faradayServer starts an in-process httptest server (loopback only, no external
// network) that the Faraday binding drives through its default net/http adapter.
// A path of the form /status/NNN replies with that status code and a JSON error
// body; every other path echoes the request (method, path, query, headers, body)
// as a JSON object, so the response middleware and the accessors have real data
// to read.
func faradayServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.HasPrefix(r.URL.Path, "/status/") {
			code, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/status/"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			fmt.Fprintf(w, `{"error":"status %d"}`, code)
			return
		}
		out := map[string]any{
			"method": r.Method,
			"path":   r.URL.Path,
			"query":  r.URL.RawQuery,
			"auth":   r.Header.Get("Authorization"),
			"ctype":  r.Header.Get("Content-Type"),
			"xtest":  r.Header.Get("X-Test"),
			"body":   string(body),
			"n":      5,
			"f":      1.5,
			"z":      nil,
			"ok":     true,
			"list":   []any{1, 2},
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Reply", "hi")
		enc, _ := json.Marshal(out)
		w.Write(enc)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// run evaluates a Faraday script with the server URL bound to the Ruby constant
// BASE, so scripts refer to it by name (avoiding brittle format substitution).
func faradayRun(t *testing.T, srv *httptest.Server, script string) string {
	t.Helper()
	return eval(t, `require "faraday"`+"\n"+fmt.Sprintf("BASE = %q\n", srv.URL)+script)
}

// TestFaradayFeature covers the require probe and the module/class/error tree.
func TestFaradayFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "faraday"`, "true\n"},
		{`require "faraday"; p require "faraday"`, "false\n"},
		{`require "faraday"; p Faraday.is_a?(Module)`, "true\n"},
		{`require "faraday"; p Faraday::Error < StandardError`, "true\n"},
		{`require "faraday"; p Faraday::ClientError < Faraday::Error`, "true\n"},
		{`require "faraday"; p Faraday::ResourceNotFound < Faraday::ClientError`, "true\n"},
		{`require "faraday"; p Faraday::ServerError < Faraday::Error`, "true\n"},
		{`require "faraday"; p Faraday::TimeoutError < Faraday::Error`, "true\n"},
		{`require "faraday"; p Faraday::ConnectionFailed < Faraday::Error`, "true\n"},
		{`require "faraday"; p Faraday::Connection.is_a?(Class)`, "true\n"},
		{`require "faraday"; p Faraday::Response.is_a?(Class)`, "true\n"},
		{`require "faraday"; p Faraday::Utils::Headers.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFaradayGetAndResponse covers a GET, the response accessors and the JSON
// value conversion (integer / float / null / bool / array / nested object).
func TestFaradayGetAndResponse(t *testing.T) {
	srv := faradayServer(t)
	got := faradayRun(t, srv, `
conn = Faraday.new(url: BASE) do |f|
  f.response :json
end
r = conn.get("/hello")
p r.class.name
p r.status
p r.success?
p r.finished?
p r.body["method"]
p r.body["path"]
p r.body["n"]
p r.body["f"]
p r.body["z"]
p r.body["ok"]
p r.body["list"]
p r.headers["X-Reply"]
p r.reason_phrase.class.name
`)
	want := strings.Join([]string{
		`"Faraday::Response"`,
		`200`,
		`true`,
		`true`,
		`"GET"`,
		`"/hello"`,
		`5`,
		`1.5`,
		`nil`,
		`true`,
		`[1, 2]`,
		`"hi"`,
		`"String"`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestFaradayPostJSON covers a JSON-encoded POST body (request :json) and the
// echoed content type.
func TestFaradayPostJSON(t *testing.T) {
	srv := faradayServer(t)
	got := faradayRun(t, srv, `
conn = Faraday.new(url: BASE) do |f|
  f.request :json
  f.response :json
end
r = conn.post("/widgets", {"name" => "gadget", "count" => 3})
p r.body["method"]
p r.body["ctype"]
p r.body["body"]
`)
	want := strings.Join([]string{
		`"POST"`,
		`"application/json"`,
		`"{\"count\":3,\"name\":\"gadget\"}"`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestFaradayFormEncoded covers request :url_encoded (a Hash body carried as
// ordered form params).
func TestFaradayFormEncoded(t *testing.T) {
	srv := faradayServer(t)
	got := faradayRun(t, srv, `
conn = Faraday.new(url: BASE) do |f|
  f.request :url_encoded
  f.response :json
end
r = conn.post("/form", {"a" => "1", "b" => "2"})
p r.body["ctype"]
p r.body["body"]
`)
	want := strings.Join([]string{
		`"application/x-www-form-urlencoded"`,
		`"a=1&b=2"`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestFaradayAuth covers the authorization and basic_auth request middleware and
// the default headers option.
func TestFaradayAuth(t *testing.T) {
	srv := faradayServer(t)
	got := faradayRun(t, srv, `
conn = Faraday.new(url: BASE, headers: {"Accept" => "application/json"}) do |f|
  f.authorization "Bearer", "tok123"
  f.response :json
end
r = conn.get("/a")
p r.body["auth"]
p conn.headers["Accept"]

c2 = Faraday.new(url: BASE) do |f|
  f.basic_auth "user", "pass"
  f.response :json
end
p c2.get("/b").body["auth"].start_with?("Basic ")
p c2.url_prefix.start_with?("http://")
`)
	want := strings.Join([]string{
		`"Bearer tok123"`,
		`"application/json"`,
		`true`,
		`true`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestFaradayRequestBlock covers the per-request block: params/headers proxies,
// body=, url override and the method/path/body readers.
func TestFaradayRequestBlock(t *testing.T) {
	srv := faradayServer(t)
	got := faradayRun(t, srv, `
conn = Faraday.new(url: BASE) do |f|
  f.response :json
end
r = conn.get("/start") do |req|
  req.params["x"] = "1"
  req.headers["X-Test"] = "y"
  p req.params["x"]
  p req.headers["X-Test"]
  p req.params.to_h["x"]
  p req.headers.to_h["X-Test"]
  req.url("/echo")
  p req.method
  p req.path
end
p r.body["path"]
p r.body["query"]
p r.body["xtest"]

r2 = conn.post("/p") do |req|
  req.body = {"k" => "v"}
  p req.body["k"]
end
p r2.body["method"]

r3 = conn.get("/u") do |req|
  req.params["a"] = "0"
  p req.params["missing"]
  p req.headers["missing"]
  req.url("/u2", {"q" => "9"})
end
p r3.body["path"]
p r3.body["query"]
`)
	want := strings.Join([]string{
		`"1"`,
		`"y"`,
		`"1"`,
		`"y"`,
		`"GET"`,
		`"/echo"`,
		`"/echo"`,
		`"x=1"`,
		`"y"`,
		`"v"`,
		`"POST"`,
		`nil`,
		`nil`,
		`"/u2"`,
		`"q=9"`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestFaradayVerbsAndParams covers every verb, the connection params option and a
// GET with a params Hash and headers Hash argument.
func TestFaradayVerbsAndParams(t *testing.T) {
	srv := faradayServer(t)
	got := faradayRun(t, srv, `
conn = Faraday.new(url: BASE, params: {"g" => "1"}) do |f|
  f.response :json
end
p conn.params["g"]
p conn.get("/g", {"h" => "2"}, {"X-Test" => "z"}).body["query"]
p conn.head("/x").status
p conn.delete("/x").body["method"]
p conn.options("/x").body["method"]
p conn.trace("/x").body["method"]
p conn.put("/x", {"a" => 1}).body["method"]
p conn.patch("/x", {"a" => 1}).body["method"]
p conn.post("/x").body["method"]
`)
	want := strings.Join([]string{
		`"1"`,
		`"g=1&h=2"`,
		`200`,
		`"DELETE"`,
		`"OPTIONS"`,
		`"TRACE"`,
		`"PUT"`,
		`"PATCH"`,
		`"POST"`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestFaradayModuleVerbs covers the module-level one-shot helpers (raw string
// body, no response middleware).
func TestFaradayModuleVerbs(t *testing.T) {
	srv := faradayServer(t)
	got := faradayRun(t, srv, `
r = Faraday.get(BASE + "/m")
p r.status
p r.body.include?("GET")
p Faraday.post(BASE + "/m", {"a" => 1}).status
`)
	want := strings.Join([]string{
		`200`,
		`true`,
		`200`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestFaradayRaiseError covers the raise_error response middleware and the error
// tree mapping, including the #response context Hash.
func TestFaradayRaiseError(t *testing.T) {
	srv := faradayServer(t)
	got := faradayRun(t, srv, `
conn = Faraday.new(url: BASE) do |f|
  f.response :raise_error
end
begin
  conn.get("/status/404")
rescue Faraday::ResourceNotFound => e
  p e.class.name
  p e.is_a?(Faraday::ClientError)
  p e.response[:status]
  p e.response[:headers]["Content-Type"]
end
begin
  conn.get("/status/500")
rescue Faraday::ServerError => e
  p e.class.name
end
begin
  conn.get("/status/422")
rescue Faraday::Error => e
  p e.class.name
end
`)
	want := strings.Join([]string{
		`"Faraday::ResourceNotFound"`,
		`true`,
		`404`,
		`"application/json"`,
		`"Faraday::ServerError"`,
		`"Faraday::UnprocessableEntityError"`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestFaradayUnknownMiddleware covers the deferred builder error (an unknown
// middleware name) surfacing as Faraday::Error with a nil #response, and the
// no-block / positional-URL / no-url paths of Faraday.new.
func TestFaradayUnknownMiddleware(t *testing.T) {
	srv := faradayServer(t)
	got := faradayRun(t, srv, `
conn = Faraday.new(BASE) do |f|
  f.request :bogus
end
begin
  conn.get("/x")
rescue Faraday::Error => e
  p e.class.name
  p e.response
end
c2 = Faraday.new
p c2.class.name
p Faraday.new(url: BASE).url_prefix.start_with?("http")
`)
	want := strings.Join([]string{
		`"Faraday::Error"`,
		`nil`,
		`"Faraday::Connection"`,
		`true`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestFaradayMisc covers the request DSL with trailing arguments (folded into
// the middleware via faradayStrArgs) and a verb call with no path argument (the
// path defaults to the base URL).
func TestFaradayMisc(t *testing.T) {
	srv := faradayServer(t)
	got := faradayRun(t, srv, `
conn = Faraday.new(url: BASE) do |f|
  f.request :authorization, "Token", "abc"
  f.response :json
end
p conn.get("/x").body["auth"]
p conn.get.body["path"]
`)
	want := strings.Join([]string{
		`"Token abc"`,
		`"/"`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestFaradayAdapterAndStringName covers conn.adapter, a String (not Symbol)
// middleware name, and a non-Hash keyword option value (ignored).
func TestFaradayAdapterAndStringName(t *testing.T) {
	srv := faradayServer(t)
	got := faradayRun(t, srv, `
conn = Faraday.new(url: BASE, params: 5) do |f|
  f.response "json"
  f.adapter :net_http
end
p conn.get("/x").body["method"]
`)
	want := "\"GET\"\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}
