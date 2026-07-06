// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	sidekiq "github.com/go-ruby-sidekiq/sidekiq"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerSidekiq installs the Sidekiq surface (require "sidekiq"), backed by
// the pure-Go engine of github.com/go-ruby-sidekiq/sidekiq over
// github.com/redis/go-redis/v9:
//
//   - the Sidekiq module: the Redis config (Sidekiq.redis = { url: … } and the
//     configure_client/configure_server blocks that yield it), the block-scoped
//     connection Sidekiq.redis { |c| … }, and the server-side drivers
//     process_one / process_all / enqueue_scheduled_jobs;
//   - Sidekiq::Client with #push (class and instance) taking a job Item Hash;
//   - the Sidekiq::Job mixin (aliased Sidekiq::Worker): `include Sidekiq::Job`
//     installs sidekiq_options plus perform_async / perform_in / perform_at,
//     which enqueue the byte-exact Sidekiq JSON payload to Redis.
//
// The enqueue / schedule / retry / processor machinery and the payload format
// live in the library; this file is the class and method wiring, and
// sidekiq_bind.go holds the Redis-client seam, the Ruby #perform seam and the
// value conversions (shared with the Resque binding).
func (vm *VM) registerSidekiq() {
	mod := newClass("Sidekiq", nil)
	mod.isModule = true
	vm.consts["Sidekiq"] = mod
	vm.registerSidekiqErrors(mod)
	vm.registerSidekiqRedisConnection(mod)
	vm.registerSidekiqClient(mod)
	vm.registerSidekiqJob(mod)
	vm.registerSidekiqModuleMethods(mod)
}

// registerSidekiqErrors installs Sidekiq::Error < StandardError and
// Sidekiq::RedisConnectionError < Sidekiq::Error, the class a bad URL or Redis
// transport fault rescues as.
func (vm *VM) registerSidekiqErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("Sidekiq::Error", std)
	mod.consts["Error"] = base
	vm.consts["Sidekiq::Error"] = base
	conn := newClass("Sidekiq::RedisConnectionError", base)
	mod.consts["RedisConnectionError"] = conn
	vm.consts["Sidekiq::RedisConnectionError"] = conn
}

// registerSidekiqRedisConnection installs Sidekiq::RedisConnection, the class of
// the connection object yielded to a Sidekiq.redis block.
func (vm *VM) registerSidekiqRedisConnection(mod *RClass) {
	cls := newClass("Sidekiq::RedisConnection", vm.cObject)
	mod.consts["RedisConnection"] = cls
	vm.consts["Sidekiq::RedisConnection"] = cls
	defineJobRedisCommands(cls)
}

// registerSidekiqClient installs Sidekiq::Client and its #push (as both a class
// method and an instance method), which enqueues a job described by an Item Hash.
func (vm *VM) registerSidekiqClient(mod *RClass) {
	cls := newClass("Sidekiq::Client", vm.cObject)
	mod.consts["Client"] = cls
	vm.consts["Sidekiq::Client"] = cls
	push := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "Sidekiq::Client push requires an item Hash")
		}
		h, ok := args[0].(*object.Hash)
		if !ok {
			raise("TypeError", "Sidekiq::Client push expects a Hash")
		}
		item := vm.sidekiqItemFromHash(h)
		return vm.withSidekiqRedis(func(rdb *redisv9.Client) object.Value {
			jid, err := sidekiq.New(rdb).Push(jobCtx(), item)
			if err != nil {
				sidekiqRaise(err)
			}
			return object.NewString(jid)
		})
	}
	cls.smethods["push"] = &Method{name: "push", owner: cls, native: push}
	cls.define("push", push)
}

