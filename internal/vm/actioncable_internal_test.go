// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// acRun runs src on a fresh VM and returns its trimmed stdout, failing on a
// parse/compile/run error.
func acRun(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	if _, err := New(&buf).Run(iseq); err != nil {
		t.Fatalf("run: %v\nsrc:\n%s", err, src)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// acRunErr runs src and returns the uncaught error's class, or "" if it ran
// clean.
func acRunErr(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, rerr := New(io.Discard).Run(iseq)
	if rerr == nil {
		return ""
	}
	if i := strings.Index(rerr.Error(), ": "); i > 0 {
		return rerr.Error()[:i]
	}
	return rerr.Error()
}

// TestActionCableHandleInspect covers the internal acHandle value's
// string/truthy surface.
func TestActionCableHandleInspect(t *testing.T) {
	h := acHandle{}
	if h.ToS() != "#<ActionCable::Handle>" || h.Inspect() != "#<ActionCable::Handle>" || !h.Truthy() {
		t.Errorf("acHandle: ToS=%q Inspect=%q Truthy=%v", h.ToS(), h.Inspect(), h.Truthy())
	}
}

// TestActionCableHelpers covers the standalone conversion helpers' every branch.
func TestActionCableHelpers(t *testing.T) {
	// acArg in-range and out-of-range.
	if acArg([]object.Value{object.IntValue(7)}, 0) != object.IntValue(7) {
		t.Error("acArg in-range")
	}
	if acArg(nil, 0) != object.NilV {
		t.Error("acArg out-of-range")
	}
	// acKey String / Symbol / default.
	if acKey(object.NewString("s")) != "s" || acKey(object.Symbol("y")) != "y" || acKey(object.IntValue(3)) != "3" {
		t.Error("acKey")
	}
	// acToGo nil / Symbol / Array / default.
	if acToGo(object.NilV) != nil {
		t.Error("acToGo nil")
	}
	if acToGo(object.Symbol("y")) != "y" {
		t.Error("acToGo symbol")
	}
	if arr, ok := acToGo(object.NewArray(object.IntValue(1))).([]any); !ok || len(arr) != 1 || arr[0] != int64(1) {
		t.Error("acToGo array")
	}
	if got := acToGo(object.NewRange(object.IntValue(1), object.IntValue(2), false)); got != "1..2" {
		t.Errorf("acToGo default = %v", got)
	}
	// acDuration Integer / Float / default.
	if acDuration(object.IntValue(2)) != 2e9 || acDuration(object.Float(0.5)) != 5e8 || acDuration(object.NewString("x")) != 0 {
		t.Error("acDuration")
	}
}

// TestActionCableFeatures covers the require surface and module/class shape.
func TestActionCableFeatures(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "action_cable"`, "true"},
		{`p require "actioncable"`, "true"},
		{`require "action_cable"; p require "action_cable"`, "false"},
		{`require "action_cable"; p ActionCable.is_a?(Module)`, "true"},
		{`require "action_cable"; p ActionCable::Server.class`, "Class"},
		{`require "action_cable"; p ActionCable::Server::Base == ActionCable::Server`, "true"},
		{`require "action_cable"; p ActionCable::Channel::Base < Object`, "true"},
		{`require "action_cable"; p ActionCable::Error < StandardError`, "true"},
		{`require "action_cable"; p ActionCable::Connection::Base.class`, "Class"},
	}
	for _, c := range cases {
		if got := acRun(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// acPrelude is the channel/connection setup shared by the flow tests.
const acPrelude = `require "action_cable"
class ChatChannel < ActionCable::Channel::Base
  def subscribed
    stream_from "room_#{params['room']}"
  end
  def unsubscribed
    ActionCable.server.broadcast("audit", "left")
  end
  def receive(data)
    transmit({ "echo" => data })
  end
  def speak(data)
    ActionCable.server.broadcast("room_#{params['room']}", { "said" => data["message"] })
  end
end
SUB = '{"command":"subscribe","identifier":"{\"channel\":\"ChatChannel\",\"room\":\"1\"}"}'
IDENT = '{"channel":"ChatChannel","room":"1"}'
`

// TestActionCableWireProtocol exercises the full subscribe/broadcast/receive
// lifecycle and asserts the byte-exact frames the library emits are captured.
func TestActionCableWireProtocol(t *testing.T) {
	src := acPrelude + `
server = ActionCable.server
conn = ActionCable::Connection::Base.new(server)
conn.connect
conn.dispatch(SUB)
p conn.subscriptions
server.broadcast("room_1", { "hello" => "world" })
conn.dispatch('{"command":"message","identifier":"' + IDENT.gsub('"', '\\"') + '","data":"{\"n\":1}"}')
conn.dispatch('{"command":"message","identifier":"' + IDENT.gsub('"', '\\"') + '","data":"{\"action\":\"speak\",\"message\":\"hi\"}"}')
conn.transmissions.each { |f| puts f }
`
	got := acRun(t, src)
	want := strings.Join([]string{
		`1`,
		`{"type":"welcome"}`,
		`{"identifier":"{\"channel\":\"ChatChannel\",\"room\":\"1\"}","type":"confirm_subscription"}`,
		`{"identifier":"{\"channel\":\"ChatChannel\",\"room\":\"1\"}","message":{"hello":"world"}}`,
		`{"identifier":"{\"channel\":\"ChatChannel\",\"room\":\"1\"}","message":{"echo":{"n":1}}}`,
		`{"identifier":"{\"channel\":\"ChatChannel\",\"room\":\"1\"}","message":{"said":"hi"}}`,
	}, "\n")
	if got != want {
		t.Errorf("frames mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestActionCableSubscriptionsAndUnsubscribe covers #subscription lookup,
// unsubscribe (firing the unsubscribed hook + broadcast) and #closed?.
func TestActionCableSubscriptionsAndUnsubscribe(t *testing.T) {
	src := acPrelude + `
server = ActionCable::Server.new
conn = ActionCable::Connection::Base.new(server)
conn.connect
conn.dispatch(SUB)
p conn.subscription(IDENT).is_a?(ChatChannel)
p conn.subscription(IDENT).identifier == IDENT
p conn.subscription('{"channel":"Missing"}').nil?
conn.dispatch('{"command":"unsubscribe","identifier":"' + IDENT.gsub('"', '\\"') + '"}')
p conn.subscriptions
p conn.subscription(IDENT).nil?
p conn.closed?
conn.disconnect("server_restart")
p conn.closed?
puts conn.transmissions.last
`
	got := acRun(t, src)
	want := "true\ntrue\ntrue\n0\ntrue\nfalse\ntrue\n" +
		`{"type":"disconnect","reason":"server_restart","reconnect":true}`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestActionCableReject covers a subscription rejected in its subscribed hook:
// no confirmation, a reject_subscription frame, and no active subscription.
func TestActionCableReject(t *testing.T) {
	src := `require "action_cable"
class GuardedChannel < ActionCable::Channel::Base
  def subscribed
    reject
  end
end
server = ActionCable::Server.new
conn = ActionCable::Connection::Base.new(server)
conn.connect
conn.dispatch('{"command":"subscribe","identifier":"{\"channel\":\"GuardedChannel\"}"}')
p conn.subscriptions
puts conn.transmissions.last
`
	got := acRun(t, src)
	want := "0\n" + `{"identifier":"{\"channel\":\"GuardedChannel\"}","type":"reject_subscription"}`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestActionCableBroadcastFanout covers a message broadcast fanning out to two
// connections subscribed to the same broadcasting.
func TestActionCableBroadcastFanout(t *testing.T) {
	src := acPrelude + `
server = ActionCable::Server.new
a = ActionCable::Connection::Base.new(server)
b = ActionCable::Connection::Base.new(server)
[a, b].each { |c| c.connect; c.dispatch(SUB) }
server.broadcast("room_1", "hi")
puts a.transmissions.last
puts b.transmissions.last
`
	got := acRun(t, src)
	line := `{"identifier":"{\"channel\":\"ChatChannel\",\"room\":\"1\"}","message":"hi"}`
	if got != line+"\n"+line {
		t.Errorf("got:\n%s", got)
	}
}

// TestActionCablePeriodic covers both periodically forms driven by the
// deterministic scheduler via #advance / #beat.
func TestActionCablePeriodic(t *testing.T) {
	src := `require "action_cable"
class TickChannel < ActionCable::Channel::Base
  def subscribed
    @n = 0
    periodically(1) { @n += 1; transmit({ "tick" => @n }) }
    periodically(:ping, every: 0.5)
    periodically(:noop)
  end
  def ping
    transmit("pong")
  end
end
server = ActionCable.server
conn = ActionCable::Connection::Base.new(server)
conn.connect
conn.dispatch('{"command":"subscribe","identifier":"{\"channel\":\"TickChannel\"}"}')
conn.beat(7)
conn.advance(2)
conn.advance("bad")
p conn.transmissions.include?('{"type":"ping","message":7}')
puts conn.transmissions.grep(/tick/).length
puts conn.transmissions.grep(/pong/).length
`
	got := acRun(t, src)
	// beat(7) => a heartbeat ping frame; advance(2) fires the 1s block timer
	// twice (tick 1,2) and the 0.5s :ping method four times; :noop (no every) is
	// inert.
	want := strings.Join([]string{
		`true`,
		`2`,
		`4`,
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestActionCableRemoteConnections covers the internal-channel remote disconnect
// path: an identified connection subscribes to its internal channel on connect,
// and remote_connections.where(...).disconnect closes it.
func TestActionCableRemoteConnections(t *testing.T) {
	src := `require "action_cable"
server = ActionCable::Server.new
conn = ActionCable::Connection::Base.new(server)
conn.identified_by("current_user", "42")
conn.identified_by("blank", nil)
conn.connect
p conn.closed?
server.remote_connections.where(current_user: "42", blank: nil).disconnect(reconnect: false)
p conn.closed?
puts conn.transmissions.last
# a where with no matching connection simply broadcasts to no one
server.remote_connections.where(current_user: "999").disconnect
`
	got := acRun(t, src)
	want := "false\ntrue\n" + `{"type":"disconnect","reason":"remote","reconnect":false}`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestActionCableServerSingleton covers the memoized ActionCable.server.
func TestActionCableServerSingleton(t *testing.T) {
	src := `require "action_cable"
p ActionCable.server.equal?(ActionCable.server)
p ActionCable.server.is_a?(ActionCable::Server)
`
	if got := acRun(t, src); got != "true\ntrue" {
		t.Errorf("got:\n%s", got)
	}
}

// TestActionCableStreamForBroadcastTo covers stream_for / broadcast_to, which
// derive the broadcasting from the channel name and a model.
func TestActionCableStreamForBroadcastTo(t *testing.T) {
	src := `require "action_cable"
class RoomChannel < ActionCable::Channel::Base
  def subscribed
    stream_for "1"
  end
  def receive(data)
    broadcast_to("1", data)
  end
end
server = ActionCable::Server.new
conn = ActionCable::Connection::Base.new(server)
conn.connect
conn.dispatch('{"command":"subscribe","identifier":"{\"channel\":\"RoomChannel\"}"}')
ident = '{"channel":"RoomChannel"}'
conn.dispatch('{"command":"message","identifier":"' + ident.gsub('"', '\\"') + '","data":"{\"hi\":true}"}')
puts conn.transmissions.last
`
	got := acRun(t, src)
	want := `{"identifier":"{\"channel\":\"RoomChannel\"}","message":{"hi":true}}`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestActionCableUnknownAction covers dispatching a message whose action has no
// matching channel method (a no-op, per Rails).
func TestActionCableUnknownAction(t *testing.T) {
	src := acPrelude + `
server = ActionCable::Server.new
conn = ActionCable::Connection::Base.new(server)
conn.connect
conn.dispatch(SUB)
before = conn.transmissions.length
conn.dispatch('{"command":"message","identifier":"' + IDENT.gsub('"', '\\"') + '","data":"{\"action\":\"nope\"}"}')
p conn.transmissions.length == before
`
	if got := acRun(t, src); got != "true" {
		t.Errorf("got: %s", got)
	}
}

// TestActionCableErrors covers the raise branches: encode failures (NaN) on
// broadcast/transmit/broadcast_to and every #dispatch error path.
func TestActionCableErrors(t *testing.T) {
	// broadcast with an unencodable value (NaN) raises ActionCable::Error.
	if got := acRunErr(t, `require "action_cable"
ActionCable::Server.new.broadcast("x", 0.0/0.0)`); got != "ActionCable::Error" {
		t.Errorf("broadcast NaN => %q", got)
	}
	// transmit NaN raises through the channel.
	transmitSrc := `require "action_cable"
class BadChannel < ActionCable::Channel::Base
  def subscribed; end
  def receive(data); transmit(0.0/0.0); end
end
server = ActionCable::Server.new
conn = ActionCable::Connection::Base.new(server)
conn.connect
conn.dispatch('{"command":"subscribe","identifier":"{\"channel\":\"BadChannel\"}"}')
ident = '{"channel":"BadChannel"}'
conn.dispatch('{"command":"message","identifier":"' + ident.gsub('"', '\\"') + '","data":"{}"}')`
	if got := acRunErr(t, transmitSrc); got != "ActionCable::Error" {
		t.Errorf("transmit NaN => %q", got)
	}
	// broadcast_to NaN raises through the channel.
	btSrc := strings.Replace(transmitSrc, "transmit(0.0/0.0)", "broadcast_to(\"m\", 0.0/0.0)", 1)
	if got := acRunErr(t, btSrc); got != "ActionCable::Error" {
		t.Errorf("broadcast_to NaN => %q", got)
	}

	base := `require "action_cable"
server = ActionCable::Server.new
conn = ActionCable::Connection::Base.new(server)
conn.connect
`
	dispatchErrs := []string{
		`conn.dispatch("not json")`,                                                        // decode error
		`conn.dispatch('{"command":"bogus","identifier":"x"}')`,                            // unknown command
		`conn.dispatch('{"command":"subscribe","identifier":"not json"}')`,                 // identifier parse error
		`conn.dispatch('{"command":"subscribe","identifier":"{\"channel\":\"Nope\"}"}')`,   // class not found (unknown const)
		`conn.dispatch('{"command":"subscribe","identifier":"{\"channel\":\"String\"}"}')`, // not a channel class
		`conn.dispatch('{"command":"unsubscribe","identifier":"x"}')`,                      // unknown subscription
		`conn.dispatch('{"command":"message","identifier":"x","data":"{}"}')`,              // unknown subscription (message)
	}
	for _, d := range dispatchErrs {
		if got := acRunErr(t, base+d); got != "ActionCable::Error" {
			t.Errorf("dispatch %q => %q, want ActionCable::Error", d, got)
		}
	}

	// A subscribed connection dispatching a message with malformed data JSON.
	dataErr := base + `
class D < ActionCable::Channel::Base
  def subscribed; end
end
conn.dispatch('{"command":"subscribe","identifier":"{\"channel\":\"D\"}"}')
conn.dispatch('{"command":"message","identifier":"{\"channel\":\"D\"}","data":"not json"}')`
	if got := acRunErr(t, dataErr); got != "ActionCable::Error" {
		t.Errorf("message bad data => %q", got)
	}
}
