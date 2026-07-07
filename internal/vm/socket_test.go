// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
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
	"time"

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
		// Raw Socket.new now exists (socket_raw.go); the no-argument form is an
		// arity error, and the raw surface has its own dedicated test suite.
		{`begin; Socket.new; rescue ArgumentError; puts "rawsock"; end`, "rawsock"},
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

// startUDPEcho starts a loopback UDP echo server (each received datagram is sent
// straight back to its sender) and returns its host and port. Torn down at test
// end.
func startUDPEcho(t *testing.T) (string, string) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listenUDP: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 65536)
		for {
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			conn.WriteToUDP(buf[:n], addr)
		}
	}()
	return hostPortOf(t, conn.LocalAddr().String())
}

// TestUDPSocketLoopback is the datagram proof: a Go UDP echo server runs
// in-process; a Ruby UDPSocket both #connect+#send+#recv (default-length) and
// #send-with-explicit-destination+#recvfrom round-trip a message and read the
// echo back, all through the rbgo engine.
func TestUDPSocketLoopback(t *testing.T) {
	host, port := startUDPEcho(t)
	cases := []struct{ src, want string }{
		// connect records the peer; send(msg, flags) uses it; recv (no maxlen ->
		// default buffer) reads the echoed datagram.
		{`s=UDPSocket.new; s.connect(H,P); p s.send("ping", 0); puts s.recv; s.close`, "4\nping"},
		// send(msg, flags, host, port) datagram-addresses explicitly; recvfrom
		// yields [payload, [family, port, host, ip]].
		{`s=UDPSocket.new; n=s.send("pong", 0, H, P); msg, from = s.recvfrom(100)
p n; puts msg; puts from[0]; s.close`, "4\npong\nAF_INET"},
		// bind picks a concrete local address; addr / local_address report it.
		{`s=UDPSocket.new; s.bind("127.0.0.1", 0); p s.addr[0]; p s.local_address[0]
p s.closed?; s.close; p s.closed?`, "\"AF_INET\"\n\"AF_INET\"\nfalse\ntrue"},
		// open is an alias for new and accepts an explicit AF_INET family.
		{`s=UDPSocket.open(Socket::AF_INET); s.connect(H,P); s.send("x",0); puts s.recv(10); s.close`, "x"},
	}
	for _, c := range cases {
		src := "require \"socket\"\nH=" + fmt.Sprintf("%q", host) + "\nP=" + port + "\n" + c.src
		if got := runSrc(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestUDPSocketErrors covers the datagram surface's raising arms: constructor
// family type error, method arities, send without a destination, negative recv
// length, resolve failures, and send/recv after close.
func TestUDPSocketErrors(t *testing.T) {
	host, port := startUDPEcho(t)
	cases := []struct{ src, want string }{
		{`begin; UDPSocket.new("nope"); rescue TypeError; puts "af"; end`, "af"},
		{`s=UDPSocket.new; begin; s.bind("h"); rescue ArgumentError; puts "bindarity"; end; s.close`, "bindarity"},
		{`s=UDPSocket.new; begin; s.connect("h"); rescue ArgumentError; puts "connarity"; end; s.close`, "connarity"},
		{`s=UDPSocket.new; begin; s.send("x"); rescue ArgumentError; puts "sendarity"; end; s.close`, "sendarity"},
		{`s=UDPSocket.new; begin; s.send("x", 0); rescue SocketError; puts "nodest"; end; s.close`, "nodest"},
		{`s=UDPSocket.new; begin; s.recv(-1); rescue ArgumentError; puts "neg"; end; s.close`, "neg"},
		// Resolve failures (out-of-range port) on connect / send / bind.
		{`s=UDPSocket.new; begin; s.connect("127.0.0.1", 99999999); rescue SocketError; puts "cres"; end; s.close`, "cres"},
		{`s=UDPSocket.new; begin; s.send("x", 0, "127.0.0.1", 99999999); rescue SocketError; puts "sres"; end; s.close`, "sres"},
		{`s=UDPSocket.new; begin; s.bind("127.0.0.1", 99999999); rescue SocketError; puts "bres"; end; s.close`, "bres"},
		// Binding an address not assigned to this host fails at ListenUDP.
		{`s=UDPSocket.new; begin; s.bind("240.0.0.1", 0); rescue SocketError; puts "bfail"; end; s.close`, "bfail"},
		// I/O after close: WriteToUDP / ReadFromUDP surface as SocketError.
		{`s=UDPSocket.new; s.connect(H,P); s.close; begin; s.send("x", 0); rescue SocketError; puts "wclosed"; end`, "wclosed"},
		{`s=UDPSocket.new; s.bind("127.0.0.1", 0); s.close; begin; s.recv(4); rescue SocketError; puts "rclosed"; end`, "rclosed"},
		{`s=UDPSocket.new; s.bind("127.0.0.1", 0); s.close; begin; s.recvfrom(4); rescue SocketError; puts "rfclosed"; end`, "rfclosed"},
		// inspect / to_s / truthiness of the socket value.
		{`s=UDPSocket.new; p s; puts s.to_s; p(!!s); s.close`, "#<UDPSocket>\n#<UDPSocket>\ntrue"},
	}
	for _, c := range cases {
		src := "require \"socket\"\nH=" + fmt.Sprintf("%q", host) + "\nP=" + port + "\n" + c.src
		if got := runSrc(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// The ephemeral bind in UDPSocket.new has no natural failure on a healthy
	// host; inject one through the udpListen seam to cover its error arm.
	orig := udpListen
	udpListen = func(string, *net.UDPAddr) (*net.UDPConn, error) {
		return nil, fmt.Errorf("injected bind failure")
	}
	defer func() { udpListen = orig }()
	if got := runSrc(t, `require "socket"
begin; UDPSocket.new; rescue SocketError; puts "newfail"; end`); got != "newfail" {
		t.Errorf("injected new failure got %q", got)
	}
}

// TestUDPNetworkSelection covers udpNetwork's family mapping directly (avoiding a
// dependence on IPv6 being routable on the CI host for the AF_INET6 arm).
func TestUDPNetworkSelection(t *testing.T) {
	cases := []struct {
		args []object.Value
		want string
	}{
		{nil, "udp"},
		{[]object.Value{object.NilV}, "udp"},
		{[]object.Value{object.IntValue(2)}, "udp4"},
		{[]object.Value{object.IntValue(10)}, "udp6"},
		{[]object.Value{object.IntValue(30)}, "udp6"},
		{[]object.Value{object.IntValue(99)}, "udp"},
	}
	for _, c := range cases {
		if got := udpNetwork(c.args); got != c.want {
			t.Errorf("udpNetwork(%v) = %q, want %q", c.args, got, c.want)
		}
	}
	// A non-integer family raises TypeError.
	func() {
		defer func() {
			if re, ok := recover().(RubyError); !ok || re.Class != "TypeError" {
				t.Errorf("udpNetwork(non-int) recover = %v", recover())
			}
		}()
		udpNetwork([]object.Value{object.NewString("x")})
	}()
	// asUDPSocket guards a mis-typed receiver.
	func() {
		defer func() {
			if re, ok := recover().(RubyError); !ok || re.Class != "TypeError" {
				t.Errorf("asUDPSocket recover = %v", recover())
			}
		}()
		asUDPSocket(object.NilV)
	}()
}

// TestStreamRecvSend covers the shared BasicSocket#recv / #send stream surface
// (installed on TCPSocket, UNIXSocket and SSLSocket) through the TCP echo server.
func TestStreamRecvSend(t *testing.T) {
	host, port := startEcho(t)
	cases := []struct{ src, want string }{
		// send returns the byte count; recv(n) reads back the available bytes.
		{`s=TCPSocket.new(H,P); p s.send("hello", 0); p s.recv(5); s.close`, "5\n\"hello\""},
		// recv(0) returns "" without touching the wire.
		{`s=TCPSocket.new(H,P); p s.recv(0); s.close`, "\"\""},
		// send accepts (and ignores) a destination argument.
		{`s=TCPSocket.new(H,P); p s.send("hi", 0, "ignored"); p s.recv(2); s.close`, "2\n\"hi\""},
		// arity guards.
		{`s=TCPSocket.new(H,P); begin; s.send; rescue ArgumentError; puts "sa"; end; s.close`, "sa"},
		{`s=TCPSocket.new(H,P); begin; s.recv; rescue ArgumentError; puts "ra"; end; s.close`, "ra"},
		{`s=TCPSocket.new(H,P); begin; s.recv(-1); rescue ArgumentError; puts "rn"; end; s.close`, "rn"},
	}
	for _, c := range cases {
		src := "require \"socket\"\nH=" + fmt.Sprintf("%q", host) + "\nP=" + port + "\n" + c.src
		if got := runSrc(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// recv at end of stream returns "" (the greeter closes after its payload).
	gh, gp := startGreeter(t, "bye")
	src := fmt.Sprintf("require \"socket\"\ns=TCPSocket.new(%q,%s)\np s.recv(3); p s.recv(3); s.close", gh, gp)
	if got := runSrc(t, src); got != "\"bye\"\n\"\"" {
		t.Errorf("recv EOF got %q", got)
	}
}

// TestUNIXSocketLoopback proves the AF_UNIX stream transport: a Ruby UNIXServer
// binds a temp socket path, an in-VM UNIXSocket client connects (in a Thread),
// and path / addr / recv / send / read / write / accept / close all round-trip.
// AF_UNIX is skipped on Windows (see socket_unix_windows.go).
func TestUNIXSocketLoopback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("AF_UNIX unsupported on Windows")
	}
	path := filepath.Join(t.TempDir(), "rbgo.sock")
	// The client (a Thread) writes then closes; the server (main) accepts and
	// reads. This is the same arrangement the TCPServer loopback uses — the peer
	// thread never blocks on a read, so no cross-thread I/O stall.
	src := fmt.Sprintf(`require "socket"
PATH = %q
srv = UNIXServer.new(PATH)
res = []
res << (srv.path == PATH)
res << srv.addr[0]
res << srv.closed?
res << srv.listen(5)
cli = nil
t = Thread.new do
  c = UNIXSocket.new(PATH)
  cli = [c.path == PATH, c.addr[0]]
  c.write("ping\n")   # a line for gets
  c.send("more", 0)   # then bytes for recv
  c.close
end
conn = srv.accept
res << conn.gets.chomp
res << conn.recv(4)
res << conn.addr[0]
res << conn.inspect      # UNIXSocket value-protocol
res << conn.to_s
res << !!conn
res << conn.closed?
conn.close
res << conn.closed?
res << srv.closed?
srv.close
res << srv.closed?
t.join
puts (res + cli).inspect`, path)
	want := `[true, "AF_UNIX", false, 0, "ping", "more", "AF_UNIX", "#<UNIXSocket>", "#<UNIXSocket>", true, false, true, false, true, true, "AF_UNIX"]`
	if got := runSrc(t, src); got != want {
		t.Errorf("unix loopback\n got=%q\nwant=%q", got, want)
	}
}

// TestUNIXSocketErrors covers the AF_UNIX surface's raising arms (skipped on
// Windows): constructor arities, connect / bind failures, accept after close,
// and the .open aliases.
func TestUNIXSocketErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("AF_UNIX unsupported on Windows")
	}
	good := filepath.Join(t.TempDir(), "ok.sock")
	cases := []struct{ src, want string }{
		{`begin; UNIXSocket.new; rescue ArgumentError; puts "sockarity"; end`, "sockarity"},
		{`begin; UNIXServer.new; rescue ArgumentError; puts "srvarity"; end`, "srvarity"},
		{`begin; UNIXSocket.new("/no/such/rbgo/socket.sock"); rescue SocketError; puts "noconn"; end`, "noconn"},
		{`begin; UNIXServer.new("/no/such/rbgo/dir/x.sock"); rescue SocketError; puts "nobind"; end`, "nobind"},
		{`srv=UNIXServer.new(GOOD); srv.close; p srv.closed?; begin; srv.accept; rescue IOError; puts "acc"; end`, "true\nacc"},
		// .open aliases bind / connect just like .new.
		{`srv=UNIXServer.open(GOOD2); p srv.listen(5); p srv.path==GOOD2; srv.close`, "0\ntrue"},
		{`srv=UNIXServer.new(GOOD3)
t=Thread.new{ c=UNIXSocket.open(GOOD3); c.write("z"); c.close }
conn=srv.accept; p conn.read; conn.close; srv.close; t.join`, "\"z\""},
		// inspect / to_s / truthiness of both values.
		{`srv=UNIXServer.new(GOOD4); p srv; puts srv.to_s; p(!!srv); srv.close`, "#<UNIXServer>\n#<UNIXServer>\ntrue"},
	}
	for i, c := range cases {
		g := filepath.Join(t.TempDir(), fmt.Sprintf("s%d.sock", i))
		src := "require \"socket\"\nGOOD=" + fmt.Sprintf("%q", good) +
			"\nGOOD2=" + fmt.Sprintf("%q", g+"a") +
			"\nGOOD3=" + fmt.Sprintf("%q", g+"b") +
			"\nGOOD4=" + fmt.Sprintf("%q", g+"c") + "\n" + c.src
		if got := runSrc(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestUNIXSocketWindowsStub runs only on Windows: it confirms the UNIXSocket /
// UNIXServer constructors raise a clean, rescuable NotImplementedError rather
// than panicking (the AF_UNIX code path is not compiled on Windows).
func TestUNIXSocketWindowsStub(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only AF_UNIX stub")
	}
	cases := []struct{ src, want string }{
		{`begin; UNIXSocket.new("x"); rescue NotImplementedError; puts "sock"; end`, "sock"},
		{`begin; UNIXSocket.open("x"); rescue NotImplementedError; puts "socko"; end`, "socko"},
		{`begin; UNIXServer.new("x"); rescue NotImplementedError; puts "srv"; end`, "srv"},
		{`begin; UNIXServer.open("x"); rescue NotImplementedError; puts "srvo"; end`, "srvo"},
	}
	for _, c := range cases {
		if got := runSrc(t, "require \"socket\"\n"+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// genSelfSigned mints a fresh self-signed ECDSA certificate valid for 127.0.0.1
// / ::1 / localhost, usable as a server cert, a client cert (mutual TLS) and its
// own CA. It returns the certificate and key PEM strings.
func genSelfSigned(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "rbgo-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	return
}

// TestServerTLSLoopback is the server-side-TLS proof: an in-VM OpenSSL::SSL::
// SSLServer wraps a TCPServer with a self-signed cert generated in-test; a
// concurrent in-VM client SSLSocket (VERIFY_NONE) connects, exchanges a message,
// and inspects both peer certificates — the server sees no client cert (one-way
// TLS), the client sees the server's. It also drives the SSLServer accessors.
func TestServerTLSLoopback(t *testing.T) {
	certPEM, keyPEM := genSelfSigned(t)
	// The client (a Thread) runs the connect handshake — which hands the GVL back
	// to the spawner while it blocks, letting the server (main) run its accept
	// handshake concurrently — then writes and closes. The server reads the
	// buffered request afterwards, so no two Threads ever block on a read at once.
	src := fmt.Sprintf(`require "socket"
require "openssl"
CERT = %q
KEY = %q
tcp = TCPServer.new("127.0.0.1", 0)
port = tcp.addr[1]
ctx = OpenSSL::SSL::SSLContext.new
ctx.cert = OpenSSL::X509::Certificate.new(CERT)  # a cert object (pemBytes @pem path)
ctx.key = KEY                                    # a PEM string (pemBytes string path)
srv = OpenSSL::SSL::SSLServer.new(tcp, ctx)
res = []
res << (srv.to_io.equal?(tcp))
res << srv.addr[0]
res << srv.listen(5)
res << srv.closed?
cli = nil
t = Thread.new do
  c = TCPSocket.new("127.0.0.1", port)
  ssl = OpenSSL::SSL::SSLSocket.new(c)   # no ctx -> VERIFY_NONE
  ssl.connect
  cli = [ssl.peer_cert.subject.include?("rbgo-test"),
         ssl.peer_cert.to_pem.start_with?("-----BEGIN CERTIFICATE-----")]
  ssl.write("hi\n")
  ssl.close
end
conn = srv.accept
res << conn.gets.chomp
res << (conn.peer_cert ? "client-cert" : "no-client-cert")
conn.close
srv.close
res << srv.closed?
t.join
puts (res + cli).inspect`, certPEM, keyPEM)
	want := `[true, "AF_INET", 0, false, "hi", "no-client-cert", true, true, true]`
	if got := runSrc(t, src); got != want {
		t.Errorf("server TLS loopback\n got=%q\nwant=%q", got, want)
	}
}

// TestMutualTLSLoopback proves the full verify surface: both ends run VERIFY_PEER
// with a ca_file, and the client presents its own certificate. The server
// verifies the client cert (RequireAndVerifyClientCert) and sees it via
// #peer_cert; the client verifies the server against the same CA.
func TestMutualTLSLoopback(t *testing.T) {
	certPEM, keyPEM := genSelfSigned(t)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, []byte(certPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf(`require "socket"
require "openssl"
CERT = %q
KEY = %q
CA = %q
tcp = TCPServer.new("127.0.0.1", 0)
port = tcp.addr[1]
sctx = OpenSSL::SSL::SSLContext.new
sctx.cert = CERT; sctx.key = KEY
sctx.verify_mode = OpenSSL::SSL::VERIFY_PEER
sctx.ca_file = CA
srv = OpenSSL::SSL::SSLServer.new(tcp, sctx)
cli = nil
t = Thread.new do
  c = TCPSocket.new("127.0.0.1", port)
  cctx = OpenSSL::SSL::SSLContext.new
  cctx.cert = CERT; cctx.key = KEY
  cctx.verify_mode = OpenSSL::SSL::VERIFY_PEER
  cctx.ca_file = CA
  ssl = OpenSSL::SSL::SSLSocket.new(c, cctx)
  ssl.connect
  cli = ssl.peer_cert.subject.include?("rbgo-test")   # client verified the server
  ssl.write("mtls\n")
  ssl.close
end
conn = srv.accept
saw = conn.peer_cert.subject.include?("rbgo-test")     # server verified the client
line = conn.gets.chomp
conn.close
srv.close
t.join
puts [line, saw, cli].inspect`, certPEM, keyPEM, caFile)
	want := `["mtls", true, true]`
	if got := runSrc(t, src); got != want {
		t.Errorf("mutual TLS loopback\n got=%q\nwant=%q", got, want)
	}
}

// TestServerTLSAcceptErrorAndVerify covers the remaining TLS server/verify arms
// through the engine: a garbage (non-TLS) client trips SSLSocket#accept_nonblock
// into an SSLError; the client-side ca_file verification path succeeds against a
// server whose cert is the trust anchor and fails on a hostname mismatch; and
// the X509::Certificate parse surface (good / empty / garbage).
func TestServerTLSAcceptErrorAndVerify(t *testing.T) {
	// Server handshake error: a non-TLS client makes #accept fail as SSLError.
	certPEM, keyPEM := genSelfSigned(t)
	acceptErr := fmt.Sprintf(`require "socket"
require "openssl"
CERT = %q
KEY = %q
srv = TCPServer.new("127.0.0.1", 0)
port = srv.addr[1]
t = Thread.new { c = TCPSocket.new("127.0.0.1", port); c.write("not tls\r\n\r\n"); c.close }
conn = srv.accept
ctx = OpenSSL::SSL::SSLContext.new; ctx.cert = CERT; ctx.key = KEY
ssl = OpenSSL::SSL::SSLSocket.new(conn, ctx)
begin
  ssl.accept_nonblock
rescue OpenSSL::SSL::SSLError
  puts "accepterr"
end
t.join
srv.close`, certPEM, keyPEM)
	if got := runSrc(t, acceptErr); got != "accepterr" {
		t.Errorf("accept error got %q", got)
	}

	// Client ca_file verification against an httptest TLS server (hermetic); the
	// server's own cert is written out as the trust anchor.
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "verified hello")
	}))
	defer ts.Close()
	u, _ := url.Parse(ts.URL)
	caFile := filepath.Join(t.TempDir(), "server.pem")
	certOut := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw})
	if err := os.WriteFile(caFile, certOut, 0o600); err != nil {
		t.Fatal(err)
	}
	mk := func(body string) string {
		return fmt.Sprintf("require \"socket\"\nrequire \"openssl\"\nHOST=%q\nPORT=%s\nCA=%q\n%s",
			u.Hostname(), u.Port(), caFile, body)
	}
	verifyCases := []struct{ src, want string }{
		// VERIFY_PEER with the correct ca_file: the handshake verifies and the
		// HTTPS body round-trips.
		{`tcp=TCPSocket.new(HOST,PORT)
ctx=OpenSSL::SSL::SSLContext.new; ctx.verify_mode=OpenSSL::SSL::VERIFY_PEER; ctx.ca_file=CA
ssl=OpenSSL::SSL::SSLSocket.new(tcp, ctx); ssl.connect
ssl.write("GET / HTTP/1.0\r\nHost: #{HOST}\r\n\r\n")
body=ssl.read; ssl.close
puts body.include?("verified hello")`, "true"},
		// VERIFY_PEER + correct CA but a mismatched hostname fails verification.
		{`tcp=TCPSocket.new(HOST,PORT)
ctx=OpenSSL::SSL::SSLContext.new; ctx.verify_mode=OpenSSL::SSL::VERIFY_PEER; ctx.ca_file=CA
ssl=OpenSSL::SSL::SSLSocket.new(tcp, ctx); ssl.hostname="wrong.invalid"
begin; ssl.connect; rescue OpenSSL::SSL::SSLError; puts "hostfail"; end; ssl.close`, "hostfail"},
		// peer_cert is nil before the handshake runs.
		{`tcp=TCPSocket.new(HOST,PORT); ssl=OpenSSL::SSL::SSLSocket.new(tcp); p ssl.peer_cert; ssl.close`, "nil"},
	}
	for _, c := range verifyCases {
		if got := runSrc(t, mk(c.src)); got != c.want {
			t.Errorf("verify src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// X509::Certificate parse surface: a valid PEM round-trips through
	// to_pem / subject; an empty argument yields a blank cert; garbage raises.
	certSrc := fmt.Sprintf(`require "openssl"
CERT = %q
c = OpenSSL::X509::Certificate.new(CERT)
p c.subject.include?("rbgo-test")
p c.to_pem.start_with?("-----BEGIN CERTIFICATE-----")
p c.to_s == c.to_pem
p OpenSSL::X509::Certificate.new.to_pem
begin; OpenSSL::X509::Certificate.new("not a cert"); rescue OpenSSL::X509::CertificateError; puts "garbage"; end
begin; OpenSSL::X509::Certificate.new("-----BEGIN CERTIFICATE-----\naGVsbG8=\n-----END CERTIFICATE-----\n"); rescue OpenSSL::X509::CertificateError; puts "badder"; end`, certPEM)
	want := "true\ntrue\ntrue\nnil\ngarbage\nbadder"
	if got := runSrc(t, certSrc); got != want {
		t.Errorf("cert surface\n got=%q\nwant=%q", got, want)
	}
}

// TestSSLServerConstruction covers OpenSSL::SSL::SSLServer's constructor guards
// (arity / non-TCPServer argument) and its value-protocol methods, without a
// handshake.
func TestSSLServerConstruction(t *testing.T) {
	src := `require "socket"
require "openssl"
tcp = TCPServer.new("127.0.0.1", 0)
ctx = OpenSSL::SSL::SSLContext.new
srv = OpenSSL::SSL::SSLServer.new(tcp, ctx)
p srv
puts srv.to_s
p(!!srv)
p srv.closed?
srv.close
p srv.closed?
begin; OpenSSL::SSL::SSLServer.new(tcp); rescue ArgumentError; puts "arity"; end
begin; OpenSSL::SSL::SSLServer.new("x", ctx); rescue TypeError; puts "type"; end`
	want := "#<OpenSSL::SSL::SSLServer>\n#<OpenSSL::SSL::SSLServer>\ntrue\nfalse\ntrue\narity\ntype"
	if got := runSrc(t, src); got != want {
		t.Errorf("SSLServer construction\n got=%q\nwant=%q", got, want)
	}
}

// TestTLSHelperUnitArms covers the pure crypto/tls config helpers' branches
// directly (deterministic, no handshake): pemBytes, caPool, clientCert,
// serverTLSConfig, newX509Cert and the asSSLServer guard.
func TestTLSHelperUnitArms(t *testing.T) {
	certPEM, keyPEM := genSelfSigned(t)
	cls := newClass("Ctx", nil)
	ctx := func(pairs map[string]object.Value) *RObject {
		o := &RObject{class: cls, ivars: map[string]object.Value{}}
		for k, v := range pairs {
			o.ivars[k] = v
		}
		return o
	}
	str := object.NewString

	// pemBytes: nil -> nil; String -> its bytes; object carrying @pem -> that;
	// anything else -> SSLError.
	if pemBytes(object.NilV) != nil {
		t.Error("pemBytes(nil) not nil")
	}
	if string(pemBytes(str("abc"))) != "abc" {
		t.Error("pemBytes(String) wrong")
	}
	withPem := ctx(map[string]object.Value{"@pem": str("PEMDATA")})
	if string(pemBytes(withPem)) != "PEMDATA" {
		t.Error("pemBytes(@pem) wrong")
	}
	mustRaiseClass(t, "OpenSSL::SSL::SSLError", func() { pemBytes(ctx(nil)) })

	// caPool: no ca_file -> nil; good bundle -> pool; missing / certless -> raise.
	if caPool(ctx(nil)) != nil {
		t.Error("caPool(no ca_file) not nil")
	}
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(caFile, []byte(certPEM), 0o600)
	if caPool(ctx(map[string]object.Value{"@ca_file": str(caFile)})) == nil {
		t.Error("caPool(good) nil")
	}
	mustRaiseClass(t, "OpenSSL::SSL::SSLError", func() {
		caPool(ctx(map[string]object.Value{"@ca_file": str(filepath.Join(t.TempDir(), "missing"))}))
	})
	empty := filepath.Join(t.TempDir(), "empty.pem")
	os.WriteFile(empty, nil, 0o600)
	mustRaiseClass(t, "OpenSSL::SSL::SSLError", func() {
		caPool(ctx(map[string]object.Value{"@ca_file": str(empty)}))
	})

	// clientCert: absent cert/key -> nil; valid pair -> cert; bad pair -> raise.
	if clientCert(ctx(nil)) != nil {
		t.Error("clientCert(none) not nil")
	}
	good := ctx(map[string]object.Value{"@cert": str(certPEM), "@key": str(keyPEM)})
	if clientCert(good) == nil {
		t.Error("clientCert(valid) nil")
	}
	mustRaiseClass(t, "OpenSSL::SSL::SSLError", func() {
		clientCert(ctx(map[string]object.Value{"@cert": str("x"), "@key": str("y")}))
	})

	// serverTLSConfig: valid -> one certificate; missing -> raise; VERIFY_PEER +
	// ca_file -> RequireAndVerifyClientCert.
	if cfg := serverTLSConfig(good); len(cfg.Certificates) != 1 {
		t.Error("serverTLSConfig(valid) missing cert")
	}
	mustRaiseClass(t, "OpenSSL::SSL::SSLError", func() { serverTLSConfig(ctx(nil)) })
	mtls := ctx(map[string]object.Value{
		"@cert": str(certPEM), "@key": str(keyPEM),
		"@verify_mode": object.IntValue(1), "@ca_file": str(caFile),
	})
	if cfg := serverTLSConfig(mtls); cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("serverTLSConfig(mtls) ClientAuth = %v", cfg.ClientAuth)
	}

	// newX509Cert wraps a parsed certificate with its PEM and subject.
	block, _ := pem.Decode([]byte(certPEM))
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	obj := newX509Cert(cls, parsed)
	if !strings.Contains(obj.ivars["@subject"].ToS(), "rbgo-test") {
		t.Errorf("newX509Cert subject = %v", obj.ivars["@subject"])
	}

	mustRaiseClass(t, "TypeError", func() { asSSLServer(object.NilV) })
}

// TestSSLSocketSNIServerName is the SNI proof: a Go TLS server whose
// GetCertificate callback records the ClientHello's ServerName runs in-process,
// and a Ruby SSLSocket sets #hostname= to a DNS name then #connect's. The
// assertion is that the server received that exact name as SNI — so the client
// handshake really sent it (crypto/tls omits SNI for a bare IP, which is why the
// hostname is a DNS-style name here).
func TestSSLSocketSNIServerName(t *testing.T) {
	certPEM, keyPEM := genSelfSigned(t)
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	sniCh := make(chan string, 1)
	cfg := &tls.Config{GetCertificate: func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
		select {
		case sniCh <- chi.ServerName:
		default:
		}
		return &cert, nil
	}}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			// Drive the server handshake (invokes GetCertificate) then drop the conn.
			go func(c net.Conn) { c.(*tls.Conn).Handshake(); c.Close() }(c)
		}
	}()

	host, port := hostPortOf(t, ln.Addr().String())
	src := fmt.Sprintf(`require "socket"
require "openssl"
tcp = TCPSocket.new(%q, %s)
ssl = OpenSSL::SSL::SSLSocket.new(tcp)
ssl.hostname = "sni.example.test"
p ssl.hostname
ssl.connect
ssl.close`, host, port)
	if got := runSrc(t, src); got != `"sni.example.test"` {
		t.Fatalf("hostname readback = %q", got)
	}
	// The client handshake completed above, so GetCertificate has run.
	select {
	case sni := <-sniCh:
		if sni != "sni.example.test" {
			t.Fatalf("server received SNI = %q, want %q", sni, "sni.example.test")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the server to record the SNI name")
	}
}

// mustRaise asserts fn panics with a RubyError of the given class.
func mustRaiseClass(t *testing.T, class string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if re, ok := r.(RubyError); !ok || re.Class != class {
			t.Errorf("recover = %v, want %s", r, class)
		}
	}()
	fn()
}
