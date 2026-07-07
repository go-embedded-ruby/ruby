// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The raw Socket surface (Socket.new + bind/listen/accept/connect/send/recv/
// recvfrom/getsockname/getpeername/setsockopt/getsockopt) is proven end-to-end
// through the rbgo engine against in-process loopback peers — a raw TCP server
// and its raw client, a raw UDP send/recvfrom exchange, and (non-Windows) AF_UNIX
// stream and datagram exchanges — so every assertion exercises the actual Go net
// path a Ruby script drives, hermetic and free of external network.

// TestRawSocketTCPLoopback is the headline raw-stream proof: a raw Socket server
// (new + bind + listen + accept) exchanges with a concurrent in-VM raw Socket
// client (new + connect + send), and getsockname / getpeername / the accepted
// peer Addrinfo all round-trip.
func TestRawSocketTCPLoopback(t *testing.T) {
	src := `require "socket"
srv = Socket.new(Socket::AF_INET, Socket::SOCK_STREAM)
srv.bind(Socket.pack_sockaddr_in(0, "127.0.0.1"))
srv.listen(5)
port, host = Socket.unpack_sockaddr_in(srv.getsockname)
res = []
res << (host == "127.0.0.1")
cli = nil
t = Thread.new do
  c = Socket.new(Socket::AF_INET, Socket::SOCK_STREAM)
  c.connect(Socket.pack_sockaddr_in(port, "127.0.0.1"))
  pport, phost = Socket.unpack_sockaddr_in(c.getpeername)
  _, lhost = Socket.unpack_sockaddr_in(c.getsockname)  # connected socket's local addr
  cli = [pport == port, phost, lhost]
  c.send("ping", 0)
  c.close
end
conn, addr = srv.accept
res << conn.recv(4)
res << addr.ip_address
res << addr.afamily
res << conn.closed?
conn.close
res << conn.closed?
srv.close
t.join
puts (res + cli).inspect`
	want := `[true, "ping", "127.0.0.1", 2, false, true, true, "127.0.0.1", "127.0.0.1"]`
	if got := runSrc(t, src); got != want {
		t.Errorf("raw TCP loopback\n got=%q\nwant=%q", got, want)
	}
}

// TestRawSocketUDPLoopback is the raw-datagram proof: an unbound raw Socket
// client #send's (with an explicit destination sockaddr, binding an ephemeral
// send socket on demand) to a bound raw Socket server, which #recvfrom's the
// payload and sender Addrinfo then replies, which the client #recv's back.
func TestRawSocketUDPLoopback(t *testing.T) {
	src := `require "socket"
srv = Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM)
srv.bind(Socket.pack_sockaddr_in(0, "127.0.0.1"))
port, _ = Socket.unpack_sockaddr_in(srv.getsockname)
cli = Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM)
n = cli.send("hello", 0, Socket.pack_sockaddr_in(port, "127.0.0.1"))
msg, from = srv.recvfrom(100)
srv.send("ack", 0, Socket.pack_sockaddr_in(from.ip_port, "127.0.0.1"))
reply = cli.recv(3)
cli.close
srv.close
puts [n, msg, from.ip_address, from.afamily, reply].inspect`
	want := `[5, "hello", "127.0.0.1", 2, "ack"]`
	if got := runSrc(t, src); got != want {
		t.Errorf("raw UDP loopback\n got=%q\nwant=%q", got, want)
	}
}

// TestRawSocketDgramConnected covers the connected-datagram arms: #connect on a
// datagram socket (net.Dial), a destination-less #send over the connected peer,
// #getpeername, and #recvfrom reporting the connected peer as the sender.
func TestRawSocketDgramConnected(t *testing.T) {
	src := `require "socket"
srv = Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM)
srv.bind(Socket.pack_sockaddr_in(0, "127.0.0.1"))
port, _ = Socket.unpack_sockaddr_in(srv.getsockname)
cli = Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM)
cli.connect(Socket.pack_sockaddr_in(port, "127.0.0.1"))
pport, _ = Socket.unpack_sockaddr_in(cli.getpeername)
cli.send("hey", 0)
msg, from = srv.recvfrom(100)
srv.send("yo", 0, Socket.pack_sockaddr_in(from.ip_port, "127.0.0.1"))
reply, peer = cli.recvfrom(2)
cli.close
srv.close
puts [pport == port, msg, reply, peer.ip_port == port].inspect`
	want := `[true, "hey", "yo", true]`
	if got := runSrc(t, src); got != want {
		t.Errorf("raw connected datagram\n got=%q\nwant=%q", got, want)
	}
}

