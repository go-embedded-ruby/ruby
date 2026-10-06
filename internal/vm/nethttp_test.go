// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Net::HTTP is registered as a loadable shell: the header surface and the
// HTTPResponse accessors are real, while all networking methods raise
// NotImplementedError. Behaviour is asserted against MRI Ruby 4.0.5 (where the
// shell differs from MRI, e.g. header values stored as scalars rather than
// arrays, we assert this implementation's documented behaviour).

// TestNetHTTPHeaderSurface covers Net::HTTPHeader's []/[]=/key?/delete over the
// downcased @header map, including the read-miss and key?-miss arms.
func TestNetHTTPHeaderSurface(t *testing.T) {
	cases := []struct{ src, want string }{
		// []= stores under the downcased key; [] is case-insensitive.
		{`require "net/http"; r=Net::HTTP::Get.new("/"); r["Content-Type"]="text/x"; p r["content-type"]`, "\"text/x\"\n"},
		{`require "net/http"; r=Net::HTTP::Get.new("/"); r["Content-Type"]="text/x"; p r["CONTENT-TYPE"]`, "\"text/x\"\n"},
		// [] returns nil for an absent header (the !ok arm).
		{`require "net/http"; r=Net::HTTP::Get.new("/"); p r["missing"]`, "nil\n"},
		// key? hits both arms.
		{`require "net/http"; r=Net::HTTP::Get.new("/"); r["A"]="1"; p r.key?("a")`, "true\n"},
		{`require "net/http"; r=Net::HTTP::Get.new("/"); p r.key?("nope")`, "false\n"},
		// delete returns the stored value, then the key is gone.
		{`require "net/http"; r=Net::HTTP::Get.new("/"); r["A"]="1"; p r.delete("a")`, "\"1\"\n"},
		{`require "net/http"; r=Net::HTTP::Get.new("/"); r["A"]="1"; r.delete("a"); p r["a"]`, "nil\n"},
		// []= returns its assigned value (the rvalue).
		{`require "net/http"; r=Net::HTTP::Get.new("/"); p(r["A"]="z")`, "\"z\"\n"},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); strings.TrimRight(got, "\n") != strings.TrimRight(c.want, "\n") {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNetHTTPHeaderLazyInit covers the @header lazy-allocation arm: Net::HTTP.new
// and Net::HTTPResponse.new build an object without an @header Hash, so the first
// header access must create it.
func TestNetHTTPHeaderLazyInit(t *testing.T) {
	cases := []struct{ src, want string }{
		// Net::HTTP.new has no @header -> [] lazy-inits (read miss path).
		{`require "net/http"; h=Net::HTTP.new; p h["x-foo"]`, "nil\n"},
		// then []= on the freshly-created hash works.
		{`require "net/http"; h=Net::HTTP.new; h["X-Foo"]="bar"; p h["x-foo"]`, "\"bar\"\n"},
		// Net::HTTPResponse.new also lacks @header; []= lazy-inits then reads back.
		{`require "net/http"; r=Net::HTTPResponse.new; r["A"]="1"; p r["a"]`, "\"1\"\n"},
		{`require "net/http"; r=Net::HTTPResponse.new; p r.key?("a")`, "false\n"},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); strings.TrimRight(got, "\n") != strings.TrimRight(c.want, "\n") {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNetHTTPHeaderTypeError exercises the defensive non-object-receiver arm of
// hashOf directly: it cannot be reached from Ruby (header methods only bind to
// RObject instances), so call the native with a non-RObject self.
func TestNetHTTPHeaderTypeError(t *testing.T) {
	vm := New(io.Discard)
	net := vm.consts["Net"].(*RClass)
	header := net.consts["HTTPHeader"].(*RClass)
	get := header.methods["[]"]
	if get == nil || get.native == nil {
		t.Fatalf("Net::HTTPHeader#[] native not found")
	}
	wantRaise(t, "TypeError", func() {
		get.native(vm, object.Integer(1), []object.Value{object.NewString("x")}, nil)
	})
}

// TestNetHTTPResponseAccessors covers code/message/body, which read the @code,
// @message and @body ivars (nil when unset).
func TestNetHTTPResponseAccessors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "net/http"; r=Net::HTTPResponse.new; p [r.code, r.message, r.body]`, "[nil, nil, nil]\n"},
		{`require "net/http"; r=Net::HTTPResponse.new
r.instance_variable_set(:@code, "200")
r.instance_variable_set(:@message, "OK")
r.instance_variable_set(:@body, "hi")
p [r.code, r.message, r.body]`, "[\"200\", \"OK\", \"hi\"]\n"},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); strings.TrimRight(got, "\n") != strings.TrimRight(c.want, "\n") {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNetHTTPNewAndVerbClasses covers Net::HTTP.new (returns a bare object) and
// each request-verb class's #new (which seeds an @header Hash).
func TestNetHTTPNewAndVerbClasses(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "net/http"; p Net::HTTP.new.is_a?(Net::HTTP)`, "true\n"},
		// Every verb class builds a header-carrying instance.
		{`require "net/http"
%w[Get Head Post Put Delete Patch Options].each do |v|
  o = Net::HTTP.const_get(v).new("/")
  o["X"]="1"
  print o["x"]
end
puts`, "1111111\n"},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); strings.TrimRight(got, "\n") != strings.TrimRight(c.want, "\n") {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNetHTTPResponseTree covers the status subclass tree: each concrete status
// class is a subclass of its category, and the categories of HTTPResponse.
func TestNetHTTPResponseTree(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "net/http"; p Net::HTTPOK.new.is_a?(Net::HTTPSuccess)`, "true\n"},
		{`require "net/http"; p Net::HTTPCreated.new.is_a?(Net::HTTPSuccess)`, "true\n"},
		{`require "net/http"; p Net::HTTPNoContent.new.is_a?(Net::HTTPSuccess)`, "true\n"},
		{`require "net/http"; p Net::HTTPMovedPermanently.new.is_a?(Net::HTTPRedirection)`, "true\n"},
		{`require "net/http"; p Net::HTTPFound.new.is_a?(Net::HTTPRedirection)`, "true\n"},
		{`require "net/http"; p Net::HTTPSeeOther.new.is_a?(Net::HTTPRedirection)`, "true\n"},
		{`require "net/http"; p Net::HTTPNotModified.new.is_a?(Net::HTTPRedirection)`, "true\n"},
		{`require "net/http"; p Net::HTTPBadRequest.new.is_a?(Net::HTTPClientError)`, "true\n"},
		{`require "net/http"; p Net::HTTPUnauthorized.new.is_a?(Net::HTTPClientError)`, "true\n"},
		{`require "net/http"; p Net::HTTPForbidden.new.is_a?(Net::HTTPClientError)`, "true\n"},
		{`require "net/http"; p Net::HTTPNotFound.new.is_a?(Net::HTTPClientError)`, "true\n"},
		{`require "net/http"; p Net::HTTPNotAcceptable.new.is_a?(Net::HTTPClientError)`, "true\n"},
		{`require "net/http"; p Net::HTTPInternalServerError.new.is_a?(Net::HTTPServerError)`, "true\n"},
		{`require "net/http"; p Net::HTTPBadGateway.new.is_a?(Net::HTTPServerError)`, "true\n"},
		{`require "net/http"; p Net::HTTPServiceUnavailable.new.is_a?(Net::HTTPServerError)`, "true\n"},
		{`require "net/http"; p Net::HTTPGatewayTimeout.new.is_a?(Net::HTTPServerError)`, "true\n"},
		// HTTPInformation and HTTPUnknownResponse are direct HTTPResponse subclasses.
		{`require "net/http"; p Net::HTTPInformation.new.is_a?(Net::HTTPResponse)`, "true\n"},
		{`require "net/http"; p Net::HTTPUnknownResponse.new.is_a?(Net::HTTPResponse)`, "true\n"},
		// Every category is itself an HTTPResponse.
		{`require "net/http"; p [Net::HTTPSuccess, Net::HTTPRedirection, Net::HTTPClientError, Net::HTTPServerError].all? { |c| c.ancestors.include?(Net::HTTPResponse) }`, "true\n"},
	}
	for _, c := range cases {
		if got := runSrc(t, c.src); strings.TrimRight(got, "\n") != strings.TrimRight(c.want, "\n") {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNetHTTPErrorClasses covers the Net error/timeout class tree (each a
// StandardError subclass).
func TestNetHTTPErrorClasses(t *testing.T) {
	for _, name := range []string{
		"HTTPError", "HTTPBadResponse", "HTTPFatalError",
		"OpenTimeout", "ReadTimeout", "WriteTimeout",
	} {
		src := `require "net/http"; p Net::` + name + ` < StandardError`
		if got := runSrc(t, src); strings.TrimRight(got, "\n") != "true" {
			t.Errorf("Net::%s < StandardError: got %q", name, got)
		}
	}
}

// The networking methods that once raised NotImplementedError (Net::HTTP.get /
// get_response / start / request and the instance verbs) are now real; their
// behaviour is proven end-to-end against in-process httptest servers in
// nethttp_bind_test.go.

// TestNetHTTPAgainstAnInVMServerThread is the regression witness for #771.
//
// Net::HTTP against a server living in one of the program's own Threads hung
// WITHOUT BOUND: MRI ran the same program in 81 ms, and the probe that first
// showed it was killed at a 30-minute limit still in the same state. Since
// ruby.Run takes no context and exposes no cancellation, an embedder lost that
// goroutine permanently, with no error and no way to interrupt it.
//
// The cause was that nethttpExchangeFramed held the emulated GVL across the
// whole round trip, so the server Thread could never be scheduled. What
// localised it was the contrast with socket_test.go's loopback tests: a raw
// TCPSocket client against the same shape of in-VM server always worked,
// because socket.go routes its reads and writes through ioBlock while the
// Net::HTTP binding routed nothing.
//
// The fix was ablated rather than assumed. Wrapping the exchange ALONE makes
// this pass, so the dial is deliberately left unwrapped: a second witness with
// the server sleeping 0.5 s before accept also passes (541 ms here against
// 554 ms under MRI), because the TCP connect completes into the listen backlog
// without the Ruby accept having run. One wrap, one defect.
//
// The deadline is the point of the shape. A regression here does not FAIL, it
// HANGS -- and an unbounded hang inside `go test` burns the package's whole
// timeout and reports "panic: test timed out" with the cause buried in a
// goroutine dump. Bounding it turns that into one legible line.
func TestNetHTTPAgainstAnInVMServerThread(t *testing.T) {
	src := `
require "socket"
require "net/http"
srv = TCPServer.new("127.0.0.1", 0)
port = srv.addr[1]
t = Thread.new do
  c = srv.accept
  c.readpartial(4096)
  c.write("HTTP/1.1 200 OK\r\nContent-Length: 7\r\n\r\nNETHTTP")
  c.close
end
body = Net::HTTP.get(URI("http://127.0.0.1:#{port}/"))
t.join
srv.close
puts body`

	done := make(chan string, 1)
	go func() { done <- runSrc(t, src) }()
	select {
	case got := <-done:
		if got != "NETHTTP" {
			t.Fatalf("Net::HTTP against an in-VM server got %q, want \"NETHTTP\"", got)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("Net::HTTP against an in-VM server did not finish in 60s: the exchange is holding the GVL again, so the server Thread cannot be scheduled (#771)")
	}
}
