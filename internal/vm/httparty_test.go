// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// httpartyServer starts an in-process httptest server (loopback only, no external
// network) that the HTTParty binding drives through its default net/http Doer
// (the transport seam). Special paths exercise the binding's behaviour: /status/
// NNN replies with that code, /xml and /plain and /arr and /badjson feed the
// content-type-aware parser, and /loop and /once drive redirect following. Every
// other path echoes the request as a JSON object so the accessors have real data.
func httpartyServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasPrefix(r.URL.Path, "/status/"):
			code, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/status/"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			fmt.Fprintf(w, `{"error":"status %d"}`, code)
		case r.URL.Path == "/xml":
			w.Header().Set("Content-Type", "text/xml")
			w.Write([]byte(`<root><a>1</a><a>2</a><b x="y">hi</b></root>`))
		case r.URL.Path == "/plain":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("hello plain"))
		case r.URL.Path == "/arr":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[10,20,30]"))
		case r.URL.Path == "/badjson":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{not json"))
		case r.URL.Path == "/loop":
			w.Header().Set("Location", "/loop")
			w.WriteHeader(302)
		case r.URL.Path == "/once":
			w.Header().Set("Location", "/hello")
			w.WriteHeader(302)
		default:
			out := fmt.Sprintf(
				`{"method":%q,"path":%q,"query":%q,"auth":%q,"ctype":%q,"xtest":%q,"body":%q,"n":5,"f":1.5,"z":null,"ok":true,"list":[1,2]}`,
				r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"),
				r.Header.Get("Content-Type"), r.Header.Get("X-Test"), string(body))
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Reply", "hi")
			w.Write([]byte(out))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// httpartyRun evaluates an HTTParty script with the server URL bound to the Ruby
// constant BASE, so scripts refer to it by name.
func httpartyRun(t *testing.T, srv *httptest.Server, script string) string {
	t.Helper()
	return eval(t, `require "httparty"`+"\n"+fmt.Sprintf("BASE = %q\n", srv.URL)+script)
}

// TestHTTPartyFeature covers the require probe and the module / class / error tree.
func TestHTTPartyFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "httparty"`, "true\n"},
		{`require "httparty"; p require "httparty"`, "false\n"},
		{`require "httparty"; p HTTParty.is_a?(Module)`, "true\n"},
		{`require "httparty"; p HTTParty::Error < StandardError`, "true\n"},
		{`require "httparty"; p HTTParty::UnsupportedFormat < HTTParty::Error`, "true\n"},
		{`require "httparty"; p HTTParty::UnsupportedURIScheme < HTTParty::Error`, "true\n"},
		{`require "httparty"; p HTTParty::ResponseError < HTTParty::Error`, "true\n"},
		{`require "httparty"; p HTTParty::RedirectionTooDeep < HTTParty::ResponseError`, "true\n"},
		{`require "httparty"; p HTTParty::DuplicateLocationHeader < HTTParty::ResponseError`, "true\n"},
		{`require "httparty"; p HTTParty::Response.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestHTTPartyGetAndResponse covers a module GET, the Response accessors, the
// content-type-aware parsed_response with JSON value conversion, and #[].
func TestHTTPartyGetAndResponse(t *testing.T) {
	srv := httpartyServer(t)
	got := httpartyRun(t, srv, `
r = HTTParty.get(BASE + "/hello", query: {"q" => "ada"})
p r.class.name
p r.code
p r.success?
p r.body.class.name
p r.headers["X-Reply"]
p r.parsed_response["method"]
p r.parsed_response["query"]
p r.parsed_response["n"]
p r.parsed_response["f"]
p r.parsed_response["z"]
p r.parsed_response["ok"]
p r.parsed_response["list"]
p r["method"]
`)
	want := strings.Join([]string{
		`"HTTParty::Response"`, `200`, `true`, `"String"`, `"hi"`,
		`"GET"`, `"q=ada"`, `5`, `1.5`, `nil`, `true`, `[1, 2]`, `"GET"`, ``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestHTTPartyVerbs covers every verb, the form-encoded Hash body, a raw JSON
// (non-Hash) body, a nil body and a String body.
func TestHTTPartyVerbs(t *testing.T) {
	srv := httpartyServer(t)
	got := httpartyRun(t, srv, `
p HTTParty.post(BASE + "/e", body: {"name" => "gadget"}).parsed_response["ctype"]
p HTTParty.post(BASE + "/e", body: {"name" => "gadget"}).parsed_response["body"]
p HTTParty.post(BASE + "/e", body: 42).parsed_response["ctype"]
p HTTParty.post(BASE + "/e", body: 42).parsed_response["body"]
p HTTParty.post(BASE + "/e", body: "raw").parsed_response["body"]
p HTTParty.post(BASE + "/e", body: nil).parsed_response["body"]
p HTTParty.put(BASE + "/e").parsed_response["method"]
p HTTParty.patch(BASE + "/e").parsed_response["method"]
p HTTParty.delete(BASE + "/e").parsed_response["method"]
p HTTParty.head(BASE + "/e").code
p HTTParty.options(BASE + "/e").parsed_response["method"]
`)
	want := strings.Join([]string{
		`"application/x-www-form-urlencoded"`, `"name=gadget"`,
		`"application/json"`, `"42"`, `"raw"`, `""`,
		`"PUT"`, `"PATCH"`, `"DELETE"`, `200`, `"OPTIONS"`, ``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestHTTPartyOptions covers the per-call options bag: query, headers, basic_auth,
// timeout, a String-keyed options Hash and a non-Hash second argument (ignored).
func TestHTTPartyOptions(t *testing.T) {
	srv := httpartyServer(t)
	got := httpartyRun(t, srv, `
r = HTTParty.get(BASE + "/hello", timeout: 5, basic_auth: {username: "x", password: "y"}, headers: {"X-Test" => "h"})
p r.parsed_response["auth"].start_with?("Basic")
p r.parsed_response["xtest"]
p HTTParty.get(BASE + "/hello", "query" => {"a" => "1"}).parsed_response["query"]
p HTTParty.get(BASE + "/hello", 5).code
`)
	want := strings.Join([]string{`true`, `"h"`, `"a=1"`, `200`, ``}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestHTTPartyParsedFormats covers the content-type-aware parser: XML to a nested
// Hash, a JSON array, a plain-text body left as a String, #[] on an Array (index,
// out-of-range, non-integer key) and on a String, and a forced :format.
func TestHTTPartyParsedFormats(t *testing.T) {
	srv := httpartyServer(t)
	got := httpartyRun(t, srv, `
x = HTTParty.get(BASE + "/xml")
p x.parsed_response["root"]["b"]["x"]
p x.parsed_response["root"]["a"]
a = HTTParty.get(BASE + "/arr")
p a.parsed_response
p a[0]
p a[10]
p a["x"]
pl = HTTParty.get(BASE + "/plain")
p pl.parsed_response
p pl["k"]
p HTTParty.get(BASE + "/hello", format: "plain").parsed_response.class.name
`)
	want := strings.Join([]string{
		`"y"`, `["1", "2"]`, `[10, 20, 30]`, `10`, `nil`, `nil`,
		`"hello plain"`, `nil`, `"String"`, ``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestHTTPartyIncludeDSL covers the `include HTTParty` class DSL (base_uri,
// headers, default_params, basic_auth, format, default_options), the class-level
// verb methods, and the instance verb methods delegating to the class config.
func TestHTTPartyIncludeDSL(t *testing.T) {
	srv := httpartyServer(t)
	got := httpartyRun(t, srv, `
class API
  include HTTParty
  base_uri BASE
  headers "X-Test" => "dsl"
  default_params "k" => "v"
  basic_auth "u", "p"
  format :json
end
p API.base_uri == BASE
p API.default_options[:base_uri] == BASE
p API.default_options[:format]
p API.headers["X-Test"]
r = API.get("/hello")
p r.parsed_response["method"]
p r.parsed_response["xtest"]
p r.parsed_response["query"]
p r.parsed_response["auth"].start_with?("Basic")
inst = API.new
p inst.get("/hello").parsed_response["path"]

class Plain
  include HTTParty
end
p Plain.default_options
p Plain.get(BASE + "/hello").code
`)
	want := strings.Join([]string{
		`true`, `true`, `:json`, `"dsl"`,
		`"GET"`, `"dsl"`, `"k=v"`, `true`, `"/hello"`,
		`{}`, `200`, ``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestHTTPartyRedirects covers redirect following (the default and the too-deep
// error carrying its #response) and follow_redirects: false.
func TestHTTPartyRedirects(t *testing.T) {
	srv := httpartyServer(t)
	got := httpartyRun(t, srv, `
r = HTTParty.get(BASE + "/once")
p r.code
p r.parsed_response["path"]
p HTTParty.get(BASE + "/once", follow_redirects: false).code
begin
  HTTParty.get(BASE + "/loop")
rescue HTTParty::RedirectionTooDeep => e
  p e.class.name
  p e.is_a?(HTTParty::ResponseError)
  p e.response.code
end
`)
	want := strings.Join([]string{
		`200`, `"/hello"`, `302`,
		`"HTTParty::RedirectionTooDeep"`, `true`, `302`, ``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestHTTPartyErrors covers the error tree mapping: an unsupported :format, an
// unsupported URI scheme, and a malformed JSON body surfacing through
// parsed_response, plus the #message and nil #response on a non-response error.
func TestHTTPartyErrors(t *testing.T) {
	srv := httpartyServer(t)
	got := httpartyRun(t, srv, `
begin
  HTTParty.get(BASE + "/hello", format: :bogus)
rescue HTTParty::UnsupportedFormat => e
  p e.class.name
  p e.response
end
begin
  HTTParty.get("ftp://example.com/x")
rescue HTTParty::UnsupportedURIScheme => e
  p e.class.name
end
begin
  HTTParty.get(BASE + "/badjson").parsed_response
rescue HTTParty::Error => e
  p e.class.name
  p e.message.length > 0
end
`)
	want := strings.Join([]string{
		`"HTTParty::UnsupportedFormat"`, `nil`,
		`"HTTParty::UnsupportedURIScheme"`,
		`"HTTParty::Error"`, `true`, ``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}
