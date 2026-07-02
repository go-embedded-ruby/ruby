// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// fakeSock is a Ruby duck-typed socket (read/write) backing the redis/pg host
// seam: writes are captured in @out, reads drain the canned @in buffer. It is
// the injected transport the bindings drive their protocol over.
const fakeSock = `
class FakeSock
  def initialize(reply) ; @in = reply.dup.force_encoding("ASCII-8BIT") ; @pos = 0 ; @out = "".b ; end
  def write(s) ; @out << s ; s.bytesize ; end
  def read(n = nil)
    avail = @in.bytesize - @pos
    return "".b if avail <= 0
    n = avail if n.nil? || n > avail
    chunk = @in.byteslice(@pos, n)
    @pos += n
    chunk
  end
  def out ; @out ; end
end
`

// TestRedisConstants covers the Redis loadable module and its error tree.
func TestRedisConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "redis"; p require "redis"`, "false\n"},
		{`p require "redis"`, "true\n"},
		{`require "redis"; p Redis.is_a?(Class)`, "true\n"},
		{`require "redis"; p Redis::BaseError < StandardError`, "true\n"},
		{`require "redis"; p Redis::CommandError < Redis::BaseError`, "true\n"},
		{`require "redis"; p Redis::ConnectionError < Redis::BaseError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRedisCommands drives the command surface over a canned RESP reply stream,
