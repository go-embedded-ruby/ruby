// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
)

// resqueSrc prefixes a program with the require and the Redis config pointing at
// an in-process miniredis. The address is passed as a bare host:port (Resque's
// config style), exercising the scheme-defaulting dial path. miniredis is
// TEST-SCOPED — imported only from _test.go.
func resqueSrc(mr *miniredis.Miniredis, body string) string {
	return "require \"resque\"\n" +
		"Resque.redis = \"" + mr.Addr() + "\"\n" + body
}

// TestResqueRequire covers feature registration and the constant tree.
func TestResqueRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "resque"`, "true\n"},
		{`require "resque"; p require "resque"`, "false\n"},
		{`require "resque"; p Resque.is_a?(Module)`, "true\n"},
		{`require "resque"; p Resque::Job.is_a?(Class)`, "true\n"},
		{`require "resque"; p Resque::Worker.is_a?(Class)`, "true\n"},
		{`require "resque"; p Resque::NoQueueError < Resque::Error`, "true\n"},
		{`require "resque"; p Resque::RedisError < Resque::Error`, "true\n"},
		{`require "resque"; p Resque::Error < StandardError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestResqueEnqueuePayload asserts the enqueued job is the byte-exact Resque JSON
// payload, RPUSH'd to resque:queue:<@queue> with the queue registered, using the
// job class's @queue convention.
func TestResqueEnqueuePayload(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, resqueSrc(mr, `
class MyJob
  @queue = :emails
  def self.perform(a, b); end
end
Resque.enqueue(MyJob, 1, "x")
puts(Resque.redis { |c| c.lrange("resque:queue:emails", 0, -1).first })
puts(Resque.redis { |c| c.smembers("resque:queues").include?("emails") })
`))
	want := "{\"class\":\"MyJob\",\"args\":[1,\"x\"]}\ntrue\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestResqueQueueViaMethod covers the .queue class-method fallback of the @queue
// resolver (a class with no @queue ivar but a self.queue method).
func TestResqueQueueViaMethod(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, resqueSrc(mr, `
class ViaMethod
  def self.queue; :vmq; end
  def self.perform; end
end
Resque.enqueue(ViaMethod)
puts Resque.size("vmq")
`))
	if got != "1\n" {
		t.Errorf("got=%q want=%q", got, "1\n")
	}
}

// TestResqueEnqueueToAndInspect covers enqueue_to, size, peek (single and range),
// pop and queues.
func TestResqueEnqueueToAndInspect(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, resqueSrc(mr, `
Resque.enqueue_to("q", "Counter", 1)
Resque.enqueue_to("q", :Counter, 2)
puts Resque.size("q")
p Resque.peek("q")
puts Resque.peek("q", 0, 2).length
p Resque.pop("q")
puts Resque.size("q")
p Resque.peek("empty")
p Resque.pop("empty")
p Resque.queues
`))
	want := "2\n" +
		"{\"class\" => \"Counter\", \"args\" => [1]}\n" +
		"2\n" +
		"{\"class\" => \"Counter\", \"args\" => [1]}\n" +
		"1\n" +
		"nil\n" +
		"nil\n" +
		"[\"q\"]\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestResqueDequeue covers Resque.dequeue removing all jobs of a class.
func TestResqueDequeue(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, resqueSrc(mr, `
class D
  @queue = :dq
  def self.perform(x); end
end
Resque.enqueue(D, 1)
Resque.enqueue(D, 2)
puts Resque.dequeue(D)
puts Resque.size("dq")
`))
	if got != "2\n0\n" {
		t.Errorf("got=%q want=%q", got, "2\n0\n")
	}
}

// TestResqueReservePerform covers Resque::Job.reserve (LPOP) and the reserved
// job's #perform running the class's self.perform, plus #queue / #args.
func TestResqueReservePerform(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, resqueSrc(mr, `
class Greeter
  @queue = :hi
  def self.perform(name); puts "hi #{name}"; end
end
Resque.enqueue(Greeter, "bob")
job = Resque::Job.reserve("hi")
puts job.queue
puts job.payload_class_name
p job.args
job.perform
puts Resque.size("hi")
p Resque::Job.reserve("hi")
`))
	want := "hi\nGreeter\n[\"bob\"]\nhi bob\n0\nnil\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestResqueReservePerformRaises covers a reserved job whose body raises: #perform
// re-raises the true Ruby exception.
func TestResqueReservePerformRaises(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, resqueSrc(mr, `
class Ouch
  @queue = :o
  def self.perform; raise ArgumentError, "bad"; end
end
Resque.enqueue(Ouch)
job = Resque::Job.reserve("o")
begin
  job.perform
rescue ArgumentError => e
  puts "raised #{e.message}"
end
`))
	if got != "raised bad\n" {
		t.Errorf("got=%q", got)
	}
}

// TestResqueWorker covers Resque::Worker#work draining a queue through the perform
// seam, and #work_one processing one job then reporting an empty queue.
func TestResqueWorker(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, resqueSrc(mr, `
class Counter
  @queue = :work
  def self.perform(n); puts "did #{n}"; end
end
Resque.enqueue(Counter, 1)
Resque.enqueue(Counter, 2)
puts Resque::Worker.new("work").work
Resque.enqueue(Counter, 5)
w = Resque::Worker.new("work")
puts w.work_one
puts w.work_one
`))
	want := "did 1\ndid 2\n2\ndid 5\ntrue\nfalse\n"
	if got != want {
		t.Errorf("got=%q\nwant=%q", got, want)
	}
}

