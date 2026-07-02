// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"math/big"

	redis "github.com/go-ruby-redis/redis"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent RESP codec + command layer of
// github.com/go-ruby-redis/redis. The wire grammar and command surface live in
// that library; rbgo only supplies the host seam — the TCP socket — and maps
// Ruby command arguments onto the library's `any` argument model and the decoded
// replies back into the object graph (string/int64/float64/bool/nil ->
// String/Integer/Float/true|false/nil, *redis.Map -> Hash, *redis.Set -> Set,
// []any -> Array, *redis.CommandError -> a raised Redis::CommandError).
//
// rbgo has no live TCP socket of its own yet, so the socket seam is an injected
// Ruby IO-like object: any object responding to #read and #write (a StringIO, or
// a real duck-typed socket a host provides). rubyConn bridges that object to the
// library's Conn (an io.ReadWriter). When a host later grows a native TCPSocket
// it is wired here the same way.

// RedisObj is the Ruby wrapper around a *redis.Client (Redis). It owns no
// socket: the client drives commands over the injected rubyConn seam.
type RedisObj struct {
	cls    *RClass
	client *redis.Client
	// conn is the Ruby IO-like object backing the seam, kept so #_connection
	// can return it and so the object stays reachable.
	conn object.Value
}

func (r *RedisObj) ToS() string     { return "#<Redis client>" }
func (r *RedisObj) Inspect() string { return "#<Redis client>" }
func (r *RedisObj) Truthy() bool    { return true }

// rubyConn bridges a Ruby IO-like object (responding to #read and #write) to the
// io.ReadWriter the redis library drives its RESP stream over. Write forwards the
// bytes to the object's #write; Read pulls bytes from the object's #read,
// buffering any surplus a #read(n) returns beyond the caller's slice. This is the
// host seam: the library owns the protocol, rbgo owns the transport.
type rubyConn struct {
	vm  *VM
	obj object.Value
	// pending holds bytes a prior #read returned that did not fit the caller's
	// buffer, to be served before the next #read.
	pending []byte
}

// Write sends p to the Ruby object's #write and reports the byte count. A Ruby
// exception from #write unwinds through the panic-based raise, so a returned
// error is never a Ruby-level failure — it only guards the io.Writer contract.
func (c *rubyConn) Write(p []byte) (int, error) {
	c.vm.send(c.obj, "write", []object.Value{&object.String{B: append([]byte(nil), p...), Enc: "ASCII-8BIT"}}, nil)
	return len(p), nil
}

// Read fills p from the Ruby object's #read, first draining any pending surplus.
// It asks the object for len(p) bytes; a nil / empty reply is EOF. Bytes beyond
// p are buffered in pending for the next call.
func (c *rubyConn) Read(p []byte) (int, error) {
	if len(c.pending) > 0 {
		n := copy(p, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	want := len(p)
	if want == 0 {
		return 0, nil
	}
	rv := c.vm.send(c.obj, "read", []object.Value{object.Integer(want)}, nil)
	data := redisReadBytes(rv)
	if len(data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, data)
	if n < len(data) {
		c.pending = append(c.pending, data[n:]...)
	}
	return n, nil
}

// redisReadBytes extracts the byte payload a Ruby #read returned: a String yields
// its bytes, nil yields none (EOF).
func redisReadBytes(v object.Value) []byte {
	switch s := v.(type) {
	case *object.String:
		return s.B
	case object.Nil, nil:
		return nil
	}
	return []byte(v.ToS())
}

// --- Ruby command argument -> library `any` --------------------------------

// redisArg maps a Ruby command argument to the library's `any` argument model.
// The library serialises every argument as a RESP bulk string, stringifying
// numbers and symbols exactly as the redis gem does, so the mapping keeps the
// primitive Go types the encoder understands and falls back to a value's to_s.
func redisArg(v object.Value) any {
	switch n := v.(type) {
	case nil, object.Nil:
		return nil
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int64(n)
	case *object.Bignum:
		return n.I.String()
	case object.Float:
		return float64(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return redis.Symbol(string(n))
	}
	return v.ToS()
}

// redisArgs maps a slice of Ruby arguments to library arguments.
func redisArgs(vals []object.Value) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = redisArg(v)
	}
	return out
}

// --- library reply -> Ruby value -------------------------------------------

// redisValue maps a decoded reply value back into the object graph, applying the
// value model documented in the library: nil -> nil, string -> String, int64 ->
// Integer, float64 -> Float, *big.Int -> Integer/Bignum, bool -> true|false,
// []any -> Array, *redis.Map -> Hash, *redis.Set -> Set, *redis.VerbatimString ->
// String, *redis.Push -> Array. A *redis.CommandError is raised as a
// Redis::CommandError rather than returned (see redisReply).
func (vm *VM) redisValue(v any) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case string:
		return object.NewString(n)
	case []byte:
		return &object.String{B: n, Enc: "ASCII-8BIT"}
	case bool:
		return object.Bool(n)
	case int64:
		return object.Integer(n)
	case int:
		return object.Integer(int64(n))
	case float64:
		return object.Float(n)
	case *redis.VerbatimString:
		return object.NewString(n.Text)
	case redis.Symbol:
		return object.NewString(string(n))
	case []any:
		return vm.redisArray(n)
	case *redis.Map:
		return vm.redisHash(n)
	case *redis.Set:
		return vm.redisSet(n)
	case *redis.Push:
		return vm.redisArray(n.Values)
	case *big.Int:
		return object.NormInt(new(big.Int).Set(n))
	case *redis.CommandError:
		raise("Redis::CommandError", "%s", n.Message)
	}
	// The library only ever produces the cases above.
	return object.NilV
}