// TestRawSocketUnixLoopback proves the raw AF_UNIX stream surface (skipped on
// Windows): a raw Socket UNIX server binds a temp path, a concurrent in-VM raw
// client connects and sends, and getsockname / accept-peer-Addrinfo round-trip.
func TestRawSocketUnixLoopback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("AF_UNIX unsupported on Windows")
	}
	path := filepath.Join(t.TempDir(), "raw.sock")
	src := fmt.Sprintf(`require "socket"
PATH = %q
srv = Socket.new(Socket::AF_UNIX, Socket::SOCK_STREAM)
srv.bind(Socket.pack_sockaddr_un(PATH))
srv.listen(1)
res = []
res << Socket.unpack_sockaddr_un(srv.getsockname)
t = Thread.new do
  c = Socket.new(Socket::AF_UNIX, Socket::SOCK_STREAM)
  c.connect(Socket.pack_sockaddr_un(PATH))
  c.send("unix-hi", 0)
  c.close
end
conn, addr = srv.accept
res << conn.recv(7)
res << addr.afamily
conn.close
srv.close
t.join
puts res.inspect`, path)
	want := fmt.Sprintf(`[%q, "unix-hi", 1]`, path)
	if got := runSrc(t, src); got != want {
		t.Errorf("raw UNIX loopback\n got=%q\nwant=%q", got, want)
	}
	// An AF_UNIX socket with an unmodelled socket type is rejected (the AF_UNIX
	// default arm of rawSocketNetwork).
	if got := runSrc(t, `require "socket"
begin; Socket.new(Socket::AF_UNIX, 99); rescue SocketError; puts "badunix"; end`); got != "badunix" {
		t.Errorf("AF_UNIX bad type got %q", got)
	}
}

// TestRawSocketUnixDgramLoopback proves the raw AF_UNIX datagram surface (skipped
// on Windows): two bound unixgram raw Sockets exchange a datagram, covering the
// unixgram bind, the AF_UNIX send-destination resolution, and recvfrom.
func TestRawSocketUnixDgramLoopback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("AF_UNIX unsupported on Windows")
	}
	dir := t.TempDir()
	spath := filepath.Join(dir, "s.sock")
	cpath := filepath.Join(dir, "c.sock")
	src := fmt.Sprintf(`require "socket"
SPATH = %q
CPATH = %q
srv = Socket.new(Socket::AF_UNIX, Socket::SOCK_DGRAM)
srv.bind(Socket.pack_sockaddr_un(SPATH))
cli = Socket.new(Socket::AF_UNIX, Socket::SOCK_DGRAM)
cli.bind(Socket.pack_sockaddr_un(CPATH))
cli.send("dgram-hi", 0, Socket.pack_sockaddr_un(SPATH))
msg, from = srv.recvfrom(100)
cli.close
srv.close
puts [msg, from.afamily].inspect`, spath, cpath)
	want := `["dgram-hi", 1]`
	if got := runSrc(t, src); got != want {
		t.Errorf("raw UNIX dgram loopback\n got=%q\nwant=%q", got, want)
	}
}

