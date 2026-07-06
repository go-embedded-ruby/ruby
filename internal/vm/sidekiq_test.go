// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"regexp"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

// sidekiqSrc prefixes a program with the require and the Redis config pointing at
// an in-process miniredis, so a test drives the whole binding end-to-end with no
// external server (miniredis is TEST-SCOPED — imported only from _test.go).
func sidekiqSrc(mr *miniredis.Miniredis, body string) string {
	return "require \"sidekiq\"\n" +
		"Sidekiq.redis = { url: \"redis://" + mr.Addr() + "\" }\n" + body
}

// TestSidekiqRequire covers the feature registration: require returns true once
// then false, and the module/class/mixin constants exist.
func TestSidekiqRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "sidekiq"`, "true\n"},
		{`require "sidekiq"; p require "sidekiq"`, "false\n"},
		{`require "sidekiq"; p Sidekiq.is_a?(Module)`, "true\n"},
		{`require "sidekiq"; p Sidekiq::Client.is_a?(Class)`, "true\n"},
		{`require "sidekiq"; p Sidekiq::Worker.equal?(Sidekiq::Job)`, "true\n"},
		{`require "sidekiq"; p Sidekiq::RedisConnectionError < Sidekiq::Error`, "true\n"},
		{`require "sidekiq"; p Sidekiq::Error < StandardError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSidekiqEnqueuePayload asserts the enqueued job is the byte-exact Sidekiq
// JSON payload (fixed key order, exact args, generated jid and float timestamps)
// on the queue named by sidekiq_options.
func TestSidekiqEnqueuePayload(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, sidekiqSrc(mr, `
class MyWorker
  include Sidekiq::Job
  sidekiq_options queue: "critical", retry: 5
  def perform(a, b); end
end
MyWorker.perform_async(1, "two")
puts(Sidekiq.redis { |c| c.lrange("queue:critical", 0, -1).first })
puts(Sidekiq.redis { |c| c.smembers("queues").include?("critical") })
`))
	re := regexp.MustCompile(`^\{"retry":5,"queue":"critical","class":"MyWorker","args":\[1,"two"\],"jid":"[0-9a-f]{24}","created_at":[0-9.]+,"enqueued_at":[0-9.]+\}\ntrue\n$`)
	if !re.MatchString(got) {
		t.Errorf("payload mismatch:\n%q", got)
	}
}

// TestSidekiqProcess covers processing: a queued job's Ruby #perform runs with
// its round-tripped arguments (integers stay integers), and process_all reports
// the count drained.
func TestSidekiqProcess(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, sidekiqSrc(mr, `
class Adder
  include Sidekiq::Job
  def perform(a, b)
    puts "sum=#{a + b}"
  end
end
Adder.perform_async(2, 3)
Adder.perform_async(10, 20)
puts "processed=#{Sidekiq.process_all}"
puts "again=#{Sidekiq.process_one}"
`))
	want := "sum=5\nsum=30\nprocessed=2\nagain=false\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestSidekiqProcessArgs covers the decoded-argument mapping: nil, bool, float,
// string, array and a single-key hash all round-trip through the payload back
// into #perform.
func TestSidekiqProcessArgs(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, sidekiqSrc(mr, `
class Echoer
  include Sidekiq::Job
  def perform(a, b, c, d, e, f)
    p [a, b, c, d, e, f]
  end
end
Echoer.perform_async(nil, true, 1.5, "s", [1, 2], {"k" => 9})
Sidekiq.process_all
`))
	want := "[nil, true, 1.5, \"s\", [1, 2], {\"k\" => 9}]\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestSidekiqProcessOneAndQueues covers process_one on a non-empty queue, an
// explicit queue-name argument, and the JobRedis #call escape hatch plus a
// nil (absent-key) reply.
func TestSidekiqProcessOneAndQueues(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, sidekiqSrc(mr, `
class Job1
  include Sidekiq::Job
  sidekiq_options queue: "critical"
  def perform(x); puts "ran #{x}"; end
end
Job1.perform_async(1)
puts "one=#{Sidekiq.process_one("critical")}"
Job1.perform_async(2)
puts "all=#{Sidekiq.process_all("critical")}"
puts(Sidekiq.redis { |c| c.call("llen", "queue:critical") })
p(Sidekiq.redis { |c| c.get("no-such-key") })
`))
	want := "ran 1\none=true\nran 2\nall=1\n0\nnil\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestSidekiqScheduled covers perform_at: a future job lands in the schedule set,
// enqueue_scheduled_jobs moves only the now-due ones onto their queue, and the
// moved job then processes.
func TestSidekiqScheduled(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, sidekiqSrc(mr, `
class Later
  include Sidekiq::Job
  def perform(x); puts "ran #{x}"; end
end
Later.perform_at(Time.now + 3600, 7)
puts(Sidekiq.redis { |c| c.zcard("schedule") })
puts Sidekiq.enqueue_scheduled_jobs
Later.perform_at(Time.now - 3600, 8)
puts Sidekiq.enqueue_scheduled_jobs
puts Sidekiq.process_all
`))
	want := "1\n0\n1\nran 8\n1\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestSidekiqPerformIn covers perform_in: a non-positive interval enqueues
// immediately, a positive interval schedules for later.
func TestSidekiqPerformIn(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, sidekiqSrc(mr, `
class Soon
  include Sidekiq::Job
  def perform(x); end
end
Soon.perform_in(-5, 1)
puts(Sidekiq.redis { |c| c.llen("queue:default") })
Soon.perform_in(3600, 2)
puts(Sidekiq.redis { |c| c.zcard("schedule") })
`))
	if got != "1\n1\n" {
		t.Errorf("got=%q want=%q", got, "1\n1\n")
	}
}

