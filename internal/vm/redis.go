// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	redis "github.com/go-ruby-redis/redis"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerRedis installs the Redis client (require "redis"): Redis.new(...) plus
// the command surface (get / set / hset / hgetall / lpush / sadd / zadd / call /
// pipelined / multi / …) over the RESP codec + command layer of
// github.com/go-ruby-redis/redis. The TCP socket is the host seam: rbgo has no
// native TCPSocket yet, so Redis.new takes an injected IO-like connection
// (Redis.new(connection: io) — any object responding to #read/#write, e.g. a
// StringIO or a duck-typed socket) that rubyConn bridges to the library's Conn.
// The Redis::BaseError tree (CommandError / ConnectionError) is registered so a
// server error or transport fault rescues as the gem-faithful Ruby class.
func (vm *VM) registerRedis() {
	mod := newClass("Redis", vm.cObject)
	vm.consts["Redis"] = object.Wrap(mod)
	vm.registerRedisErrors(mod)

	// Redis.new(connection: io) / Redis.new(io): build a client over an injected
	// IO-like seam. The connection is taken from the `connection:` / `conn:`
	// keyword or a single positional IO argument; protocol keywords (db:,
	// username:, password:, protocol:) configure the recorded Options.
	mod.smethods["new"] = &Method{name: "new", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		conn, opts := redisNewArgs(args)
		if conn == nil {
			raise("ArgumentError", "Redis.new requires a connection: IO-like object (rbgo has no native socket yet)")
		}
		// New records opts for a host-driven Handshake; it never fails for a
		// non-nil Conn, so the error is not actionable here.
		client, _ := redis.New(&rubyConn{vm: vm, obj: conn}, opts)
		return object.Wrap(&RedisObj{cls: mod, client: client, conn: conn})
	}}

	vm.registerRedisCommands(mod)
}

// registerRedisErrors installs the Redis::BaseError hierarchy (BaseError <
// StandardError; CommandError / ConnectionError < BaseError), mirroring the
// redis gem's error tree closely enough that a raised library error rescues as
// its gem-faithful class.
func (vm *VM) registerRedisErrors(mod *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	reg := func(simple string, super *RClass) *RClass {
		qualified := "Redis::" + simple
		c := newClass(qualified, super)
		mod.consts[simple] = object.Wrap(c)
		vm.consts[qualified] = object.Wrap(c)
		return c
	}
	base := reg("BaseError", std)
	reg("CommandError", base)
	reg("ConnectionError", base)
}

