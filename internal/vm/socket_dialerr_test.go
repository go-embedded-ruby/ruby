// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"runtime"
	"testing"
)

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

		// The SHAPE of the message, asserted everywhere: whatever the platform
		// calls a refused connection, the peer it failed against must be in there.
		// A message that names no peer leaves a caller unable to say which one.
		{`require "socket"
s = TCPServer.new("127.0.0.1", 0)
port = s.addr[1]
s.close
begin
  TCPSocket.new("127.0.0.1", port)
rescue => e
  puts e.message.end_with?("- connect(2) for \"127.0.0.1\" port #{port}")
end`, "true\n"},

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

// TestForkExitStopsTheChildNotTheProgram (#774, the SystemExit half).
//
// `fork { exit 7 }` ended the WHOLE program at the fork call: the line after
// the fork never ran and the process exited 7. MRI runs on and reports 7
// through Process.wait2, because under a real fork the SystemExit unwinds the
// CHILD. rbgo runs the block in-process by design, so the block IS the child,
// and the child's exit is runForkBlock returning the status -- not a panic
// escaping to the top.
//
// This does not close #774: the other half, an UNCAUGHT exception in the block,
// still takes the parent down (MRI prints it in the child and exits 1). That
// needs an exception renderer inside the VM, which lives in the CLI today.
func TestForkExitStopsTheChildNotTheProgram(t *testing.T) {
	checkCases(t, []runCase{
		{`pid = fork { exit 7 }
_, st = Process.wait2(pid)
puts "status=#{st.exitstatus} parent-alive"`, "status=7 parent-alive\n"},
		{`pid = fork { exit! 3 }
_, st = Process.wait2(pid)
puts "status=#{st.exitstatus} parent-alive"`, "status=3 parent-alive\n"},
		// abort raises SystemExit with EXIT_FAILURE, so it travels the same path.
		{`pid = fork { abort }
_, st = Process.wait2(pid)
puts "status=#{st.exitstatus} parent-alive"`, "status=1 parent-alive\n"},
		// A block that returns normally is still 0 -- a catch-everything recover
		// would pass every case above while breaking this one.
		{`pid = fork { 1 + 1 }
_, st = Process.wait2(pid)
puts "status=#{st.exitstatus}"`, "status=0\n"},
	})
}

// TestAConnectFailureMessageIsMRIsWording pins the exact bytes, and only where
// they were measured.
//
// `Connection refused - connect(2) for "127.0.0.1" port N` is MRI 4.0.7's
// message, witnessed on darwin. The windows wording was never measured against
// a Windows MRI here -- there is no Windows host on this machine, and the local
// MRI source extract has no win32/win32.c and no version stamp either -- so
// asserting it there would be asserting a guess.
//
// What IS asserted on every platform is the class (Errno::ECONNREFUSED) and the
// shape of the message, above. Scoping a claim to where it was measured is not
// the same as loosening it: the claim that was measured stays exact.
func TestAConnectFailureMessageIsMRIsWording(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("MRI's Windows wording for a refused connect was not measured here; the class and the message shape are asserted for every platform in TestAConnectFailureKeepsItsErrno")
	}
	checkCases(t, []runCase{
		{`require "socket"
s = TCPServer.new("127.0.0.1", 0)
port = s.addr[1]
s.close
begin
  TCPSocket.new("127.0.0.1", port)
rescue => e
  puts e.message.sub(port.to_s, "<PORT>")
end`, "Connection refused - connect(2) for \"127.0.0.1\" port <PORT>\n"},
	})
}
