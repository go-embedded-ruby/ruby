// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The Net::HTTP networking surface is proven end-to-end against in-process
// httptest servers started inside each test: every assertion drives a Ruby script
// through the rbgo engine, which builds the request with the net-http codec,
// writes it to a real TCPSocket (or TLS SSLSocket), reads the response off the
// socket and parses it — hermetic, no external network.

// nethttpTestMux is the loopback HTTP handler the Net::HTTP tests drive: a set of
// endpoints exercising the status / body / header / framing arms MRI callers see.
func nethttpTestMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "hello net/http")
	})
	// /echo reports method|request-uri|X-Probe header|body so requests can be
	// asserted on the wire.
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s|%s|%s|%s", r.Method, r.URL.RequestURI(), r.Header.Get("X-Probe"), body)
	})
	// /chunked flushes before finishing with no Content-Length, so Go frames the
	// body with Transfer-Encoding: chunked (exercises the codec's chunked path).
	mux.HandleFunc("/chunked", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "chunk-")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		io.WriteString(w, "body")
	})
	mux.HandleFunc("/created", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated) // 201 -> Net::HTTPCreated
		io.WriteString(w, "made")
	})
	mux.HandleFunc("/partial", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent) // 206 -> category fallback (HTTPSuccess)
		io.WriteString(w, "part")
	})
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent) // 204 -> body not permitted
	})
	mux.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // 404 -> Net::HTTPNotFound
		io.WriteString(w, "nope")
	})
	mux.HandleFunc("/form", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		fmt.Fprintf(w, "form:a=%s", r.FormValue("a"))
	})
	return mux
}

// nethttpRun runs a Ruby script (with require "net/http"; require "uri" prepended
// and BASE bound to the server's base URL) through the engine, returning stdout.
func nethttpRun(t *testing.T, base, body string) string {
	t.Helper()
	src := "require \"net/http\"\nrequire \"uri\"\nBASE=" + fmt.Sprintf("%q", base) + "\n" + body
	return runSrc(t, src)
}

// TestNetHTTPGetOverHTTP is the headline proof: Net::HTTP.get / get_response over
// a real socket to an in-process HTTP server, covering the body String, the
// response object, status classification, headers, and the Content-Length and
// chunked framing arms.
func TestNetHTTPGetOverHTTP(t *testing.T) {
	ts := httptest.NewServer(nethttpTestMux())
	defer ts.Close()

	cases := []struct{ src, want string }{
		// Net::HTTP.get(URI) returns the body String.
		{`puts Net::HTTP.get(URI(BASE + "/"))`, "hello net/http"},
		// Net::HTTP.get accepts a bare String URL too.
		{`puts Net::HTTP.get(BASE + "/")`, "hello net/http"},
		// get(host, path, port) form.
		{`u=URI(BASE); puts Net::HTTP.get(u.host, "/", u.port)`, "hello net/http"},
		// get(host, path) form (default port on the URI's host:port authority).
		{`u=URI(BASE); puts Net::HTTP.get(u.host + ":" + u.port.to_s, "/")`, "hello net/http"},
		// get_response yields a real Net::HTTPResponse: code / body / classification.
		{`r=Net::HTTP.get_response(URI(BASE + "/")); puts r.code; puts r.body; puts r.is_a?(Net::HTTPOK); puts r.is_a?(Net::HTTPSuccess)`,
			"200\nhello net/http\ntrue\ntrue"},
		// message + http_version + content_type + case-insensitive [] header access.
		{`r=Net::HTTP.get_response(URI(BASE + "/")); puts r.message; puts r.http_version; puts r.content_type; puts r["Content-Type"]`,
			"OK\n1.1\ntext/plain\ntext/plain"},
		// read_body returns the same body.
		{`r=Net::HTTP.get_response(URI(BASE + "/")); puts r.read_body`, "hello net/http"},
		// each_header iterates the parsed headers (assert the content-type pair is present).
		{`r=Net::HTTP.get_response(URI(BASE + "/")); r.each_header { |k,v| puts "#{k}=#{v}" if k=="content-type" }`, "content-type=text/plain"},
		// A chunked response body is decoded transparently.
		{`puts Net::HTTP.get(URI(BASE + "/chunked"))`, "chunk-body"},
		// 201 maps to Net::HTTPCreated (a registered subclass).
		{`r=Net::HTTP.get_response(URI(BASE + "/created")); puts r.code; puts r.body; puts r.is_a?(Net::HTTPCreated)`, "201\nmade\ntrue"},
		// 206 has no dedicated class -> falls back to its category Net::HTTPSuccess.
		{`r=Net::HTTP.get_response(URI(BASE + "/partial")); puts r.code; puts(r.class==Net::HTTPSuccess); puts r.is_a?(Net::HTTPSuccess)`, "206\ntrue\ntrue"},
		// 204 permits no body: Net::HTTP.get returns "" (the body-nil fallback).
		{`p Net::HTTP.get(URI(BASE + "/empty"))`, "\"\""},
		{`r=Net::HTTP.get_response(URI(BASE + "/empty")); puts r.code; p r.body`, "204\nnil"},
		// 404 maps to Net::HTTPNotFound / category Net::HTTPClientError.
		{`r=Net::HTTP.get_response(URI(BASE + "/missing")); puts r.code; puts r.body; puts r.is_a?(Net::HTTPNotFound); puts r.is_a?(Net::HTTPClientError)`,
			"404\nnope\ntrue\ntrue"},
		// each_header with no block returns the response (self).
		{`r=Net::HTTP.get_response(URI(BASE + "/")); puts r.each_header.equal?(r)`, "true"},
	}
	for _, c := range cases {
		if got := nethttpRun(t, ts.URL, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestNetHTTPInstanceAndStart covers the instance surface: Net::HTTP.new + the
// verb helpers, Net::HTTP.start (with and without a block), #request over a
// request object, and the configuration accessors.
func TestNetHTTPInstanceAndStart(t *testing.T) {
	ts := httptest.NewServer(nethttpTestMux())
	defer ts.Close()

	cases := []struct{ src, want string }{
		// Net::HTTP.new(host, port).get(path) -> a response.
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port); r=h.get("/"); puts r.code; puts r.body`, "200\nhello net/http"},
		// The instance carries address / port and starts un-started.
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port); puts h.address; puts h.port; puts h.use_ssl?; puts h.started?`,
			"127.0.0.1\n" + mustPort(t, ts.URL) + "\nfalse\nfalse"},
		// get with a default path ("/" when omitted).
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port); puts h.get.body`, "hello net/http"},
		// A verb helper reaches the right method + path + header (via /echo).
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port); r=h.get("/echo?q=1", {"X-Probe"=>"yo"}); puts r.body`, "GET|/echo?q=1|yo|"},
		// head carries no body.
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port); r=h.head("/echo"); puts r.code`, "200"},
		// delete / options reach the server with their method.
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port); puts h.delete("/echo").body`, "DELETE|/echo||"},
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port); puts h.options("/echo").body`, "OPTIONS|/echo||"},
		// post/put/patch carry a body.
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port); puts h.post("/echo", "payload").body`, "POST|/echo||payload"},
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port); puts h.put("/echo", "pdata").body`, "PUT|/echo||pdata"},
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port); puts h.patch("/echo", "cdata").body`, "PATCH|/echo||cdata"},
		// Net::HTTP.start yields a started instance and returns the block value.
		{`u=URI(BASE); v=Net::HTTP.start(u.host, u.port) { |http| puts http.started?; http.get("/").body }; puts v`,
			"true\nhello net/http"},
		// Net::HTTP.start with no block returns the (started) instance.
		{`u=URI(BASE); h=Net::HTTP.start(u.host, u.port); puts h.is_a?(Net::HTTP); puts h.started?; h.finish; puts h.started?`,
			"true\ntrue\nfalse"},
		// Instance #start with a block toggles started? around the block; without a
		// block returns self.
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port); h.start { |c| puts c.started? }; puts h.started?; puts h.start.equal?(h)`,
			"true\nfalse\ntrue"},
		// #request with a Net::HTTP::Get request object, plus a header set on it.
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port)
req=Net::HTTP::Get.new("/echo"); req["X-Probe"]="hdr"
r=h.request(req); puts r.body`, "GET|/echo|hdr|"},
		// #request yields the response to a block and still returns it.
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port)
r=h.request(Net::HTTP::Get.new("/")) { |resp| puts resp.code }
puts r.body`, "200\nhello net/http"},
		// #request(Post) uses the request object's own body.
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port)
req=Net::HTTP::Post.new("/echo"); req.body="viabody"
puts h.request(req).body`, "POST|/echo||viabody"},
		// #request(Post, body) uses the fallback body argument when @body is unset.
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port)
puts h.request(Net::HTTP::Post.new("/echo"), "argbody").body`, "POST|/echo||argbody"},
		// A request object exposes method / path / body= / body.
		{`req=Net::HTTP::Post.new("/p"); puts req.method; puts req.path; req.body="x"; puts req.body`, "POST\n/p\nx"},
		// A verb object with no path defaults to "/".
		{`puts Net::HTTP::Get.new.path`, "/"},
	}
	for _, c := range cases {
		if got := nethttpRun(t, ts.URL, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestNetHTTPPostAndForm covers the class-level Net::HTTP.post / post_form and the
// request object's set_form_data.
func TestNetHTTPPostAndForm(t *testing.T) {
	ts := httptest.NewServer(nethttpTestMux())
	defer ts.Close()

	cases := []struct{ src, want string }{
		// Net::HTTP.post(uri, data) reaches the server with the body.
		{`r=Net::HTTP.post(URI(BASE + "/echo"), "hi"); puts r.body`, "POST|/echo||hi"},
		// A trailing header Hash is sent (X-Probe echoed back).
		{`r=Net::HTTP.post(URI(BASE + "/echo"), "hi", {"X-Probe"=>"p"}); puts r.body`, "POST|/echo|p|hi"},
		// A non-Hash third argument is ignored (hashToPairs non-Hash arm).
		{`r=Net::HTTP.post(URI(BASE + "/echo"), "hi", "notahash"); puts r.body`, "POST|/echo||hi"},
		// Net::HTTP.post_form URL-encodes the params.
		{`r=Net::HTTP.post_form(URI(BASE + "/form"), {"a"=>"1"}); puts r.body`, "form:a=1"},
		// set_form_data on a request object seeds the urlencoded body.
		{`u=URI(BASE); h=Net::HTTP.new(u.host, u.port)