// TestResqueWorkerFailure covers the failure path: a raising job still counts as
// processed and is recorded in the resque:failed list (Resque::Failure.count).
func TestResqueWorkerFailure(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, resqueSrc(mr, `
class Fails
  @queue = :bad
  def self.perform; raise "nope"; end
end
Resque.enqueue(Fails)
puts Resque::Worker.new("bad").work
puts Resque::Failure.count
`))
	if got != "1\n1\n" {
		t.Errorf("got=%q want=%q", got, "1\n1\n")
	}
}

// TestResqueRedisBlock covers the Resque.redis { |c| … } block connection.
func TestResqueRedisBlock(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, resqueSrc(mr, `
class Counter
  @queue = :work
  def self.perform(n); end
end
Resque.enqueue(Counter, 1)
puts(Resque.redis { |c| c.llen("resque:queue:work") })
`))
	if got != "1\n" {
		t.Errorf("got=%q want=%q", got, "1\n")
	}
}

// TestResqueNoQueue covers enqueue of a class that declares no queue (and of an
// undefined class name) raising Resque::NoQueueError.
func TestResqueNoQueue(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, resqueSrc(mr, `
class NoQ
  def self.perform; end
end
begin; Resque.enqueue(NoQ); rescue Resque::NoQueueError; puts "noq"; end
begin; Resque.enqueue("Ghost"); rescue Resque::NoQueueError; puts "ghost"; end
begin; Resque.dequeue(NoQ); rescue Resque::NoQueueError; puts "deq"; end
`))
	if got != "noq\nghost\ndeq\n" {
		t.Errorf("got=%q", got)
	}
}

// TestResqueBadURL covers a malformed Redis URL raising Resque::RedisError, both
// on the queue API and on the redis { |c| … } block connection.
func TestResqueBadURL(t *testing.T) {
	got := eval(t, `require "resque"
Resque.redis = "http://nope"
begin
  Resque.enqueue_to("q", "X", 1)
rescue Resque::RedisError
  puts "bad-url"
end
begin
  Resque.redis { |c| c.llen("x") }
rescue Resque::RedisError
  puts "bad-block"
end`)
	if got != "bad-url\nbad-block\n" {
		t.Errorf("got=%q", got)
	}
}

// TestResqueQueueStringIvar covers the @queue convention when the class instance
// variable is a String rather than a Symbol.
func TestResqueQueueStringIvar(t *testing.T) {
	mr := miniredis.RunT(t)
	got := eval(t, resqueSrc(mr, `
class StrQueue
  @queue = "strq"
  def self.perform; end
end
Resque.enqueue(StrQueue)
puts Resque.size("strq")
`))
	if got != "1\n" {
		t.Errorf("got=%q want=%q", got, "1\n")
	}
}

// TestResqueRedisDown covers every Resque surface raising Resque::RedisError when
// the server is unreachable, exercising each operation's Redis-error branch.
func TestResqueRedisDown(t *testing.T) {
	got := eval(t, `require "resque"
Resque.redis = "127.0.0.1:1"
class J
  @queue = :q
  def self.perform; end
end
ops = [
  ->{ Resque.enqueue(J, 1) },
  ->{ Resque.enqueue_to("q", J, 1) },
  ->{ Resque.dequeue(J) },
  ->{ Resque.size("q") },
  ->{ Resque.peek("q") },
  ->{ Resque.pop("q") },
  ->{ Resque.queues },
  ->{ Resque::Job.reserve("q") },
  ->{ Resque::Worker.new("q").work },
  ->{ Resque::Worker.new("q").work_one },
  ->{ Resque::Failure.count },
  ->{ Resque.redis { |c| c.llen("x") } },
]
ops.each do |op|
  begin
    op.call
    puts "no-raise"
  rescue Resque::RedisError
    puts "raised"
  end
end`)
	want := ""
	for i := 0; i < 12; i++ {
		want += "raised\n"
	}
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestResqueArgErrors covers the argument guards on the queue API.
func TestResqueArgErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "resque"; begin; Resque.enqueue; rescue ArgumentError; print "a"; end`, "a"},
		{`require "resque"; begin; Resque.enqueue_to("q"); rescue ArgumentError; print "b"; end`, "b"},
		{`require "resque"; begin; Resque.dequeue; rescue ArgumentError; print "c"; end`, "c"},
		{`require "resque"; begin; Resque.size; rescue ArgumentError; print "d"; end`, "d"},
		{`require "resque"; begin; Resque.peek; rescue ArgumentError; print "e"; end`, "e"},
		{`require "resque"; begin; Resque.pop; rescue ArgumentError; print "f"; end`, "f"},
		{`require "resque"; begin; Resque::Job.reserve; rescue ArgumentError; print "g"; end`, "g"},
		{`require "resque"; begin; Resque.redis; rescue ArgumentError; print "h"; end`, "h"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
