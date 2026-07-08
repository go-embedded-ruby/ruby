// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-ruby-webmock/webmock"
)

// The WebMock binding is proven in-process: a Ruby program declares stubs through
// the WebMock module and then drives the bound Net::HTTP, whose transport is
// intercepted BEFORE any socket is opened (webmockIntercept), so every assertion
// here is hermetic — no real network, which is the whole point of webmock.
// webmock.Default is process-global, so each script resets it and re-declares its
// net-connect policy up front.

// wmReset is the preamble every WebMock script shares: activate the binding, clear
// the process-wide registry, and refuse unstubbed traffic (webmock's default).
const wmReset = `require "net/http"
require "webmock"
WebMock.reset!
WebMock.disable_net_connect!
`

// TestWebMockStubbedResponse proves a to_return stub answers the bound Net::HTTP
// entirely in-process: status/body/content-type come back, and the request is
// recorded for the bare assert_requested (installed by require "webmock/minitest").
func TestWebMockStubbedResponse(t *testing.T) {
	t.Cleanup(webmock.Reset)
	got := runSrc(t, `require "net/http"
require "webmock/minitest"
WebMock.reset!
WebMock.disable_net_connect!
WebMock.stub_request(:get, "http://example.com/").
  to_return(status: 200, body: "hello", headers: {"Content-Type" => "text/plain"})
resp = Net::HTTP.get_response("http://example.com/")
puts resp.code
puts resp.body
puts resp.content_type
assert_requested(:get, "http://example.com/")
assert_not_requested(:post, "http://example.com/")
puts "ok"`)
	want := "200\nhello\ntext/plain\nok"
	if got != want {
		t.Fatalf("stubbed response:\n got %q\nwant %q", got, want)
	}
}

// TestWebMockConstraints proves .with(headers:, body:, query:) narrows the match,
// the explicit Content-Length header path in webmockBuildResponse, and the
// times:-counting WebMock.assert_requested over two identical requests.
func TestWebMockConstraints(t *testing.T) {
	t.Cleanup(webmock.Reset)
	got := runSrc(t, wmReset+`WebMock.stub_request(:post, "http://example.com/p").
  with(headers: {"X-Probe" => "v"}, body: "payload", query: {"q" => "x"}).
  to_return(status: 201, body: "created", headers: {"Content-Length" => "7"})
http = Net::HTTP.new("example.com")
2.times do
  r = http.post("/p?q=x", "payload", {"X-Probe" => "v"})
  puts "#{r.code}:#{r.body}"
end
WebMock.assert_requested(:post, "http://example.com/p", times: 2, body: "payload", query: {"q" => "x"})
WebMock.assert_not_requested(:get, "http://example.com/p")
puts "ok"`)
	want := "201:created\n201:created\nok"
	if got != want {
		t.Fatalf("constraints:\n got %q\nwant %q", got, want)
	}
}

// TestWebMockQuery proves the query: constraint matches a request whose URL
// carries that query string.
func TestWebMockQuery(t *testing.T) {
	t.Cleanup(webmock.Reset)
	got := runSrc(t, wmReset+`WebMock.stub_request(:get, "http://example.com/s").
  with(query: {"q" => "ruby"}).to_return(body: "found")
puts Net::HTTP.get_response("http://example.com/s?q=ruby").body`)
	if got != "found" {
		t.Fatalf("query match = %q, want %q", got, "found")
	}
}

// TestWebMockToRaise proves every to_raise argument form maps to the right raised
// class/message through the *RaiseError arm of the interception.
func TestWebMockToRaise(t *testing.T) {
	t.Cleanup(webmock.Reset)
	got := runSrc(t, wmReset+`class MyErr < StandardError; end
def probe(stub_arg = :__none__)
  WebMock.reset!
  if stub_arg == :__none__
    WebMock.stub_request(:get, "http://example.com/").to_raise
  else
    WebMock.stub_request(:get, "http://example.com/").to_raise(stub_arg)
  end
  begin
    Net::HTTP.get_response("http://example.com/")
    "NORAISE"
  rescue => e
    "#{e.class}:#{e.message}"
  end
end
puts probe(MyErr)
puts probe("boom")
puts probe(123)
puts probe`)
	want := "MyErr:Exception from WebMock\nRuntimeError:boom\nRuntimeError:123\nRuntimeError:Exception from WebMock"
	if got != want {
		t.Fatalf("to_raise forms:\n got %q\nwant %q", got, want)
	}
}