req=Net::HTTP::Post.new("/form"); req.set_form_data({"a"=>"2"})
puts h.request(req).body`, "form:a=2"},
	}
	for _, c := range cases {
		if got := nethttpRun(t, ts.URL, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestNetHTTPOverHTTPS proves the crypto/tls path: Net::HTTP.get against an
// httptest TLS server (self-signed) succeeds with the default VERIFY_NONE, and an
// instance with use_ssl + VERIFY_PEER fails the self-signed handshake as a
// SocketError.
func TestNetHTTPOverHTTPS(t *testing.T) {
	ts := httptest.NewTLSServer(nethttpTestMux())
	defer ts.Close()
	u, _ := url.Parse(ts.URL)

	cases := []struct{ src, want string }{
		// Default (VERIFY_NONE) https GET round-trips the body.
		{`puts Net::HTTP.get(URI(BASE + "/"))`, "hello net/http"},
		{`r=Net::HTTP.get_response(URI(BASE + "/")); puts r.code; puts r.body`, "200\nhello net/http"},
		// An instance with use_ssl and default verify also works.
		{fmt.Sprintf(`h=Net::HTTP.new(%q, %s); h.use_ssl=true; puts h.use_ssl?; puts h.get("/").body`,
			u.Hostname(), u.Port()), "true\nhello net/http"},
		// verify_mode = VERIFY_PEER rejects the self-signed cert (handshake error).
		{fmt.Sprintf(`require "openssl"
