// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestRedisEveryCommand exercises every command method at least once over a
// reply it accepts, so the whole command surface (and its coercions) is covered.
// The reply is chosen per command's return type.
func TestRedisEveryCommand(t *testing.T) {
	// Each case: a command invocation and the canned reply the server sends.
	cmds := []struct{ call, reply string }{
		{`r.set("k","v")`, "+OK\r\n"},
		{`r.setnx("k","v")`, ":1\r\n"},
		{`r.getset("k","v")`, "$1\r\no\r\n"},
		{`r.append("k","v")`, ":3\r\n"},
		{`r.strlen("k")`, ":3\r\n"},
		{`r.incr("k")`, ":1\r\n"},
		{`r.incrby("k",2)`, ":3\r\n"},
		{`r.incrbyfloat("k",1.5)`, ",2.5\r\n"},
		{`r.decr("k")`, ":0\r\n"},
		{`r.decrby("k",2)`, ":-2\r\n"},
		{`r.mget("a","b")`, "*2\r\n$1\r\nx\r\n$-1\r\n"},
		{`r.mset("a",1,"b",2)`, "+OK\r\n"},
		{`r.del("a","b")`, ":2\r\n"},
		{`r.expire("k",10)`, ":1\r\n"},
		{`r.ttl("k")`, ":10\r\n"},
		{`r.persist("k")`, ":1\r\n"},
		{`r.type("k")`, "+string\r\n"},
		{`r.keys("*")`, "*1\r\n$1\r\nk\r\n"},
		{`r.hget("h","f")`, "$1\r\n1\r\n"},
		{`r.hdel("h","f")`, ":1\r\n"},
		{`r.hexists("h","f")`, ":1\r\n"},
		{`r.hkeys("h")`, "*1\r\n$1\r\nf\r\n"},
		{`r.hvals("h")`, "*1\r\n$1\r\n1\r\n"},
		{`r.hlen("h")`, ":1\r\n"},
		{`r.hmget("h","f","g")`, "*2\r\n$1\r\n1\r\n$-1\r\n"},
		{`r.lpush("l","a")`, ":1\r\n"},
		{`r.rpush("l","a")`, ":2\r\n"},
		{`r.lpop("l")`, "$1\r\na\r\n"},
		{`r.rpop("l")`, "$1\r\na\r\n"},
		{`r.llen("l")`, ":2\r\n"},
		{`r.lrange("l",0,-1)`, "*1\r\n$1\r\na\r\n"},
		{`r.srem("s","m")`, ":1\r\n"},
		{`r.scard("s")`, ":1\r\n"},
		{`r.zadd("z",1,"m")`, ":1\r\n"},
		{`r.zscore("z","m")`, "$1\r\n1\r\n"},
		{`r.zrange("z",0,-1)`, "*1\r\n$1\r\nm\r\n"},
		{`r.zrank("z","m")`, ":0\r\n"},
		{`r.zcard("z")`, ":1\r\n"},
		{`r.zrem("z","m")`, ":1\r\n"},
		{`r.echo("hi")`, "$2\r\nhi\r\n"},
		{`r.select(0)`, "+OK\r\n"},
		{`r.flushdb`, "+OK\r\n"},
		{`r.hset("h","f",1)`, ":1\r\n"},
		{`r.sadd("s","m")`, ":1\r\n"},
	}
	for _, c := range cmds {
		src := fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new(` + goQuote(c.reply) + `))
` + c.call + `
p :ok`
		if got := eval(t, src); got != ":ok\n" {
			t.Errorf("cmd=%q reply=%q got=%q", c.call, c.reply, got)
		}
	}
}

// goQuote renders s as a Ruby double-quoted string literal so the escape
// sequences (\r\n, \x..) survive into the test source.
func goQuote(s string) string {
	var b []byte
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\r':
			b = append(b, '\\', 'r')
		case '\n':
			b = append(b, '\\', 'n')
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		default:
			b = append(b, s[i])
		}
	}
	b = append(b, '"')
	return string(b)
}

// TestRedisValueMappings covers the remaining value-model reply types: a verbatim
// string, a push, a RESP3 map key that is an integer, and a []byte via a bulk
// string with binary content.
func TestRedisValueMappings(t *testing.T) {
	cases := []struct{ src, want string }{
		// Verbatim string ("=15\r\ntxt:hello world"): payload only.
		{fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new("=15\r\ntxt:hello world\r\n"))
p r.call("X")`, "\"hello world\"\n"},
		// Push (">2\r\n...") maps to an Array of its values.
		{fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new(">2\r\n$7\r\nmessage\r\n$2\r\nhi\r\n"))