// TestRawSocketOptsAndValue covers setsockopt / getsockopt round-tripping and the
// socket value protocol (inspect / to_s / truthiness).
func TestRawSocketOptsAndValue(t *testing.T) {
	cases := []struct{ src, want string }{
		// setsockopt returns 0; getsockopt reads the stored value back; an unset
		// option reads back Integer 0.
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM)
p s.setsockopt(Socket::SOL_SOCKET, Socket::SO_REUSEADDR, 1)
p s.getsockopt(Socket::SOL_SOCKET, Socket::SO_REUSEADDR)
p s.getsockopt(Socket::SOL_SOCKET, 4242)
s.close`, "0\n1\n0"},
		// inspect / to_s / truthiness.
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM); p s; puts s.to_s; p(!!s); s.close`,
			"#<Socket>\n#<Socket>\ntrue"},
		// A protocol argument is accepted (3-arg form).
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM, Socket::IPPROTO_TCP); p s.closed?; s.close; p s.closed?`,
			"false\ntrue"},
		// Symbol / String domain and type names are accepted.
		{`s=Socket.new(:INET, :STREAM); p s.closed?; s.close`, "false"},
	}
	for _, c := range cases {
		if got := runSrc(t, "require \"socket\"\n"+c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRawSocketErrors covers the raw Socket surface's raising arms through the
// engine: constructor arity / unsupported combinations, per-method arities,
// bind / connect / accept failures, send / recv on a socket in the wrong state,
// and I/O after close.
func TestRawSocketErrors(t *testing.T) {
	// A listener opened then closed yields a port that refuses connections.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	_, refusedPort := hostPortOf(t, ln.Addr().String())
	ln.Close()

	cases := []struct{ src, want string }{
		// Constructor arity and unsupported domain / type.
		{`begin; Socket.new(Socket::AF_INET); rescue ArgumentError; puts "arity"; end`, "arity"},
		{`begin; Socket.new(99, Socket::SOCK_STREAM); rescue SocketError; puts "baddom"; end`, "baddom"},
		{`begin; Socket.new(Socket::AF_INET, 3); rescue SocketError; puts "badtype"; end`, "badtype"},
		// bind arity + failures (stream and datagram) binding an unassigned address.
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); begin; s.bind; rescue ArgumentError; puts "ba"; end`, "ba"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM)
begin; s.bind(Socket.pack_sockaddr_in(0, "240.0.0.1")); rescue SocketError; puts "sbind"; end`, "sbind"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM)
begin; s.bind(Socket.pack_sockaddr_in(0, "240.0.0.1")); rescue SocketError; puts "dbind"; end`, "dbind"},
		// accept before bind, and after close.
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); begin; s.accept; rescue IOError; puts "nolisten"; end`, "nolisten"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); s.bind(Socket.pack_sockaddr_in(0,"127.0.0.1"))
s.close; begin; s.accept; rescue IOError; puts "accclosed"; end`, "accclosed"},
		// connect arity + failure to a refused port.
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); begin; s.connect; rescue ArgumentError; puts "ca"; end`, "ca"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM)
begin; s.connect(Socket.pack_sockaddr_in(RP, "127.0.0.1")); rescue SocketError; puts "crefused"; end`, "crefused"},
		// send arity, not-connected stream, datagram missing / nil destination.
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); begin; s.send("x"); rescue ArgumentError; puts "sa"; end`, "sa"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); begin; s.send("x", 0); rescue SocketError; puts "notconn"; end`, "notconn"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM); begin; s.send("x", 0); rescue SocketError; puts "nodest"; end`, "nodest"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM); begin; s.send("x", 0, nil); rescue SocketError; puts "nildest"; end`, "nildest"},
		// recv arity, negative length, and a socket that is neither connected nor bound.
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); begin; s.recv; rescue ArgumentError; puts "ra"; end`, "ra"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM); s.bind(Socket.pack_sockaddr_in(0,"127.0.0.1"))
begin; s.recv(-1); rescue ArgumentError; puts "rneg"; end; s.close`, "rneg"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); begin; s.recv(4); rescue SocketError; puts "runbound"; end`, "runbound"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); begin; s.recvfrom(4); rescue SocketError; puts "rfunbound"; end`, "rfunbound"},
		// I/O after close on a bound datagram socket (recv / recvfrom / send-with-dest).
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM); s.bind(Socket.pack_sockaddr_in(0,"127.0.0.1")); s.close
begin; s.recv(4); rescue SocketError; puts "rclosed"; end`, "rclosed"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM); s.bind(Socket.pack_sockaddr_in(0,"127.0.0.1")); s.close
begin; s.recvfrom(4); rescue SocketError; puts "rfclosed"; end`, "rfclosed"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM); s.bind(Socket.pack_sockaddr_in(0,"127.0.0.1")); s.close
begin; s.send("x", 0, Socket.pack_sockaddr_in(9, "127.0.0.1")); rescue SocketError; puts "sclosed"; end`, "sclosed"},
		// getsockname on an unbound socket; getpeername on an unconnected socket.
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); begin; s.getsockname; rescue IOError; puts "gsn"; end`, "gsn"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); begin; s.getpeername; rescue IOError; puts "gpn"; end`, "gpn"},
		// setsockopt / getsockopt arities.
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); begin; s.setsockopt(1,2); rescue ArgumentError; puts "ssa"; end`, "ssa"},
		{`s=Socket.new(Socket::AF_INET, Socket::SOCK_STREAM); begin; s.getsockopt(1); rescue ArgumentError; puts "gsa"; end`, "gsa"},
	}
	for _, c := range cases {
		src := "require \"socket\"\nRP=" + refusedPort + "\n" + c.src
		if got := runSrc(t, src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// The connected-write error arm: a connected raw socket that is closed then
	// #send'd surfaces the write failure as SocketError (a real echo peer supplies
	// the connection).
	host, port := startEcho(t)
	writeClosed := fmt.Sprintf(`require "socket"
s = Socket.new(Socket::AF_INET, Socket::SOCK_STREAM)
s.connect(Socket.pack_sockaddr_in(%s, %q))
s.close
begin; s.send("x", 0); rescue SocketError; puts "wclosed"; end`, port, host)
	if got := runSrc(t, writeClosed); got != "wclosed" {
		t.Errorf("connected write-after-close got %q", got)
	}

	// The ephemeral send-socket bind has no natural failure on a healthy host;
	// inject one through the rawListenPacket seam to cover its error arm.
	orig := rawListenPacket
	rawListenPacket = func(string, string) (net.PacketConn, error) {
		return nil, fmt.Errorf("injected listen-packet failure")
	}
	defer func() { rawListenPacket = orig }()
	if got := runSrc(t, `require "socket"
s = Socket.new(Socket::AF_INET, Socket::SOCK_DGRAM)
begin; s.send("x", 0, Socket.pack_sockaddr_in(9, "127.0.0.1")); rescue SocketError; puts "epfail"; end`); got != "epfail" {
		t.Errorf("injected ensurePconn failure got %q", got)
	}
}

// TestRawSocketUnixWindowsStub runs only on Windows: it confirms a raw AF_UNIX
// Socket.new raises a clean, rescuable NotImplementedError (the AF_UNIX raw path
// is not supported there, matching the UNIXSocket transport stub).
func TestRawSocketUnixWindowsStub(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only AF_UNIX raw stub")
	}
	cases := []struct{ src, want string }{
		{`begin; Socket.new(Socket::AF_UNIX, Socket::SOCK_STREAM); rescue NotImplementedError; puts "stream"; end`, "stream"},
		{`begin; Socket.new(Socket::AF_UNIX, Socket::SOCK_DGRAM); rescue NotImplementedError; puts "dgram"; end`, "dgram"},
	}
	for _, c := range cases {
		if got := runSrc(t, "require \"socket\"\n"+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRawNetworkAndGuard covers rawINETNetwork's family/type mapping directly (so
// the IPv6 and unsupported arms are exercised without needing routable IPv6) and
// the asRawSocket type-narrowing guard. The AF_UNIX arms of the platform-split
// rawSocketNetwork are covered by the raw AF_UNIX loopback tests (non-Windows)
// and the Windows stub test.
func TestRawNetworkAndGuard(t *testing.T) {
	cases := []struct {
		domain, typ int
		want        string
		ok          bool
	}{
		{afINET, sockStream, "tcp4", true},
		{afINET, sockDgram, "udp4", true},
		{afINET6, sockStream, "tcp6", true},
		{afINET6, sockDgram, "udp6", true},
		{afINET, 99, "", false},
		{afINET6, 99, "", false},
		{afUNIX, sockStream, "", false},
		{12345, sockStream, "", false},
	}
	for _, c := range cases {
		got, ok := rawINETNetwork(c.domain, c.typ)
		if got != c.want || ok != c.ok {
			t.Errorf("rawINETNetwork(%d,%d) = (%q,%v), want (%q,%v)", c.domain, c.typ, got, ok, c.want, c.ok)
		}
	}
	func() {
		defer func() {
			if re, ok := recover().(RubyError); !ok || re.Class != "TypeError" {
				t.Errorf("asRawSocket recover = %v", recover())
			}
		}()
		asRawSocket(object.NilV)
	}()
}