h=Net::HTTP.new(%q, %s); h.use_ssl=true; h.verify_mode=OpenSSL::SSL::VERIFY_PEER
puts h.verify_mode
begin; h.get("/"); rescue SocketError; puts "verifyfail"; end`, u.Hostname(), u.Port()),
			fmt.Sprintf("%d\nverifyfail", 1)},
	}
	for _, c := range cases {
		if got := nethttpRun(t, ts.URL, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestNetHTTPErrors covers the raising arms: bad URIs, arity, a connection
// refused, and a malformed (non-HTTP) response.
func TestNetHTTPErrors(t *testing.T) {
	// A listener opened then closed gives a host:port that refuses connections.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	refused := ln.Addr().String()
	ln.Close()

	// A raw server that writes a non-HTTP payload then closes, so ParseResponse
	// rejects it as a bad response.
	bad, _ := net.Listen("tcp", "127.0.0.1:0")
	defer bad.Close()
	go func() {
		for {
			c, err := bad.Accept()
			if err != nil {
				return
			}
			// Drain the client's request first so there is no unread data forcing a
			// RST on close; then reply with a non-HTTP payload and close cleanly, so
			// the client reads it to EOF and ParseResponse rejects the status line.
			c.Read(make([]byte, 1024))
			c.Write([]byte("this is not a valid HTTP response\r\n\r\n"))
			c.Close()
		}
	}()

	cases := []struct{ src, want string }{
		// Net::HTTP.get with no arguments.
		{`begin; Net::HTTP.get; rescue ArgumentError; puts "arity"; end`, "arity"},
		// A malformed / authority-less URI.
		{`begin; Net::HTTP.get("::::"); rescue ArgumentError; puts "baduri"; end`, "baduri"},
		{`begin; Net::HTTP.get("noscheme-no-host"); rescue ArgumentError; puts "nohost"; end`, "nohost"},
		// The (host, path) form with a path that fails to parse as a URL.
		{`begin; Net::HTTP.get("h", "/%zz"); rescue ArgumentError; puts "badpath"; end`, "badpath"},
		// Net::HTTP.post with too few arguments.
		{`begin; Net::HTTP.post(URI("http://x/")); rescue ArgumentError; puts "postarity"; end`, "postarity"},
		{`begin; Net::HTTP.post_form(URI("http://x/")); rescue ArgumentError; puts "formarity"; end`, "formarity"},
		// #request with no argument.
		{`begin; Net::HTTP.new("x",1).request; rescue ArgumentError; puts "reqarity"; end`, "reqarity"},
		// A refused connection surfaces as SocketError.
		{fmt.Sprintf(`begin; Net::HTTP.get(URI("http://%s/")); rescue SocketError; puts "refused"; end`, refused), "refused"},
		// A non-HTTP response surfaces as Net::HTTPBadResponse.
		{fmt.Sprintf(`begin; Net::HTTP.get(URI("http://%s/")); rescue Net::HTTPBadResponse; puts "badresp"; end`, bad.Addr().String()), "badresp"},
	}
	for _, c := range cases {
		src := "require \"net/http\"\nrequire \"uri\"\n" + c.src
		if got := runSrc(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestNetHTTPWhiteBox covers the arms unreachable through normal Ruby dispatch:
// the codec Request-Line CR/LF guard, an unknown method, the final
// Net::HTTPResponse class fallback, and the non-Hash / nil @header response arms.
func TestNetHTTPWhiteBox(t *testing.T) {
	vm := New(io.Discard)
	netMod := vm.consts["Net"].(*RClass)

	// nethttpBuildRequest: NewRequest rejects an unknown method (ArgumentError).
	cfg := &nethttpXfer{scheme: "http", host: "127.0.0.1", port: "1", hostHdr: "h"}
	wantRaise(t, "ArgumentError", func() {
		vm.nethttpBuildRequest(cfg, "BOGUS", "/", nil, nil, true)
	})
	// nethttpBuildRequest: Request.Bytes rejects a Request-Line with CR/LF
	// (Net::HTTPError), before any dial.
	wantRaise(t, "Net::HTTPError", func() {
		vm.nethttpBuildRequest(cfg, "GET", "/a\r\nb", nil, nil, true)
	})

	// nethttpResponseClass falls back to Net::HTTPResponse for an unknown class and
	// category.
	if got := vm.nethttpResponseClass(netMod, "NoSuch", "AlsoNoSuch"); got != netMod.consts["HTTPResponse"].(*RClass) {
		t.Errorf("class fallback = %v, want HTTPResponse", got)
	}

	// content_type: a response whose @header is not a Hash returns nil (defensive arm).
	got := runSrc(t, `require "net/http"
r = Net::HTTPResponse.new
r.instance_variable_set(:@header, 5)
p r.content_type`)
	if got != "nil" {
		t.Errorf("content_type non-hash = %q, want nil", got)
	}
	// content_type: a response whose @header is a Hash without a content-type entry
	// returns nil (the has-hash-but-miss arm).
	got = runSrc(t, `require "net/http"
r = Net::HTTPResponse.new
r.instance_variable_set(:@header, {})
p r.content_type`)
	if got != "nil" {
		t.Errorf("content_type empty-hash miss = %q, want nil", got)
	}
	// each_header: a non-Hash @header is a no-op returning self.
	got = runSrc(t, `require "net/http"
r = Net::HTTPResponse.new
r.instance_variable_set(:@header, 5)
puts r.each_header { |k,v| puts "unexpected" }.equal?(r)`)
	if got != "true" {
		t.Errorf("each_header non-hash = %q, want true", got)
	}
	// nethttpNewInstance with no host defaults the address to "" and the port to 80.
	got = runSrc(t, `require "net/http"
h = Net::HTTP.new
puts h.address.empty?
puts h.port`)
	if got != "true\n80" {
		t.Errorf("new-no-host = %q", got)
	}
}

// TestNetHTTPThroughRbgoBinary is the literal "through the rbgo binary" proof: it
// builds cmd/rbgo and runs a script that does Net::HTTP.get against in-process
// HTTP and HTTPS servers, asserting each body on the binary's stdout. Skipped
// (not failed) if the binary cannot be built, mirroring the socket suite.
func TestNetHTTPThroughRbgoBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary build in -short")
	}
	bin := filepath.Join(t.TempDir(), "rbgo")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "./cmd/rbgo")
	build.Dir = repoRoot(t)
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build cmd/rbgo (%v): %s", err, out)
	}

	httpSrv := httptest.NewServer(nethttpTestMux())
	defer httpSrv.Close()
	tlsSrv := httptest.NewTLSServer(nethttpTestMux())
	defer tlsSrv.Close()

	script := filepath.Join(t.TempDir(), "main.rb")
	body := fmt.Sprintf(`require "net/http"
require "uri"
print "http:"
print Net::HTTP.get(URI(%q))
print " https:"
print Net::HTTP.get(URI(%q))`, httpSrv.URL+"/", tlsSrv.URL+"/")
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, script).CombinedOutput()
	if err != nil {
		t.Fatalf("run binary: %v\n%s", err, out)
	}
	if want := "http:hello net/http https:hello net/http"; !strings.Contains(string(out), want) {
		t.Fatalf("binary stdout = %q, want to contain %q", out, want)
	}
}

// TestNetHTTPSchemePort covers nethttpSchemePort's default-scheme and
// default-port arms with authorities that omit a scheme or a port (unreachable
// from the httptest suite, whose URLs always carry an explicit port).
func TestNetHTTPSchemePort(t *testing.T) {
	cases := []struct {
		raw, scheme, port string
	}{
		{"http://h:5/p", "http", "5"},   // explicit scheme + port
		{"http://h/p", "http", "80"},    // default http port
		{"https://h/p", "https", "443"}, // default https port
		{"//h/p", "http", "80"},         // no scheme -> http, default port
	}
	for _, c := range cases {
		u, err := url.Parse(c.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", c.raw, err)
		}
		if s, p := nethttpSchemePort(u); s != c.scheme || p != c.port {
			t.Errorf("%q -> (%q,%q), want (%q,%q)", c.raw, s, p, c.scheme, c.port)
		}
	}
}

// failWriter / errReader drive httpExchange's write and read error arms.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// fakeStream is a streamIO whose writer / reader are supplied, so both
// I/O-error arms of httpExchange can be exercised without a real socket.
type fakeStream struct {
	w io.Writer
	r *bufio.Reader
}

func (f *fakeStream) reader() *bufio.Reader { return f.r }
func (f *fakeStream) writer() io.Writer     { return f.w }
func (f *fakeStream) markClosed()           {}
func (f *fakeStream) isClosed() bool        { return false }
func (f *fakeStream) closeConn() error      { return nil }

// TestNetHTTPExchangeErrors covers nethttpExchangeFramed's write-error and
// read-error arms and trimResponseToHeaders' no-terminator arm.
func TestNetHTTPExchangeErrors(t *testing.T) {
	vm := New(io.Discard)
	cfg := &nethttpXfer{}
	// Write error: the writer fails immediately (phase "write").
	if _, _, phase, err := vm.nethttpExchangeFramed(cfg, &fakeStream{w: failWriter{}, r: bufio.NewReader(strings.NewReader(""))}, []byte("x"), false); err != io.ErrClosedPipe || phase != "write" {
		t.Errorf("write-error arm = (%q,%v), want (write,ErrClosedPipe)", phase, err)
	}
	// Read error: the write succeeds, the read fails (phase "read").
	if _, _, phase, err := vm.nethttpExchangeFramed(cfg, &fakeStream{w: io.Discard, r: bufio.NewReader(errReader{})}, []byte("x"), false); err != io.ErrUnexpectedEOF || phase != "read" {
		t.Errorf("read-error arm = (%q,%v), want (read,ErrUnexpectedEOF)", phase, err)
	}
	// trimResponseToHeaders returns its input unchanged when there is no header
	// terminator (the i < 0 arm).
	raw := []byte("HTTP/1.1 200 OK\r\nno-terminator")
	if got := trimResponseToHeaders(raw); string(got) != string(raw) {
		t.Errorf("trim no-terminator = %q, want unchanged", got)
	}
}

// mustPort returns the port of a URL string as a decimal string, for embedding in
// an expected-output comparison.
func mustPort(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Port()
}

// countingServer starts an httptest server over nethttpTestMux that counts the
// distinct TCP connections it accepts (via ConnState StateNew), so keep-alive can
// be proven by the connection count rather than the request count.
func countingServer(t *testing.T) (*httptest.Server, *int64) {
	t.Helper()
	var conns int64
	ts := httptest.NewUnstartedServer(nethttpTestMux())
	ts.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt64(&conns, 1)
		}
	}
	ts.Start()
	return ts, &conns
}

// TestNetHTTPKeepAlive is the persistent-connection proof: three requests inside a
// single Net::HTTP.start block return the right bodies while the server accepts
// exactly one TCP connection (keep-alive reuse). A separate case shows requests
// outside a start block each open their own connection.
func TestNetHTTPKeepAlive(t *testing.T) {
	ts, conns := countingServer(t)
	defer ts.Close()
	u, _ := url.Parse(ts.URL)

	// Three requests, one start block: bodies correct AND one connection.
	got := nethttpRun(t, ts.URL, `h=Net::HTTP.new(URI(BASE).host, URI(BASE).port)