// asserting both the mapped Ruby reply and (where load-bearing) the RESP bytes
// the client wrote.
func TestRedisCommands(t *testing.T) {
	cases := []struct{ src, want string }{
		// PONG simple string, a bulk string, an integer.
		{fakeSock + `require "redis"
s = FakeSock.new("+PONG\r\n$5\r\nhello\r\n:3\r\n")
r = Redis.new(connection: s)
p r.ping
p r.get("k")
p r.exists("a")`, "\"PONG\"\n\"hello\"\n3\n"},
		// A nil bulk ($-1) and a set (SMEMBERS as a RESP array coerced to a Set).
		{fakeSock + `require "redis"
s = FakeSock.new("$-1\r\n*2\r\n$1\r\na\r\n$1\r\nb\r\n")
r = Redis.new(connection: s)
p r.get("missing")
p r.smembers("s").class`, "nil\nSet\n"},
		// HGETALL coerces a RESP array of pairs to a Hash (via #hgetall -> Map path
		// when RESP3, or array when RESP2; the gem returns a Hash either way).
		{fakeSock + `require "redis"
s = FakeSock.new("%2\r\n$1\r\nf\r\n$1\r\n1\r\n$1\r\ng\r\n$1\r\n2\r\n")
r = Redis.new(connection: s)
p r.hgetall("h")`, "{\"f\" => \"1\", \"g\" => \"2\"}\n"},
		// A float reply (INCRBYFLOAT via RESP3 double).
		{fakeSock + `require "redis"
s = FakeSock.new(",3.14\r\n")
r = Redis.new(connection: s)
p r.incrbyfloat("k", 1.0)`, "3.14\n"},
		// A boolean reply (RESP3 #t).
		{fakeSock + `require "redis"
s = FakeSock.new("\x23t\r\n")
r = Redis.new(connection: s)
p r.sismember("s", "m")`, "true\n"},
		// call() escape hatch + the written command frame carries the args.
		{fakeSock + `require "redis"
s = FakeSock.new("+OK\r\n")
r = Redis.new(connection: s)
p r.call("SET", "k", "v")
p s.out.include?("SET")`, "\"OK\"\ntrue\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRedisBignum covers the RESP3 big-number reply mapping to a Ruby Integer.
func TestRedisBignum(t *testing.T) {
	src := fakeSock + `require "redis"
s = FakeSock.new("(3492890328409238509324850943850943825024385\r\n")
r = Redis.new(connection: s)
p r.call("DEBUG")`
	if got := eval(t, src); got != "3492890328409238509324850943850943825024385\n" {
		t.Errorf("bignum got=%q", got)
	}
}

// TestRedisError covers a Redis error reply raising Redis::CommandError.
func TestRedisError(t *testing.T) {
	src := fakeSock + `require "redis"
s = FakeSock.new("-WRONGTYPE nope\r\n")
r = Redis.new(connection: s)
begin
  r.get("k")
rescue Redis::CommandError => e
  p e.message
end`
	if got := eval(t, src); got != "\"WRONGTYPE nope\"\n" {
		t.Errorf("error got=%q", got)
	}
}

// TestRedisConnectionError covers a truncated/malformed reply raising
// Redis::ConnectionError (a transport/protocol fault), not a CommandError.
func TestRedisConnectionError(t *testing.T) {
	src := fakeSock + `require "redis"
s = FakeSock.new("")
r = Redis.new(connection: s)
begin
  r.ping
rescue Redis::ConnectionError
  p :conn
end`
	if got := eval(t, src); got != ":conn\n" {
		t.Errorf("conn error got=%q", got)
	}
}

// TestRedisNoConnection covers Redis.new without a connection raising
// ArgumentError.
func TestRedisNoConnection(t *testing.T) {
	if err := runErr(t, `require "redis"; Redis.new`); err == nil ||
		!strings.Contains(err.Error(), "connection") {
		t.Errorf("want connection ArgumentError, got %v", err)
	}
}

// TestRedisPipelined covers #pipelined: a batch of commands in one write, replies
// returned as an Array; and #multi: MULTI/EXEC returning EXEC's array.
func TestRedisPipelined(t *testing.T) {
	cases := []struct{ src, want string }{
		// Two queued GETs; two bulk replies come back as an Array.
		{fakeSock + `require "redis"
s = FakeSock.new("$1\r\na\r\n$1\r\nb\r\n")
r = Redis.new(connection: s)
res = r.pipelined { |p| p.get("x"); p.get("y") }
p res`, "[\"a\", \"b\"]\n"},
		// #multi: +OK (MULTI), +QUEUED, then EXEC's array reply.
		{fakeSock + `require "redis"
s = FakeSock.new("+OK\r\n+QUEUED\r\n*1\r\n:1\r\n")
r = Redis.new(connection: s)
res = r.multi { |m| m.incr("n") }
p res`, "[1]\n"},
		// pipelined via #call queues an arbitrary command.
		{fakeSock + `require "redis"
s = FakeSock.new("+OK\r\n")
r = Redis.new(connection: s)
res = r.pipelined { |p| p.call("SET", "k", "v") }
p res`, "[\"OK\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestRedisPipelinedNoBlock covers #pipelined / #multi without a block raising
// LocalJumpError.
func TestRedisPipelinedNoBlock(t *testing.T) {
	for _, m := range []string{"pipelined", "multi"} {
		src := fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new(""))
begin; r.` + m + `; rescue LocalJumpError; p :nb; end`
		if got := eval(t, src); got != ":nb\n" {
			t.Errorf("%s no-block got=%q", m, got)
		}
	}
}

// TestRedisArity covers the argument-count guards.
func TestRedisArity(t *testing.T) {
	src := fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new(""))
begin; r.get; rescue ArgumentError; p :a; end
begin; r.set("k"); rescue ArgumentError; p :b; end
begin; r.hset("h"); rescue ArgumentError; p :c; end`
	if got := eval(t, src); got != ":a\n:b\n:c\n" {
		t.Errorf("arity got=%q", got)
	}
}

// TestRedisConnectionAccessor covers Redis.new options and #_connection.
func TestRedisConnectionAccessor(t *testing.T) {
	src := fakeSock + `require "redis"
s = FakeSock.new("+PONG\r\n")
r = Redis.new(connection: s, db: 2, username: "u", password: "p", protocol: 3)
p r._connection.equal?(s)
p r.class`
	if got := eval(t, src); got != "true\nRedis\n" {
		t.Errorf("accessor got=%q", got)
	}
}

// TestRedisPositionalConn covers passing the connection as a positional argument.
func TestRedisPositionalConn(t *testing.T) {
	src := fakeSock + `require "redis"
s = FakeSock.new("+PONG\r\n")
r = Redis.new(s)
p r.ping`
	if got := eval(t, src); got != "\"PONG\"\n" {
		t.Errorf("positional got=%q", got)
	}
}
