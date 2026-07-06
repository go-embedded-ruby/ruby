// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	sidekiq "github.com/go-ruby-sidekiq/sidekiq"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the binding between rbgo's Ruby object graph and the pure-Go
// Sidekiq engine of github.com/go-ruby-sidekiq/sidekiq (which drives Redis
// through github.com/redis/go-redis/v9). The deterministic enqueue / schedule /
// retry / processor machinery — and the byte-exact Sidekiq JSON payload — live
// in that library; rbgo supplies the two inherently interpreter-dependent seams
// it documents:
//
//   - the Redis socket: a go-redis client dialled from the URL configured via
//     Sidekiq.redis = { url: … } (or a configure_client/server block), falling
//     back to ENV["REDIS_URL"] and then a local default. A fresh client is built
//     and Closed per operation (withSidekiqRedis) so nothing leaks;
//   - the job body: a worker's #perform is Ruby, so the library's Perform seam
//     is a closure that instantiates the worker class and sends #perform, mapping
//     a raised Ruby exception back into the retry machinery (sidekiqPerform).
//
// It also carries the shared job-binding helpers reused by the Resque binding
// (see resque_bind.go): the go-redis reply/argument conversions, the JSON
// argument coder and the Ruby-value job-argument decoder.

// jobBRPopTimeout bounds the processor's blocking fetch. The Sidekiq processor
// only ever runs against a queue rbgo has already confirmed is non-empty (see
// sidekiqProcessOne), so BRPOP returns immediately; the timeout is only a safety
// bound and never actually elapses.
const jobBRPopTimeout = time.Second

// defaultRedisURL is the connection dialled when neither the module config nor
// ENV["REDIS_URL"] names one, matching the conventional local Redis.
const defaultRedisURL = "redis://localhost:6379/0"

// jobCtx is the context every go-redis command runs under. Operations are
// synchronous and short-lived, so a background context is sufficient.
func jobCtx() context.Context { return context.Background() }

// --- Redis client seam -----------------------------------------------------

// sidekiqURL resolves the Sidekiq Redis URL: the value set via Sidekiq.redis =
// takes precedence, then ENV["REDIS_URL"], then the local default.
func (vm *VM) sidekiqURL() string { return resolveRedisURL(vm.sidekiqRedisURL) }

// resolveRedisURL applies the shared URL precedence used by both job bindings.
func resolveRedisURL(configured string) string {
	if configured != "" {
		return configured
	}
	if env := os.Getenv("REDIS_URL"); env != "" {
		return env
	}
	return defaultRedisURL
}

// dialRedis builds a go-redis client for url, tolerating a bare host:port (the
// Resque config style) by defaulting the redis:// scheme. It returns an error
// for a malformed URL rather than raising, so each caller attributes it to its
// own gem-faithful exception class.
func dialRedis(url string) (*redisv9.Client, error) {
	if !strings.Contains(url, "://") {
		url = "redis://" + url
	}
	opts, err := redisv9.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return redisv9.NewClient(opts), nil
}

// withSidekiqRedis dials a fresh go-redis client for the configured Sidekiq URL,
// runs fn against it and Closes it afterwards, so no connection or background
// goroutine outlives the operation. A malformed URL raises
// Sidekiq::RedisConnectionError.
func (vm *VM) withSidekiqRedis(fn func(rdb *redisv9.Client) object.Value) object.Value {
	rdb, err := dialRedis(vm.sidekiqURL())
	if err != nil {
		raise("Sidekiq::RedisConnectionError", "%s", err.Error())
	}
	defer func() { _ = rdb.Close() }()
	return fn(rdb)
}

// jobRaiseAs maps a go-redis command error onto the named Ruby exception class.
// redis.Nil is never routed here (callers treat it as an absent value), so any
// error is a genuine command or transport fault.
func jobRaiseAs(class string, err error) {
	raise(class, "%s", err.Error())
}

// sidekiqRaise / resqueRaise attribute a Redis fault to each binding's own
// gem-faithful connection-error class.
func sidekiqRaise(err error) { jobRaiseAs("Sidekiq::RedisConnectionError", err) }
func resqueRaise(err error)  { jobRaiseAs("Resque::RedisError", err) }

// --- Perform seam (Ruby worker body) ---------------------------------------

// sidekiqPerform builds the library Perform seam: it runs a worker's Ruby
// #perform by instantiating the class and sending #perform with the decoded
// args, returning nil on success or a sidekiqJobError (carrying the true Ruby
// exception class) on a raise, so the retry machinery records it faithfully.
func (vm *VM) sidekiqPerform() sidekiq.Perform {
	return func(class string, args []any) error {
		rc, msg, failed := vm.runJobPerform(class, args, false)
		if failed {
			return &sidekiqJobError{class: rc, msg: msg}
		}
		return nil
	}
}

// sidekiqJobError carries a Ruby exception class name through the Go error the
// Perform seam returns, implementing sidekiq.ClassedError so the retry payload
// records the real class (e.g. "RuntimeError") in error_class.
type sidekiqJobError struct {
	class string
	msg   string
}