h.start do
  puts h.get("/").body
  puts h.get("/echo?n=2").body
  puts h.get("/created").body
end`)
	want := "hello net/http\nGET|/echo?n=2||\nmade"
	if got != want {
		t.Fatalf("keep-alive bodies = %q, want %q", got, want)
	}
	if n := atomic.LoadInt64(conns); n != 1 {
		t.Fatalf("keep-alive used %d connections, want 1", n)
	}
	// A chunked response inside the same reused connection frames correctly and
	// still leaves the connection reusable for a following request.
	atomic.StoreInt64(conns, 0)
	got = nethttpRun(t, ts.URL, `h=Net::HTTP.new(URI(BASE).host, URI(BASE).port)
h.start do
  puts h.get("/chunked").body
  puts h.get("/").body
end`)
	if want = "chunk-body\nhello net/http"; got != want {
		t.Fatalf("keep-alive chunked = %q, want %q", got, want)
	}
	if n := atomic.LoadInt64(conns); n != 1 {
		t.Fatalf("keep-alive chunked used %d connections, want 1", n)
	}
	// Without a start block, each request opens (and closes) its own connection.
	atomic.StoreInt64(conns, 0)
	got = nethttpRun(t, ts.URL, `h=Net::HTTP.new(URI(BASE).host, URI(BASE).port)
puts h.get("/").body
puts h.get("/").body`)
	if want = "hello net/http\nhello net/http"; got != want {
		t.Fatalf("non-persistent bodies = %q, want %q", got, want)
	}
	if n := atomic.LoadInt64(conns); n != 2 {
		t.Fatalf("non-persistent used %d connections, want 2", n)
	}
	_ = u
}

// TestNetHTTPKeepAliveServerClose proves the fallback arm: when the server sends
// Connection: close, the cached connection is dropped and the next request in the
// same start block redials (so the body is still correct and a new connection is
// accepted).
func TestNetHTTPKeepAliveServerClose(t *testing.T) {
	var conns int64
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close") // ask the client to not reuse
		io.WriteString(w, "closed-body")
	})
	ts := httptest.NewUnstartedServer(mux)
	ts.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt64(&conns, 1)
		}
	}
	ts.Start()
	defer ts.Close()

	got := nethttpRun(t, ts.URL, `h=Net::HTTP.new(URI(BASE).host, URI(BASE).port)
h.start do
  puts h.get("/").body
  puts h.get("/").body
end`)
	if want := "closed-body\nclosed-body"; got != want {
		t.Fatalf("server-close bodies = %q, want %q", got, want)
	}
	if n := atomic.LoadInt64(&conns); n != 2 {
		t.Fatalf("server-close used %d connections, want 2 (one per request)", n)
	}
}

// TestNetHTTPReadTimeout is the timeout proof: a slow handler that never answers
// within read_timeout makes the request raise Net::ReadTimeout, and the accessor
// round-trips the configured value.
func TestNetHTTPReadTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done(): // client hung up (its read timed out) -> return promptly
		}
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	got := nethttpRun(t, ts.URL, `h=Net::HTTP.new(URI(BASE).host, URI(BASE).port)
h.read_timeout = 0.2
puts h.read_timeout
begin
  h.get("/slow")
rescue Net::ReadTimeout
  puts "readtimeout"
end`)
	if want := "0.2\nreadtimeout"; got != want {
		t.Fatalf("read-timeout = %q, want %q", got, want)
	}
}

// TestNetHTTPTimeoutAccessors covers the open/read/write_timeout accessor surface
// and the MRI defaults (60 each), independent of any I/O.
func TestNetHTTPTimeoutAccessors(t *testing.T) {
	got := runSrc(t, `require "net/http"
h = Net::HTTP.new("h", 80)
puts h.open_timeout
puts h.read_timeout
puts h.write_timeout
h.open_timeout = 5
h.write_timeout = 3
puts h.open_timeout
puts h.write_timeout`)
	if want := "60\n60\n60\n5\n3"; got != want {
		t.Fatalf("timeout accessors = %q, want %q", got, want)
	}
}

// TestNetHTTPOpenTimeout proves open_timeout bounds the dial: a routeable but
// unreachable address (RFC 5737 TEST-NET-1) with a tiny open_timeout raises
// Net::OpenTimeout rather than hanging.
func TestNetHTTPOpenTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network dial-timeout test in -short")
	}
	got := runSrc(t, `require "net/http"
h = Net::HTTP.new("192.0.2.1", 80)
h.open_timeout = 0.2
begin
  h.get("/")
rescue Net::OpenTimeout
  puts "opentimeout"
rescue SocketError
  puts "socketerror"
