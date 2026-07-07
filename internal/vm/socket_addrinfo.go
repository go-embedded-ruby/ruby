// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"context"
	binpkg "encoding/binary"
	"net"
	"strconv"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the name-resolution + address-utility half of rbgo's socket
// transport, the follow-up the connected/datagram socket work (socket.go /
// socket_udp.go / socket_unix.go) deferred: Socket.getaddrinfo, the Addrinfo
// value class, and the sockaddr pack/unpack helpers. All of it is backed by Go's
// net package (net.Resolver for lookups) and pure byte manipulation for the
// sockaddr layout — no cgo, no live network beyond the resolver the caller asks
// for.
//
// Endianness note: the sockaddr byte layout modelled here is the Linux one (no
// BSD sa_len byte). sin_family / sin6_family are written in the host's native
// byte order (a C sa_family_t is a host-endian short), while sin_port and the
// address bytes are network byte order (big-endian) regardless of host — exactly
// the C semantics. pack and unpack use the same native order for the family
// field, so the [port, host] round-trip is byte-exact on every arch, including
// big-endian s390x (where the family short lands as 00 02 rather than 02 00).

// Address-family / socket-type / protocol numbers rbgo uses across the address
// utilities. They match the Socket:: constants registered in socket.go, so a
// tuple's family integer (2 / 30) and an Addrinfo#afamily agree with what a
// script reads from Socket::AF_INET etc. v4-vs-v6 is decided by sockaddr length,
// not this number, so the exact AF_INET6 value (BSD 30 here) never affects a
// round-trip.
const (
	afINET     = 2
	afINET6    = 30
	afUNIX     = 1
	sockStream = 1
	sockDgram  = 2
	ipprotoTCP = 6
	ipprotoUDP = 17
)

// resolveIPs is the net.Resolver.LookupIP seam Socket.getaddrinfo / Addrinfo
// resolve a hostname through. It is a package var so a test can inject a
// resolution failure (a numeric-literal host never reaches the resolver, so its
// error arm has no natural trigger). network is "ip" / "ip4" / "ip6".
var resolveIPs = func(network, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(context.Background(), network, host)
}

// lookupPort is the net.LookupPort seam a service-name port ("http") resolves
// through. It is a package var so a test can drive both its success and failure
// arms deterministically without depending on the host's /etc/services.
var lookupPort = net.LookupPort

// lookupAddr is the net.LookupAddr seam Socket.getnameinfo reverse-resolves an
// IP to a hostname through. It is a package var so a test can drive its success
// and failure arms deterministically without depending on live reverse DNS (a
// numeric NI_NUMERICHOST request never reaches it).
var lookupAddr = net.LookupAddr

// getnameinfo flag bits (they mirror the Socket::NI_* constants; only the
// relative bitmask matters).
const (
	niNumericHost = 2
	niNameReqd    = 4
	niNumericServ = 8
	niDGRAM       = 16
)

// wellKnownServices maps a small set of well-known ports to their service names
// for the reverse (port → service) half of Socket.getnameinfo. Go's net offers
// no reverse-service lookup, so rbgo carries the common entries and falls back to
// the numeric port for anything else — matching what getnameinfo returns when
// /etc/services has no entry.
var wellKnownServices = map[int]map[string]string{
	20:   {"tcp": "ftp-data"},
	21:   {"tcp": "ftp"},
	22:   {"tcp": "ssh", "udp": "ssh"},
	23:   {"tcp": "telnet"},
	25:   {"tcp": "smtp"},
	53:   {"tcp": "domain", "udp": "domain"},
	80:   {"tcp": "http", "udp": "http"},
	110:  {"tcp": "pop3"},
	143:  {"tcp": "imap"},
	443:  {"tcp": "https", "udp": "https"},
	587:  {"tcp": "submission"},
	993:  {"tcp": "imaps"},
	995:  {"tcp": "pop3s"},
	3306: {"tcp": "mysql"},
	5432: {"tcp": "postgresql"},
	6379: {"tcp": "redis"},
}

// serviceName reports the service name for a well-known (port, proto) pair, or
// ok=false when there is no entry (the caller then falls back to the numeric
// port).
func serviceName(port int, proto string) (string, bool) {
	if m, ok := wellKnownServices[port]; ok {
		if name, ok := m[proto]; ok {
			return name, true
		}
	}
	return "", false
}