func (e *sidekiqJobError) Error() string      { return e.msg }
func (e *sidekiqJobError) ErrorClass() string { return e.class }

// runJobPerform runs a worker body and reports the outcome as (rubyClass,
// message, failed). It resolves class to a Ruby class constant (an absent or
// non-class constant is a NameError), then either sends #perform to a fresh
// instance (Sidekiq) or to the class itself (Resque's self.perform, when
// classMethod is true), classifying any raised Ruby exception via its class and
// message. A non-Ruby Go panic propagates untouched.
func (vm *VM) runJobPerform(class string, args []any, classMethod bool) (rubyClass, message string, failed bool) {
	rcls, ok := vm.consts[class].(*RClass)
	if !ok {
		return "NameError", "uninitialized constant " + class, true
	}
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(RubyError)
			if !ok {
				panic(r)
			}
			rubyClass, message, failed = vm.minitestRaisedClass(e).name, e.Message, true
		}
	}()
	rubyArgs := anyArgsToRuby(args)
	if classMethod {
		vm.send(rcls, "perform", rubyArgs, nil)
	} else {
		inst := vm.send(rcls, "new", nil, nil)
		vm.send(inst, "perform", rubyArgs, nil)
	}
	return "", "", false
}

// --- job argument coding ----------------------------------------------------

// jobArgs encodes Ruby #perform arguments into the []any the job libraries
// marshal into their payload. Each argument is rendered with rbgo's own
// JSON.generate and carried as a json.RawMessage, so the enqueued payload is
// byte-for-byte what a Ruby process would write (integers stay integers, floats
// keep their decimal, strings/arrays/hashes serialise identically) rather than
// going through a lossy intermediate.
func (vm *VM) jobArgs(args []object.Value) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = json.RawMessage(jsonGenerate(a))
	}
	return out
}

// anyArgsToRuby maps a decoded []any argument list back into Ruby values for a
// #perform call.
func anyArgsToRuby(args []any) []object.Value {
	out := make([]object.Value, len(args))
	for i, a := range args {
		out[i] = jobAnyToRuby(a)
	}
	return out
}

// jobAnyToRuby maps one JSON-decoded Go value into the Ruby object graph. Numbers
// arrive as float64 (Sidekiq's encoding/json) or json.Number (Resque's
// UseNumber decoder); an integral number becomes an Integer and any other a
// Float, matching how a Ruby process sees a round-tripped JSON argument. A
// JSON object becomes a Hash with String keys (Go map order is unspecified, so
// callers relying on exact key order use single-key hashes).
func jobAnyToRuby(v any) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(n)
	case float64:
		if n == math.Trunc(n) && !math.IsInf(n, 0) {
			return object.IntValue(int64(n))
		}
		return object.Float(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return object.IntValue(i)
		}
		f, _ := n.Float64()
		return object.Float(f)
	case string:
		return object.NewString(n)
	case []any:
		el := make([]object.Value, len(n))
		for i, e := range n {
			el[i] = jobAnyToRuby(e)
		}
		return object.NewArrayFromSlice(el)
	case map[string]any:
		h := object.NewHash()
		for k, val := range n {
			h.Set(object.NewString(k), jobAnyToRuby(val))
		}
		return h
	}
	return object.NewString(fmt.Sprintf("%v", v))
}

// --- JobRedis: the block-scoped connection surface --------------------------

// JobRedis is the Redis connection object yielded to a Sidekiq.redis { |c| … }
// (or Resque.redis) block. It funnels every command through the shared go-redis
// client for the block's lifetime; the client is Closed when the block returns.
// It reports the class stamped on it (Sidekiq::RedisConnection or
// Resque::RedisConnection) so `c.class` is gem-faithful.
type JobRedis struct {
	cls      *RClass
	rdb      *redisv9.Client
	errClass string // Ruby exception class raised on a Redis fault (per binding)
}

func (j *JobRedis) ToS() string     { return "#<" + j.cls.name + ">" }
func (j *JobRedis) Inspect() string { return "#<" + j.cls.name + ">" }
func (j *JobRedis) Truthy() bool    { return true }

// jobRedisCommands is the command surface exposed on a JobRedis connection —
// enough of the Redis vocabulary to inspect a queue, set or sorted-set that a
// job binding writes. Every entry funnels through Do, so one closure backs them
// all (mirroring redisBatchClass).
var jobRedisCommands = []string{
	"get", "set", "del", "exists", "incr",
	"llen", "lrange", "lpush", "rpush", "lpop", "rpop",
	"sadd", "srem", "smembers", "scard", "sismember",
	"zadd", "zcard", "zscore", "zrange",
	"hset", "hget", "hgetall",
	"keys", "flushdb", "ping",
}

// defineJobRedisCommands installs the JobRedis command methods on cls: each
// named command prepends its name to the arguments and runs it via Do, and #call
// takes the whole command (name first) as its arguments — the escape hatch for
// anything without a dedicated method.
func defineJobRedisCommands(cls *RClass) {
	cmd := func(name string) NativeFn {
		return func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			jr := self.(*JobRedis)
			full := append([]any{name}, jobRedisArgs(args)...)
			return vm.jobRedisDo(jr, full)
		}
	}
	for _, name := range jobRedisCommands {
		cls.define(name, cmd(name))
	}
	cls.define("call", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.jobRedisDo(self.(*JobRedis), jobRedisArgs(args))
	})
}

