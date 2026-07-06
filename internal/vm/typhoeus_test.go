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
	"strings"
	"testing"
)

// typhoeusServer starts an in-process httptest server (loopback only, no external
// network) driven through the Typhoeus binding's default net/http transport. It
// echoes the request as JSON, so the Response accessors and the Hydra callbacks
// have real data.
func typhoeusServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
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

// typhoeusRun evaluates a Typhoeus script with the server URL bound to BASE.
func typhoeusRun(t *testing.T, srv *httptest.Server, script string) string {
	t.Helper()
	return eval(t, `require "typhoeus"`+"\n"+fmt.Sprintf("BASE = %q\n", srv.URL)+script)
}

// TestTyphoeusFeature covers the require probe and the module/class tree.
func TestTyphoeusFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "typhoeus"`, "true\n"},
		{`require "typhoeus"; p require "typhoeus"`, "false\n"},
		{`require "typhoeus"; p Typhoeus.is_a?(Module)`, "true\n"},
		{`require "typhoeus"; p Typhoeus::Request.is_a?(Class)`, "true\n"},
		{`require "typhoeus"; p Typhoeus::Response.is_a?(Class)`, "true\n"},
		{`require "typhoeus"; p Typhoeus::Hydra.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestTyphoeusOneShot covers the module verbs, the Response accessors and the
// request options (params/body/headers/userpwd).
func TestTyphoeusOneShot(t *testing.T) {
	srv := typhoeusServer(t)
	got := typhoeusRun(t, srv, `
r = Typhoeus.get(BASE + "/hello", params: {"q" => "go ruby"}, headers: {"X-Test" => "y"})
p r.class.name
p r.code
p r.response_code
p r.success?
p r.timed_out?
p r.return_code
p r.total_time.is_a?(Float)
p r.headers["X-Reply"]
b = JSON.parse(r.body)
p b["method"]
p b["query"]
p b["xtest"]

p JSON.parse(Typhoeus.post(BASE + "/p", body: "raw").body)["body"]
p JSON.parse(Typhoeus.post(BASE + "/p", body: {"a" => "1"}).body)["body"]
p JSON.parse(Typhoeus.get(BASE + "/a", userpwd: "u:p").body)["auth"].start_with?("Basic ")
p Typhoeus.put(BASE + "/x").code
p Typhoeus.delete(BASE + "/x").code
p Typhoeus.head(BASE + "/x").code
p Typhoeus.patch(BASE + "/x").code
`)
	want := strings.Join([]string{
		`"Typhoeus::Response"`,
		`200`,
		`200`,
		`true`,
		`false`,
		`:ok`,
		`true`,
		`"hi"`,
		`"GET"`,
		`"q=go%20ruby"`,
		`"y"`,
		`"raw"`,
		`"a=1"`,
		`true`,
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

// TestTyphoeusRequestObject covers Typhoeus::Request.new, #url, #on_complete,
// #run and #response (nil before run, wrapped after).
func TestTyphoeusRequestObject(t *testing.T) {
	srv := typhoeusServer(t)
	got := typhoeusRun(t, srv, `
req = Typhoeus::Request.new(BASE + "/r", method: :post, body: "hi")
p req.url.end_with?("/r")
p req.response.nil?
seen = []
req.on_complete { |resp| seen << resp.code }
r = req.run
p r.code
p JSON.parse(r.body)["method"]
p req.response.code
p seen
`)
	want := strings.Join([]string{
		`true`,
		`true`,
		`200`,
		`"POST"`,
		`200`,
		`[200]`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestTyphoeusHydra covers the signature parallel runner: queued requests run
// concurrently through the seam transport and their on_complete callbacks fire in
// queue order on the calling goroutine (deterministic, no leaked goroutine).
func TestTyphoeusHydra(t *testing.T) {
	srv := typhoeusServer(t)
	got := typhoeusRun(t, srv, `
hydra = Typhoeus::Hydra.new(max_concurrency: 2)
seen = []
paths = ["/a", "/b", "/c"]
paths.each do |pth|
  req = Typhoeus::Request.new(BASE + pth)
  req.on_complete { |resp| seen << JSON.parse(resp.body)["path"] }
  hydra.queue(req)
end
p hydra.queued_count
hydra.run
p seen
p hydra.queued_count
`)
	want := strings.Join([]string{
		`3`,
		`["/a", "/b", "/c"]`,
		`0`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestTyphoeusReturnCodeFailure covers a failed transfer: a connection to a closed
// port yields a Response (not an error) with a non-OK return_code, code 0 and
// success? false — mirroring libcurl's CURLcode model.
func TestTyphoeusReturnCodeFailure(t *testing.T) {
	got := eval(t, `require "typhoeus"
r = Typhoeus.get("http://127.0.0.1:1/x")
p r.code
p r.success?
p r.return_code != :ok
p r.return_message.is_a?(String)
`)
	want := strings.Join([]string{
		`0`,
		`false`,
		`true`,
		`true`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}
