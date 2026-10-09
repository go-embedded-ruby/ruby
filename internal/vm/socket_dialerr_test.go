// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestAConnectFailureKeepsItsErrno (#772).
//
// rbgo raised SocketError for everything that went wrong on the way to a peer,
// with "getaddrinfo: " in front of a Go error — a label that is false for a
// refused connection, since no name was being resolved. The cost is that the
// ordinary idioms stop working in both directions: code rescuing
// Errno::ECONNREFUSED to back off and retry never matches, and code rescuing
// SocketError to report a bad hostname matches a perfectly resolvable host
// whose port is simply shut.
//
// The port is obtained by binding a listener and closing it, rather than by
// picking a number believed to be unused: "port 1 is surely closed" is a
// statement about the host the test happens to run on.
func TestAConnectFailureKeepsItsErrno(t *testing.T) {
	checkCases(t, []runCase{
		{`require "socket"
s = TCPServer.new("127.0.0.1", 0)
port = s.addr[1]
s.close
begin
  TCPSocket.new("127.0.0.1", port)
  puts "NO RAISE"
rescue => e
  puts e.class
end`, "Errno::ECONNREFUSED\n"},

		// The message is MRI's, byte for byte -- measured against
		// ruby 4.0.7: `Connection refused - connect(2) for "127.0.0.1" port N`.
		{`require "socket"
s = TCPServer.new("127.0.0.1", 0)
port = s.addr[1]
s.close
begin
  TCPSocket.new("127.0.0.1", port)
rescue => e
  puts e.message.sub(port.to_s, "<PORT>")
end`, "Connection refused - connect(2) for \"127.0.0.1\" port <PORT>\n"},

		// Net::HTTP is the path the issue was filed on, and it reaches the peer
		// through a different raiser (raiseTransportErr); both had to change.
		{`require "net/http"
require "socket"
s = TCPServer.new("127.0.0.1", 0)
port = s.addr[1]
s.close
begin
  Net::HTTP.get(URI("http://127.0.0.1:#{port}/"))
rescue => e
  puts e.class
end`, "Errno::ECONNREFUSED\n"},
	})
}

// TestANameThatDoesNotResolveIsNotAConnectFailure: the other half. MRI has two
// classes here and rbgo had one, so a test that only checked the errno case
// could be satisfied by raising Errno::ECONNREFUSED for everything.
func TestANameThatDoesNotResolveIsNotAConnectFailure(t *testing.T) {
	checkCases(t, []runCase{
		{`require "socket"
begin
  TCPSocket.new("no-such-host.invalid", 80)
  puts "NO RAISE"
rescue => e
  puts e.class
end`, "Socket::ResolutionError\n"},

		// It is a SUBCLASS of SocketError, so every `rescue SocketError` written
		// before this change still catches a resolution failure. Without this
		// case the change would be silently breaking.
		{`require "socket"
begin
  TCPSocket.new("no-such-host.invalid", 80)
rescue SocketError => e
  puts "caught: #{e.class}"
end`, "caught: Socket::ResolutionError\n"},

		{`require "socket"; p Socket::ResolutionError.ancestors.include?(SocketError)`, "true\n"},
	})
}
