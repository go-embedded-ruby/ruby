// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// grpcEchoProgram is the shared preamble: it defines an echo service exercising
// all four RPC cardinalities plus metadata, deadline and error behaviours, starts
// a server on the in-process bufconn transport and connects a stub. Because every
// blocking client call releases the GVL, the server's handler goroutine runs the
// Ruby handler in the same VM, so one program acts as both peers. The caller
// appends the assertions and a teardown (stub.close; server.stop).
const grpcEchoProgram = `
require "grpc"
svc = GRPC::Service.new("test.Echo") do |s|
  s.request_response("Unary") do |req, call|
    user = call.metadata["x-user"]
    if user; "hello #{req} from #{user}"
    elsif req == "boom"; raise GRPC::InvalidArgument.new("bad argument")
    elsif req == "panic"; raise "unexpected"
    elsif req == "dl"; call.deadline ? "has-deadline" : "no-deadline"
    else "hello #{req}"
    end
  end
  s.server_streamer("Split") do |req, call|
    req.split(" ").each { |w| call.remote_send("<#{w}>") }
  end
  s.client_streamer("Join") do |call|
    parts = []
    call.each_remote_read { |m| parts << m }
    parts.join(",")
  end
  s.bidi_streamer("Chat") do |call|
    call.each_remote_read { |m| call.remote_send("re:#{m}") }
  end
end
$server = GRPC::RpcServer.new
$server.handle(svc)
$server.add_http2_port("bufnet:1", ":this_port_is_insecure")
$server.run
$stub = GRPC::ClientStub.new("bufnet:1", ":this_channel_is_insecure")
`

const grpcTeardown = "\n$stub.close; $server.stop\n"

// TestGRPCRoundTrip drives real client<->server calls over bufconn for each RPC
// cardinality and asserts the responses, then tears the server down cleanly (no
// leaked serving goroutine).
func TestGRPCRoundTrip(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"unary", `p $stub.request_response("/test.Echo/Unary", "world")`, "\"hello world\"\n"},
		{"unary-metadata", `p $stub.request_response("/test.Echo/Unary", "world", metadata: {"x-user" => "Alice"})`,
			"\"hello world from Alice\"\n"},
		{"unary-deadline", `p $stub.request_response("/test.Echo/Unary", "dl", deadline: 5)`, "\"has-deadline\"\n"},
		{"unary-no-deadline", `p $stub.request_response("/test.Echo/Unary", "dl")`, "\"no-deadline\"\n"},
		{"server-stream", `p $stub.server_streamer("/test.Echo/Split", "a b c")`, "[\"<a>\", \"<b>\", \"<c>\"]\n"},
		{"client-stream", `p $stub.client_streamer("/test.Echo/Join", ["a", "b", "c"])`, "\"a,b,c\"\n"},
		{"bidi-stream", `p $stub.bidi_streamer("/test.Echo/Chat", ["p", "q"])`, "[\"re:p\", \"re:q\"]\n"},

		// Handler error propagation: a raised BadStatus keeps its code; a bare raise
		// becomes UNKNOWN; an unknown method is UNIMPLEMENTED.
		{"unary-badstatus",
			`begin; $stub.request_response("/test.Echo/Unary", "boom"); rescue GRPC::BadStatus => e; p [e.class, e.code, e.details]; end`,
			"[GRPC::InvalidArgument, 3, \"bad argument\"]\n"},
		{"unary-unknown-error",
			`begin; $stub.request_response("/test.Echo/Unary", "panic"); rescue GRPC::BadStatus => e; p e.code; end`, "2\n"},
		{"unimplemented",
			`begin; $stub.request_response("/test.Echo/Nope", "x"); rescue GRPC::BadStatus => e; p e.code; end`, "12\n"},
		{"stream-unimplemented",
			`begin; $stub.server_streamer("/test.Echo/Nope", "x"); rescue GRPC::BadStatus => e; p e.code; end`, "12\n"},
		{"clientstream-unimplemented",
			`begin; $stub.client_streamer("/test.Echo/Nope", ["a"]); rescue GRPC::BadStatus => e; p e.code; end`, "12\n"},
		{"bidi-unimplemented",
			`begin; $stub.bidi_streamer("/test.Echo/Nope", ["a"]); rescue GRPC::BadStatus => e; p e.code; end`, "12\n"},

		// running? is true while serving.
		{"running", `p $server.running?`, "true\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, grpcEchoProgram+c.body+grpcTeardown); got != c.want {
				t.Errorf("got=%q want=%q", got, c.want)
			}
		})
	}
}