end`)
	// A blackholed TEST-NET address times out; some CI networks reject it outright
	// (SocketError). Either proves open_timeout did not hang.
	if got != "opentimeout" && got != "socketerror" {
		t.Fatalf("open-timeout = %q, want opentimeout or socketerror", got)
	}
}

// TestNetHTTPProxyHTTP is the plain-http proxy proof: a tiny forwarding proxy sees
// the absolute-URI request-line (and any Proxy-Authorization), forwards to the
// backend, and the client gets the backend's body — proving the request was
// routed through the proxy.
func TestNetHTTPProxyHTTP(t *testing.T) {
	backend := httptest.NewServer(nethttpTestMux())
	defer backend.Close()

	var proxyHits int64
	var sawAuth string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&proxyHits, 1)
		sawAuth = r.Header.Get("Proxy-Authorization")
		// The request-line is absolute-form for a plain-http proxy: r.RequestURI is
		// the full target URL. Forward it to the backend and copy the response back.
		resp, err := http.Get(r.RequestURI)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		w.Write(body)
	}))
	defer proxy.Close()

	bu, _ := url.Parse(backend.URL)
	pu, _ := url.Parse(proxy.URL)

	// Route a GET through the proxy to the backend /echo.
	got := runSrc(t, fmt.Sprintf(`require "net/http"
h = Net::HTTP.new(%q, %s, %q, %s)
puts h.proxy?
puts h.proxy_address
puts h.proxy_port
puts h.get("/echo").body`, bu.Hostname(), bu.Port(), pu.Hostname(), pu.Port()))
	want := "true\n" + pu.Hostname() + "\n" + pu.Port() + "\nGET|/echo||"
	if got != want {
		t.Fatalf("proxy GET = %q, want %q", got, want)
	}
	if n := atomic.LoadInt64(&proxyHits); n < 1 {
		t.Fatalf("proxy was not hit (%d)", n)
	}

	// Proxy credentials are sent as Proxy-Authorization.
	atomic.StoreInt64(&proxyHits, 0)
	got = runSrc(t, fmt.Sprintf(`require "net/http"
h = Net::HTTP.new(%q, %s, %q, %s, "usr", "pwd")
puts h.proxy_user
puts h.proxy_pass
puts h.get("/").body`, bu.Hostname(), bu.Port(), pu.Hostname(), pu.Port()))
	if want = "usr\npwd\nhello net/http"; got != want {
		t.Fatalf("proxy auth GET = %q, want %q", got, want)
	}
	wantAuth := "Basic " + base64Std("usr:pwd")
	if sawAuth != wantAuth {
		t.Fatalf("Proxy-Authorization = %q, want %q", sawAuth, wantAuth)
	}
}

// TestNetHTTPProxyConnect proves the https-via-proxy path: a CONNECT-tunnelling
// proxy splices bytes to the TLS backend, and the client completes the TLS
// handshake and request through the tunnel.
func TestNetHTTPProxyConnect(t *testing.T) {
	backend := httptest.NewTLSServer(nethttpTestMux())
	defer backend.Close()

	var connects int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "expected CONNECT", http.StatusMethodNotAllowed)
			return
		}
		atomic.AddInt64(&connects, 1)
		dst, err := net.Dial("tcp", r.Host) // r.Host is the CONNECT target host:port
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			dst.Close()
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			dst.Close()
			return
		}
		conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() { io.Copy(dst, conn); dst.Close() }()
		io.Copy(conn, dst)
		conn.Close()
	}))
	defer proxy.Close()

	bu, _ := url.Parse(backend.URL)
	pu, _ := url.Parse(proxy.URL)

	got := runSrc(t, fmt.Sprintf(`require "net/http"
h = Net::HTTP.new(%q, %s, %q, %s)
h.use_ssl = true
puts h.get("/").body`, bu.Hostname(), bu.Port(), pu.Hostname(), pu.Port()))
	if want := "hello net/http"; got != want {
		t.Fatalf("proxy CONNECT body = %q, want %q", got, want)
	}
	if n := atomic.LoadInt64(&connects); n != 1 {
		t.Fatalf("CONNECT count = %d, want 1", n)
	}
}

// TestNetHTTPProxyConnectFailure covers the CONNECT-refused arm: a proxy that
// answers CONNECT with a non-2xx status makes the tunnel dial fail (SocketError).
func TestNetHTTPProxyConnectFailure(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		conn.Write([]byte("HTTP/1.1 403 Forbidden\r\n\r\n"))
		conn.Close()
	}))
	defer proxy.Close()
	pu, _ := url.Parse(proxy.URL)

	got := runSrc(t, fmt.Sprintf(`require "net/http"
h = Net::HTTP.new("example.com", 443, %q, %s)
h.use_ssl = true
begin
  h.get("/")
rescue SocketError
  puts "connectfail"