// registerSidekiqJob installs the Sidekiq::Job mixin (aliased Sidekiq::Worker):
// its `included` hook installs the enqueue DSL on the including class.
func (vm *VM) registerSidekiqJob(mod *RClass) {
	job := newClass("Sidekiq::Job", nil)
	job.isModule = true
	mod.consts["Job"] = job
	mod.consts["Worker"] = job
	vm.consts["Sidekiq::Job"] = job
	vm.consts["Sidekiq::Worker"] = job
	job.smethods["included"] = &Method{name: "included", owner: job, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			if base, ok := args[0].(*RClass); ok {
				vm.sidekiqInclude(base)
			}
		}
		return object.NilV
	}}
}

// sidekiqInclude installs the class-level enqueue DSL on a worker class that does
// `include Sidekiq::Job`: sidekiq_options (recording queue/retry defaults in the
// class's @sidekiq_options) and perform_async / perform_in / perform_at.
func (vm *VM) sidekiqInclude(base *RClass) {
	sm := func(name string, fn NativeFn) {
		base.smethods[name] = &Method{name: name, owner: base, native: fn}
	}
	sm("sidekiq_options", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			if h, ok := args[0].(*object.Hash); ok {
				setIvar(self, "@sidekiq_options", h)
			}
		}
		return getIvar(self, "@sidekiq_options")
	})
	sm("perform_async", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		q, r := sidekiqOptions(self)
		jobArgs := vm.jobArgs(args)
		return vm.sidekiqEnqueue(self, q, r, func(w *sidekiq.Worker) (string, error) {
			return w.PerformAsync(jobCtx(), jobArgs...)
		})
	})
	sm("perform_in", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "perform_in requires an interval")
		}
		q, r := sidekiqOptions(self)
		dur := time.Duration(jobFloat(vm.send(args[0], "to_f", nil, nil)) * float64(time.Second))
		jobArgs := vm.jobArgs(args[1:])
		return vm.sidekiqEnqueue(self, q, r, func(w *sidekiq.Worker) (string, error) {
			return w.PerformIn(jobCtx(), dur, jobArgs...)
		})
	})
	sm("perform_at", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "perform_at requires a time")
		}
		q, r := sidekiqOptions(self)
		tm := jobTime(vm, args[0])
		jobArgs := vm.jobArgs(args[1:])
		return vm.sidekiqEnqueue(self, q, r, func(w *sidekiq.Worker) (string, error) {
			return w.PerformAt(jobCtx(), tm, jobArgs...)
		})
	})
}

// sidekiqEnqueue runs one worker enqueue (perform_async/in/at) over a fresh
// go-redis client, building the library Worker from the class's queue/retry
// options and returning the new job's jid.
func (vm *VM) sidekiqEnqueue(self object.Value, queue string, retry any, do func(w *sidekiq.Worker) (string, error)) object.Value {
	className := vm.classOf(self).name
	if cls, ok := self.(*RClass); ok {
		className = cls.name
	}
	return vm.withSidekiqRedis(func(rdb *redisv9.Client) object.Value {
		w := sidekiq.New(rdb).Worker(className, sidekiq.WorkerOptions{Queue: queue, Retry: retry})
		jid, err := do(w)
		if err != nil {
			sidekiqRaise(err)
		}
		return object.NewString(jid)
	})
}

