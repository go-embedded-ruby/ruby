// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestGRPC covers the Ruby GRPC module (backed by github.com/go-ruby-grpc/grpc
// over google.golang.org/grpc): the status-code constants, the exception tree,
// the GRPC::Service definition DSL, and the RpcServer / ClientStub lifecycle. The
// full client<->server round trips over the in-process bufconn transport are in
// grpc_roundtrip_test.go; here are the parts that raise or read metadata without
// a live call.
func TestGRPC(t *testing.T) {
	const req = `require "grpc"; `
	for _, c := range []struct{ src, want string }{
		// Status-code constants (GRPC::Core::StatusCodes).
		{`p GRPC::Core::StatusCodes::OK`, "0\n"},
		{`p GRPC::Core::StatusCodes::INVALID_ARGUMENT`, "3\n"},
		{`p GRPC::Core::StatusCodes::UNAUTHENTICATED`, "16\n"},

		// Exception tree: GRPC::Error < StandardError, GRPC::BadStatus < GRPC::Error,
		// per-code subclasses < GRPC::BadStatus, Core::CallError < GRPC::Error.
		{`p(GRPC::Error.ancestors.include?(StandardError))`, "true\n"},
		{`p(GRPC::BadStatus.ancestors.include?(GRPC::Error))`, "true\n"},
		{`p(GRPC::InvalidArgument.ancestors.include?(GRPC::BadStatus))`, "true\n"},
		{`p(GRPC::NotFound.ancestors.include?(GRPC::BadStatus))`, "true\n"},
		{`p(GRPC::Core::CallError.ancestors.include?(GRPC::Error))`, "true\n"},

		// GRPC::BadStatus.new(code, details, metadata) and its readers.
		{`e = GRPC::BadStatus.new(3, "bad", {"k" => "v"}); p [e.code, e.details, e.metadata]`,
			"[3, \"bad\", {\"k\" => \"v\"}]\n"},
		{`e = GRPC::BadStatus.new(5, "nf"); p e.message`, "\"5:nf\"\n"},
		{`e = GRPC::BadStatus.new(5); p e.details`, "\"\"\n"},
		{`e = GRPC::BadStatus.new(5); p e.metadata`, "{}\n"},

		// A per-code subclass implies its code (no code argument).
		{`e = GRPC::InvalidArgument.new("oops"); p [e.code, e.details]`, "[3, \"oops\"]\n"},
		{`e = GRPC::NotFound.new; p e.code`, "5\n"},

		// BadStatus#to_status returns a GRPC::Status carrying the triple.
		{`s = GRPC::BadStatus.new(4, "late", {"a" => "b"}).to_status; p [s.class, s.code, s.details, s.metadata]`,
			"[GRPC::Status, 4, \"late\", {\"a\" => \"b\"}]\n"},

		// raise / rescue a BadStatus subclass.
		{`begin; raise GRPC::InvalidArgument.new("x"); rescue GRPC::BadStatus => e; p [e.class, e.code]; end`,
			"[GRPC::InvalidArgument, 3]\n"},
		{`begin; raise GRPC::Core::CallError.new("boom"); rescue GRPC::Error => e; p e.message; end`,
			"\"boom\"\n"},

		// BadStatus.new arity error.
		{`begin; GRPC::BadStatus.new; rescue ArgumentError; p :argerr; end`, ":argerr\n"},

		// GRPC::Service definition DSL.
		{`svc = GRPC::Service.new("t.S") { |s| s.request_response("U") { |r,c| r } }; p svc.name`, "\"t.S\"\n"},
		{`svc = GRPC::Service.new("t.S"); p svc.class`, "GRPC::Service\n"},
		{`svc = GRPC::Service.new("t.S") { |s| s.rpc("U", :request_response) { |r,c| r } }; p svc.name`, "\"t.S\"\n"},
		{`svc = GRPC::Service.new("t.S"); svc.marshal = ->(v){v}; svc.unmarshal = ->(b){b}; p svc.class`, "GRPC::Service\n"},

		// Service DSL argument errors.
		{`begin; GRPC::Service.new; rescue ArgumentError; p :svc; end`, ":svc\n"},
		{`begin; GRPC::Service.new("t") { |s| s.rpc("U") { } }; rescue ArgumentError; p :rpc; end`, ":rpc\n"},
		{`begin; GRPC::Service.new("t") { |s| s.rpc("U", :request_response) }; rescue ArgumentError; p :noblk; end`, ":noblk\n"},
		{`begin; GRPC::Service.new("t") { |s| s.rpc("U", :bad) { |a,b| } }; rescue ArgumentError; p :badtype; end`, ":badtype\n"},
		{`begin; GRPC::Service.new("t") { |s| s.request_response }; rescue ArgumentError; p :noargs; end`, ":noargs\n"},
		{`begin; GRPC::Service.new("t") { |s| s.server_streamer("U") }; rescue ArgumentError; p :noblk2; end`, ":noblk2\n"},

		// RpcServer construction + accessors.
		{`p GRPC::RpcServer.new.class`, "GRPC::RpcServer\n"},
		{`s = GRPC::RpcServer.new; p s.add_http2_port("bufnet:1", ":this_port_is_insecure")`, "\"bufnet:1\"\n"},
		{`s = GRPC::RpcServer.new; p s.add_http2_port("bufnet:1")`, "\"bufnet:1\"\n"},
		{`begin; GRPC::RpcServer.new.add_http2_port; rescue ArgumentError; p :port; end`, ":port\n"},
		{`s = GRPC::RpcServer.new; begin; s.handle(42); rescue TypeError; p :handle; end`, ":handle\n"},
		{`p GRPC::RpcServer.new.running?`, "false\n"},

		// ClientStub construction + errors.
		{`p GRPC::ClientStub.new("bufnet:1", ":this_channel_is_insecure").class`, "GRPC::ClientStub\n"},
		{`p GRPC::ClientStub.new("bufnet:1").class`, "GRPC::ClientStub\n"},
		{`p GRPC::ClientStub.new("bufnet:1", ":c", timeout: 2).class`, "GRPC::ClientStub\n"},
		{`p GRPC::ClientStub.new("bufnet:1", ":c", other: 1).class`, "GRPC::ClientStub\n"},
		{`begin; GRPC::ClientStub.new; rescue ArgumentError; p :stub; end`, ":stub\n"},
		{`begin; GRPC::ClientStub.new("bad\x00host"); rescue GRPC::BadStatus; p :dialerr; end`, ":dialerr\n"},
		{`begin; GRPC::ClientStub.new("h"); rescue; end; p :ok`, ":ok\n"},

		// Non-integer status code, a marshal proc that yields a non-String, and a
		// non-number / good deadline all resolve before any network use.
		{`begin; GRPC::BadStatus.new("x"); rescue TypeError; p :code; end`, ":code\n"},
		{`s = GRPC::ClientStub.new("bufnet:1"); begin; s.request_response("/m", "x", marshal: ->(v){123}); rescue TypeError; p :m; end`, ":m\n"},
		{`s = GRPC::ClientStub.new("bufnet:1"); begin; s.request_response("/m", "x", deadline: "bad"); rescue TypeError; p :dl; end`, ":dl\n"},

		// Call-argument arity / type errors (before any network use).
		{`s = GRPC::ClientStub.new("bufnet:1"); begin; s.request_response("/m"); rescue ArgumentError; p :arity; end`, ":arity\n"},
		{`s = GRPC::ClientStub.new("bufnet:1"); begin; s.client_streamer("/m"); rescue ArgumentError; p :arity2; end`, ":arity2\n"},
		{`s = GRPC::ClientStub.new("bufnet:1"); begin; s.client_streamer("/m", "notarray"); rescue TypeError; p :notarr; end`, ":notarr\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