// addrinfo is a resolved address (MRI's Addrinfo): an address family plus the
// numeric IP, port, and the socket-type / protocol that qualify it (0 when the
// Addrinfo is a bare IP). It is a value object — construction resolves the host,
// the readers below just report the stored fields.
type addrinfo struct {
	cls      *RClass
	afamily  int
	ip       string
	port     int
	socktype int
	protocol int
}

func (a *addrinfo) ToS() string     { return a.inspect() }
func (a *addrinfo) Inspect() string { return a.inspect() }
func (a *addrinfo) Truthy() bool    { return true }

// inspect renders the MRI Addrinfo#inspect form: "#<Addrinfo: HOST>" for a bare
// IP, "#<Addrinfo: HOST:PORT>" with a " TCP" / " UDP" suffix when a protocol
// qualifies it. An IPv6 host is bracketed ("[::1]:443").
func (a *addrinfo) inspect() string {
	host := a.ip
	if a.afamily == afINET6 {
		host = "[" + a.ip + "]"
	}
	s := "#<Addrinfo: " + host
	if a.port != 0 || a.socktype != 0 || a.protocol != 0 {
		s += ":" + strconv.Itoa(a.port)
		switch {
		case a.protocol == ipprotoTCP || a.socktype == sockStream:
			s += " TCP"
		case a.protocol == ipprotoUDP || a.socktype == sockDgram:
			s += " UDP"
		}
	}
	return s + ">"
}

// registerSocketAddr installs the address-utility surface on the Socket class
// (Socket.getaddrinfo + the sockaddr pack/unpack methods) and the Addrinfo value
// class. It runs from registerSocket after registerSocketClass has published the
// Socket constant.
func (vm *VM) registerSocketAddr() {
	sock := vm.consts["Socket"].(*RClass)

	// Socket.getaddrinfo(host, port [, family, socktype, protocol, flags]) →
	// [[afamily_string, port, host, addr, pfamily, socktype, protocol], ...],
	// MRI's tuple shape. host is the numeric address (no reverse lookup, keeping
	// the result hermetic).
	sock.smethods["getaddrinfo"] = &Method{name: "getaddrinfo", owner: sock,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 2 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 2..7)", len(args))
			}
			list := resolveAddrinfo(sock, args)
			tuples := make([]object.Value, len(list))
			for i, a := range list {
				tuples[i] = object.NewArray(
					object.NewString(afamilyName(a.afamily)),
					object.IntValue(int64(a.port)),
					object.NewString(a.ip),
					object.NewString(a.ip),
					object.IntValue(int64(a.afamily)),
					object.IntValue(int64(a.socktype)),
					object.IntValue(int64(a.protocol)),
				)
			}
			return object.NewArrayFromSlice(tuples)
		}}

	// Socket.getnameinfo(sockaddr [, flags]) → [hostname, service], the reverse of
	// getaddrinfo: sockaddr is a packed sockaddr_in / sockaddr_in6 String or an
	// [afamily, port, host, addr] array. hostname is the reverse-resolved name
	// (or the numeric address under NI_NUMERICHOST / on lookup failure), service
	// is the port's well-known name (or the numeric port under NI_NUMERICSERV / a
	// port with no entry).
	sock.smethods["getnameinfo"] = &Method{name: "getnameinfo", owner: sock,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 1 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
			}
			flags := 0
			if len(args) > 1 && !object.IsNil(args[1]) {
				flags = int(intArg(args[1]))
			}
			host, port := nameinfoTarget(args[0])
			return object.NewArray(
				object.NewString(nameinfoHost(host, flags)),
				object.NewString(nameinfoService(port, flags)),
			)
		}}

	// Socket.pack_sockaddr_in(port, host) / Socket.sockaddr_in(port, host) → the
	// packed sockaddr_in / sockaddr_in6 bytes for host:port.
	packIn := &Method{name: "pack_sockaddr_in", owner: sock,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 2 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
			}
			return object.NewStringBytesEnc(packSockaddrIn(int(intArg(args[0])), strArg(args[1])), "ASCII-8BIT")
		}}
	sock.smethods["pack_sockaddr_in"] = packIn
	sock.smethods["sockaddr_in"] = &Method{name: "sockaddr_in", owner: sock, native: packIn.native}

	// Socket.unpack_sockaddr_in(sockaddr) → [port, host]. A sockaddr that is
	// neither AF_INET nor AF_INET6 raises ArgumentError, as MRI does.
	sock.smethods["unpack_sockaddr_in"] = &Method{name: "unpack_sockaddr_in", owner: sock,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 1 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
			}
			port, host := unpackSockaddrIn(sockaddrBytes(args[0]))
			return object.NewArray(object.IntValue(int64(port)), object.NewString(host))
		}}

	// The AF_UNIX sockaddr helpers (pack_sockaddr_un / unpack_sockaddr_un) are
	// platform-gated: real on non-Windows, NotImplementedError stubs on Windows
	// (socket_addrinfo_unix.go / socket_addrinfo_windows.go), mirroring the
	// UNIXSocket transport.
	registerSockaddrUn(sock)

	vm.registerAddrinfo(sock)
}

