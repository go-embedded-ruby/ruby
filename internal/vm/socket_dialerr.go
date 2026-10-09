// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"net"
	"syscall"
)

// raiseDialErr reports a failed connect the way MRI's socket layer does, which
// is NOT one exception class for everything that can go wrong on the way to a
// peer. Measured against MRI 4.0.7:
//
//	TCPSocket.new("127.0.0.1", 1)            Errno::ECONNREFUSED
//	  message: Connection refused - connect(2) for "127.0.0.1" port 1
//	TCPSocket.new("no-such-host.invalid", 80) Socket::ResolutionError
//	Socket.tcp("192.0.2.1", 80, …)            Errno::ETIMEDOUT
//
// rbgo raised SocketError for all of them, with the text "getaddrinfo: " in
// front of a Go error — a label that is simply false for a refused connection,
// since no name was being resolved. The practical cost is that the ordinary
// retry idiom does not work: code that rescues Errno::ECONNREFUSED to back off
// and try again never matches, and code that rescues SocketError to report a
// bad hostname matches a perfectly resolvable host whose port is shut.
//
// The errno is carried by Go in a *net.OpError wrapping *os.SyscallError
// wrapping syscall.Errno, so errors.As digs it out; this is the same shape
// raiseChdirErr (dir.go) uses for filesystem calls, and errnoClasses is the
// same table.
//
// addr is rendered as MRI renders it for the family: a TCP peer as `"host" port
// N`, a Unix peer as the bare path. MRI's own messages are the authority for
// that difference — `connect(2) for "127.0.0.1" port 1` against `connect(2) for
// /tmp/nope.sock`.
func raiseDialErr(err error, addr string) {
	// A name that does not resolve is a resolution failure, not a system call
	// failure, and MRI says so with its own class. Checked FIRST because a
	// DNSError can wrap an errno on some platforms, and the outer meaning is
	// the one MRI reports.
	var dns *net.DNSError
	if errors.As(err, &dns) {
		raise("Socket::ResolutionError", "getaddrinfo: %s", dns.Err)
	}
	var eno syscall.Errno
	if errors.As(err, &eno) {
		// Windows fails socket calls with WSAE* codes in the 10000 range, which
		// errnoClasses does not name -- so a refused connection arrived as a bare
		// SystemCallError there while darwin and linux were green, and `rescue
		// Errno::ECONNREFUSED` matched nothing on the one platform nobody tested
		// it on. MRI translates the same way (rb_w32_map_errno); posixErrno is
		// the identity everywhere else.
		eno = posixErrno(eno)
		// An empty addr means the caller has no single peer to name -- the
		// Net::HTTP transport, where the phase may be a read rather than a
		// connect and Go's own text already carries the address. Naming
		// connect(2) there would describe a call that did not happen.
		suffix := ""
		if addr != "" {
			suffix = " - connect(2) for " + addr
		}
		if name, ok := errnoClasses[int64(eno)]; ok {
			raise("Errno::"+name, "%s%s", errnoStrerror(int64(eno)), suffix)
		}
		raise("SystemCallError", "%s%s", errnoStrerror(int64(eno)), suffix)
	}
	// Anything with no errno and no resolution failure behind it keeps the old
	// class. Falling through to SocketError is deliberate: inventing an errno
	// for an error that does not carry one would make the class a guess.
	raise("SocketError", "%s", err.Error())
}

// dialAddr renders a host and port the way MRI's connect(2) message does.
func dialAddr(host, port string) string { return `"` + host + `" port ` + port }
