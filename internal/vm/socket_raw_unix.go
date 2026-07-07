// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows

package vm

import "net"

// This is the non-Windows half of the raw Socket's domain dispatch: the AF_UNIX
// address handling, layered over the AF_INET / AF_INET6 cores in socket_raw.go.
// AF_UNIX is unsupported on Windows (matching the UNIXSocket transport), so on
// that platform Socket.new(AF_UNIX, ...) is rejected instead (socket_raw_windows
// .go); keeping every AF_UNIX branch here means the Windows build carries no
// AF_UNIX code path to leave uncovered.

// rawSocketNetwork maps a raw socket's (domain, type) to the Go net network
// string. AF_UNIX resolves to "unix" / "unixgram"; the INET families defer to
// rawINETNetwork.
func rawSocketNetwork(domain, typ int) (string, bool) {
	if domain == afUNIX {
		switch typ {
		case sockStream:
			return "unix", true
		case sockDgram:
			return "unixgram", true
		}
		return "", false
	}
	return rawINETNetwork(domain, typ)
}

// resolveAddr turns a packed sockaddr into the Go net address for #bind /
// #connect: the filesystem path for AF_UNIX, else the INET host:port.
func (s *rawSocket) resolveAddr(sa []byte) string {
	if s.domain == afUNIX {
		return unpackSockaddrUn(sa)
	}
	return s.resolveINETAddr(sa)
}

// resolveNetAddr turns a packed destination sockaddr into a net.Addr for a
// datagram #send: a unixgram *net.UnixAddr for AF_UNIX, else a *net.UDPAddr.
func (s *rawSocket) resolveNetAddr(sa []byte) net.Addr {
	if s.domain == afUNIX {
		return &net.UnixAddr{Name: unpackSockaddrUn(sa), Net: "unixgram"}
	}
	return s.resolveINETNetAddr(sa)
}

// packAddr renders a net.Addr as the packed sockaddr #getsockname / #getpeername
// return: a sockaddr_un for AF_UNIX, else a sockaddr_in / sockaddr_in6.
func (s *rawSocket) packAddr(a net.Addr) []byte {
	if s.domain == afUNIX {
		return packSockaddrUn(a.String())
	}
	return s.packINETAddr(a)
}

// addrinfoOf builds the Addrinfo #accept / #recvfrom report for a peer address,
// with the AF_UNIX form carrying the peer path and the INET form the host:port.
func (s *rawSocket) addrinfoOf(cls *RClass, a net.Addr) *addrinfo {
	st, proto := rawStProto(s.typ)
	if s.domain == afUNIX {
		return &addrinfo{cls: cls, afamily: afUNIX, ip: a.String(), socktype: st, protocol: proto}
	}
	return s.addrinfoINET(cls, a, st, proto)
}