// registerAddrinfo installs the Addrinfo value class: the .tcp / .udp / .ip /
// .getaddrinfo / .new constructors and the ip_address / ip_port / afamily /
// pfamily / socktype / protocol / ipv4? / ipv6? / to_sockaddr / inspect readers.
func (vm *VM) registerAddrinfo(sock *RClass) {
	cls := newClass("Addrinfo", vm.cObject)
	vm.consts["Addrinfo"] = cls

	// Addrinfo.tcp(host, port) / Addrinfo.udp(host, port): an Addrinfo qualified
	// by SOCK_STREAM+TCP / SOCK_DGRAM+UDP.
	mkQualified := func(socktype, protocol int) NativeFn {
		return func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 2 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
			}
			ip, af := resolveOneIP("ip", strArg(args[0]))
			return &addrinfo{cls: cls, afamily: af, ip: ip, port: int(intArg(args[1])), socktype: socktype, protocol: protocol}
		}
	}
	cls.smethods["tcp"] = &Method{name: "tcp", owner: cls, native: mkQualified(sockStream, ipprotoTCP)}
	cls.smethods["udp"] = &Method{name: "udp", owner: cls, native: mkQualified(sockDgram, ipprotoUDP)}

	// Addrinfo.ip(host): a bare-IP Addrinfo (no port, socktype 0, protocol 0).
	cls.smethods["ip"] = &Method{name: "ip", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 1 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 1)", len(args))
			}
			ip, af := resolveOneIP("ip", strArg(args[0]))
			return &addrinfo{cls: cls, afamily: af, ip: ip}
		}}

	// Addrinfo.getaddrinfo(host, port [, family, socktype, protocol, flags]) →
	// an array of Addrinfo, the object form of Socket.getaddrinfo.
	cls.smethods["getaddrinfo"] = &Method{name: "getaddrinfo", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 2 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 2..7)", len(args))
			}
			list := resolveAddrinfo(cls, args)
			out := make([]object.Value, len(list))
			for i := range list {
				out[i] = list[i]
			}
			return object.NewArrayFromSlice(out)
		}}

	// Addrinfo.new(sockaddr [, family, socktype, protocol]): sockaddr is either a
	// packed sockaddr_in / sockaddr_in6 String or a [afamily, port, host, addr]
	// array (MRI accepts both). The optional family/socktype/protocol override the
	// derived ones.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 1 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 1..4)", len(args))
			}
			a := addrinfoFromSockaddr(cls, args[0])
			if len(args) > 1 && !object.IsNil(args[1]) {
				a.afamily = familyNumber(args[1])
			}
			if len(args) > 2 && !object.IsNil(args[2]) {
				a.socktype = socktypeNumber(args[2])
			}
			if len(args) > 3 && !object.IsNil(args[3]) {
				a.protocol = int(intArg(args[3]))
			}
			return a
		}}

	cls.define("ip_address", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(asAddrinfo(self).ip)
	})
	cls.define("ip_port", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(asAddrinfo(self).port))
	})
	cls.define("afamily", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(asAddrinfo(self).afamily))
	})
	// pfamily mirrors afamily for the IP families rbgo models (PF_INET == AF_INET).
	cls.define("pfamily", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(asAddrinfo(self).afamily))
	})
	cls.define("socktype", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(asAddrinfo(self).socktype))
	})
	cls.define("protocol", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(asAddrinfo(self).protocol))
	})
	cls.define("ipv4?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(asAddrinfo(self).afamily == afINET)
	})
	cls.define("ipv6?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(asAddrinfo(self).afamily == afINET6)
	})
	// to_sockaddr (alias to_s) returns the packed sockaddr bytes for this address.
	toSockaddr := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		a := asAddrinfo(self)
		return object.NewStringBytesEnc(packSockaddrIn(a.port, a.ip), "ASCII-8BIT")
	}
	cls.define("to_sockaddr", toSockaddr)
	cls.define("to_s", toSockaddr)
	cls.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(asAddrinfo(self).inspect())
	})
}

