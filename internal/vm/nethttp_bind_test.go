// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
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
	"testing"
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

	// nethttpDo: NewRequest rejects an unknown method (ArgumentError).
	wantRaise(t, "ArgumentError", func() {
		vm.nethttpDo("http", "127.0.0.1", "1", "h", "BOGUS", "/", nil, nil, 0)
	})
	// nethttpDo: Request.Bytes rejects a Request-Line with CR/LF (Net::HTTPError),
	// before any dial.
	wantRaise(t, "Net::HTTPError", func() {
		vm.nethttpDo("http", "127.0.0.1", "1", "h", "GET", "/a\r\nb", nil, nil, 0)
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

// TestNetHTTPExchangeErrors covers httpExchange's write-error and read-error arms
// and trimResponseToHeaders' no-terminator arm.
func TestNetHTTPExchangeErrors(t *testing.T) {
	// Write error: the writer fails immediately.
	if _, err := httpExchange(&fakeStream{w: failWriter{}, r: bufio.NewReader(strings.NewReader(""))}, []byte("x")); err != io.ErrClosedPipe {
		t.Errorf("write-error arm = %v, want ErrClosedPipe", err)
	}
	// Read error: the write succeeds, the read fails.
	if _, err := httpExchange(&fakeStream{w: io.Discard, r: bufio.NewReader(errReader{})}, []byte("x")); err != io.ErrUnexpectedEOF {
		t.Errorf("read-error arm = %v, want ErrUnexpectedEOF", err)
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