// redisArray maps a []any reply to a Ruby Array.
func (vm *VM) redisArray(vals []any) *object.Array {
	arr := &object.Array{Elems: make([]object.Value, len(vals))}
	for i, e := range vals {
		arr.Elems[i] = vm.redisValue(e)
	}
	return arr
}

// redisHash maps a *redis.Map reply to a Ruby Hash, preserving insertion order.
func (vm *VM) redisHash(m *redis.Map) *object.Hash {
	h := object.NewHash()
	m.Each(func(key, value any) {
		h.Set(vm.redisValue(key), vm.redisValue(value))
	})
	return h
}

// redisSet maps a *redis.Set reply to a Ruby Set, preserving insertion order.
func (vm *VM) redisSet(s *redis.Set) object.Value {
	set := newSet()
	for _, m := range s.Members() {
		set.add(vm.redisValue(m))
	}
	return set
}

// redisReply turns a (value, error) command result into a Ruby value, raising a
// Redis::CommandError for a *redis.CommandError error and a
// Redis::ConnectionError for a transport/protocol fault.
func (vm *VM) redisReply(v any, err error) object.Value {
	if err != nil {
		if ce, ok := err.(*redis.CommandError); ok {
			raise("Redis::CommandError", "%s", ce.Message)
		}
		raise("Redis::ConnectionError", "%s", err.Error())
	}
	return vm.redisValue(v)
}

// --- construction argument parsing -----------------------------------------

// redisNewArgs reads the Redis.new argument list: a trailing keyword Hash may
// carry connection: / conn: (the IO-like seam) plus the protocol options db: /
// username: / password: / protocol:; a single positional non-Hash argument is
// taken as the connection. It returns the connection object (nil if none) and
// the decoded Options.
func redisNewArgs(args []object.Value) (object.Value, redis.Options) {
	var opts redis.Options
	var conn object.Value
	// A trailing Hash is the keyword set.
	if len(args) > 0 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			conn = redisOptsFromHash(h, &opts)
			args = args[:len(args)-1]
		}
	}
	// A leading positional argument is the connection when the keywords did not
	// supply one.
	if conn == nil && len(args) > 0 {
		conn = args[0]
	}
	return conn, opts
}

// redisOptsFromHash reads the keyword Hash into opts and returns the connection
// object it names (connection: / conn:), or nil.
func redisOptsFromHash(h *object.Hash, opts *redis.Options) object.Value {
	var conn object.Value
	get := func(name string) (object.Value, bool) {
		if v, ok := h.Get(object.Symbol(name)); ok {
			return v, true
		}
		return h.Get(object.NewString(name))
	}
	if v, ok := get("connection"); ok {
		conn = v
	} else if v, ok := get("conn"); ok {
		conn = v
	}
	if v, ok := get("db"); ok {
		opts.DB = int(redisInt(v))
	}
	if v, ok := get("username"); ok {
		opts.Username = redisStr(v)
	}
	if v, ok := get("password"); ok {
		opts.Password = redisStr(v)
	}
	if v, ok := get("protocol"); ok {
		opts.Protocol = int(redisInt(v))
	}
	return conn
}

// redisInt coerces a keyword value to an int64 (0 for a non-integer).
func redisInt(v object.Value) int64 {
	switch n := v.(type) {
	case object.Integer:
		return int64(n)
	case *object.Bignum:
		return n.I.Int64()
	}
	return 0
}