// resolveAddrinfo is the shared resolution engine behind Socket.getaddrinfo and
// Addrinfo.getaddrinfo: it resolves args[0] (host) to one or more IPs, honouring
// an optional family filter (args[2]) and socket-type (args[3]) / protocol
// (args[4]), and returns one addrinfo per resolved IP. args[1] is the port
// (Integer, numeric / service-name String, or nil).
func resolveAddrinfo(cls *RClass, args []object.Value) []*addrinfo {
	network := "ip"
	if len(args) > 2 && !object.IsNil(args[2]) {
		switch familyNumber(args[2]) {
		case afINET:
			network = "ip4"
		case afINET6:
			network = "ip6"
		}
	}
	socktype := 0
	if len(args) > 3 && !object.IsNil(args[3]) {
		socktype = socktypeNumber(args[3])
	}
	protocol := 0
	if len(args) > 4 && !object.IsNil(args[4]) {
		protocol = int(intArg(args[4]))
	} else {
		switch socktype {
		case sockStream:
			protocol = ipprotoTCP
		case sockDgram:
			protocol = ipprotoUDP
		}
	}
	port := resolvePort(args[1], socktype)

	ips := lookupHost(network, strArg(args[0]))
	out := make([]*addrinfo, 0, len(ips))
	for _, ip := range ips {
		out = append(out, &addrinfo{cls: cls, afamily: familyOf(ip), ip: ip.String(), port: port, socktype: socktype, protocol: protocol})
	}
	return out
}

// lookupHost resolves host to its IPs on the requested network ("ip" / "ip4" /
// "ip6"), short-circuiting a numeric literal through net.ParseIP (so it never
// touches the resolver) and filtering it to the requested family. A resolution
// failure raises SocketError, matching MRI's getaddrinfo error.
func lookupHost(network, host string) []net.IP {
	if ip := net.ParseIP(host); ip != nil {
		if network == "ip4" && ip.To4() == nil {
			raise("SocketError", "getaddrinfo: Address family for hostname not supported")
		}
		if network == "ip6" && ip.To4() != nil {
			raise("SocketError", "getaddrinfo: Address family for hostname not supported")
		}
		return []net.IP{ip}
	}
	ips, err := resolveIPs(network, host)
	if err != nil {
		raise("SocketError", "getaddrinfo: %s", err.Error())
	}
	return ips
}

// resolveOneIP resolves host to a single numeric IP + family for the singular
// Addrinfo constructors (.tcp / .udp / .ip), taking the first resolved address.
func resolveOneIP(network, host string) (string, int) {
	ips := lookupHost(network, host)
	if len(ips) == 0 {
		raise("SocketError", "getaddrinfo: no address for %s", host)
	}
	return ips[0].String(), familyOf(ips[0])
}

// resolvePort resolves a port argument to an integer: an Integer is taken as-is,
// a numeric String is parsed, and a service-name String ("http") is looked up
// through net (the socktype selecting tcp / udp). nil is port 0.
func resolvePort(v object.Value, socktype int) int {
	switch p := v.(type) {
	case object.Integer:
		return int(p)
	case *object.String:
		s := p.Str()
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		proto := "tcp"
		if socktype == sockDgram {
			proto = "udp"
		}
		n, err := lookupPort(proto, s)
		if err != nil {
			raise("SocketError", "getaddrinfo: %s", err.Error())
		}
		return n
	default:
		if object.IsNil(v) {
			return 0
		}
		raise("TypeError", "no implicit conversion of %s into Integer", v.Inspect())
		return 0
	}
}

// familyOf reports the rbgo address-family number for a resolved IP.
func familyOf(ip net.IP) int {
	if ip.To4() != nil {
		return afINET
	}
	return afINET6
}