// registerRedisCommands installs the command methods on Redis. Each maps its
// Ruby arguments onto the library's typed command method and its reply back into
// the object graph (see redis_bind.go).
func (vm *VM) registerRedisCommands(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	cl := func(v object.Value) *redis.Client { return object.Kind[*RedisObj](v).client }

	// #call(*args) sends an arbitrary command (Redis#call), the escape hatch for
	// any command without a dedicated method.
	d("call", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.redisReply(cl(v).Call(redisArgs(args)...))
	})

	// The typed command methods below each forward to the library's matching
	// method; the argument shapes mirror the redis gem.
	d("get", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "get")
		return vm.redisReply(cl(v).Get(redisArg(a[0])))
	})
	d("set", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArityMin(a, 2, "set")
		return vm.redisReply(cl(v).Set(redisArg(a[0]), redisArg(a[1]), redisArgs(a[2:])...))
	})
	d("setnx", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 2, "setnx")
		return vm.redisReply(cl(v).SetNX(redisArg(a[0]), redisArg(a[1])))
	})
	d("getset", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 2, "getset")
		return vm.redisReply(cl(v).GetSet(redisArg(a[0]), redisArg(a[1])))
	})
	d("append", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 2, "append")
		return vm.redisReply(cl(v).Append(redisArg(a[0]), redisArg(a[1])))
	})
	d("strlen", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "strlen")
		return vm.redisReply(cl(v).Strlen(redisArg(a[0])))
	})
	d("incr", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "incr")
		return vm.redisReply(cl(v).Incr(redisArg(a[0])))
	})
	d("incrby", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 2, "incrby")
		return vm.redisReply(cl(v).IncrBy(redisArg(a[0]), redisArg(a[1])))
	})
	d("incrbyfloat", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 2, "incrbyfloat")
		return vm.redisReply(cl(v).IncrByFloat(redisArg(a[0]), redisArg(a[1])))
	})
	d("decr", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "decr")
		return vm.redisReply(cl(v).Decr(redisArg(a[0])))
	})
	d("decrby", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 2, "decrby")
		return vm.redisReply(cl(v).DecrBy(redisArg(a[0]), redisArg(a[1])))
	})
	d("mget", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		return vm.redisReply(cl(v).MGet(redisArgs(a)...))
	})
	d("mset", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		return vm.redisReply(cl(v).MSet(redisArgs(a)...))
	})
	d("del", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		return vm.redisReply(cl(v).Del(redisArgs(a)...))
	})
	d("exists", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		return vm.redisReply(cl(v).Exists(redisArgs(a)...))
	})
	d("expire", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 2, "expire")
		return vm.redisReply(cl(v).Expire(redisArg(a[0]), redisArg(a[1])))
	})
	d("ttl", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "ttl")
		return vm.redisReply(cl(v).TTL(redisArg(a[0])))
	})
	d("persist", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "persist")
		return vm.redisReply(cl(v).Persist(redisArg(a[0])))
	})
	d("type", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "type")
		return vm.redisReply(cl(v).Type(redisArg(a[0])))
	})
	d("keys", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "keys")
		return vm.redisReply(cl(v).Keys(redisArg(a[0])))
	})

	// Hashes.
	d("hset", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArityMin(a, 3, "hset")
		return vm.redisReply(cl(v).HSet(redisArg(a[0]), redisArgs(a[1:])...))
	})
	d("hget", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 2, "hget")
		return vm.redisReply(cl(v).HGet(redisArg(a[0]), redisArg(a[1])))
	})
	d("hgetall", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "hgetall")
		return vm.redisReply(cl(v).HGetAll(redisArg(a[0])))
	})
	d("hdel", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArityMin(a, 2, "hdel")
		return vm.redisReply(cl(v).HDel(redisArg(a[0]), redisArgs(a[1:])...))
	})
	d("hexists", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 2, "hexists")
		return vm.redisReply(cl(v).HExists(redisArg(a[0]), redisArg(a[1])))
	})
	d("hkeys", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "hkeys")
		return vm.redisReply(cl(v).HKeys(redisArg(a[0])))
	})
	d("hvals", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "hvals")
		return vm.redisReply(cl(v).HVals(redisArg(a[0])))
	})
	d("hlen", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "hlen")
		return vm.redisReply(cl(v).HLen(redisArg(a[0])))
	})
	d("hmget", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArityMin(a, 2, "hmget")
		return vm.redisReply(cl(v).HMGet(redisArg(a[0]), redisArgs(a[1:])...))
	})

	// Lists.
	d("lpush", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArityMin(a, 2, "lpush")
		return vm.redisReply(cl(v).LPush(redisArg(a[0]), redisArgs(a[1:])...))
	})
	d("rpush", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArityMin(a, 2, "rpush")
		return vm.redisReply(cl(v).RPush(redisArg(a[0]), redisArgs(a[1:])...))
	})
	d("lpop", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "lpop")
		return vm.redisReply(cl(v).LPop(redisArg(a[0])))
	})
	d("rpop", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "rpop")
		return vm.redisReply(cl(v).RPop(redisArg(a[0])))
	})
	d("llen", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "llen")
		return vm.redisReply(cl(v).LLen(redisArg(a[0])))
	})
	d("lrange", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 3, "lrange")
		return vm.redisReply(cl(v).LRange(redisArg(a[0]), redisArg(a[1]), redisArg(a[2])))
	})

	// Sets.
	d("sadd", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArityMin(a, 2, "sadd")
		return vm.redisReply(cl(v).SAdd(redisArg(a[0]), redisArgs(a[1:])...))
	})
	d("srem", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArityMin(a, 2, "srem")
		return vm.redisReply(cl(v).SRem(redisArg(a[0]), redisArgs(a[1:])...))
	})
	d("smembers", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "smembers")
		return vm.redisReply(cl(v).SMembers(redisArg(a[0])))
	})
	d("sismember", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 2, "sismember")
		return vm.redisReply(cl(v).SIsMember(redisArg(a[0]), redisArg(a[1])))
	})
	d("scard", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "scard")
		return vm.redisReply(cl(v).SCard(redisArg(a[0])))
	})

	// Sorted sets.
	d("zadd", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArityMin(a, 3, "zadd")
		return vm.redisReply(cl(v).ZAdd(redisArg(a[0]), redisArgs(a[1:])...))
	})
	d("zscore", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 2, "zscore")
		return vm.redisReply(cl(v).ZScore(redisArg(a[0]), redisArg(a[1])))
	})
	d("zrange", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArityMin(a, 3, "zrange")
		return vm.redisReply(cl(v).ZRange(redisArg(a[0]), redisArg(a[1]), redisArg(a[2]), redisArgs(a[3:])...))
	})
	d("zrank", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 2, "zrank")
		return vm.redisReply(cl(v).ZRank(redisArg(a[0]), redisArg(a[1])))
	})
	d("zcard", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "zcard")
		return vm.redisReply(cl(v).ZCard(redisArg(a[0])))
	})
	d("zrem", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArityMin(a, 2, "zrem")
		return vm.redisReply(cl(v).ZRem(redisArg(a[0]), redisArgs(a[1:])...))
	})

	// Connection / server.
	d("ping", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		return vm.redisReply(cl(v).Ping(redisArgs(a)...))
	})
	d("echo", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "echo")
		return vm.redisReply(cl(v).Echo(redisArg(a[0])))
	})
	d("select", func(vm *VM, v object.Value, a []object.Value, _ *Proc) object.Value {
		redisArity(a, 1, "select")
		return vm.redisReply(cl(v).Select(redisArg(a[0])))
	})
	d("flushdb", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.redisReply(cl(v).FlushDB())
	})

	// #pipelined { |p| p.call(...) } sends a batch of commands in one write and
	// returns their replies as an Array (Redis#pipelined). The block yields a
	// RedisBatch whose command methods queue onto the batch.
	d("pipelined", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		return vm.redisPipelined(cl(v), blk)
	})

	// #multi { |m| m.call(...) } wraps the queued commands in a MULTI/EXEC
	// transaction and returns EXEC's array of results, or nil if aborted.
	d("multi", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		return vm.redisMulti(cl(v), blk)
	})

	// #_connection returns the injected IO-like seam object (a rbgo-specific
	// accessor, so a host can inspect or close the transport).
	d("_connection", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Kind[*RedisObj](v).conn
	})
}