end`, pu.Hostname(), pu.Port()))
	if got != "connectfail" {
		t.Fatalf("CONNECT-failure = %q, want connectfail", got)
	}
}

// TestNetHTTPTransportUnits covers the transport helpers directly on the arms that
// normal Ruby dispatch does not reach: duration conversion, the chunk-size hex
// scanner, chunked detection, net.Conn extraction, the timeout error mapping and
// the CONNECT / read framing error arms.
func TestNetHTTPTransportUnits(t *testing.T) {
	// nethttpDuration: Integer / Float / nil / non-positive.
	if d := nethttpDuration(object.IntValue(2)); d != 2*time.Second {
		t.Errorf("duration(2) = %v", d)
	}
	if d := nethttpDuration(object.Float(0.5)); d != 500*time.Millisecond {
		t.Errorf("duration(0.5) = %v", d)
	}
	if d := nethttpDuration(object.NilV); d != 0 {
		t.Errorf("duration(nil) = %v, want 0", d)
	}
	if d := nethttpDuration(object.IntValue(0)); d != 0 {
		t.Errorf("duration(0) = %v, want 0", d)
	}
	if d := nethttpDuration(object.Float(-1)); d != 0 {
		t.Errorf("duration(-1) = %v, want 0", d)
	}

	// nethttpFirstHexRun: leading junk, trailing run, and no-hex.
	for _, c := range []struct{ in, want string }{
		{"1a\r\n", "1a"}, {" ;\r\n", ""}, {"ff", "ff"}, {"; 5 ", "5"},
	} {
		if got := nethttpFirstHexRun(c.in); got != c.want {
			t.Errorf("firstHexRun(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// nethttpIsChunked.
	if !nethttpIsChunked("chunked") || nethttpIsChunked("gzip") {
		t.Error("isChunked mismatch")
	}
	// nethttpNetConn: a non-socket stream (test fake) yields nil.
	if c := nethttpNetConn(&fakeStream{}); c != nil {
		t.Errorf("netConn(fake) = %v, want nil", c)
	}
	// nethttpSetDeadline on a no-conn stream is a no-op (both the 0 and >0 arms).
	nethttpSetDeadline(&fakeStream{}, 0)
	nethttpSetDeadline(&fakeStream{}, time.Second)

	// basicAuthHeader.
	if got := basicAuthHeader("a", "b"); got != "Basic "+base64Std("a:b") {
		t.Errorf("basicAuthHeader = %q", got)
	}

	// raiseTransportErr: timeout maps per phase; non-timeout is a SocketError.
	for phase, class := range map[string]string{
		"open": "Net::OpenTimeout", "write": "Net::WriteTimeout", "read": "Net::ReadTimeout",
	} {
		vm := New(io.Discard)
		wantRaise(t, class, func() { vm.raiseTransportErr(timeoutErr{}, phase) })
	}
	vmS := New(io.Discard)
	wantRaise(t, "SocketError", func() { vmS.raiseTransportErr(errors.New("boom"), "read") })

	// nethttpReadResponse: a Content-Length that cannot parse is a framing error.
	_, _, err := nethttpReadResponse(bufio.NewReader(strings.NewReader(
		"HTTP/1.1 200 OK\r\nContent-Length: xx\r\n\r\n")), false)
	if err == nil {
		t.Error("bad Content-Length: expected error")
	}
	// nethttpReadResponse: an EOF before the status line.
	if _, _, err := nethttpReadResponse(bufio.NewReader(strings.NewReader("")), false); err == nil {
		t.Error("empty stream: expected error")
	}
	// nethttpReadResponse: a truncated header block (EOF before the blank line).
	if _, _, err := nethttpReadResponse(bufio.NewReader(strings.NewReader("HTTP/1.1 200 OK\r\n")), false); err == nil {
		t.Error("truncated headers: expected error")
	}
	// nethttpReadResponse: a body shorter than Content-Length is an EOF error.
	if _, _, err := nethttpReadResponse(bufio.NewReader(strings.NewReader(
		"HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nhi")), false); err == nil {
		t.Error("short body: expected error")
	}
	// nethttpReadResponse: an unframed body (no Content-Length, no chunked) reads to
	// EOF and marks the connection non-reusable.
	raw, keep, err := nethttpReadResponse(bufio.NewReader(strings.NewReader(
		"HTTP/1.1 200 OK\r\n\r\nunframed")), false)
	if err != nil || keep {
		t.Errorf("unframed: err=%v keep=%v, want nil/false", err, keep)
	}
	if !strings.HasSuffix(string(raw), "unframed") {
		t.Errorf("unframed raw = %q", raw)
	}
	// nethttpReadResponse: a repeated header key accumulates (both-values arm).
	raw, _, err = nethttpReadResponse(bufio.NewReader(strings.NewReader(
		"HTTP/1.1 200 OK\r\nX-A: 1\r\nX-A: 2\r\nContent-Length: 0\r\n\r\n")), false)
	if err != nil {
		t.Errorf("dup-header: %v", err)
	}
	// nethttpReadResponse: a chunked body whose framing is broken propagates the
	// chunk error.
	if _, _, err := nethttpReadResponse(bufio.NewReader(strings.NewReader(
		"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\nzz\r\n")), false); err == nil {
		t.Error("chunked framing error: expected error")
	}
	// nethttpReadResponse: an unframed body whose read fails after the headers
	// surfaces the read error (the io.ReadAll error arm).
	if _, _, err := nethttpReadResponse(bufio.NewReader(&prefixErrReader{
		prefix: []byte("HTTP/1.1 200 OK\r\n\r\n")}), false); err == nil {
		t.Error("unframed read error: expected error")
	}
	// nethttpCopyChunked: a chunk size that overflows int64 is a framing error.
	b0 := &bytes.Buffer{}
	if err := nethttpCopyChunked(bufio.NewReader(strings.NewReader("fffffffffffffffff\r\n")), b0); err == nil {
		t.Error("chunk-size overflow: expected error")
	}
	// nethttpCopyChunked: an EOF while consuming the trailing CRLF after the data.
	b0.Reset()
	if err := nethttpCopyChunked(bufio.NewReader(strings.NewReader("1\r\nX")), b0); err == nil {
		t.Error("missing chunk CRLF: expected error")
	}

	// nethttpCopyChunked: a bad chunk-size line and a mid-chunk EOF both error.
	var b bytes.Buffer
	if err := nethttpCopyChunked(bufio.NewReader(strings.NewReader("zzz\r\n")), &b); err == nil {
		t.Error("bad chunk size: expected error")
	}
	b.Reset()
	if err := nethttpCopyChunked(bufio.NewReader(strings.NewReader("5\r\nab")), &b); err == nil {
		t.Error("truncated chunk data: expected error")
	}
	b.Reset()
	if err := nethttpCopyChunked(bufio.NewReader(strings.NewReader("")), &b); err == nil {
		t.Error("EOF at chunk size: expected error")
	}

	// nethttpProxyConnect: a non-2xx CONNECT reply and a mid-header EOF both error.
	if err := nethttpProxyConnect(&scriptConn{r: strings.NewReader("HTTP/1.1 407 Denied\r\n\r\n")}, "h:1", ""); err == nil {
		t.Error("CONNECT non-2xx: expected error")
	}
	if err := nethttpProxyConnect(&scriptConn{r: strings.NewReader("HTTP/1.1 20")}, "h:1", "Basic x"); err == nil {
		t.Error("CONNECT truncated: expected error")
	}
	// nethttpProxyConnect: a write failure surfaces.
	if err := nethttpProxyConnect(&scriptConn{writeErr: io.ErrClosedPipe}, "h:1", ""); err == nil {
		t.Error("CONNECT write error: expected error")
	}
	// nethttpProxyConnect: a 2xx reply succeeds.
	if err := nethttpProxyConnect(&scriptConn{r: strings.NewReader("HTTP/1.1 200 OK\r\n\r\n")}, "h:1", ""); err != nil {
		t.Errorf("CONNECT 2xx: %v", err)
	}
}

// TestNetHTTPProxyNilArms covers nethttpNewInstance's proxy-argument arms not
// exercised elsewhere: with no proxy in the environment a nil / :ENV / omitted /
// empty p_addr leaves the instance proxy-less, and proxy_user / proxy_pass default
// to nil. The proxy environment is cleared so the :ENV arms resolve to direct
// regardless of the host's own settings.
func TestNetHTTPProxyNilArms(t *testing.T) {
	nethttpClearProxyEnv(t)
	got := runSrc(t, `require "net/http"
puts Net::HTTP.new("h", 80).proxy?
puts Net::HTTP.new("h", 80, nil).proxy?
puts Net::HTTP.new("h", 80, :ENV).proxy?
puts Net::HTTP.new("h", 80, "").proxy?
h = Net::HTTP.new("h", 80, "px", 3128)
puts h.proxy?
p h.proxy_user
p h.proxy_pass`)
	if want := "false\nfalse\nfalse\nfalse\ntrue\nnil\nnil"; got != want {
		t.Fatalf("proxy nil-arms = %q, want %q", got, want)
	}
}

// TestNetHTTPDialXferError covers nethttpDialXfer's proxy dial-error arms (both the
// plain-http and the CONNECT-tunnel dial) against a refused proxy endpoint, via a
// started instance so the persistent dial path is taken.
func TestNetHTTPDialXferError(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	refused := ln.Addr().(*net.TCPAddr)
	ln.Close()
	port := fmt.Sprintf("%d", refused.Port)

	// Plain-http proxy dial refused.
	got := runSrc(t, fmt.Sprintf(`require "net/http"
h = Net::HTTP.new("backend", 80, "127.0.0.1", %s)
begin; h.get("/"); rescue SocketError; puts "httpdial"; end`, port))
	if got != "httpdial" {
		t.Fatalf("proxy http dial = %q, want httpdial", got)
	}
	// CONNECT-tunnel proxy dial refused (use_ssl -> connectTunnel).
	got = runSrc(t, fmt.Sprintf(`require "net/http"