// TestGRPCCustomCodec drives a round trip with explicit marshal / unmarshal procs
// (rather than the default String codec), covering the proc branch of the
// message boundary on both the service and the call.
func TestGRPCCustomCodec(t *testing.T) {
	src := `
require "grpc"
svc = GRPC::Service.new("t.Rev") do |s|
  s.marshal = ->(v) { v.reverse }
  s.unmarshal = ->(b) { b.reverse }
  s.request_response("Echo") { |req, call| "seen:#{req}" }
end
$server = GRPC::RpcServer.new
$server.handle(svc)
$server.add_http2_port("bufnet:1")
$server.run
$stub = GRPC::ClientStub.new("bufnet:1")
p $stub.request_response("/t.Rev/Echo", "abc", marshal: ->(v){v.reverse}, unmarshal: ->(b){b.reverse})
$stub.close; $server.stop
`
	if got := eval(t, src); got != "\"seen:abc\"\n" {
		t.Errorf("got=%q want=%q", got, "\"seen:abc\"\n")
	}
}

// TestGRPCRunTillTerminated covers run_till_terminated as the (background)
// server start, mirroring the gem's blocking entry point but returning so the
// same program can drive a client.
func TestGRPCRunTillTerminated(t *testing.T) {
	src := `
require "grpc"
svc = GRPC::Service.new("t.S") { |s| s.request_response("U") { |r,c| "ok:#{r}" } }
$server = GRPC::RpcServer.new
$server.handle(svc)
$server.add_http2_port("bufnet:1")
$server.run_till_terminated
$stub = GRPC::ClientStub.new("bufnet:1")
p $stub.request_response("/t.S/U", "hi")
$stub.close; $server.stop
`
	if got := eval(t, src); got != "\"ok:hi\"\n" {
		t.Errorf("got=%q want=%q", got, "\"ok:hi\"\n")
	}
}

// TestGRPCActiveCallSurface covers the ActiveCall handler surface not touched by
// the echo service: remote_read (drive the request stream one message at a time),
// each_remote_read without a block (raises), and a raising handler for every
// streaming cardinality (so each stream handler's error return is exercised, the
// raise surfacing to the client as a non-OK status).
func TestGRPCActiveCallSurface(t *testing.T) {
	prog := `
require "grpc"
svc = GRPC::Service.new("t.Surface") do |s|
  s.client_streamer("JoinRR") do |call|
    parts = []
    loop do
      m = call.remote_read
      break if m.nil?
      parts << m
    end
    parts.join("-")
  end
  s.client_streamer("CErr") { |call| raise GRPC::Internal.new("client-boom") }
  s.server_streamer("SErr") { |req, call| raise GRPC::Internal.new("server-boom") }
  s.bidi_streamer("BErr") { |call| call.each_remote_read }
end
$server = GRPC::RpcServer.new
$server.handle(svc)
$server.add_http2_port("bufnet:1")
$server.run
$stub = GRPC::ClientStub.new("bufnet:1")
`
	for _, c := range []struct{ name, body, want string }{
		{"remote-read", `p $stub.client_streamer("/t.Surface/JoinRR", ["a", "b", "c"])`, "\"a-b-c\"\n"},
		{"client-stream-raise",
			`begin; $stub.client_streamer("/t.Surface/CErr", ["a"]); rescue GRPC::BadStatus => e; p [e.code, e.details]; end`,
			"[13, \"client-boom\"]\n"},
		{"server-stream-raise",
			`begin; $stub.server_streamer("/t.Surface/SErr", "a"); rescue GRPC::BadStatus => e; p e.code; end`, "13\n"},
		{"bidi-no-block",
			`begin; $stub.bidi_streamer("/t.Surface/BErr", ["a"]); rescue GRPC::BadStatus => e; p e.code; end`, "2\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, prog+c.body+grpcTeardown); got != c.want {
				t.Errorf("got=%q want=%q", got, c.want)
			}
		})
	}
}

// TestGRPCDeadlineFloat covers a floating-point (fractional-second) deadline on a
// unary call.
func TestGRPCDeadlineFloat(t *testing.T) {
	src := `
require "grpc"
svc = GRPC::Service.new("t.S") { |s| s.request_response("U") { |r,c| c.deadline ? "dl" : "no" } }
$server = GRPC::RpcServer.new
$server.handle(svc)
$server.add_http2_port("bufnet:1")
$server.run
$stub = GRPC::ClientStub.new("bufnet:1")
p $stub.request_response("/t.S/U", "x", deadline: 1.5)
$stub.close; $server.stop
`
	if got := eval(t, src); got != "\"dl\"\n" {
		t.Errorf("got=%q want=%q", got, "\"dl\"\n")
	}
}
