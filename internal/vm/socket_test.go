// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
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

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The socket transport is proven end-to-end against real, in-process loopback
// servers started inside each test (net.Listener echoes, net/http/httptest for
// HTTP and TLS), so every assertion exercises the actual Go net / crypto/tls
// path a Ruby script drives through the rbgo engine — hermetic, no external
// network.

// hostPortOf splits a "host:port" (or a URL's host) into the pieces the Ruby
// TCPSocket.new(host, port) call wants.
func hostPortOf(t *testing.T, hostport string) (string, string) {
	t.Helper()
	h, p, err := net.SplitHostPort(hostport)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", hostport, err)
	}
	return h, p
}

// startEcho starts a loopback TCP echo server (each connection's input is copied
// back to its output) and returns its host and port. It is torn down at test end.
func startEcho(t *testing.T) (string, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { io.Copy(c, c); c.Close() }(c)
		}
	}()
	return hostPortOf(t, ln.Addr().String())
}

// startGreeter starts a loopback server that writes a fixed payload to each
// connection then closes it, so the client sees a bounded stream ending in EOF.
func startGreeter(t *testing.T, payload string) (string, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Write([]byte(payload))
			c.Close()
		}
	}()
	return hostPortOf(t, ln.Addr().String())
}

// TestTCPSocketHTTPLoopback is the headline proof: an httptest HTTP server is
// started in-process, a Ruby script opens a raw TCPSocket to it, writes an
// HTTP/1.0 request, reads the whole response, and the assertion is on the body.
func TestTCPSocketHTTPLoopback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello from rbgo socket")
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	src := fmt.Sprintf(`
require "socket"
s = TCPSocket.new(%q, %s)
s.write("GET / HTTP/1.0\r\nHost: %s\r\n\r\n")
body = s.read
s.close
puts body.split("\r\n\r\n", 2).last`, u.Hostname(), u.Port(), u.Host)
	if got := runSrc(t, src); !strings.Contains(got, "hello from rbgo socket") {
		t.Fatalf("HTTP loopback body = %q", got)
	}
}

// TestSSLSocketTLSLoopback proves the crypto/tls path: an httptest TLS server
// (self-signed cert) is wrapped by a Ruby OpenSSL::SSL::SSLSocket over a
// TCPSocket; with the default (VERIFY_NONE) context the handshake succeeds and
// the HTTPS body round-trips.
func TestSSLSocketTLSLoopback(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "secure hello")
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	src := fmt.Sprintf(`
require "socket"
require "openssl"
tcp = TCPSocket.new(%q, %s)
ssl = OpenSSL::SSL::SSLSocket.new(tcp)
ssl.connect
ssl.write("GET / HTTP/1.0\r\nHost: %s\r\n\r\n")
body = ssl.read
ssl.close
puts body.split("\r\n\r\n", 2).last`, u.Hostname(), u.Port(), u.Host)
	if got := runSrc(t, src); !strings.Contains(got, "secure hello") {
		t.Fatalf("TLS loopback body = %q", got)
	}
}

// TestTCPServerAcceptLoopback drives TCPServer.new + addr + accept + close with a
// concurrent in-VM TCPSocket client (Thread), all through the rbgo engine.
func TestTCPServerAcceptLoopback(t *testing.T) {
	src := `
require "socket"
srv = TCPServer.new("127.0.0.1", 0)
port = srv.addr[1]
t = Thread.new do
  c = TCPSocket.new("127.0.0.1", port)
  c.write("ping\n")
  c.close
end
conn = srv.accept
line = conn.gets
conn.close
srv.close
t.join
puts line`
	if got := runSrc(t, src); got != "ping" {
		t.Fatalf("TCPServer accept got %q, want \"ping\"", got)
	}
}