// registerSidekiqModuleMethods installs the Sidekiq module methods: the Redis
// config (redis=, configure_client/server), the block connection (redis) and the
// server drivers (process_one/process_all/enqueue_scheduled_jobs).
func (vm *VM) registerSidekiqModuleMethods(mod *RClass) {
	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// Sidekiq.redis = { url: … } (or a bare URL String) records the connection URL.
	sm("redis=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.sidekiqRedisURL = redisURLFromConfig(args[0])
		return args[0]
	})

	// Sidekiq.configure_client / configure_server yield the module (which responds
	// to redis=) so a block can set config.redis = { url: … }, matching Sidekiq.
	configure := func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk != nil {
			vm.callBlock(blk, []object.Value{self})
		}
		return object.NilV
	}
	sm("configure_client", configure)
	sm("configure_server", configure)

	// Sidekiq.redis { |c| … } yields a block-scoped connection over the shared
	// go-redis client (Closed when the block returns).
	sm("redis", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "Sidekiq.redis requires a block")
		}
		cls := vm.consts["Sidekiq::RedisConnection"].(*RClass)
		return vm.withSidekiqRedis(func(rdb *redisv9.Client) object.Value {
			jr := &JobRedis{cls: cls, rdb: rdb, errClass: "Sidekiq::RedisConnectionError"}
			return vm.callBlock(blk, []object.Value{jr})
		})
	})

	// Sidekiq.process_one(*queues) fetches and runs at most one job, returning
	// whether one was processed.
	sm("process_one", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		queues := queueArgs(args)
		return vm.withSidekiqRedis(func(rdb *redisv9.Client) object.Value {
			if !anyQueueNonEmpty(rdb, queues) {
				return object.Bool(false)
			}
			ok := vm.sidekiqProcess(rdb, queues)
			return object.Bool(ok)
		})
	})

	// Sidekiq.process_all(*queues) drains the queues, running every waiting job,
	// and returns the number processed.
	sm("process_all", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		queues := queueArgs(args)
		return vm.withSidekiqRedis(func(rdb *redisv9.Client) object.Value {
			return object.IntValue(int64(vm.sidekiqDrain(rdb, queues)))
		})
	})

	// Sidekiq.enqueue_scheduled_jobs moves every now-due scheduled/retry job onto
	// its queue and returns the number moved.
	sm("enqueue_scheduled_jobs", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.withSidekiqRedis(func(rdb *redisv9.Client) object.Value {
			n, err := sidekiq.New(rdb).EnqueueScheduledJobs(jobCtx())
			if err != nil {
				sidekiqRaise(err)
			}
			return object.IntValue(int64(n))
		})
	})
}

// sidekiqDrain runs every waiting job across the queues, returning the number
// processed. It re-checks a queue is non-empty before each fetch (so BRPOP never
// blocks) and stops if a fetch yields nothing — the guard against a job another
// consumer claimed between the length check and the fetch.
func (vm *VM) sidekiqDrain(rdb *redisv9.Client, queues []string) int {
	count := 0
	for anyQueueNonEmpty(rdb, queues) {
		if !vm.sidekiqProcess(rdb, queues) {
			break
		}
		count++
	}
	return count
}

// sidekiqProcess runs one job through the library processor over rdb, dispatching
// its body to the Ruby #perform seam. The caller has already confirmed a queue is
// non-empty, so BRPOP returns immediately.
func (vm *VM) sidekiqProcess(rdb *redisv9.Client, queues []string) bool {
	p := sidekiq.New(rdb).NewProcessor(vm.sidekiqPerform(), queues...)
	p.SetTimeout(jobBRPopTimeout)
	ok, err := p.ProcessOne(jobCtx())
	if err != nil {
		sidekiqRaise(err)
	}
	return ok
}

// anyQueueNonEmpty reports whether any of the named queues holds a job, so a
// processor fetch never blocks on an empty set.
func anyQueueNonEmpty(rdb *redisv9.Client, queues []string) bool {
	for _, q := range queues {
		n, err := rdb.LLen(jobCtx(), "queue:"+q).Result()
		if err != nil {
			sidekiqRaise(err)
		}
		if n > 0 {
			return true
		}
	}
	return false
}

// queueArgs reads a *queues argument list into queue-name strings, defaulting to
// the single "default" queue when none are given.
func queueArgs(args []object.Value) []string {
	if len(args) == 0 {
		return []string{"default"}
	}
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = a.ToS()
	}
	return out
}

// redisURLFromConfig extracts a connection URL from a redis= value: a Hash's
// :url (or "url") key, or a bare String / to_s of any other value.
func redisURLFromConfig(v object.Value) string {
	if h, ok := v.(*object.Hash); ok {
		if u, ok := hashOption(h, "url"); ok {
			return u.ToS()
		}
	}
	return v.ToS()
}