// familyNumber resolves a family argument (an Integer such as Socket::AF_INET,
// or a Symbol / String such as :INET / "AF_INET6") to the rbgo family number.
func familyNumber(v object.Value) int {
	switch x := v.(type) {
	case object.Integer:
		// Accept both the BSD (30) and Linux (10) AF_INET6 spellings.
		if int(x) == 10 {
			return afINET6
		}
		return int(x)
	case object.Symbol:
		return familyFromName(string(x))
	case *object.String:
		return familyFromName(x.Str())
	default:
		raise("TypeError", "invalid address family %s", v.Inspect())
		return 0
	}
}

// familyFromName maps a family name (with or without the AF_ / PF_ prefix) to its
// number: INET → AF_INET, INET6 → AF_INET6, UNIX / LOCAL → AF_UNIX.
func familyFromName(name string) int {
	switch name {
	case "INET", "AF_INET", "PF_INET":
		return afINET
	case "INET6", "AF_INET6", "PF_INET6":
		return afINET6
	case "UNIX", "LOCAL", "AF_UNIX":
		return afUNIX
	default:
		raise("SocketError", "getaddrinfo: unknown address family %q", name)
		return 0
	}
}

// socktypeNumber resolves a socket-type argument (an Integer such as
// Socket::SOCK_STREAM, or a Symbol / String such as :STREAM) to its number.
func socktypeNumber(v object.Value) int {
	switch x := v.(type) {
	case object.Integer:
		return int(x)
	case object.Symbol:
		return socktypeFromName(string(x))
	case *object.String:
		return socktypeFromName(x.Str())
	default:
		raise("TypeError", "invalid socktype %s", v.Inspect())
		return 0
	}
}

// socktypeFromName maps a socket-type name (with or without the SOCK_ prefix) to
// its number: STREAM → SOCK_STREAM, DGRAM → SOCK_DGRAM.
func socktypeFromName(name string) int {
	switch name {
	case "STREAM", "SOCK_STREAM":
		return sockStream
	case "DGRAM", "SOCK_DGRAM":
		return sockDgram
	default:
		raise("SocketError", "getaddrinfo: unknown socket type %q", name)
		return 0
	}
}

// afamilyName renders an address-family number as its MRI string ("AF_INET" /
// "AF_INET6" / "AF_UNIX"), the first element of a getaddrinfo tuple.
func afamilyName(af int) string {
	switch af {
	case afINET:
		return "AF_INET"
	case afINET6:
		return "AF_INET6"
	case afUNIX:
		return "AF_UNIX"
	default:
		return "AF_UNSPEC"
	}
}

// packSockaddrIn packs host:port as a sockaddr_in (IPv4) or sockaddr_in6 (IPv6).
// The family field is native byte order; the port and address are network byte
// order. An unresolvable host raises SocketError.
func packSockaddrIn(port int, host string) []byte {
	ip := net.ParseIP(host)
	if ip == nil {
		ips, err := resolveIPs("ip", host)
		if err != nil || len(ips) == 0 {
			raise("SocketError", "getaddrinfo: %s", host)
		}
		ip = ips[0]
	}
	if v4 := ip.To4(); v4 != nil {
		buf := make([]byte, 16)
		binpkg.NativeEndian.PutUint16(buf[0:2], uint16(afINET))
		binpkg.BigEndian.PutUint16(buf[2:4], uint16(port))
		copy(buf[4:8], v4)
		return buf
	}
	buf := make([]byte, 28)
	binpkg.NativeEndian.PutUint16(buf[0:2], uint16(afINET6))
	binpkg.BigEndian.PutUint16(buf[2:4], uint16(port))
	copy(buf[8:24], ip.To16())
	return buf
}

// unpackSockaddrIn reads a packed sockaddr_in / sockaddr_in6 back into its port
// and numeric host. The v4-vs-v6 shape is decided by length (16 vs 28); any
// other length raises ArgumentError, as MRI does for a non-AF_INET sockaddr.
func unpackSockaddrIn(sa []byte) (int, string) {
	switch len(sa) {
	case 16:
		port := binpkg.BigEndian.Uint16(sa[2:4])
		ip := net.IP(sa[4:8])
		return int(port), ip.String()
	case 28:
		port := binpkg.BigEndian.Uint16(sa[2:4])
		ip := net.IP(sa[8:24])
		return int(port), ip.String()
	default:
		raise("ArgumentError", "not an AF_INET/AF_INET6 sockaddr")
		return 0, ""
	}
}