p r.call("X")`, "[\"message\", \"hi\"]\n"},
		// A nested array with a null and an integer.
		{fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new("*2\r\n:7\r\n$-1\r\n"))
p r.call("X")`, "[7, nil]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRedisArgCoercion covers the redisArg branches: a Symbol, a Bignum and a
// Float argument all serialise into the written command frame.
func TestRedisArgCoercion(t *testing.T) {
	src := fakeSock + `require "redis"
s = FakeSock.new("+OK\r\n")
r = Redis.new(connection: s)
r.call(:SET, 10000000000000000000000, 1.5, nil, true, "x")
out = s.out
p out.include?("SET")
p out.include?("10000000000000000000000")
p out.include?("1.5")`
	want := "true\ntrue\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("arg coercion got=%q want=%q", got, want)
	}
}

// TestRedisOptsStringKeys covers redisOptsFromHash reading String (not Symbol)
// keyword keys and redisStr/redisInt coercing non-native values.
func TestRedisOptsStringKeys(t *testing.T) {
	src := fakeSock + `require "redis"
s = FakeSock.new("+PONG\r\n")
r = Redis.new({"connection" => s, "db" => 1, "username" => :admin, "password" => 42, "protocol" => 2})
p r.ping`
	if got := eval(t, src); got != "\"PONG\"\n" {
		t.Errorf("string-key opts got=%q", got)
	}
}

// TestRedisMultiAbort covers #multi returning nil when EXEC's reply is a null
// array (a watched key changed / transaction discarded).
func TestRedisMultiAbort(t *testing.T) {
	src := fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new("+OK\r\n+QUEUED\r\n*-1\r\n"))
p r.multi { |m| m.incr("n") }`
	if got := eval(t, src); got != "nil\n" {
		t.Errorf("multi abort got=%q", got)
	}
}

// TestRedisMultiError covers #multi surfacing a Redis::CommandError when EXEC
// returns an error reply.
func TestRedisMultiError(t *testing.T) {
	src := fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new("+OK\r\n+QUEUED\r\n-ERR bad\r\n"))
begin
  r.multi { |m| m.incr("n") }
rescue Redis::CommandError
  p :err
end`
	if got := eval(t, src); got != ":err\n" {
		t.Errorf("multi error got=%q", got)
	}
}

// TestRedisConnKeyword covers the conn: (short) keyword form and redisArg's
// generic ToS fallback for a non-primitive argument (an Array stringifies).
func TestRedisConnKeyword(t *testing.T) {
	src := fakeSock + `require "redis"
s = FakeSock.new("+OK\r\n")
r = Redis.new(conn: s)
r.call("SET", "k", [1, 2])
p s.out.include?("[1, 2]")`
	if got := eval(t, src); got != "true\n" {
		t.Errorf("conn keyword got=%q", got)
	}
}

// TestRedisTruthy covers the wrapper Truthy paths (used as a condition).
func TestRedisTruthy(t *testing.T) {
	src := fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new(""))
p(r ? :y : :n)
r.pipelined { |pl| p(pl ? :y : :n) }`
	if got := eval(t, src); got != ":y\n:y\n" {
		t.Errorf("truthy got=%q", got)
	}
}

// TestRedisReadChunking covers the rubyConn Read pending-surplus path: a socket
// whose #read returns the whole buffer at once (more than the reader asked for)
// buffers the surplus in pending and serves it on the next read.
func TestRedisReadChunking(t *testing.T) {
	src := fakeSock + `require "redis"