h = Net::HTTP.new("backend", 443, "127.0.0.1", %s)
h.use_ssl = true
begin; h.get("/"); rescue SocketError; puts "connectdial"; end`, port))
	if got != "connectdial" {
		t.Fatalf("proxy connect dial = %q, want connectdial", got)
	}
	// Direct (non-proxy) started-instance dial refused (persistent open-error arm).
	got = runSrc(t, fmt.Sprintf(`require "net/http"
h = Net::HTTP.new("127.0.0.1", %s)
begin; h.start { h.get("/") }; rescue SocketError; puts "directdial"; end`, port))
	if got != "directdial" {
		t.Fatalf("direct started dial = %q, want directdial", got)
	}
}

// prefixErrReader yields prefix once, then fails every read with io.ErrUnexpectedEOF,
// so nethttpReadResponse can read a header block and then hit a body read error.
type prefixErrReader struct {
	prefix []byte
	done   bool
}

func (r *prefixErrReader) Read(p []byte) (int, error) {
	if !r.done {
		n := copy(p, r.prefix)
		r.done = true
		return n, nil
	}
	return 0, io.ErrUnexpectedEOF
}

// TestNetHTTPKeepAliveRetry proves the stale-connection retry arm: a raw server
// that answers one request per connection (keep-alive implied, but the socket is
// then closed) makes the second request in a start block find its cached
// connection dead, drop it and redial — so both bodies are correct and two
// connections are used.
func TestNetHTTPKeepAliveRetry(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var accepts int64
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			seq := atomic.AddInt64(&accepts, 1)
			go func(c net.Conn, n int64) {
				// Read the request headers, answer with a keep-alive-looking response
				// (no Connection: close) but then close the socket, so a reused
				// connection is dead on the next request.
				br := bufio.NewReader(c)
				for {
					line, err := br.ReadString('\n')
					if err != nil || strings.TrimRight(line, "\r\n") == "" {
						break
					}
				}
				body := fmt.Sprintf("R%d", n)
				fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
				c.Close()
			}(conn, seq)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	got := runSrc(t, fmt.Sprintf(`require "net/http"
h = Net::HTTP.new("127.0.0.1", %d)
h.start do
  puts h.get("/").body
  puts h.get("/").body
end`, addr.Port))
	if want := "R1\nR2"; got != want {
		t.Fatalf("keep-alive retry bodies = %q, want %q", got, want)
	}
	if n := atomic.LoadInt64(&accepts); n != 2 {
		t.Fatalf("keep-alive retry used %d connections, want 2", n)
	}
}

// base64Std is the std base64 of s, for building expected Basic credentials.
func base64Std(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// timeoutErr is a net.Error whose Timeout() is true, for raiseTransportErr.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

// scriptConn is a minimal net.Conn whose reads come from r and whose writes
// optionally fail with writeErr, for driving nethttpProxyConnect off a socket.
type scriptConn struct {
	r        io.Reader
	writeErr error
}

func (c *scriptConn) Read(p []byte) (int, error) {
	if c.r == nil {
		return 0, io.EOF
	}
	return c.r.Read(p)
}
func (c *scriptConn) Write(p []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return len(p), nil
}
func (c *scriptConn) Close() error                       { return nil }
func (c *scriptConn) LocalAddr() net.Addr                { return nil }
func (c *scriptConn) RemoteAddr() net.Addr               { return nil }
func (c *scriptConn) SetDeadline(_ time.Time) error      { return nil }
func (c *scriptConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *scriptConn) SetWriteDeadline(_ time.Time) error { return nil }

// nethttpClearProxyEnv blanks every proxy-related environment variable for the
// duration of a test so that :ENV resolution is driven only by what the test sets
// (t.Setenv restores the prior values on cleanup).
func nethttpClearProxyEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY",
		"no_proxy", "NO_PROXY", "REQUEST_METHOD",
	} {
		t.Setenv(k, "")
	}
}

// TestNetHTTPEnvProxyRouting is the end-to-end :ENV proof: with http_proxy set to
// an in-test forwarding proxy, Net::HTTP.new(host, port) (p_addr defaulting to
// :ENV) routes through it — proxy? / proxy_address / proxy_port reflect the
// resolved endpoint and the proxy sees the request. no_proxy on the target host,
// and an explicit nil p_addr, both force a direct connection even with the
// environment set.
func TestNetHTTPEnvProxyRouting(t *testing.T) {
	nethttpClearProxyEnv(t)

	backend := httptest.NewServer(nethttpTestMux())
	defer backend.Close()

	var proxyHits int64
	var sawAuth string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&proxyHits, 1)
		sawAuth = r.Header.Get("Proxy-Authorization")
		resp, err := http.Get(r.RequestURI)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		w.Write(body)
	}))
	defer proxy.Close()

	bu, _ := url.Parse(backend.URL)
	pu, _ := url.Parse(proxy.URL)

	// http_proxy carries userinfo so the resolved credentials become a
	// Proxy-Authorization header on the routed request.
	t.Setenv("http_proxy", "http://usr:pwd@"+pu.Host)

	got := runSrc(t, fmt.Sprintf(`require "net/http"
h = Net::HTTP.new(%q, %s)
puts h.proxy?
puts h.proxy_address
puts h.proxy_port
puts h.proxy_user
puts h.proxy_pass
puts h.get("/echo").body`, bu.Hostname(), bu.Port()))
	want := "true\n" + pu.Hostname() + "\n" + pu.Port() + "\nusr\npwd\nGET|/echo||"
	if got != want {
		t.Fatalf("env proxy GET = %q, want %q", got, want)
	}
	if n := atomic.LoadInt64(&proxyHits); n < 1 {
		t.Fatalf("env proxy was not hit (%d)", n)
	}
	if wantAuth := "Basic " + base64Std("usr:pwd"); sawAuth != wantAuth {
		t.Fatalf("Proxy-Authorization = %q, want %q", sawAuth, wantAuth)
	}

	// no_proxy on the target host bypasses the proxy: a direct connection reaches
	// the backend and the proxy stays untouched.
	atomic.StoreInt64(&proxyHits, 0)
	t.Setenv("no_proxy", bu.Hostname())
	got = runSrc(t, fmt.Sprintf(`require "net/http"
h = Net::HTTP.new(%q, %s)
puts h.proxy?
puts h.get("/echo").body`, bu.Hostname(), bu.Port()))
	if want = "false\nGET|/echo||"; got != want {
		t.Fatalf("no_proxy bypass GET = %q, want %q", got, want)
	}
	if n := atomic.LoadInt64(&proxyHits); n != 0 {
		t.Fatalf("no_proxy: proxy was hit (%d)", n)
	}

	// An explicit nil p_addr opts out of environment resolution entirely, so even
	// with http_proxy set (and no_proxy cleared) the request goes direct.
	atomic.StoreInt64(&proxyHits, 0)
	t.Setenv("no_proxy", "")
	got = runSrc(t, fmt.Sprintf(`require "net/http"