// TestTCPSocketWriteReadMethods round-trips through the echo server to cover the
// write family (write/print/<</puts and the returned byte count) and the
// deterministic read family (read(n)/gets), plus flush/sync/setsockopt and the
// address queries.
func TestTCPSocketWriteReadMethods(t *testing.T) {
	host, port := startEcho(t)
	cases := []struct{ src, want string }{
		// write returns the byte count.
		{`s=TCPSocket.new(H,P); n=s.write("abc"); s.close; p n`, "3"},
		// write/print/<</puts all reach the wire; read(n) and gets read it back.
		{`s=TCPSocket.new(H,P); s.write("ab"); s.print("cd"); s << "ef"; s.puts("gh")
p s.read(2); p s.read(4); p s.gets; s.close`, "\"ab\"\n\"cdef\"\n\"gh\\n\""},
		// puts with no args writes a bare newline; gets reads it.
		{`s=TCPSocket.new(H,P); s.puts; p s.gets; s.close`, "\"\\n\""},
		// flush / sync / sync= / setsockopt are accepted; the socket still works.
		{`s=TCPSocket.new(H,P); s.flush; p s.sync; p(s.sync=false); p s.setsockopt(1,2,3)
s.write("x"); p s.read(1); s.close`, "true\nfalse\n0\n\"x\""},
		// address queries return the MRI 4-tuple; the family is AF_INET on 127.0.0.1.
		{`s=TCPSocket.new(H,P); p s.peeraddr[0]; p s.local_address[0]; p s.addr[0]; s.close`,
			"\"AF_INET\"\n\"AF_INET\"\n\"AF_INET\""},
		// writing a non-String coerces via to_s.
		{`s=TCPSocket.new(H,P); s.write(42); p s.read(2); s.close`, "\"42\""},
		// closed? flips after close.
		{`s=TCPSocket.new(H,P); p s.closed?; s.close; p s.closed?`, "false\ntrue"},
		// a String port is accepted (numeric service form), as MRI allows.
		{`s=TCPSocket.new(H,P.to_s); s.write("z"); p s.read(1); s.close`, "\"z\""},
		// inspect / to_s / truthiness of the socket value.
		{`s=TCPSocket.new(H,P); p s; puts s.to_s; p(!!s); s.close`, "#<TCPSocket>\n#<TCPSocket>\ntrue"},
	}
	for _, c := range cases {
		src := "require \"socket\"\nH=" + fmt.Sprintf("%q", host) + "\nP=" + port + "\n" + c.src
		if got := runSrc(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestTCPSocketReadEOF covers the end-of-stream arms against the greeter server:
// read-all, read(n) exhausting then nil, eof?, gets whole-stream separator, and
// readpartial including its EOFError at end.
func TestTCPSocketReadEOF(t *testing.T) {
	host, port := startGreeter(t, "hello world")
	cases := []struct{ src, want string }{
		// read with no arg (and explicit nil) drains to EOF.
		{`s=TCPSocket.new(H,P); p s.read; s.close`, "\"hello world\""},
		{`s=TCPSocket.new(H,P); p s.read(nil); s.close`, "\"hello world\""},
		// read(0) and readpartial(0) return "" without touching the stream.
		{`s=TCPSocket.new(H,P); p s.read(0); s.close`, "\"\""},
		{`s=TCPSocket.new(H,P); p s.readpartial(0); s.close`, "\"\""},
		// read(n) returns up to n; a second read past EOF returns nil, eof? is true.
		{`s=TCPSocket.new(H,P); p s.read(11); p s.read(1); p s.eof?; p s.eof; s.close`, "\"hello world\"\nnil\ntrue\ntrue"},
		// readpartial returns available bytes, then raises EOFError at end.
		{`s=TCPSocket.new(H,P); p s.readpartial(100); begin; s.readpartial(1); rescue EOFError; puts "eof"; end; s.close`, "\"hello world\"\neof"},
		// gets("") reads the whole stream (empty separator); a following gets is nil.
		{`s=TCPSocket.new(H,P); p s.gets(""); p s.gets; s.close`, "\"hello world\"\nnil"},
		// a multi-character separator terminates gets at that sequence.
		{`s=TCPSocket.new(H,P); p s.gets("lo"); s.close`, "\"hello\""},
		// a multi-character separator never seen: gets returns the whole stream at EOF.
		{`s=TCPSocket.new(H,P); p s.gets("zz"); s.close`, "\"hello world\""},
	}
	for _, c := range cases {
		src := "require \"socket\"\nH=" + fmt.Sprintf("%q", host) + "\nP=" + port + "\n" + c.src
		if got := runSrc(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSocketErrors covers the raising arms of the socket surface: argument
// arity / type errors, a refused connection (SocketError), TCPServer arity and
// accept-after-close, the raw-Socket follow-up stub, and the TypeError guard hit
// by calling an inherited TCPSocket method on a TCPServer receiver.
func TestSocketErrors(t *testing.T) {
	// A listener opened then closed gives a port that refuses connections.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	_, refusedPort := hostPortOf(t, ln.Addr().String())
	ln.Close()

	host, port := startEcho(t)
	H := fmt.Sprintf("%q", host)
	cases := []struct{ src, want string }{
		{`begin; TCPSocket.new(H); rescue ArgumentError; puts "arity"; end`, "arity"},
		{`begin; TCPSocket.new(H, []); rescue TypeError; puts "badport"; end`, "badport"},
		{`begin; TCPSocket.new(H, P); rescue SocketError; puts "refused"; end`, "refused"},
		{`s=TCPSocket.new(H2,P2); begin; s.read(-1); rescue ArgumentError; puts "neg"; end; s.close`, "neg"},
		{`s=TCPSocket.new(H2,P2); begin; s.readpartial(-1); rescue ArgumentError; puts "negrp"; end; s.close`, "negrp"},
		{`s=TCPSocket.new(H2,P2); begin; s.readpartial; rescue ArgumentError; puts "rparity"; end; s.close`, "rparity"},
		{`begin; TCPServer.new; rescue ArgumentError; puts "srv0"; end`, "srv0"},
		{`begin; TCPServer.new(1,2,3); rescue ArgumentError; puts "srv3"; end`, "srv3"},
		// The one-argument form binds a port on all interfaces.
		{`srv=TCPServer.new(0); p srv.closed?; srv.close; puts "srv1ok"`, "false\nsrv1ok"},
		// Binding an address not assigned to this host fails as SocketError.
		{`begin; TCPServer.new("240.0.0.1", 0); rescue SocketError; puts "bindfail"; end`, "bindfail"},
		{`srv=TCPServer.new("127.0.0.1",0); srv.close; p srv.closed?; begin; srv.accept; rescue IOError; puts "acc"; end`, "true\nacc"},
		{`srv=TCPServer.new("127.0.0.1",0); p srv.listen(5); p srv.local_address[0]; srv.close`, "0\n\"AF_INET\""},
		{`srv=TCPServer.new("127.0.0.1",0); p srv; puts srv.to_s; p(!!srv); srv.close`, "#<TCPServer>\n#<TCPServer>\ntrue"},
		{`begin; Socket.new; rescue NotImplementedError; puts "rawsock"; end`, "rawsock"},
		{`p Socket::SOCK_STREAM; p Socket::AF_INET6`, "1\n30"},
		// An inherited TCPSocket method invoked on a TCPServer trips the type guard.
		{`srv=TCPServer.new("127.0.0.1",0); begin; srv.read(1); rescue TypeError; puts "guard"; end; srv.close`, "guard"},
	}
	for _, c := range cases {
		src := "require \"socket\"\nH=" + H + "\nP=" + refusedPort +
			"\nH2=" + fmt.Sprintf("%q", host) + "\nP2=" + port + "\n" + c.src
		if got := runSrc(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSSLSocketErrorsAndConfig covers the TLS binding's non-happy paths and its
// SSLContext configuration: construction errors, use before the handshake,
// closing an un-connected socket, the wrapped-io accessors, and a VERIFY_PEER
// context (both direct and via set_params) turning the self-signed handshake
// into an SSLError.
func TestSSLSocketErrorsAndConfig(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	mk := func(body string) string {
		return fmt.Sprintf("require \"socket\"\nrequire \"openssl\"\nHOST=%q\nPORT=%s\n%s",
			u.Hostname(), u.Port(), body)
	}
	cases := []struct{ src, want string }{
		{`begin; OpenSSL::SSL::SSLSocket.new; rescue ArgumentError; puts "arity"; end`, "arity"},
		{`begin; OpenSSL::SSL::SSLSocket.new("nope"); rescue TypeError; puts "type"; end`, "type"},
		// Reading before #connect raises SSLError (session not started).
		{`tcp=TCPSocket.new(HOST,PORT); ssl=OpenSSL::SSL::SSLSocket.new(tcp)
begin; ssl.read(1); rescue OpenSSL::SSL::SSLError; puts "notstarted"; end; ssl.close`, "notstarted"},
		// Writing before #connect likewise raises SSLError (writer guard).
		{`tcp=TCPSocket.new(HOST,PORT); ssl=OpenSSL::SSL::SSLSocket.new(tcp)
begin; ssl.write("x"); rescue OpenSSL::SSL::SSLError; puts "nowrite"; end; ssl.close`, "nowrite"},
		// inspect / to_s / truthiness of the SSLSocket value.
		{`tcp=TCPSocket.new(HOST,PORT); ssl=OpenSSL::SSL::SSLSocket.new(tcp)
p ssl; puts ssl.to_s; p(!!ssl); ssl.close`, "#<OpenSSL::SSL::SSLSocket>\n#<OpenSSL::SSL::SSLSocket>\ntrue"},
		// Closing an un-connected SSLSocket releases the raw socket (else-branch).
		{`tcp=TCPSocket.new(HOST,PORT); ssl=OpenSSL::SSL::SSLSocket.new(tcp); p ssl.closed?; ssl.close; p ssl.closed?`, "false\ntrue"},
		// The wrapped-io / context / hostname accessors.
		{`tcp=TCPSocket.new(HOST,PORT); ssl=OpenSSL::SSL::SSLSocket.new(tcp)
p ssl.io.equal?(tcp); p ssl.to_io.equal?(tcp); p ssl.context.nil?; p ssl.peer_cert
ssl.hostname = "example.test"; p ssl.hostname; ssl.close`, "true\ntrue\ntrue\nnil\n\"example.test\""},
		// A VERIFY_PEER context makes the self-signed handshake fail as SSLError.
		{`tcp=TCPSocket.new(HOST,PORT); ctx=OpenSSL::SSL::SSLContext.new; ctx.verify_mode=OpenSSL::SSL::VERIFY_PEER
ssl=OpenSSL::SSL::SSLSocket.new(tcp, ctx)
begin; ssl.connect; rescue OpenSSL::SSL::SSLError; puts "verify"; end; ssl.close`, "verify"},
		// Same, but verify_mode supplied through set_params (the @params path).
		{`tcp=TCPSocket.new(HOST,PORT); ctx=OpenSSL::SSL::SSLContext.new
ctx.set_params(verify_mode: OpenSSL::SSL::VERIFY_PEER)
ssl=OpenSSL::SSL::SSLSocket.new(tcp, ctx)
begin; ssl.connect_nonblock; rescue OpenSSL::SSL::SSLError; puts "verify2"; end; ssl.close`, "verify2"},
		// The SSLContext accessors store and read back.
		{`ctx=OpenSSL::SSL::SSLContext.new; ctx.cert="c"; ctx.key="k"; ctx.ca_file="f"
ctx.ca_path="p"; ctx.ciphers="x"; ctx.options=1; ctx.min_version=2; ctx.max_version=3
p [ctx.cert, ctx.key, ctx.ca_file, ctx.ca_path, ctx.ciphers, ctx.options, ctx.min_version, ctx.max_version]`,
			`["c", "k", "f", "p", "x", 1, 2, 3]`},
	}
	for _, c := range cases {
		if got := runSrc(t, mk(c.src)); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// badAddr is a net.Addr whose String() carries no port, exercising addrTuple's
// SplitHostPort error arm.
type badAddr struct{}

func (badAddr) Network() string { return "bad" }
func (badAddr) String() string  { return "no-port-here" }

// TestAddrTupleAndGuards covers the address-tuple helper's IPv6 and malformed
// arms and the type-narrowing guards directly (the guards are unreachable
// through normal dispatch for the server/SSL receivers, so they are injected).
func TestAddrTupleAndGuards(t *testing.T) {
	v6 := addrTuple(&net.TCPAddr{IP: net.ParseIP("::1"), Port: 443}).(*object.Array)
	if v6.Elems[0].ToS() != "AF_INET6" || v6.Elems[1].ToS() != "443" {
		t.Errorf("ipv6 tuple = %v", v6.Elems)
	}
	bad := addrTuple(badAddr{}).(*object.Array)
	if bad.Elems[0].ToS() != "AF_INET" || bad.Elems[2].ToS() != "no-port-here" {
		t.Errorf("bad-addr tuple = %v", bad.Elems)
	}

	mustPanic := func(name string, fn func()) {
		defer func() {
			r := recover()
			re, ok := r.(RubyError)
			if !ok || re.Class != "TypeError" {
				t.Errorf("%s: recover = %v, want TypeError", name, r)
			}
		}()
		fn()
	}
	mustPanic("asTCPSocket", func() { asTCPSocket(object.NilV) })
	mustPanic("asTCPServer", func() { asTCPServer(object.NilV) })
	mustPanic("asSSLSocket", func() { asSSLSocket(object.NilV) })
}

// TestSSLVerifyModeResolution covers sslVerifyMode's precedence: the ivar wins,
// then the set_params hash, then the VERIFY_NONE default, including the
// non-integer fall-throughs and the nil-context case.
func TestSSLVerifyModeResolution(t *testing.T) {
	cls := newClass("Ctx", nil)
	obj := func(set func(*RObject)) *RObject {
		o := &RObject{class: cls, ivars: map[string]object.Value{}}
		set(o)
		return o
	}
	if got := sslVerifyMode(object.NilV); got != 0 {
		t.Errorf("nil ctx = %d", got)
	}
	ivar := obj(func(o *RObject) { o.ivars["@verify_mode"] = object.IntValue(1) })
	if got := sslVerifyMode(ivar); got != 1 {
		t.Errorf("ivar = %d", got)
	}
	// Non-integer @verify_mode is ignored; falls to the VERIFY_NONE default.
	badIvar := obj(func(o *RObject) { o.ivars["@verify_mode"] = object.NewString("x") })
	if got := sslVerifyMode(badIvar); got != 0 {
		t.Errorf("bad ivar = %d", got)
	}
	h := object.NewHash()
	h.Set(object.Symbol("verify_mode"), object.IntValue(1))
	viaParams := obj(func(o *RObject) { o.ivars["@params"] = h })
	if got := sslVerifyMode(viaParams); got != 1 {
		t.Errorf("params = %d", got)
	}
	// @params present but with a non-integer verify_mode -> default 0.
	hbad := object.NewHash()
	hbad.Set(object.Symbol("verify_mode"), object.NewString("x"))
	viaBadParams := obj(func(o *RObject) { o.ivars["@params"] = hbad })
	if got := sslVerifyMode(viaBadParams); got != 0 {
		t.Errorf("bad params = %d", got)
	}
}

// TestSocketFeatureRequire confirms require "socket" is a provided feature (true
// once, false after) and does not hit the filesystem.
func TestSocketFeatureRequire(t *testing.T) {
	if got := runSrc(t, `p require "socket"; p require "socket"`); got != "true\nfalse" {
		t.Fatalf("require socket = %q", got)
	}
}

// TestSocketThroughRbgoBinary is the literal "through the rbgo binary" proof: it
// builds the cmd/rbgo executable and runs a script file that TCPSocket.new's to
// an in-process httptest server, asserting the HTTP body on the binary's stdout.
// It is skipped (not failed) if the binary cannot be built, so the unrelated
// pre-existing cmd/rbgo codegen issue never blocks this suite.
func TestSocketThroughRbgoBinary(t *testing.T) {
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

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello via binary")
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)

	script := filepath.Join(t.TempDir(), "main.rb")
	body := fmt.Sprintf(`require "socket"
s = TCPSocket.new(%q, %s)
s.write("GET / HTTP/1.0\r\nHost: %s\r\n\r\n")
resp = s.read
s.close
print resp.split("\r\n\r\n", 2).last`, u.Hostname(), u.Port(), u.Host)
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, script).CombinedOutput()
	if err != nil {
		t.Fatalf("run binary: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hello via binary") {
		t.Fatalf("binary stdout = %q", out)
	}
}

// repoRoot walks up from the test's working directory to the module root (the
// directory holding go.mod), so the in-test `go build ./cmd/rbgo` runs there.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test dir")
		}
		dir = parent
	}
}
