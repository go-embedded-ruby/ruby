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

// exconServer starts an in-process httptest server (loopback only, no external
// network) driven through the Excon binding's default net/http Doer. /status/NNN
// replies with that status; every other path echoes the request as JSON.
func exconServer(t *testing.T) *httptest.Server {
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
			"xtest":  r.Header.Get("X-Test"),
			"body":   string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Reply", "hi")
		enc, _ := json.Marshal(out)
		w.Write(enc)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// exconRun evaluates an Excon script with the server URL bound to BASE.
func exconRunScript(t *testing.T, srv *httptest.Server, script string) string {
	t.Helper()
	return eval(t, `require "excon"`+"\n"+fmt.Sprintf("BASE = %q\n", srv.URL)+script)
}

// TestExconFeature covers the require probe and the module/class/error tree,
// including the legacy Excon::Errors alias namespace.
func TestExconFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "excon"`, "true\n"},
		{`require "excon"; p require "excon"`, "false\n"},
		{`require "excon"; p Excon.is_a?(Module)`, "true\n"},
		{`require "excon"; p Excon::Connection.is_a?(Class)`, "true\n"},
		{`require "excon"; p Excon::Response.is_a?(Class)`, "true\n"},
		{`require "excon"; p Excon::Error < StandardError`, "true\n"},
		{`require "excon"; p Excon::Error::Socket < Excon::Error`, "true\n"},
		{`require "excon"; p Excon::Error::Certificate < Excon::Error::Socket`, "true\n"},
		{`require "excon"; p Excon::Error::Timeout < Excon::Error`, "true\n"},
		{`require "excon"; p Excon::Error::HTTPStatus < Excon::Error`, "true\n"},
		{`require "excon"; p Excon::Error::Client < Excon::Error::HTTPStatus`, "true\n"},
		{`require "excon"; p Excon::Error::Server < Excon::Error::HTTPStatus`, "true\n"},
		{`require "excon"; p Excon::Error::NotFound < Excon::Error::Client`, "true\n"},
		{`require "excon"; p Excon::Error::InternalServerError < Excon::Error::Server`, "true\n"},
		{`require "excon"; p Excon::Errors::NotFound == Excon::Error::NotFound`, "true\n"},
		{`require "excon"; p Excon::Errors::Error == Excon::Error`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestExconConnectionAndResponse covers Excon.new, the connection verbs, the
// :query/:headers/:body options and the Response accessors.
func TestExconConnectionAndResponse(t *testing.T) {
	srv := exconServer(t)
	got := exconRunScript(t, srv, `
conn = Excon.new(BASE, headers: {"X-Test" => "y"})
r = conn.get(path: "/hello", query: {"q" => "gadget"})
p r.class.name
p r.status
p r.success?
p r.reason_phrase.class.name
p r.remote_ip.is_a?(String)
p r.headers["X-Reply"]
b = JSON.parse(r.body)
p b["method"]
p b["path"]
p b["query"]
p b["xtest"]

p conn.post(path: "/p", body: "hi").status
p conn.put(path: "/x").status
p conn.delete(path: "/x").status
p conn.head(path: "/x").status
p conn.patch(path: "/x").status
p conn.request(method: :get, path: "/req").status
p conn.request(path: "/noverb").status
`)
	want := strings.Join([]string{
		`"Excon::Response"`,
		`200`,
		`true`,
		`"String"`,
		`true`,
		`"hi"`,
		`"GET"`,
		`"/hello"`,
		`"q=gadget"`,
		`"y"`,
		`200`,
		`200`,
		`200`,
		`200`,
		`200`,
		`200`,
		`200`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestExconOptionsAndSocketError covers the remaining request options (:body,
// :idempotent, the retry and timeout Integers) on a successful call, and a
// transport failure (connection to a closed port) raising an Excon::Error::Socket
// with a nil #response — the transport-error arm of the error mapping.
func TestExconOptionsAndSocketError(t *testing.T) {
	srv := exconServer(t)
	got := exconRunScript(t, srv, `
conn = Excon.new(BASE)
r = conn.post(path: "/opt", body: "payload", idempotent: true,
              retry_limit: 2, retry_interval: 0,
              read_timeout: 5, write_timeout: 5, connect_timeout: 5)
p r.status
p JSON.parse(r.body)["body"]

begin
  Excon.get("http://127.0.0.1:1/x")
rescue Excon::Error::Socket => e
  p e.is_a?(Excon::Error)
  p e.response
end
`)
	want := strings.Join([]string{
		`200`,
		`"payload"`,
		`true`,
		`nil`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestExconOneShotAndAuth covers the module-level one-shot verbs and the
// :user/:password Basic-auth option.
func TestExconOneShotAndAuth(t *testing.T) {
	srv := exconServer(t)
	got := exconRunScript(t, srv, `
r = Excon.get(BASE + "/one")
p JSON.parse(r.body)["method"]
p Excon.post(BASE + "/one", body: "x").status
r2 = Excon.get(BASE + "/auth", user: "u", password: "p")
p JSON.parse(r2.body)["auth"].start_with?("Basic ")
`)
	want := strings.Join([]string{
		`"GET"`,
		`200`,
		`true`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestExconExpects covers the :expects status assertion and the error tree
// mapping, including the #response context and rescuing a superclass.
func TestExconExpects(t *testing.T) {
	srv := exconServer(t)
	got := exconRunScript(t, srv, `
conn = Excon.new(BASE)
begin
  conn.get(path: "/status/404", expects: [200])
rescue Excon::Error::NotFound => e
  p e.class.name
  p e.is_a?(Excon::Error::Client)
  p e.response.status
end
begin
  conn.get(path: "/status/500", expects: 200)
rescue Excon::Error::Server => e
  p e.class.name
end
begin
  conn.get(path: "/status/418", expects: [200])
rescue Excon::Error::Client => e
  p e.class.name
end
`)
	want := strings.Join([]string{
		`"Excon::Error::NotFound"`,
		`true`,
		`404`,
		`"Excon::Error::InternalServerError"`,
		`"Excon::Error::Client"`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}
