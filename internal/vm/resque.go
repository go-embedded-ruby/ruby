// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	resque "github.com/go-ruby-resque/resque"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerResque installs the Resque surface (require "resque"), backed by the
// pure-Go engine of github.com/go-ruby-resque/resque over
// github.com/redis/go-redis/v9:
//
//   - the Resque module: the Redis config (Resque.redis = …), the block-scoped
//     connection Resque.redis { |c| … }, and the queue API enqueue / enqueue_to /
//     dequeue / size / peek / pop / queues (enqueue derives the queue from the
//     job class's @queue convention);
//   - Resque::Job with the .reserve class method (LPOP the next job) and the
//     reserved job's #perform / #queue / #args;
//   - Resque::Worker (#work / #work_one) draining its queues through the Ruby
//     perform seam and recording failures;
//   - Resque::Failure.count.
//
// The queue/key layout, the byte-exact Resque JSON payload and the worker
// bookkeeping live in the library; this file is the class and method wiring, and
// resque_bind.go holds the Redis-client seam, the Ruby self.perform seam, the
// @queue resolver and the value conversions.
func (vm *VM) registerResque() {
	mod := newClass("Resque", nil)
	mod.isModule = true
	vm.consts["Resque"] = mod
	vm.registerResqueErrors(mod)
	vm.registerResqueRedisConnection(mod)
	vm.registerResqueJob(mod)
	vm.registerResqueWorker(mod)
	vm.registerResqueFailure(mod)
	vm.registerResqueModuleMethods(mod)
}

// registerResqueErrors installs the Resque error tree: Resque::Error <
// StandardError, with Resque::NoQueueError (a job class with no queue) and
// Resque::RedisError (a connection/transport fault) beneath it.
func (vm *VM) registerResqueErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple string, super *RClass) *RClass {
		qualified := "Resque::" + simple
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	base := reg("Error", std)
	reg("NoQueueError", base)
	reg("RedisError", base)
}

// registerResqueRedisConnection installs Resque::RedisConnection, the class of
// the connection object yielded to a Resque.redis block.
func (vm *VM) registerResqueRedisConnection(mod *RClass) {
	cls := newClass("Resque::RedisConnection", vm.cObject)
	mod.consts["RedisConnection"] = cls
	vm.consts["Resque::RedisConnection"] = cls
	defineJobRedisCommands(cls)
}

// registerResqueJob installs Resque::Job: the .reserve class method that LPOPs
// the next job off a queue, plus the reserved job's instance methods (#perform
// runs the body through the Ruby seam; #queue / #args expose its fields).
func (vm *VM) registerResqueJob(mod *RClass) {
	cls := newClass("Resque::Job", vm.cObject)
	mod.consts["Job"] = cls
	vm.consts["Resque::Job"] = cls

	cls.smethods["reserve"] = &Method{name: "reserve", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "Resque::Job.reserve requires a queue")
		}
		queue := args[0].ToS()
		return vm.withResqueRedis(func(r *resque.Resque) object.Value {
			j, err := r.Reserve(queue)
			if err != nil {
				resqueRaise(err)
			}
			if j == nil {
				return object.NilV
			}
			return &ResqueJob{cls: cls, queue: queue, class: j.Class, args: j.Args}
		})
	}}

	cls.define("perform", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		rj := self.(*ResqueJob)
		if err := vm.resquePerform()(rj.class, rj.args); err != nil {
			je := err.(*resque.JobError)
			raise(je.Exception, "%s", je.Message)
		}
		return object.Bool(true)
	})
	cls.define("queue", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ResqueJob).queue)
	})
	cls.define("args", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(anyArgsToRuby(self.(*ResqueJob).args))
	})
	cls.define("payload_class_name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ResqueJob).class)
	})
}

// registerResqueWorker installs Resque::Worker: .new(*queues) builds a worker
// over an ordered queue list, #work drains it (recording failures) and returns
// the number processed, and #work_one processes at most one job.
func (vm *VM) registerResqueWorker(mod *RClass) {
	cls := newClass("Resque::Worker", vm.cObject)
	mod.consts["Worker"] = cls
	vm.consts["Resque::Worker"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		queues := make([]string, len(args))
		for i, a := range args {
			queues[i] = a.ToS()
		}
		return &ResqueWorker{cls: cls, queues: queues}
	}}

	cls.define("work", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		rw := self.(*ResqueWorker)
		return vm.withResqueRedis(func(r *resque.Resque) object.Value {
			n, err := r.NewWorker(resqueWorkerConfig(rw)).Work()
			return resqueWorkResult(n, err)
		})
	})
	cls.define("work_one", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		rw := self.(*ResqueWorker)
		return vm.withResqueRedis(func(r *resque.Resque) object.Value {
			ok, err := r.NewWorker(resqueWorkerConfig(rw)).WorkOne()
			if err != nil {
				resqueRaise(err)
			}
			return object.Bool(ok)
		})
	})
}

