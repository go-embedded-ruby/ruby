// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	binpkg "encoding/binary"
	"fmt"
	"net"
	"runtime"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// The address utilities (Socket.getaddrinfo, the Addrinfo value class, and the
// sockaddr pack/unpack helpers) are proven through the rbgo engine against
// hermetic, loopback-resolvable inputs (127.0.0.1 / ::1 / localhost). The name
// resolver and the service-name port lookup are reached only for non-numeric
// inputs; their success and failure arms are driven deterministically through
// the resolveIPs / lookupPort seams so the suite never depends on live DNS.

// TestGetaddrinfoLoopback covers Socket.getaddrinfo's tuple shape and its
// family / socktype / protocol arguments against loopback inputs.
func TestGetaddrinfoLoopback(t *testing.T) {
	cases := []struct{ src, want string }{
		// Numeric IPv4 + explicit family/socktype: one AF_INET STREAM/TCP tuple.
		{`p Socket.getaddrinfo("127.0.0.1", 80, Socket::AF_INET, Socket::SOCK_STREAM)`,
			`[["AF_INET", 80, "127.0.0.1", "127.0.0.1", 2, 1, 6]]`},
		// SOCK_DGRAM defaults the protocol to IPPROTO_UDP.
		{`p Socket.getaddrinfo("127.0.0.1", 53, Socket::AF_INET, Socket::SOCK_DGRAM)`,
			`[["AF_INET", 53, "127.0.0.1", "127.0.0.1", 2, 2, 17]]`},
		// No socktype -> socktype 0, protocol 0.
		{`p Socket.getaddrinfo("127.0.0.1", 80, Socket::AF_INET)`,
			`[["AF_INET", 80, "127.0.0.1", "127.0.0.1", 2, 0, 0]]`},
		// An explicit protocol argument overrides the socktype-derived default.
		{`p Socket.getaddrinfo("127.0.0.1", 80, Socket::AF_INET, Socket::SOCK_STREAM, Socket::IPPROTO_TCP)`,
			`[["AF_INET", 80, "127.0.0.1", "127.0.0.1", 2, 1, 6]]`},
		// The AF_INET filter over the localhost lookup yields just the v4 loopback.
		{`p Socket.getaddrinfo("localhost", 80, Socket::AF_INET, Socket::SOCK_STREAM)`,
			`[["AF_INET", 80, "127.0.0.1", "127.0.0.1", 2, 1, 6]]`},
		// A numeric IPv6 literal with the AF_INET6 filter.
		{`p Socket.getaddrinfo("::1", 443, Socket::AF_INET6, Socket::SOCK_STREAM)`,
			`[["AF_INET6", 443, "::1", "::1", 30, 1, 6]]`},
		// A String port that names a numeric value is parsed.
		{`p Socket.getaddrinfo("127.0.0.1", "8080", Socket::AF_INET, Socket::SOCK_STREAM)`,
			`[["AF_INET", 8080, "127.0.0.1", "127.0.0.1", 2, 1, 6]]`},
		// A nil port is port 0.
		{`p Socket.getaddrinfo("127.0.0.1", nil, Socket::AF_INET)`,
			`[["AF_INET", 0, "127.0.0.1", "127.0.0.1", 2, 0, 0]]`},
	}
	for _, c := range cases {
		if got := runSrc(t, "require \"socket\"\n"+c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestAddrinfoSurface covers the Addrinfo constructors and instance readers
// through the engine against loopback inputs.
func TestAddrinfoSurface(t *testing.T) {
	cases := []struct{ src, want string }{
		// tcp: the full reader surface.
		{`a=Addrinfo.tcp("127.0.0.1", 8080)
p [a.ip_address, a.ip_port, a.afamily, a.pfamily, a.socktype, a.protocol, a.ipv4?, a.ipv6?]`,
			`["127.0.0.1", 8080, 2, 2, 1, 6, true, false]`},
		// tcp / udp / ip inspect forms (v4).
		{`p Addrinfo.tcp("127.0.0.1", 8080).inspect`, `"#<Addrinfo: 127.0.0.1:8080 TCP>"`},
		{`p Addrinfo.udp("127.0.0.1", 53).inspect`, `"#<Addrinfo: 127.0.0.1:53 UDP>"`},
		{`p Addrinfo.ip("127.0.0.1").inspect`, `"#<Addrinfo: 127.0.0.1>"`},
		// A bare-IP Addrinfo has port 0 and socktype/protocol 0.
		{`a=Addrinfo.ip("127.0.0.1"); p [a.ip_port, a.socktype, a.protocol]`, `[0, 0, 0]`},
		// v6 inspect brackets the host, and ipv6? flips.
		{`a=Addrinfo.tcp("::1", 443); p a.inspect; p [a.ipv4?, a.ipv6?, a.afamily]`,
			"\"#<Addrinfo: [::1]:443 TCP>\"\n[false, true, 30]"},
		// to_sockaddr / to_s return the same packed bytes; a round-trip recovers it.
		{`a=Addrinfo.tcp("1.2.3.4", 443); p(a.to_sockaddr == a.to_s)
p Socket.unpack_sockaddr_in(a.to_sockaddr)`, "true\n[443, \"1.2.3.4\"]"},
		// getaddrinfo yields Addrinfo objects.
		{`p Addrinfo.getaddrinfo("127.0.0.1", 80, Socket::AF_INET, Socket::SOCK_STREAM).map(&:inspect)`,
			`["#<Addrinfo: 127.0.0.1:80 TCP>"]`},
		// getaddrinfo accepts Symbol family / socktype.
		{`p Addrinfo.getaddrinfo("127.0.0.1", 80, :INET, :STREAM).map(&:inspect)`,
			`["#<Addrinfo: 127.0.0.1:80 TCP>"]`},
		// new(packed sockaddr) reconstructs the address.
		{`b=Addrinfo.new(Socket.pack_sockaddr_in(80, "127.0.0.1"))
p [b.ip_address, b.ip_port, b.afamily]`, `["127.0.0.1", 80, 2]`},
		// new(packed v6 sockaddr) sets AF_INET6.
		{`b=Addrinfo.new(Socket.pack_sockaddr_in(443, "::1")); p [b.ip_address, b.ip_port, b.afamily]`,
			`["::1", 443, 30]`},
		// new([afamily, port, host, addr]) accepts the array address form.
		{`b=Addrinfo.new(["AF_INET", 25, "127.0.0.1", "127.0.0.1"])
p [b.ip_address, b.ip_port, b.afamily]`, `["127.0.0.1", 25, 2]`},
		// new(sockaddr, family, socktype, protocol) overrides the derived fields.
		{`b=Addrinfo.new(Socket.pack_sockaddr_in(80, "127.0.0.1"), Socket::AF_INET, Socket::SOCK_STREAM, Socket::IPPROTO_TCP)
p [b.socktype, b.protocol]; p b.inspect`, "[1, 6]\n\"#<Addrinfo: 127.0.0.1:80 TCP>\""},
		// value protocol: inspect / to truthiness.
		{`a=Addrinfo.tcp("127.0.0.1", 1); p a; p(!!a)`, "#<Addrinfo: 127.0.0.1:1 TCP>\ntrue"},
	}
	for _, c := range cases {
		if got := runSrc(t, "require \"socket\"\n"+c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSockaddrPackUnpack is the byte-exact round-trip proof for the sockaddr
// helpers, asserting the concrete IPv4 / IPv6 layout as well as the round-trip.
func TestSockaddrPackUnpack(t *testing.T) {
	cases := []struct{ src, want string }{
		// The headline round-trip.
		{`p(Socket.unpack_sockaddr_in(Socket.pack_sockaddr_in(443, "1.2.3.4")) == [443, "1.2.3.4"])`, "true"},
		{`p Socket.unpack_sockaddr_in(Socket.pack_sockaddr_in(443, "1.2.3.4"))`, `[443, "1.2.3.4"]`},
		// sockaddr_in is the alias for pack_sockaddr_in.
		{`p(Socket.sockaddr_in(80, "127.0.0.1") == Socket.pack_sockaddr_in(80, "127.0.0.1"))`, "true"},
		// The IPv4 packed form is 16 bytes; port is network order (443 -> 1,187),
		// address is the natural dotted order.
		{`p Socket.pack_sockaddr_in(443, "1.2.3.4").bytes[2..7]`, `[1, 187, 1, 2, 3, 4]`},
		{`p Socket.pack_sockaddr_in(443, "1.2.3.4").bytesize`, "16"},
		// The IPv6 packed form is 28 bytes and round-trips.
		{`p Socket.pack_sockaddr_in(8080, "::1").bytesize`, "28"},
		{`p Socket.unpack_sockaddr_in(Socket.pack_sockaddr_in(8080, "::1"))`, `[8080, "::1"]`},
	}
	for _, c := range cases {
		if got := runSrc(t, "require \"socket\"\n"+c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// The family field is the host's native byte order (a C sa_family_t), while
	// the port is always network order — verified against the running arch's
	// endianness so the s390x big-endian lane checks the same invariant.
	sa := packSockaddrIn(443, "1.2.3.4")
	var fam [2]byte
	binpkg.NativeEndian.PutUint16(fam[:], uint16(afINET))
	if sa[0] != fam[0] || sa[1] != fam[1] {
		t.Errorf("family bytes = %v, want native-order %v", sa[0:2], fam)
	}
	if got := binpkg.BigEndian.Uint16(sa[2:4]); got != 443 {
		t.Errorf("port (network order) = %d, want 443", got)
	}
}

// TestSockaddrUnPackUnpack covers the AF_UNIX sockaddr helpers on non-Windows.
func TestSockaddrUnPackUnpack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("AF_UNIX unsupported on Windows")
	}
	cases := []struct{ src, want string }{
		{`p Socket.unpack_sockaddr_un(Socket.pack_sockaddr_un("/tmp/rbgo.sock"))`, `"/tmp/rbgo.sock"`},
		// sockaddr_un is the alias for pack_sockaddr_un.
		{`p(Socket.sockaddr_un("/x") == Socket.pack_sockaddr_un("/x"))`, "true"},
		// The packed form is family (2 bytes) + sun_path (108 bytes) = 110.
		{`p Socket.pack_sockaddr_un("/x").bytesize`, "110"},
		// Arity guards on both helpers.
		{`begin; Socket.pack_sockaddr_un; rescue ArgumentError; puts "packarity"; end`, "packarity"},
		{`begin; Socket.unpack_sockaddr_un; rescue ArgumentError; puts "unarity"; end`, "unarity"},
		// A path that overflows sun_path is rejected.
		{`begin; Socket.pack_sockaddr_un("/" + ("x" * 200)); rescue ArgumentError; puts "toolong"; end`, "toolong"},
		// A too-short sockaddr (no family field) is rejected.
		{`begin; Socket.unpack_sockaddr_un("x"); rescue ArgumentError; puts "tooshort"; end`, "tooshort"},
	}
	for _, c := range cases {
		if got := runSrc(t, "require \"socket\"\n"+c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestSockaddrUnWindowsStub confirms the AF_UNIX sockaddr helpers raise a clean,
// rescuable NotImplementedError on Windows (the byte layout is not compiled there).
func TestSockaddrUnWindowsStub(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only AF_UNIX stub")
	}
	cases := []struct{ src, want string }{
		{`begin; Socket.pack_sockaddr_un("/x"); rescue NotImplementedError; puts "pack"; end`, "pack"},
		{`begin; Socket.sockaddr_un("/x"); rescue NotImplementedError; puts "alias"; end`, "alias"},
		{`begin; Socket.unpack_sockaddr_un("x"); rescue NotImplementedError; puts "unpack"; end`, "unpack"},
	}
	for _, c := range cases {
		if got := runSrc(t, "require \"socket\"\n"+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestAddrinfoErrors covers the raising arms of the address surface through the
// engine: argument arities, bad family / socktype names, unpack of a malformed
// sockaddr, and the family/type guards.
func TestAddrinfoErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		// Arities.
		{`begin; Socket.getaddrinfo("h"); rescue ArgumentError; puts "gai"; end`, "gai"},
		{`begin; Socket.pack_sockaddr_in(80); rescue ArgumentError; puts "pin"; end`, "pin"},
		{`begin; Socket.unpack_sockaddr_in; rescue ArgumentError; puts "uin"; end`, "uin"},
		{`begin; Addrinfo.tcp("127.0.0.1"); rescue ArgumentError; puts "tcp"; end`, "tcp"},
		{`begin; Addrinfo.udp("127.0.0.1"); rescue ArgumentError; puts "udp"; end`, "udp"},
		{`begin; Addrinfo.ip; rescue ArgumentError; puts "ip"; end`, "ip"},
		{`begin; Addrinfo.getaddrinfo("h"); rescue ArgumentError; puts "agai"; end`, "agai"},
		{`begin; Addrinfo.new; rescue ArgumentError; puts "new"; end`, "new"},
		// A numeric IPv4 literal filtered to AF_INET6 (and the reverse) fails.
		{`begin; Socket.getaddrinfo("127.0.0.1", 80, Socket::AF_INET6); rescue SocketError; puts "v6filter"; end`, "v6filter"},
		{`begin; Socket.getaddrinfo("::1", 80, Socket::AF_INET); rescue SocketError; puts "v4filter"; end`, "v4filter"},
		// Unknown family / socktype names.
		{`begin; Addrinfo.getaddrinfo("127.0.0.1", 80, "AF_BOGUS"); rescue SocketError; puts "badfam"; end`, "badfam"},
		{`begin; Addrinfo.getaddrinfo("127.0.0.1", 80, :INET, "SOCK_BOGUS"); rescue SocketError; puts "badsock"; end`, "badsock"},
		// A non-Integer/Symbol/String family / socktype is a TypeError.
		{`begin; Addrinfo.getaddrinfo("127.0.0.1", 80, []); rescue TypeError; puts "famtype"; end`, "famtype"},
		{`begin; Addrinfo.getaddrinfo("127.0.0.1", 80, :INET, []); rescue TypeError; puts "socktype"; end`, "socktype"},
		// A non-Integer/String port is a TypeError.
		{`begin; Socket.getaddrinfo("127.0.0.1", []); rescue TypeError; puts "porttype"; end`, "porttype"},
		// unpack of a wrong-length sockaddr raises ArgumentError.
		{`begin; Socket.unpack_sockaddr_in("short"); rescue ArgumentError; puts "unlen"; end`, "unlen"},
		// unpack of a non-String sockaddr is a TypeError.
		{`begin; Socket.unpack_sockaddr_in(42); rescue TypeError; puts "unstr"; end`, "unstr"},
		// Addrinfo.new with a too-short array / a non-String,non-Array argument.
		{`begin; Addrinfo.new(["AF_INET", 80]); rescue ArgumentError; puts "arrlen"; end`, "arrlen"},
		{`begin; Addrinfo.new(42); rescue TypeError; puts "argtype"; end`, "argtype"},
		// Family names / the Linux AF_INET6 spelling round-trip through new's array form.
		{`b=Addrinfo.new(["AF_INET6", 443, "::1", "::1"]); p b.afamily`, "30"},
		{`p Addrinfo.getaddrinfo("::1", 443, 10, :STREAM).map(&:ipv6?)`, "[true]"},
		// The :DGRAM / SOCK_DGRAM socket-type names map to SOCK_DGRAM+UDP.
		{`p Addrinfo.getaddrinfo("127.0.0.1", 53, :INET, :DGRAM).map(&:inspect)`,
			`["#<Addrinfo: 127.0.0.1:53 UDP>"]`},
		{`p Addrinfo.getaddrinfo("127.0.0.1", 53, "AF_INET", "SOCK_DGRAM")[0].protocol`, "17"},
	}
	for _, c := range cases {
		if got := runSrc(t, "require \"socket\"\n"+c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestAddrinfoResolveSeams drives the resolver-backed arms deterministically
// through the resolveIPs / lookupPort seams: a resolution failure, an empty
// result, a hostname packed into a sockaddr, and both service-name port arms.
func TestAddrinfoResolveSeams(t *testing.T) {
	origIP, origPort := resolveIPs, lookupPort
	defer func() { resolveIPs, lookupPort = origIP, origPort }()

	// A resolution failure surfaces as SocketError from getaddrinfo.
	resolveIPs = func(string, string) ([]net.IP, error) { return nil, fmt.Errorf("injected resolve failure") }
	if got := runSrc(t, `require "socket"
begin; Socket.getaddrinfo("no.such.host.invalid", 80); rescue SocketError; puts "resfail"; end`); got != "resfail" {
		t.Errorf("resolve failure got %q", got)
	}
	// The same failure from pack_sockaddr_in's hostname resolution.
	if got := runSrc(t, `require "socket"
begin; Socket.pack_sockaddr_in(80, "no.such.host.invalid"); rescue SocketError; puts "packfail"; end`); got != "packfail" {
		t.Errorf("pack resolve failure got %q", got)
	}
	// An empty (but error-free) result from the singular Addrinfo.tcp constructor.
	resolveIPs = func(string, string) ([]net.IP, error) { return nil, nil }
	if got := runSrc(t, `require "socket"
begin; Addrinfo.tcp("no.such.host.invalid", 80); rescue SocketError; puts "empty"; end`); got != "empty" {
		t.Errorf("empty resolve got %q", got)
	}

	// A hostname resolving to a concrete IP is packed into a sockaddr.
	resolveIPs = func(string, string) ([]net.IP, error) { return []net.IP{net.ParseIP("9.8.7.6")}, nil }
	if got := runSrc(t, `require "socket"
p Socket.unpack_sockaddr_in(Socket.pack_sockaddr_in(80, "myhost"))`); got != `[80, "9.8.7.6"]` {
		t.Errorf("hostname pack got %q", got)
	}
	// getaddrinfo over a resolved hostname (default network, no family filter).
	if got := runSrc(t, `require "socket"
p Socket.getaddrinfo("myhost", 80, nil, Socket::SOCK_STREAM)`); got != `[["AF_INET", 80, "9.8.7.6", "9.8.7.6", 2, 1, 6]]` {
		t.Errorf("hostname getaddrinfo got %q", got)
	}

	// A service-name port: success and failure through the lookupPort seam.
	lookupPort = func(network, service string) (int, error) {
		if service == "http" {
			return 80, nil
		}
		return 0, fmt.Errorf("unknown service %q", service)
	}
	if got := runSrc(t, `require "socket"
p Socket.getaddrinfo("127.0.0.1", "http", Socket::AF_INET, Socket::SOCK_STREAM)[0][1]`); got != "80" {
		t.Errorf("service port got %q", got)
	}
	if got := runSrc(t, `require "socket"
begin; Socket.getaddrinfo("127.0.0.1", "bogus-service", Socket::AF_INET, Socket::SOCK_DGRAM); rescue SocketError; puts "svcfail"; end`); got != "svcfail" {
		t.Errorf("service failure got %q", got)
	}
}

// TestGetnameinfo covers Socket.getnameinfo's numeric arms hermetically: the
// packed-sockaddr and array address forms, NI_NUMERICHOST / NI_NUMERICSERV, the
// well-known port → service map (tcp and, under NI_DGRAM, udp), and the numeric
// fallback for a port with no service entry.
func TestGetnameinfo(t *testing.T) {
	cases := []struct{ src, want string }{
		// Numeric host + well-known service name (tcp).
		{`p Socket.getnameinfo(Socket.pack_sockaddr_in(80, "127.0.0.1"), Socket::NI_NUMERICHOST)`,
			`["127.0.0.1", "http"]`},
		// Numeric host + numeric service.
		{`p Socket.getnameinfo(Socket.pack_sockaddr_in(80, "127.0.0.1"), Socket::NI_NUMERICHOST | Socket::NI_NUMERICSERV)`,
			`["127.0.0.1", "80"]`},
		// A port with no service entry falls back to the numeric service.
		{`p Socket.getnameinfo(Socket.pack_sockaddr_in(12345, "127.0.0.1"), Socket::NI_NUMERICHOST)`,
			`["127.0.0.1", "12345"]`},
		// NI_DGRAM selects the udp service name.
		{`p Socket.getnameinfo(Socket.pack_sockaddr_in(53, "127.0.0.1"), Socket::NI_NUMERICHOST | Socket::NI_DGRAM)`,
			`["127.0.0.1", "domain"]`},
		// A v6 sockaddr keeps the bracket-free numeric host.
		{`p Socket.getnameinfo(Socket.pack_sockaddr_in(443, "::1"), Socket::NI_NUMERICHOST)`,
			`["::1", "https"]`},
		// The [afamily, port, host, addr] array form (numeric addr preferred as host).
		{`p Socket.getnameinfo(["AF_INET", 22, "example", "127.0.0.1"], Socket::NI_NUMERICHOST)`,
			`["127.0.0.1", "ssh"]`},
		// The three-element array form with a numeric-String port.
		{`p Socket.getnameinfo(["AF_INET", "25", "127.0.0.1"], Socket::NI_NUMERICHOST)`,
			`["127.0.0.1", "smtp"]`},
	}
	for _, c := range cases {
		if got := runSrc(t, "require \"socket\"\n"+c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestGetnameinfoReverseSeams drives the reverse-lookup arms deterministically
// through the lookupAddr seam: a successful reverse resolution (trailing dot
// stripped), a lookup failure falling back to the numeric host, an empty result,
// and NI_NAMEREQD turning a failure into a SocketError.
func TestGetnameinfoReverseSeams(t *testing.T) {
	orig := lookupAddr
	defer func() { lookupAddr = orig }()

	// Success: the reverse name is returned with its trailing dot stripped.
	lookupAddr = func(addr string) ([]string, error) {
		if addr == "127.0.0.1" {
			return []string{"localhost."}, nil
		}
		return nil, fmt.Errorf("no reverse for %s", addr)
	}
	if got := runSrc(t, `require "socket"
p Socket.getnameinfo(Socket.pack_sockaddr_in(80, "127.0.0.1"))`); got != `["localhost", "http"]` {
		t.Errorf("reverse success got %q", got)
	}
	// Failure without NI_NAMEREQD falls back to the numeric host.
	if got := runSrc(t, `require "socket"
p Socket.getnameinfo(Socket.pack_sockaddr_in(80, "9.9.9.9"))`); got != `["9.9.9.9", "http"]` {
		t.Errorf("reverse fallback got %q", got)
	}
	// An empty (error-free) result also falls back to the numeric host.
	lookupAddr = func(string) ([]string, error) { return nil, nil }
	if got := runSrc(t, `require "socket"
p Socket.getnameinfo(Socket.pack_sockaddr_in(80, "9.9.9.9"))`); got != `["9.9.9.9", "http"]` {
		t.Errorf("reverse empty got %q", got)
	}
	// NI_NAMEREQD turns a lookup failure into a SocketError.
	lookupAddr = func(string) ([]string, error) { return nil, fmt.Errorf("boom") }
	if got := runSrc(t, `require "socket"
begin; Socket.getnameinfo(Socket.pack_sockaddr_in(80, "9.9.9.9"), Socket::NI_NAMEREQD); rescue SocketError; puts "reqd"; end`); got != "reqd" {
		t.Errorf("NI_NAMEREQD failure got %q", got)
	}
}

// TestGetnameinfoErrors covers Socket.getnameinfo's raising arms: arity, a
// non-String/Array target, a too-short array, and the port element's type /
// numeric-parse failures.
func TestGetnameinfoErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`begin; Socket.getnameinfo; rescue ArgumentError; puts "arity"; end`, "arity"},
		{`begin; Socket.getnameinfo(42); rescue TypeError; puts "type"; end`, "type"},
		{`begin; Socket.getnameinfo(["AF_INET"]); rescue ArgumentError; puts "short"; end`, "short"},
		{`begin; Socket.getnameinfo(["AF_INET", [], "127.0.0.1"]); rescue TypeError; puts "porttype"; end`, "porttype"},
		{`begin; Socket.getnameinfo(["AF_INET", "notaport", "127.0.0.1"]); rescue SocketError; puts "badport"; end`, "badport"},
		// A wrong-length packed sockaddr is rejected by the unpack.
		{`begin; Socket.getnameinfo("short"); rescue ArgumentError; puts "salen"; end`, "salen"},
	}
	for _, c := range cases {
		if got := runSrc(t, "require \"socket\"\n"+c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
	// serviceName's no-entry arm is also reachable directly (a port present in the
	// table but not for the requested protocol).
	if _, ok := serviceName(21, "udp"); ok {
		t.Error("serviceName(21, udp) should have no entry")
	}
}

// TestAddrinfoHelperArms covers the pure helper branches that normal dispatch
// cannot reach (the AF_UNIX / AF_UNSPEC family names, the value-protocol methods,
// and the type-narrowing guard).
func TestAddrinfoHelperArms(t *testing.T) {
	// afamilyName over every arm, including AF_UNIX and the unspecified default.
	for af, want := range map[int]string{afINET: "AF_INET", afINET6: "AF_INET6", afUNIX: "AF_UNIX", 99: "AF_UNSPEC"} {
		if got := afamilyName(af); got != want {
			t.Errorf("afamilyName(%d) = %q, want %q", af, got, want)
		}
	}
	// familyFromName / socktypeFromName over the AF_UNIX / LOCAL and prefixed forms.
	for _, name := range []string{"UNIX", "LOCAL", "AF_UNIX"} {
		if got := familyFromName(name); got != afUNIX {
			t.Errorf("familyFromName(%q) = %d, want afUNIX", name, got)
		}
	}
	// The addrinfo value's Go-level ToS / Inspect / Truthy.
	a := &addrinfo{afamily: afINET, ip: "127.0.0.1", port: 80, socktype: sockStream, protocol: ipprotoTCP}
	if a.ToS() != "#<Addrinfo: 127.0.0.1:80 TCP>" || a.Inspect() != a.ToS() || !a.Truthy() {
		t.Errorf("addrinfo value protocol: ToS=%q Truthy=%v", a.ToS(), a.Truthy())
	}
	// The type-narrowing guard raises TypeError on a mis-typed receiver.
	func() {
		defer func() {
			if re, ok := recover().(RubyError); !ok || re.Class != "TypeError" {
				t.Errorf("asAddrinfo recover = %v", recover())
			}
		}()
		asAddrinfo(object.NilV)
	}()
}