h = Net::HTTP.new(%q, %s, nil)
puts h.proxy?
puts h.get("/echo").body`, bu.Hostname(), bu.Port()))
	if want = "false\nGET|/echo||"; got != want {
		t.Fatalf("nil p_addr direct GET = %q, want %q", got, want)
	}
	if n := atomic.LoadInt64(&proxyHits); n != 0 {
		t.Fatalf("nil p_addr: proxy was hit (%d)", n)
	}
}

// TestNetHTTPEnvProxyHTTPS proves the https_proxy arm end-to-end: with use_ssl set
// the instance resolves https_proxy and reaches the TLS backend through a
// CONNECT-tunnelling proxy.
func TestNetHTTPEnvProxyHTTPS(t *testing.T) {
	nethttpClearProxyEnv(t)

	backend := httptest.NewTLSServer(nethttpTestMux())
	defer backend.Close()

	var connects int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "expected CONNECT", http.StatusMethodNotAllowed)
			return
		}
		atomic.AddInt64(&connects, 1)
		dst, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			dst.Close()
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			dst.Close()
			return
		}
		conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() { io.Copy(dst, conn); dst.Close() }()
		io.Copy(conn, dst)
		conn.Close()
	}))
	defer proxy.Close()

	bu, _ := url.Parse(backend.URL)
	pu, _ := url.Parse(proxy.URL)
	// Uppercase HTTPS_PROXY exercises the fallback lookup order.
	t.Setenv("HTTPS_PROXY", "http://"+pu.Host)

	got := runSrc(t, fmt.Sprintf(`require "net/http"
h = Net::HTTP.new(%q, %s)
h.use_ssl = true
h.verify_mode = OpenSSL::SSL::VERIFY_NONE
puts h.proxy?
puts h.proxy_address
puts h.get("/").body`, bu.Hostname(), bu.Port()))
	want := "true\n" + pu.Hostname() + "\nhello net/http"
	if got != want {
		t.Fatalf("env https proxy GET = %q, want %q", got, want)
	}
	if n := atomic.LoadInt64(&connects); n < 1 {
		t.Fatalf("CONNECT tunnel was not used (%d)", n)
	}
}

// TestNetHTTPEnvProxyCGISafety covers MRI's CGI-safety rule: when REQUEST_METHOD is
// present, the uppercase HTTP_PROXY (attacker-controllable via the Proxy header in
// CGI) is ignored, while the lowercase http_proxy is still honored.
func TestNetHTTPEnvProxyCGISafety(t *testing.T) {
	nethttpClearProxyEnv(t)
	t.Setenv("REQUEST_METHOD", "GET")

	// Uppercase HTTP_PROXY alone under CGI ⇒ ignored ⇒ direct.
	t.Setenv("HTTP_PROXY", "http://blackhole.invalid:8080")
	if got := runSrc(t, `require "net/http"
puts Net::HTTP.new("h", 80).proxy?`); got != "false" {
		t.Fatalf("CGI HTTP_PROXY (upper) proxy? = %q, want false", got)
	}

	// Lowercase http_proxy under CGI ⇒ honored.
	t.Setenv("http_proxy", "http://proxy.internal:3128")
	got := runSrc(t, `require "net/http"
h = Net::HTTP.new("h", 80)
puts h.proxy?
puts h.proxy_address
puts h.proxy_port`)
	if want := "true\nproxy.internal\n3128"; got != want {
		t.Fatalf("CGI http_proxy (lower) = %q, want %q", got, want)
	}
}

// TestNetHTTPEnvProxyResolveUnits exercises the resolver and no_proxy matcher arms
// directly, including the branches the end-to-end tests do not naturally reach
// (scheme-less proxy strings, malformed values, https default port, CIDR / bare
// domain / port-qualified no_proxy entries).
func TestNetHTTPEnvProxyResolveUnits(t *testing.T) {
	if !nethttpIsENV(object.Symbol("ENV")) || nethttpIsENV(object.NewString("ENV")) {
		t.Fatal("nethttpIsENV must match only the :ENV symbol")
	}

	// No proxy in the environment ⇒ direct.
	nethttpClearProxyEnv(t)
	if _, _, _, _, ok := nethttpResolveEnvProxy("h", 80, false); ok {
		t.Fatal("empty env should resolve direct")
	}

	// A scheme-less "host:port" is retried as an http:// URL.
	t.Setenv("http_proxy", "proxy.example:8080")
	if a, p, _, _, ok := nethttpResolveEnvProxy("h", 80, false); !ok || a != "proxy.example" || p != 8080 {
		t.Fatalf("scheme-less proxy = (%q,%d,%v), want proxy.example,8080,true", a, p, ok)
	}

	// A value malformed even after the http:// retry ⇒ direct.
	t.Setenv("http_proxy", "%zz")
	if _, _, _, _, ok := nethttpResolveEnvProxy("h", 80, false); ok {
		t.Fatal("malformed proxy should resolve direct")
	}

	// The https branch defaults the port to 443 when the URL omits it.
	t.Setenv("https_proxy", "https://secure.proxy")
	if a, p, _, _, ok := nethttpResolveEnvProxy("h", 443, true); !ok || a != "secure.proxy" || p != 443 {
		t.Fatalf("https default-port proxy = (%q,%d,%v), want secure.proxy,443,true", a, p, ok)
	}

	// no_proxy bypass arms.
	if !nethttpNoProxyBypass("api.example.com", 80, ".example.com") {
		t.Fatal("leading-dot suffix should bypass")
	}
	if !nethttpNoProxyBypass("api.example.com", 80, "example.com") {
		t.Fatal("bare domain should bypass its subdomain")
	}
	if !nethttpNoProxyBypass("host", 8080, "host:8080") {
		t.Fatal("port-qualified entry on matching port should bypass")
	}
	if nethttpNoProxyBypass("host", 80, "host:8080") {
		t.Fatal("port-qualified entry on other port should not bypass")
	}
	if !nethttpNoProxyBypass("10.1.2.3", 80, "10.0.0.0/8") {
		t.Fatal("CIDR should bypass a contained IP host")
	}
	if !nethttpNoProxyBypass("10.1.2.3", 80, "10.1.2.3") {
		t.Fatal("exact IP entry should bypass")
	}
	// Differently-spelled but equal IPs match via net.IP equality, not the string
	// compare (::0.0.0.1 and ::1 are the same address).
	if !nethttpNoProxyBypass("::1", 80, "::0.0.0.1") {
		t.Fatal("equal-but-differently-spelled IP entry should bypass")
	}
	if nethttpNoProxyBypass("other.net", 80, "example.com, .foo.com") {
		t.Fatal("non-matching host should not bypass")
	}
	if nethttpNoProxyBypass("10.1.2.3", 80, "192.168.0.0/16") {
		t.Fatal("IP host outside the CIDR should not bypass")
	}
	if nethttpNoProxyBypass("10.1.2.3", 80, "10.9.9.9") {
		t.Fatal("non-matching IP entry should not bypass")
	}
	if nethttpNoProxyBypass("10.1.2.3", 80, ":80") {
		t.Fatal("a port-only entry (empty host) should not bypass")
	}
	if nethttpNoProxyBypass("", 80, "example.com") || nethttpNoProxyBypass("h", 80, "") {
		t.Fatal("empty host or empty list should not bypass")
	}
}