// TestWebMockToTimeout proves a to_timeout stub raises Net::OpenTimeout (MRI's
// connect timeout) without touching a socket.
func TestWebMockToTimeout(t *testing.T) {
	t.Cleanup(webmock.Reset)
	got := runSrc(t, wmReset+`WebMock.stub_request(:get, "http://example.com/").to_timeout
begin
  Net::HTTP.get_response("http://example.com/")
  puts "NORAISE"
rescue Net::OpenTimeout => e
  puts "timeout:#{e.message}"
end`)
	if got != "timeout:execution expired" {
		t.Fatalf("to_timeout = %q", got)
	}
}

// TestWebMockNoStub proves an unregistered request with net connections disabled
// raises WebMock::NetConnectNotAllowedError carrying the diff.
func TestWebMockNoStub(t *testing.T) {
	t.Cleanup(webmock.Reset)
	got := runSrc(t, wmReset+`begin
  Net::HTTP.get_response("http://nope.example/")
  puts "NORAISE"
rescue WebMock::NetConnectNotAllowedError
  puts "blocked"
end`)
	if got != "blocked" {
		t.Fatalf("no-stub = %q, want blocked", got)
	}
}

// TestWebMockAllowNetConnect proves that with allow_net_connect! an unstubbed
// request falls through to the real transport (handled=false), reaching a live
// loopback server.
func TestWebMockAllowNetConnect(t *testing.T) {
	t.Cleanup(webmock.Reset)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "real-server")
	}))
	defer ts.Close()
	got := runSrc(t, fmt.Sprintf(`require "net/http"
require "webmock"
WebMock.reset!
WebMock.allow_net_connect!
puts Net::HTTP.get_response(%q).body`, ts.URL+"/"))
	if got != "real-server" {
		t.Fatalf("passthrough = %q, want real-server", got)
	}
}

// TestWebMockAssertionFailures proves assert_requested / assert_not_requested
// raise Minitest::Assertion on a count mismatch, and pass otherwise.
func TestWebMockAssertionFailures(t *testing.T) {
	t.Cleanup(webmock.Reset)
	got := runSrc(t, `require "net/http"
require "webmock/minitest"
WebMock.reset!
WebMock.disable_net_connect!
WebMock.stub_request(:get, "http://example.com/").to_return(body: "x")
Net::HTTP.get_response("http://example.com/")
begin
  assert_requested(:get, "http://example.com/", times: 2)
  puts "NORAISE"
rescue Minitest::Assertion
  puts "reqfail"
end
begin
  assert_not_requested(:get, "http://example.com/")
  puts "NORAISE"
rescue Minitest::Assertion
  puts "notreqfail"
end
puts "ok"`)
	want := "reqfail\nnotreqfail\nok"
	if got != want {
		t.Fatalf("assertion failures:\n got %q\nwant %q", got, want)
	}
}

// TestWebMockToggles exercises enable! / disable! / reset! and the stub object's
// value protocol (to_s / inspect / truthiness).
func TestWebMockToggles(t *testing.T) {
	t.Cleanup(webmock.Reset)
	got := runSrc(t, `require "webmock"
WebMock.reset!
WebMock.enable!
WebMock.disable!
WebMock.enable!
s = WebMock.stub_request(:get, "http://x/")
puts s
p s
puts(s ? "truthy" : "falsy")`)
	want := "#<WebMock::RequestStub>\n#<WebMock::RequestStub>\ntruthy"
	if got != want {
		t.Fatalf("toggles:\n got %q\nwant %q", got, want)
	}
}

// TestWebMockBuildResponseBadHeader covers the defensive arm of webmockBuildResponse:
// a stub whose header value injects a CR/LF makes the synthesised bytes unparseable,
// surfacing as Net::HTTPBadResponse rather than a malformed Ruby response.
func TestWebMockBuildResponseBadHeader(t *testing.T) {
	vm := New(io.Discard)
	wantRaise(t, "Net::HTTPBadResponse", func() {
		vm.webmockBuildResponse(webmock.StubResponse{
			Status:  200,
			Headers: map[string][]string{"X-Bad": {"a\r\nGARBAGE-NO-COLON"}},
		})
	})
}

// TestWebMockRubyErrorMessage covers the error string of the internal to_raise
// carrier (it implements error so the engine can hold it).
func TestWebMockRubyErrorMessage(t *testing.T) {
	e := &webmockRubyError{class: "MyErr", message: "boom"}
	if got := e.Error(); got != "MyErr: boom" {
		t.Fatalf("Error() = %q, want %q", got, "MyErr: boom")
	}
}