class GreedySock
  def initialize(reply) ; @in = reply.dup.force_encoding("ASCII-8BIT") ; @out = "".b ; end
  def write(s) ; @out << s ; s.bytesize ; end
  # Ignore n: hand back the entire remaining buffer in one go.
  def read(n = nil) ; @in.slice!(0, @in.bytesize) ; end
end
r = Redis.new(connection: GreedySock.new("+PONG\r\n:5\r\n"))
p r.ping
p r.call("X")`
	if got := eval(t, src); got != "\"PONG\"\n5\n" {
		t.Errorf("read chunking got=%q", got)
	}
}

// TestRedisPipelinedDecodeError covers redisPipelined / redisMulti surfacing a
// Redis::ConnectionError when the reply stream is truncated mid-decode.
func TestRedisPipelinedDecodeError(t *testing.T) {
	cases := []string{
		`res = r.pipelined { |p| p.get("x"); p.get("y") }`,
		`res = r.multi { |m| m.incr("n") }`,
	}
	for _, call := range cases {
		src := fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new("$5\r\nab"))
begin
  ` + call + `
rescue Redis::ConnectionError
  p :ce
end`
		if got := eval(t, src); got != ":ce\n" {
			t.Errorf("call=%q decode-error got=%q", call, got)
		}
	}
}

// TestRedisEOFNil covers the rubyConn Read EOF path when #read returns nil (a
// closed socket), and redisReadBytes's nil branch.
func TestRedisEOFNil(t *testing.T) {
	src := fakeSock + `require "redis"
class ClosedSock
  def write(s) ; s.bytesize ; end
  def read(n = nil) ; nil ; end
end
r = Redis.new(connection: ClosedSock.new)
begin; r.ping; rescue Redis::ConnectionError; p :eof; end`
	if got := eval(t, src); got != ":eof\n" {
		t.Errorf("eof nil got=%q", got)
	}
}

// TestRedisNestedCommandError covers redisValue raising when a *CommandError is
// nested in a pipelined reply array (a per-command error surfaced through the
// value mapper rather than the command return).
func TestRedisNestedCommandError(t *testing.T) {
	src := fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new("*1\r\n-ERR nested\r\n"))
begin
  r.call("X")
rescue Redis::CommandError => e
  p e.message
end`
	if got := eval(t, src); got != "\"ERR nested\"\n" {
		t.Errorf("nested cmd error got=%q", got)
	}
}

// TestRedisIntDefault covers redisInt's non-Integer default (a String db:).
func TestRedisIntDefault(t *testing.T) {
	src := fakeSock + `require "redis"
s = FakeSock.new("+PONG\r\n")
r = Redis.new(connection: s, db: "not-an-int")
p r.ping`
	if got := eval(t, src); got != "\"PONG\"\n" {
		t.Errorf("int default got=%q", got)
	}
}

// TestRedisBatchMemoised covers redisBatchClass returning the same class on a
// second #pipelined (the memoisation branch).
func TestRedisBatchMemoised(t *testing.T) {
	src := fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new("$1\r\na\r\n$1\r\nb\r\n"))
c1 = nil; c2 = nil
r.pipelined { |p| c1 = p.class; p.get("x") }
r.pipelined { |p| c2 = p.class; p.get("y") }
p c1.equal?(c2)`
	if got := eval(t, src); got != "true\n" {
		t.Errorf("batch memo got=%q", got)
	}
}

// TestRedisToS covers the wrapper #to_s / #inspect rendering.
func TestRedisToS(t *testing.T) {
	src := fakeSock + `require "redis"
r = Redis.new(connection: FakeSock.new(""))
p r.to_s
p r.inspect
res = r.pipelined { |pl| p pl.to_s; p pl.inspect }`
	want := "\"#<Redis client>\"\n\"#<Redis client>\"\n\"#<Redis::Pipeline>\"\n\"#<Redis::Pipeline>\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("to_s got=%q want=%q", got, want)
	}
}