// redisStr coerces a keyword value to a string: a String yields its contents,
// any other value its to_s.
func redisStr(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// --- arity helpers ---------------------------------------------------------

// redisArity raises an ArgumentError when args does not have exactly n elements.
func redisArity(args []object.Value, n int, name string) {
	if len(args) != n {
		raise("ArgumentError", "wrong number of arguments for '%s' (given %d, expected %d)", name, len(args), n)
	}
}

// redisArityMin raises an ArgumentError when args has fewer than n elements.
func redisArityMin(args []object.Value, n int, name string) {
	if len(args) < n {
		raise("ArgumentError", "wrong number of arguments for '%s' (given %d, expected %d+)", name, len(args), n)
	}
}

// --- pipelined / multi -----------------------------------------------------

// RedisBatch is the Ruby object yielded to a #pipelined / #multi block: its
// command methods queue onto the underlying *redis.Batch instead of sending. The
// gem yields a similar pipeline object.
type RedisBatch struct {
	cls   *RClass
	batch *redis.Batch
}

func (b *RedisBatch) ToS() string     { return "#<Redis::Pipeline>" }
func (b *RedisBatch) Inspect() string { return "#<Redis::Pipeline>" }
func (b *RedisBatch) Truthy() bool    { return true }

// redisPipelined runs blk to queue commands on a fresh Batch, sends them in one
// write and returns their replies as a Ruby Array (Redis#pipelined). A block is
// required.
func (vm *VM) redisPipelined(cl *redis.Client, blk *Proc) object.Value {
	if blk == nil {
		raise("LocalJumpError", "no block given (yield)")
	}
	b := &RedisBatch{cls: vm.redisBatchClass(), batch: &redis.Batch{}}
	results, err := cl.Pipelined(func(batch *redis.Batch) {
		b.batch = batch
		vm.callBlock(blk, []object.Value{b})
	})
	if err != nil {
		raise("Redis::ConnectionError", "%s", err.Error())
	}
	return vm.redisArray(results)
}

// redisMulti runs blk to queue commands and wraps them in a MULTI/EXEC
// transaction, returning EXEC's array of results (or nil if the transaction was
// aborted). A block is required.
func (vm *VM) redisMulti(cl *redis.Client, blk *Proc) object.Value {
	if blk == nil {
		raise("LocalJumpError", "no block given (yield)")
	}
	b := &RedisBatch{cls: vm.redisBatchClass()}
	result, err := cl.Multi(func(batch *redis.Batch) {
		b.batch = batch
		vm.callBlock(blk, []object.Value{b})
	})
	if err != nil {
		if ce, ok := err.(*redis.CommandError); ok {
			raise("Redis::CommandError", "%s", ce.Message)
		}
		raise("Redis::ConnectionError", "%s", err.Error())
	}
	return vm.redisValue(result)
}

// redisBatchClass returns (memoised) the Redis::Pipeline class, defining its
// #call (queue any command) and #method_missing-style command methods lazily on
// first use.
func (vm *VM) redisBatchClass() *RClass {
	if c, ok := vm.consts["Redis::Pipeline"].(*RClass); ok {
		return c
	}
	cls := newClass("Redis::Pipeline", vm.cObject)
	vm.consts["Redis::Pipeline"] = cls
	if mod, ok := vm.consts["Redis"].(*RClass); ok {
		mod.consts["Pipeline"] = cls
	}
	// #call(*args) and every command method queue onto the batch. Because a
	// queued command is just a name plus arguments, one native closure keyed by
	// method name backs them all.
	queue := func(name string) NativeFn {
		return func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
			b := v.(*RedisBatch)
			if name == "call" {
				b.batch.Add(redisArgs(args)...)
			} else {
				b.batch.Add(append([]any{name}, redisArgs(args)...)...)
			}
			return v
		}
	}
	for _, name := range []string{
		"call", "get", "set", "setnx", "getset", "append", "strlen",
		"incr", "incrby", "incrbyfloat", "decr", "decrby", "mget", "mset",
		"del", "exists", "expire", "ttl", "persist", "type", "keys",
		"hset", "hget", "hgetall", "hdel", "hexists", "hkeys", "hvals", "hlen", "hmget",
		"lpush", "rpush", "lpop", "rpop", "llen", "lrange",
		"sadd", "srem", "smembers", "sismember", "scard",
		"zadd", "zscore", "zrange", "zrank", "zcard", "zrem",
		"ping", "echo", "select", "flushdb",
	} {
		cls.define(name, queue(name))
	}
	return cls
}