// registerResqueFailure installs Resque::Failure.count (the length of the
// resque:failed list).
func (vm *VM) registerResqueFailure(mod *RClass) {
	f := newClass("Resque::Failure", nil)
	f.isModule = true
	mod.consts["Failure"] = f
	vm.consts["Resque::Failure"] = f
	f.smethods["count"] = &Method{name: "count", owner: f, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.withResqueRedis(func(r *resque.Resque) object.Value {
			n, err := r.FailedCount()
			if err != nil {
				resqueRaise(err)
			}
			return object.IntValue(n)
		})
	}}
}

// registerResqueModuleMethods installs the Resque module methods: the Redis
// config/connection and the queue API.
func (vm *VM) registerResqueModuleMethods(mod *RClass) {
	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// Resque.redis = "host:port" / "redis://…" records the connection URL.
	sm("redis=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.resqueRedisURL = redisURLFromConfig(args[0])
		return args[0]
	})

	// Resque.redis { |c| … } yields a block-scoped connection over the shared
	// go-redis client (Closed when the block returns).
	sm("redis", func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "Resque.redis requires a block")
		}
		rdb, err := dialRedis(vm.resqueURL())
		if err != nil {
			raise("Resque::RedisError", "%s", err.Error())
		}
		defer func() { _ = rdb.Close() }()
		cls := vm.consts["Resque::RedisConnection"].(*RClass)
		return vm.callBlock(blk, []object.Value{&JobRedis{cls: cls, rdb: rdb, errClass: "Resque::RedisError"}})
	})

	// Resque.enqueue(klass, *args) pushes onto the queue the job class declares.
	sm("enqueue", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "Resque.enqueue requires a job class")
		}
		className := resqueClassName(args[0])
		jobArgs := vm.jobArgs(args[1:])
		return vm.withResqueRedis(func(r *resque.Resque) object.Value {
			if err := r.Enqueue(className, jobArgs...); err != nil {
				resqueEnqueueError(err)
			}
			return object.Bool(true)
		})
	})

	// Resque.enqueue_to(queue, klass, *args) pushes onto an explicit queue.
	sm("enqueue_to", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "Resque.enqueue_to requires a queue and a job class")
		}
		queue := args[0].ToS()
		className := resqueClassName(args[1])
		jobArgs := vm.jobArgs(args[2:])
		return vm.withResqueRedis(func(r *resque.Resque) object.Value {
			if err := r.EnqueueTo(queue, className, jobArgs...); err != nil {
				resqueRaise(err)
			}
			return object.Bool(true)
		})
	})

	// Resque.dequeue(klass, *args) removes matching jobs and returns the count.
	sm("dequeue", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "Resque.dequeue requires a job class")
		}
		className := resqueClassName(args[0])
		jobArgs := vm.jobArgs(args[1:])
		return vm.withResqueRedis(func(r *resque.Resque) object.Value {
			n, err := r.Dequeue(className, jobArgs...)
			if err != nil {
				resqueEnqueueError(err)
			}
			return object.IntValue(n)
		})
	})

	// Resque.size(queue) returns the number of jobs waiting on a queue.
	sm("size", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "Resque.size requires a queue")
		}
		queue := args[0].ToS()
		return vm.withResqueRedis(func(r *resque.Resque) object.Value {
			n, err := r.Size(queue)
			if err != nil {
				resqueRaise(err)
			}
			return object.IntValue(n)
		})
	})

	// Resque.peek(queue, start=0, count=1) returns waiting jobs without removing
	// them: a single job Hash (or nil) for the default count of 1, else an Array.
	sm("peek", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "Resque.peek requires a queue")
		}
		queue := args[0].ToS()
		start, count := int64(0), int64(1)
		if len(args) >= 2 {
			start = intArg(args[1])
		}
		if len(args) >= 3 {
			count = intArg(args[2])
		}
		return vm.withResqueRedis(func(r *resque.Resque) object.Value {
			jobs, err := r.Peek(queue, start, count)
			if err != nil {
				resqueRaise(err)
			}
			if count == 1 {
				if len(jobs) == 0 {
					return object.NilV
				}
				return resqueJobHash(jobs[0])
			}
			out := make([]object.Value, len(jobs))
			for i, j := range jobs {
				out[i] = resqueJobHash(j)
			}
			return object.NewArrayFromSlice(out)
		})
	})

	// Resque.pop(queue) removes and returns the next job Hash, or nil.
	sm("pop", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "Resque.pop requires a queue")
		}
		queue := args[0].ToS()
		return vm.withResqueRedis(func(r *resque.Resque) object.Value {
			j, err := r.Pop(queue)
			if err != nil {
				resqueRaise(err)
			}
			if j == nil {
				return object.NilV
			}
			return resqueJobHash(j)
		})
	})

	// Resque.queues returns the registered queue names.
	sm("queues", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.withResqueRedis(func(r *resque.Resque) object.Value {
			qs, err := r.Queues()
			if err != nil {
				resqueRaise(err)
			}
			out := make([]object.Value, len(qs))
			for i, q := range qs {
				out[i] = object.NewString(q)
			}
			return object.NewArrayFromSlice(out)
		})
	})
}
