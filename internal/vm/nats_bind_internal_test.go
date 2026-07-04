// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	server "github.com/nats-io/nats-server/v2/server"

	nats "github.com/go-ruby-nats/nats"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// startEmbeddedNATS starts an embedded nats-server on an ephemeral 127.0.0.1 port
// (Port -1 = server.RANDOM_PORT) and returns its client URL. The server is shut
// down and awaited in a t.Cleanup, so no serving goroutine or port leaks past the
// test — a leaked server goroutine would hang the whole suite. There is no fixed
// port: every test gets its own random one.
func startEmbeddedNATS(t *testing.T) string {
	t.Helper()
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("embedded nats-server not ready")
	}
	t.Cleanup(func() {
		ns.Shutdown()
		ns.WaitForShutdown()
	})
	return ns.ClientURL()
}

// natsRun runs a Ruby program against an embedded server: it binds a URL constant
// to the server's client URL and returns the program's trimmed stdout. Every
// integration test drives the whole binding through rbgo exactly as a user would.
func natsRun(t *testing.T, body string) string {
	t.Helper()
	url := startEmbeddedNATS(t)
	src := fmt.Sprintf("require \"nats\"\nURL = %q\n%s", url, body)
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
		t.Fatalf("run: %v", err)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// TestNATSRequestReply drives the synchronous request/reply path: a responder
// subscription echoes each request, and #request publishes and waits for the
// reply while releasing the GVL so the responder (delivered on a nats.go
// goroutine, serialized onto the VM) can answer. It covers subscribe, the
// message-delivery seam, Msg#data / #respond, request and close.
func TestNATSRequestReply(t *testing.T) {
	got := natsRun(t, `
nc = NATS.connect(URL)
nc.subscribe("svc.echo") { |m| m.respond("re:" + m.data) }
rep = nc.request("svc.echo", "hi", timeout: 2)
puts rep.data
rep2 = nc.request("svc.echo", "yo")
puts rep2.data
puts rep.subject.class
nc.close
puts nc.closed?
`)
	want := "re:hi\nre:yo\nString\ntrue"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestNATSPubSubQueue drives asynchronous pub/sub deterministically through a
// Thread::Queue: the subscriber block pushes each message onto the queue and the
// main thread pops it (releasing the GVL so the delivery can run) — no fixed
// sleep. It covers publish_msg with headers, Msg.new, the subject/data/headers
// readers, a queue-group subscribe, flush, the Subscription readers/unsubscribe
// and drain.
func TestNATSPubSubQueue(t *testing.T) {
	got := natsRun(t, `
nc = NATS.connect(URL) { |c| c }
q = Queue.new
sub = nc.subscribe("evt") { |m| q.push([m.subject, m.data, m.reply, m.headers["X"]].join(",")) }
nc.publish_msg(NATS::Msg.new(subject: "evt", data: "payload", header: {"X" => "1"}))
nc.flush
puts q.pop
puts sub.subject
sub.unsubscribe

wq = Queue.new
ws = nc.subscribe("work", queue: "q") { |m| wq.push(m.data) }
nc.publish("work", "job1")
nc.flush(1)
puts wq.pop
puts ws.queue
ws.unsubscribe(5)
dsub = nc.subscribe("drainme") { |m| }
dsub.drain
nc.drain
puts "drained"
`)
	want := "evt,payload,,1\nevt\njob1\nq\ndrained"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestNATSErrors covers the error surface: the request no-responders and timeout
// mappings (NATS::IO::NoRespondersError / NATS::Timeout), a connect failure
// (NATS::IO::NoServersError), the argument/type guards, subscribe on a bad
// subject, and the unmatched-error fallback plus Msg#respond's no-reply path
// (both surfacing as the root NATS::Error, raised from inside a delivered block).
func TestNATSErrors(t *testing.T) {
	got := natsRun(t, `
nc = NATS.connect(URL)

begin; nc.request("nobody", "x", timeout: 1); rescue NATS::IO::NoRespondersError; puts "no_responders"; end
nc.subscribe("silent") { |m| }
begin; nc.request("silent", "x", timeout: 0.1); rescue NATS::Timeout; puts "timeout"; end

begin; NATS.connect("nats://127.0.0.1:1"); rescue NATS::IO::NoServersError; puts "no_servers"; end

[->{ nc.publish }, ->{ nc.request }, ->{ nc.subscribe }, ->{ nc.subscribe("x") }, ->{ nc.publish_msg }].each do |f|
  begin; f.call; rescue ArgumentError; print "A"; end
end
puts
begin; nc.publish_msg(5); rescue TypeError; puts "type"; end
begin; nc.subscribe(""); rescue ArgumentError; end
begin; nc.subscribe("") { |m| }; rescue NATS::IO::BadSubject; puts "bad_subject"; end

# A delivered message with no reply subject: Msg#respond raises the root
# NATS::Error (an unmapped library error), rescued inside the block.
rq = Queue.new
nc.subscribe("nr") { |m| begin; m.respond("x"); rescue NATS::Error => e; rq.push(e.class.to_s); end }
nc.publish("nr", "hi")
nc.flush
puts rq.pop

# Msg.new (no connection) #respond also raises via the map (connection closed).
begin; NATS::Msg.new(reply: "r").respond("x"); rescue NATS::IO::ConnectionClosedError; puts "closed"; end

nc.close
`)
	want := "no_responders\ntimeout\nno_servers\nAAAAA\ntype\nbad_subject\nNATS::Error\nclosed"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestNATSPublishCoercionAndClosedErrors covers publish's data-coercion arms (a
// nil, an Integer and a Symbol subject), the reply-carrying publish path
// (PublishRequest, via both the reply: keyword and a positional reply), and the
// post-close error branches of publish / publish_msg / flush / drain / the
// Subscription methods.
func TestNATSPublishCoercionAndClosedErrors(t *testing.T) {
	got := natsRun(t, `
nc = NATS.connect(URL)

# Reply delivered to an inbox subscription, via keyword then positional reply.
# The second publish also exercises a Symbol subject and an Integer payload.
inbox = Queue.new
nc.subscribe("my.reply") { |m| inbox.push(m.data) }
nc.subscribe("q.in") { |m| m.respond("pong") }
nc.publish("q.in", "ping", reply: "my.reply")
nc.flush
puts inbox.pop
nc.subscribe(:"q.in2") { |m| m.respond("pong2") }
nc.publish(:"q.in2", 42, "my.reply")
nc.flush
puts inbox.pop

# A no-op delivery (nil payload) still reaches the block.
seen = Queue.new
nc.subscribe("np") { |m| seen.push(m.data.empty? ? "empty" : m.data) }
nc.publish("np")
nc.flush
puts seen.pop

sub = nc.subscribe("later") { |m| }
nc.close

# Every operation on the closed connection / subscription raises.
{
  "pub"     => ->{ nc.publish("x", "y") },
  "pub_msg" => ->{ nc.publish_msg(NATS::Msg.new(subject: "x")) },
  "flush"   => ->{ nc.flush },
  "flush_t" => ->{ nc.flush(1) },
  "drain"   => ->{ nc.drain },
  "unsub"   => ->{ sub.unsubscribe },
  "sdrain"  => ->{ sub.drain },
}.each { |name, f| begin; f.call; rescue NATS::Error; print "x"; end }
puts
`)
	want := "pong\npong2\nempty\nxxxxxxx"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestNATSDeliverRecovers covers the delivery seam's recover: a subscriber block
// that raises is an asynchronous error that must not crash the nats.go dispatcher
// goroutine — natsDeliver recovers it, the connection stays up and a later
// request/reply still works.
func TestNATSDeliverRecovers(t *testing.T) {
	got := natsRun(t, `
nc = NATS.connect(URL)
nc.subscribe("boom") { |m| raise "kaboom" }
nc.publish("boom", "x")
nc.flush
nc.subscribe("echo") { |m| m.respond(m.data) }
puts nc.request("echo", "alive", timeout: 2).data
nc.close
`)
	if got != "alive" {
		t.Errorf("got %q, want %q", got, "alive")
	}
}

// natsRecover asserts fn raises a Ruby exception of the given class (used for the
// pure helpers that raise directly).
func natsRecover(t *testing.T, wantClass string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		re, ok := r.(RubyError)
		if !ok {
			t.Fatalf("expected a RubyError %s, got %#v", wantClass, r)
		}
		if re.Class != wantClass {
			t.Fatalf("raised %s, want %s", re.Class, wantClass)
		}
	}()
	fn()
}

// TestNATSHelpers covers the pure Go↔Ruby helper functions across every branch —
// including the ones an ordinary program never exercises — deterministically and
// without a server, mirroring how puma_internal_test covers its helpers.
func TestNATSHelpers(t *testing.T) {
	// natsBytes: String, Ruby nil, Go nil, and a stringified fallback.
	if string(natsBytes(object.NewString("hi"))) != "hi" {
		t.Errorf("natsBytes(String)")
	}
	if natsBytes(object.NilV) != nil || natsBytes(nil) != nil {
		t.Errorf("natsBytes(nil)")
	}
	if string(natsBytes(object.IntValue(42))) != "42" {
		t.Errorf("natsBytes(Integer)")
	}
	// natsStr: String and a stringified fallback.
	if natsStr(object.NewString("s")) != "s" || natsStr(object.Symbol("y")) != "y" {
		t.Errorf("natsStr")
	}
	// natsSeconds: Integer, Float and the non-numeric default.
	if natsSeconds(object.IntValue(2)) != 2*time.Second {
		t.Errorf("natsSeconds(Integer)")
	}
	if natsSeconds(object.Float(0.5)) != 500*time.Millisecond {
		t.Errorf("natsSeconds(Float)")
	}
	if natsSeconds(object.NilV) != 0 {
		t.Errorf("natsSeconds(other)")
	}
	// natsInt: Integer and non-Integer.
	if natsInt(object.IntValue(3)) != 3 || natsInt(object.NewString("x")) != 0 {
		t.Errorf("natsInt")
	}
	// natsServers: Array and single String.
	arr := object.NewArrayFromSlice([]object.Value{object.NewString("a"), object.NewString("b")})
	if s := natsServers(arr); len(s) != 2 || s[0] != "a" || s[1] != "b" {
		t.Errorf("natsServers(Array) = %v", s)
	}
	if s := natsServers(object.NewString("u")); len(s) != 1 || s[0] != "u" {
		t.Errorf("natsServers(String) = %v", s)
	}
	// natsKwarg / natsKwHash.
	kw := object.NewHash()
	kw.Set(object.Symbol("timeout"), object.IntValue(5))
	if v := natsKwarg([]object.Value{kw}, "timeout"); v != object.IntValue(5) {
		t.Errorf("natsKwarg(present) = %v", v)
	}
	if natsKwarg([]object.Value{kw}, "absent") != nil {
		t.Errorf("natsKwarg(absent)")
	}
	if natsKwarg(nil, "x") != nil || natsKwarg([]object.Value{object.IntValue(1)}, "x") != nil {
		t.Errorf("natsKwarg(no-hash)")
	}
	if _, ok := natsKwHash(nil); ok {
		t.Errorf("natsKwHash(empty)")
	}
	if _, ok := natsKwHash([]object.Value{object.IntValue(1)}); ok {
		t.Errorf("natsKwHash(non-hash)")
	}
	if _, ok := natsKwHash([]object.Value{kw}); !ok {
		t.Errorf("natsKwHash(hash)")
	}
	// natsReply: keyword, positional, and neither.
	rkw := object.NewHash()
	rkw.Set(object.Symbol("reply"), object.NewString("r1"))
	if natsReply([]object.Value{object.NewString("s"), rkw}) != "r1" {
		t.Errorf("natsReply(keyword)")
	}
	pos := []object.Value{object.NewString("s"), object.NewString("d"), object.NewString("r2")}
	if natsReply(pos) != "r2" {
		t.Errorf("natsReply(positional)")
	}
	if natsReply([]object.Value{object.NewString("s")}) != "" {
		t.Errorf("natsReply(none)")
	}
	// natsHeaderHash: keeps a valued key, skips an empty one.
	hh := natsHeaderHash(nats.Header{"A": {"1"}, "B": {}}).(*object.Hash)
	if v, _ := hh.Get(object.NewString("A")); v.ToS() != "1" {
		t.Errorf("natsHeaderHash A = %v", v)
	}
	if _, ok := hh.Get(object.NewString("B")); ok {
		t.Errorf("natsHeaderHash kept empty B")
	}
	// natsBuildHeader: Hash and non-Hash.
	bh := object.NewHash()
	bh.Set(object.NewString("K"), object.NewString("V"))
	if got := natsBuildHeader(bh); got.Get("K") != "V" {
		t.Errorf("natsBuildHeader(Hash) = %v", got)
	}
	if natsBuildHeader(object.IntValue(1)) != nil {
		t.Errorf("natsBuildHeader(non-Hash)")
	}
}

// TestNATSConnectOptions covers natsConnectOptions across every keyword arm and
// the no-keyword path, asserting the option count so each arm is exercised
// without opening a connection.
func TestNATSConnectOptions(t *testing.T) {
	// No keywords, a leading URL only.
	if got := natsConnectOptions([]object.Value{object.NewString("nats://x")}); len(got) != 1 {
		t.Errorf("URL-only opts = %d, want 1", len(got))
	}
	// Every keyword arm, including user with and without a password.
	full := object.NewHash()
	full.Set(object.Symbol("servers"), object.NewArrayFromSlice([]object.Value{object.NewString("a"), object.NewString("b")}))
	full.Set(object.Symbol("name"), object.NewString("svc"))
	full.Set(object.Symbol("user"), object.NewString("u"))
	full.Set(object.Symbol("password"), object.NewString("p"))
	full.Set(object.Symbol("token"), object.NewString("tok"))
	full.Set(object.Symbol("max_reconnect_attempts"), object.IntValue(3))
	full.Set(object.Symbol("reconnect_time_wait"), object.IntValue(1))
	full.Set(object.Symbol("connect_timeout"), object.Float(2))
	// servers, name, user(+password), token, max_reconnect, reconnect_wait, timeout = 7.
	if got := natsConnectOptions([]object.Value{full}); len(got) != 7 {
		t.Errorf("full opts = %d, want 7", len(got))
	}
	// user without a password still yields one UserInfo option.
	up := object.NewHash()
	up.Set(object.Symbol("user"), object.NewString("u"))
	if got := natsConnectOptions([]object.Value{up}); len(got) != 1 {
		t.Errorf("user-only opts = %d, want 1", len(got))
	}
}

// TestNATSWrapperStrings covers the object.Value ToS/Inspect/Truthy protocol on
// the NATSX wrapper for each Ruby class it reports, and the raiseNATS mapping for
// a known sentinel and an unmapped error.
func TestNATSWrapperStrings(t *testing.T) {
	vm := New(&bytes.Buffer{})
	for _, w := range []*NATSX{
		vm.newNATSClient(&nats.Client{}),
		vm.newNATSSub(&nats.Subscription{}),
		vm.newNATSMsg(&nats.Msg{}),
	} {
		if w.ToS() == "" || w.Inspect() == "" || !w.Truthy() {
			t.Errorf("%s: unexpected ToS/Inspect/Truthy", w.cls.name)
		}
	}
	// raiseNATS: a mapped sentinel and an unmapped error (the NATS::Error fallback).
	natsRecover(t, "NATS::Timeout", func() { raiseNATS(nats.ErrTimeout) })
	natsRecover(t, "NATS::Error", func() { raiseNATS(errors.New("weird transport error")) })

	// raiseNATSConnect classifies a failed connect OS-independently: a known nats
	// sentinel maps as usual, a bare network/dial error (no sentinel) becomes
	// NATS::IO::NoServersError — this is the Windows path, where nats.go surfaces a
	// raw *net.OpError instead of wrapping nats.ErrNoServers — and anything else
	// falls back to NATS::Error.
	natsRecover(t, "NATS::IO::NoServersError", func() { raiseNATSConnect(nats.ErrNoServers) })
	netErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	natsRecover(t, "NATS::IO::NoServersError", func() { raiseNATSConnect(netErr) })
	natsRecover(t, "NATS::Error", func() { raiseNATSConnect(errors.New("non-net connect failure")) })
}