// addrinfoFromSockaddr builds an Addrinfo from the argument to Addrinfo.new:
// either a packed sockaddr String or a [afamily, port, host, addr] array.
func addrinfoFromSockaddr(cls *RClass, v object.Value) *addrinfo {
	if s, ok := v.(*object.String); ok {
		port, host := unpackSockaddrIn(s.Bytes())
		af := afINET
		if len(s.Bytes()) == 28 {
			af = afINET6
		}
		return &addrinfo{cls: cls, afamily: af, ip: host, port: port}
	}
	if arr, ok := v.(*object.Array); ok {
		if len(arr.Elems) < 4 {
			raise("ArgumentError", "array address must have 4 elements [afamily, port, host, addr]")
		}
		af := familyNumber(arr.Elems[0])
		return &addrinfo{cls: cls, afamily: af, ip: strArg(arr.Elems[3]), port: int(intArg(arr.Elems[1]))}
	}
	raise("TypeError", "expected a packed sockaddr String or an address Array")
	return nil
}

// sockaddrBytes extracts the raw bytes of a sockaddr String argument, raising
// TypeError for a non-String (matching MRI's implicit-conversion error).
func sockaddrBytes(v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	raise("TypeError", "no implicit conversion of %s into String", v.Inspect())
	return nil
}

// nameinfoTarget extracts the numeric host and port from a Socket.getnameinfo
// argument: a packed sockaddr_in / sockaddr_in6 String, or an [afamily, port,
// host, addr] array (the numeric addr element is preferred as the host when
// present). A non-String, non-Array argument raises TypeError.
func nameinfoTarget(v object.Value) (string, int) {
	switch x := v.(type) {
	case *object.String:
		port, host := unpackSockaddrIn(x.Bytes())
		return host, port
	case *object.Array:
		if len(x.Elems) < 3 {
			raise("ArgumentError", "array address must have at least 3 elements [afamily, port, host]")
		}
		host := strArg(x.Elems[2])
		if len(x.Elems) >= 4 {
			host = strArg(x.Elems[3])
		}
		return host, nameinfoPort(x.Elems[1])
	default:
		raise("TypeError", "expected a packed sockaddr String or an address Array")
		return "", 0
	}
}

// nameinfoPort resolves the port element of a getnameinfo address array: an
// Integer is taken as-is, a numeric String is parsed. Anything else is a
// TypeError.
func nameinfoPort(v object.Value) int {
	switch p := v.(type) {
	case object.Integer:
		return int(p)
	case *object.String:
		if n, err := strconv.Atoi(p.Str()); err == nil {
			return n
		}
		raise("SocketError", "getnameinfo: invalid port %q", p.Str())
		return 0
	default:
		raise("TypeError", "no implicit conversion of %s into Integer", v.Inspect())
		return 0
	}
}

// nameinfoHost resolves the hostname half of getnameinfo: the numeric address
// under NI_NUMERICHOST, else the reverse-resolved name (via the lookupAddr seam).
// A lookup failure falls back to the numeric address, unless NI_NAMEREQD demands
// a name, in which case it raises SocketError — matching getnameinfo(3).
func nameinfoHost(host string, flags int) string {
	if flags&niNumericHost != 0 {
		return host
	}
	names, err := lookupAddr(host)
	if err != nil || len(names) == 0 {
		if flags&niNameReqd != 0 {
			raise("SocketError", "getnameinfo: Name or service not known")
		}
		return host
	}
	return strings.TrimSuffix(names[0], ".")
}

// nameinfoService resolves the service half of getnameinfo: the numeric port
// under NI_NUMERICSERV, else the well-known service name for the port (tcp, or
// udp under NI_DGRAM), falling back to the numeric port when there is no entry.
func nameinfoService(port, flags int) string {
	if flags&niNumericServ != 0 {
		return strconv.Itoa(port)
	}
	proto := "tcp"
	if flags&niDGRAM != 0 {
		proto = "udp"
	}
	if name, ok := serviceName(port, proto); ok {
		return name
	}
	return strconv.Itoa(port)
}

// asAddrinfo narrows a receiver to *addrinfo, raising TypeError otherwise so a
// mis-typed self surfaces as a Ruby error rather than a Go panic.
func asAddrinfo(v object.Value) *addrinfo {
	if a, ok := v.(*addrinfo); ok {
		return a
	}
	raise("TypeError", "not an Addrinfo")
	return nil
}