// jobRedisDo runs one arbitrary command through the connection's go-redis client
// and maps its reply into the object graph. A redis.Nil (absent key) becomes
// nil; any other error is raised as the connection's binding-specific error
// class; otherwise the typed reply is converted via goRedisReply.
func (vm *VM) jobRedisDo(jr *JobRedis, cmd []any) object.Value {
	res, err := jr.rdb.Do(jobCtx(), cmd...).Result()
	if err != nil {
		if err == redisv9.Nil {
			return object.NilV
		}
		jobRaiseAs(jr.errClass, err)
	}
	return goRedisReply(res)
}

// jobRedisArgs maps Ruby command arguments to the go-redis `any` argument model,
// stringifying the primitive types go-redis serialises directly and falling back
// to a value's to_s.
func jobRedisArgs(args []object.Value) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = jobRedisArg(a)
	}
	return out
}

// jobRedisArg maps one Ruby argument to a go-redis argument.
func jobRedisArg(v object.Value) any {
	switch n := v.(type) {
	case object.Integer:
		return int64(n)
	case object.Float:
		return float64(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	case object.Bool:
		return bool(n)
	}
	return v.ToS()
}

// goRedisReply maps a decoded go-redis reply into the Ruby object graph: nil ->
// nil, a bulk/simple string -> String, an integer -> Integer, an array ->
// Array (recursively), and any other type -> its Go string form.
func goRedisReply(v any) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case string:
		return object.NewString(n)
	case int64:
		return object.IntValue(n)
	case []any:
		el := make([]object.Value, len(n))
		for i, e := range n {
			el[i] = goRedisReply(e)
		}
		return object.NewArrayFromSlice(el)
	}
	return object.NewString(fmt.Sprintf("%v", v))
}

// --- Sidekiq item / options helpers -----------------------------------------

// jobFloat coerces a numeric Ruby value already reduced to a primitive (via
// #to_f) to a float64; a non-numeric value yields 0.
func jobFloat(v object.Value) float64 {
	switch n := v.(type) {
	case object.Integer:
		return float64(n)
	case object.Float:
		return float64(n)
	}
	return 0
}

// sidekiqOptions reads a worker class's sidekiq_options declaration (stored in
// the class instance variable @sidekiq_options) into the queue and retry policy
// used to build an enqueue Item. An absent option leaves the library default
// (queue "default", retry true).
func sidekiqOptions(self object.Value) (queue string, retry any) {
	h, ok := getIvar(self, "@sidekiq_options").(*object.Hash)
	if !ok {
		return "", nil
	}
	if v, ok := hashOption(h, "queue"); ok {
		queue = v.ToS()
	}
	if v, ok := hashOption(h, "retry"); ok {
		retry = retryOption(v)
	}
	return queue, retry
}

// retryOption maps a Ruby sidekiq_options retry: value to the library's Retry
// model: a Bool stays a bool, an Integer an int (an explicit max attempt count),
// and anything else is treated as the default (true).
func retryOption(v object.Value) any {
	switch n := v.(type) {
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int(n)
	}
	return true
}

// hashOption fetches key from a Ruby options Hash, accepting either a Symbol or
// a String key (sidekiq_options is written with symbol keys, but a string key is
// tolerated).
func hashOption(h *object.Hash, key string) (object.Value, bool) {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}

// sidekiqItemFromHash builds a sidekiq.Item from a Ruby push item Hash
// (Sidekiq::Client#push), reading the class/args/queue/retry/at keys (symbol or
// string). A missing "class" raises ArgumentError, matching Sidekiq.
func (vm *VM) sidekiqItemFromHash(h *object.Hash) sidekiq.Item {
	classV, ok := hashOption(h, "class")
	if !ok {
		raise("ArgumentError", "Sidekiq::Client push requires a :class")
	}
	item := sidekiq.Item{Class: classV.ToS()}
	if v, ok := hashOption(h, "args"); ok {
		if arr, ok := v.(*object.Array); ok {
			item.Args = vm.jobArgs(arr.Elems)
		}
	}
	if v, ok := hashOption(h, "queue"); ok {
		item.Queue = v.ToS()
	}
	if v, ok := hashOption(h, "retry"); ok {
		item.Retry = retryOption(v)
	}
	if v, ok := hashOption(h, "at"); ok {
		item.At = jobTime(vm, v)
	}
	return item
}

// jobTime converts a Ruby time-ish value (a Time, or a numeric epoch) to a
// time.Time via #to_f, so both a Time object and a raw epoch number work.
func jobTime(vm *VM, v object.Value) time.Time {
	f := jobFloat(vm.send(v, "to_f", nil, nil))
	sec := int64(f)
	nsec := int64((f - float64(sec)) * 1e9)
	return time.Unix(sec, nsec)
}