// TestSidekiqClientPush covers Sidekiq::Client#push as a class and instance
// method, with string and symbol item keys and the queue/retry/at options.
func TestSidekiqClientPush(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, sidekiqSrc(mr, `
Sidekiq::Client.push("class" => "Foo", "args" => [1], "queue" => "q")
Sidekiq::Client.new.push(class: "Bar", args: [2])
Sidekiq::Client.push(class: "Baz", args: [3], retry: false, at: Time.now + 100)
puts(Sidekiq.redis { |c| c.llen("queue:q") })
puts(Sidekiq.redis { |c| c.llen("queue:default") })
puts(Sidekiq.redis { |c| c.zcard("schedule") })
`))
	if got != "1\n1\n1\n" {
		t.Errorf("got=%q want=%q", got, "1\n1\n1\n")
	}
}

// TestSidekiqRetry covers the failure path: a raising #perform is retried (ZADD
// to the retry set) with the true Ruby exception class recorded in the payload.
func TestSidekiqRetry(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, sidekiqSrc(mr, `
class Boom
  include Sidekiq::Job
  sidekiq_options retry: 3
  def perform; raise "kaboom"; end
end
Boom.perform_async
puts Sidekiq.process_all
puts(Sidekiq.redis { |c| c.zcard("retry") })
puts(Sidekiq.redis { |c| c.zrange("retry", 0, -1).first.include?("RuntimeError") })
`))
	want := "1\n1\ntrue\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestSidekiqUnknownConstant covers a job whose worker class is not defined: the
// perform seam reports a NameError, so (with the default retry policy) it is
// retried rather than crashing the processor.
func TestSidekiqUnknownConstant(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, sidekiqSrc(mr, `
Sidekiq::Client.push(class: "DoesNotExist", args: [])
puts Sidekiq.process_all
puts(Sidekiq.redis { |c| c.zcard("retry") })
`))
	if got != "1\n1\n" {
		t.Errorf("got=%q want=%q", got, "1\n1\n")
	}
}

// TestSidekiqConfigureBlock covers configure_client/server yielding the module so
// a block can set the Redis URL.
func TestSidekiqConfigureBlock(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, "require \"sidekiq\"\n"+`
Sidekiq.configure_server { |config| config.redis = { url: "redis://`+mr.Addr()+`" } }
Sidekiq.configure_client { |config| config.redis = { url: "redis://`+mr.Addr()+`" } }
class W
  include Sidekiq::Job
  def perform; end
end
W.perform_async
puts(Sidekiq.redis { |c| c.llen("queue:default") })
`)
	if got != "1\n" {
		t.Errorf("got=%q want=%q", got, "1\n")
	}
}

// TestSidekiqBadURL covers a malformed Redis URL raising
// Sidekiq::RedisConnectionError before any command runs.
func TestSidekiqBadURL(t *testing.T) {
	got := eval(t, `require "sidekiq"
Sidekiq.redis = { url: "http://nope" }
class W
  include Sidekiq::Job
  def perform; end
end
begin
  W.perform_async
rescue Sidekiq::RedisConnectionError
  puts "bad-url"
end`)
	if got != "bad-url\n" {
		t.Errorf("got=%q", got)
	}
}

// TestSidekiqRedisDown covers every module surface raising
// Sidekiq::RedisConnectionError when the server is unreachable (a refused port),
// exercising each operation's Redis-error branch.
func TestSidekiqRedisDown(t *testing.T) {
	got := eval(t, `require "sidekiq"
Sidekiq.redis = { url: "redis://127.0.0.1:1" }
class W
  include Sidekiq::Job
  def perform; end
end
ops = [
  ->{ W.perform_async },
  ->{ W.perform_in(1, 2) },
  ->{ W.perform_at(Time.now, 3) },
  ->{ Sidekiq::Client.push(class: "W") },
  ->{ Sidekiq.process_one },
  ->{ Sidekiq.process_all },
  ->{ Sidekiq.enqueue_scheduled_jobs },
  ->{ Sidekiq.redis { |c| c.llen("x") } },
]
ops.each do |op|
  begin
    op.call
    puts "no-raise"
  rescue Sidekiq::RedisConnectionError
    puts "raised"
  end
end`)
	want := "raised\nraised\nraised\nraised\nraised\nraised\nraised\nraised\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestSidekiqArgErrors covers the argument guards on the enqueue/push surface.
func TestSidekiqArgErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "sidekiq"; begin; Sidekiq::Client.push; rescue ArgumentError; print "a"; end`, "a"},
		{`require "sidekiq"; begin; Sidekiq::Client.push(42); rescue TypeError; print "b"; end`, "b"},
		{`require "sidekiq"; begin; Sidekiq::Client.push(args: [1]); rescue ArgumentError; print "c"; end`, "c"},
		{`require "sidekiq"; begin; Sidekiq.redis; rescue ArgumentError; print "d"; end`, "d"},
		{"require \"sidekiq\"\nclass W; include Sidekiq::Job; def perform; end; end\nbegin; W.perform_in; rescue ArgumentError; print \"f\"; end", "f"},
		{"require \"sidekiq\"\nclass W; include Sidekiq::Job; def perform; end; end\nbegin; W.perform_at; rescue ArgumentError; print \"g\"; end", "g"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
