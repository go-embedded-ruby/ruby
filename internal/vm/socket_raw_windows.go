// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

import "net"

// This is the Windows counterpart to socket_raw_unix.go: AF_UNIX is unsupported
// on Windows (matching the UNIXSocket transport stub), so a raw AF_UNIX socket is
// rejected at construction — rawSocketNetwork raises NotImplementedError for the
// AF_UNIX domain, so Socket.new(AF_UNIX, ...) fails cleanly and no AF_UNIX raw
// socket is ever created. The address helpers therefore only ever see INET
// sockets and delegate straight to the INET cores.

// rawSocketNetwork maps a raw socket's (domain, type) to the Go net network
// string. The AF_UNIX domain is unsupported on Windows and raises; the INET
// families defer to rawINETNetwork.
func rawSocketNetwork(domain, typ int) (string, bool) {
	if domain == afUNIX {
		raise("NotImplementedError", "AF_UNIX raw sockets are not supported on Windows")
	}
	return rawINETNetwork(domain, typ)
}

func (s *rawSocket) resolveAddr(sa []byte) string { return s.resolveINETAddr(sa) }

func (s *rawSocket) resolveNetAddr(sa []byte) net.Addr { return s.resolveINETNetAddr(sa) }

func (s *rawSocket) packAddr(a net.Addr) []byte { return s.packINETAddr(a) }

func (s *rawSocket) addrinfoOf(cls *RClass, a net.Addr) *addrinfo {
	st, proto := rawStProto(s.typ)
	return s.addrinfoINET(cls, a, st, proto)
}
