// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	resque "github.com/go-ruby-resque/resque"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the binding between rbgo's Ruby object graph and the pure-Go
// Resque engine of github.com/go-ruby-resque/resque (over
// github.com/redis/go-redis/v9). The queue/job/worker model, the Redis key
// layout and the byte-exact Resque JSON payload live in the library; rbgo
// supplies the three interpreter-dependent seams it documents:
//
//   - the Redis socket: a go-redis client dialled from the URL configured via
//     Resque.redis = … (a URL or bare host:port), falling back to
//     ENV["REDIS_URL"] then the local default. A fresh client is built and
//     Closed per operation (withResqueRedis) so nothing leaks;
//   - the class→queue mapping: Resque's @queue convention, read from the job
//     class's @queue class instance variable (or its .queue method) by
//     resqueResolver;
//   - the job body: a job class's self.perform is Ruby, so the library's
//     PerformFunc seam sends #perform to the class (resquePerform), mapping a
//     raised Ruby exception into a resque.JobError so the failure record names
//     the true class.
//
// It reuses the shared job-binding helpers in sidekiq_bind.go (the go-redis
// client dialling, the JSON argument coder, the decoded-argument→Ruby mapping
// and the JobRedis connection surface).

// errResqueNoQueue signals that a job class did not declare a queue (@queue is
// unset and it has no .queue method); the enqueue binding turns it into a
// Resque::NoQueueError.
var errResqueNoQueue = errors.New("resque: no queue declared for job class")

// resqueURL resolves the Resque Redis URL: the value set via Resque.redis= takes
// precedence, then ENV["REDIS_URL"], then the local default.
func (vm *VM) resqueURL() string { return resolveRedisURL(vm.resqueRedisURL) }

// withResqueRedis dials a fresh go-redis client for the configured Resque URL,
// wraps it in a resque handle carrying the Ruby perform and queue-resolver seams,
// runs fn and Closes the client afterwards. A malformed URL raises
// Resque::RedisError.
func (vm *VM) withResqueRedis(fn func(r *resque.Resque) object.Value) object.Value {
	rdb, err := dialRedis(vm.resqueURL())
	if err != nil {
		raise("Resque::RedisError", "%s", err.Error())
	}
	defer func() { _ = rdb.Close() }()
	r := resque.New(rdb,
		resque.WithContext(jobCtx()),
		resque.WithPerform(vm.resquePerform()),
		resque.WithQueueResolver(vm.resqueResolver()),
	)
	return fn(r)
}

// resquePerform builds the library PerformFunc seam: it runs a job class's Ruby
// self.perform with the decoded args, returning nil on success or a
// *resque.JobError (carrying the true Ruby exception class and message) on a
// raise, so the failure record is gem-faithful.
func (vm *VM) resquePerform() resque.PerformFunc {
	return func(class string, args []any) error {
		rc, msg, failed := vm.runJobPerform(class, args, true)
		if failed {
			return &resque.JobError{Exception: rc, Message: msg}
		}
		return nil
	}
}

// resqueResolver builds the class→queue seam from Resque's @queue convention: it
// reads the job class's @queue class instance variable, or its .queue method,
// returning errResqueNoQueue when neither yields a name.
func (vm *VM) resqueResolver() resque.QueueResolver {
	return func(class string) (string, error) {
		rcls, ok := vm.consts[class].(*RClass)
		if !ok {
			return "", errResqueNoQueue
		}
		if q := resqueQueueName(getIvar(rcls, "@queue")); q != "" {
			return q, nil
		}
		if vm.send(rcls, "respond_to?", []object.Value{object.Symbol("queue")}, nil).Truthy() {
			if q := resqueQueueName(vm.send(rcls, "queue", nil, nil)); q != "" {
				return q, nil
			}
		}
		return "", errResqueNoQueue
	}
}

// resqueQueueName coerces a @queue / .queue value (a Symbol or String) to a queue
// name string; nil or any other type yields "".
func resqueQueueName(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return ""
}

// resqueClassName reads a job class argument into its name: a class constant
// yields its name, and a String / Symbol yields its to_s.
func resqueClassName(v object.Value) string {
	if cls, ok := v.(*RClass); ok {
		return cls.name
	}
	return v.ToS()
}

// resqueEnqueueError maps an enqueue/dequeue error onto a Ruby exception: a
// missing-queue error (from the resolver, or the library's own) becomes
// Resque::NoQueueError; anything else is a Redis fault raised as
// Resque::RedisError.
func resqueEnqueueError(err error) {
	if errors.Is(err, errResqueNoQueue) || errors.Is(err, resque.ErrNoQueueResolver) {
		raise("Resque::NoQueueError", "%s", err.Error())
	}
	resqueRaise(err)
}

// ResqueJob is the Ruby wrapper around a reserved unit of work
// (Resque::Job.reserve): the decoded class name, arguments and source queue. Its
// #perform runs the job body through the Ruby seam; it holds no Redis handle, so
// it can be performed after the reserving connection has closed.
type ResqueJob struct {
	cls   *RClass
	queue string
	class string
	args  []any
}

func (j *ResqueJob) ToS() string { return "(Job{" + j.queue + "} | " + j.class + ")" }
func (j *ResqueJob) Inspect() string {
	return "#<Resque::Job queue=" + j.queue + " class=" + j.class + ">"
}
func (j *ResqueJob) Truthy() bool { return true }

// resqueJobHash renders a decoded job as the {"class"=>…, "args"=>[…]} Hash that
// Resque.peek / Resque.pop return.
func resqueJobHash(j *resque.Job) *object.Hash {
	h := object.NewHash()
	h.Set(object.NewString("class"), object.NewString(j.Class))
	h.Set(object.NewString("args"), object.NewArrayFromSlice(anyArgsToRuby(j.Args)))
	return h
}

// ResqueWorker is the Ruby wrapper around a Resque::Worker: an ordered list of
// queues it drains. Each #work builds a fresh library worker over a fresh
// go-redis client (Closed when the drive returns), so it holds no live handle.
type ResqueWorker struct {
	cls    *RClass
	queues []string
}

func (w *ResqueWorker) ToS() string     { return "#<Resque::Worker>" }
func (w *ResqueWorker) Inspect() string { return "#<Resque::Worker>" }
func (w *ResqueWorker) Truthy() bool    { return true }

// resqueWorkerConfig builds the library worker config for a ResqueWorker with
// deterministic hostname/pid (rbgo has no process identity seam of its own).
func resqueWorkerConfig(w *ResqueWorker) resque.WorkerConfig {
	return resque.WorkerConfig{Hostname: "localhost", PID: 1, Queues: w.queues}
}

// resqueWorkResult maps a (count, err) worker drive result to a Ruby Integer,
// raising a Redis fault as Resque::RedisError.
func resqueWorkResult(n int, err error) object.Value {
	if err != nil {
		resqueRaise(err)
	}
	return object.IntValue(int64(n))
}
