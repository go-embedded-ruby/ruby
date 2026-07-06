// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// ajSrc prefixes a program with require "active_job".
func ajSrc(body string) string { return "require \"active_job\"\n" + body }

// TestActiveJobRequire covers the feature registration: require returns true once
// then false, and the module / class / error constants exist.
func TestActiveJobRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "active_job"`, "true\n"},
		{`require "active_job"; p require "active_job"`, "false\n"},
		{`p require "activejob"`, "true\n"},
		{`require "active_job"; p ActiveJob.is_a?(Module)`, "true\n"},
		{`require "active_job"; p ActiveJob::Base.is_a?(Class)`, "true\n"},
		{`require "active_job"; p ActiveJob::Arguments.is_a?(Module)`, "true\n"},
		{`require "active_job"; p ActiveJob::SerializationError < ArgumentError`, "true\n"},
		{`require "active_job"; p ActiveJob::DeserializationError < StandardError`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveJobPerform covers perform_now / perform_later as class and instance
// methods, running the Ruby #perform inline through the default inline adapter.
func TestActiveJobPerform(t *testing.T) {
	got := eval(t, ajSrc(`
class GreetJob < ActiveJob::Base
  def perform(name)
    puts "hello #{name}"
  end
end
GreetJob.perform_now("a")
GreetJob.perform_later("b")
GreetJob.new("z").perform_now
GreetJob.new("q").perform_later
puts GreetJob.queue_adapter

class FailLater < ActiveJob::Base
  def perform; raise "nope"; end
end
begin
  FailLater.perform_later
rescue RuntimeError
  puts "flater"
end
`))
	want := "hello a\nhello b\nhello z\nhello q\ninline\nflater\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestActiveJobReaders covers queue_as (static and block), the instance readers
// (job_id / queue_name / executions / priority / arguments / serialize) and the
// unimplemented-#perform stub.
func TestActiveJobReaders(t *testing.T) {
	got := eval(t, ajSrc(`
class MailJob < ActiveJob::Base
  queue_as :mailers
  def perform; end
end
job = MailJob.perform_later
puts job.queue_name
puts job.executions
puts job.priority.inspect
puts job.job_id.length

class DynJob < ActiveJob::Base
  queue_as { "dyn" }
  def perform; end
end
puts DynJob.perform_later.queue_name

class ArgJob < ActiveJob::Base
  def perform(a, b); end
end
j = ArgJob.new(1, "x")
p j.arguments
puts j.serialize["job_class"]
puts j.serialize["arguments"].length

class NoImpl < ActiveJob::Base; end
begin
  NoImpl.perform_now
rescue NotImplementedError
  puts "noimpl"
end
`))
	want := "mailers\n1\nnil\n36\ndyn\n[1, \"x\"]\nArgJob\n2\nnoimpl\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestActiveJobArgumentRoundTrip covers the Ruby <-> activejob argument coding on
// the perform path: nil, bool, float, symbol, array, hash (symbol key) and an
// unknown object all round-trip through #perform.
func TestActiveJobArgumentRoundTrip(t *testing.T) {
	got := eval(t, ajSrc(`
class EchoJob < ActiveJob::Base
  def perform(x); p x; end
end
EchoJob.perform_now(true)
EchoJob.perform_now(nil)
EchoJob.perform_now(1.5)
EchoJob.perform_now(:hi)

class MixJob < ActiveJob::Base
  def perform(a, h); p a; puts h[:k]; end
end
MixJob.perform_now([1, 2], {k: 9})

class Box
  def to_s; "box"; end
end
class BoxJob < ActiveJob::Base
  def perform(b); puts b.to_s; end
end
BoxJob.perform_now(Box.new)
`))
	want := "true\nnil\n1.5\n:hi\n[1, 2]\n9\nbox\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestActiveJobSet covers set(queue:/priority:/wait:) building a configured single
// run, the ConfiguredJob wrapper value (to_s / inspect / truthiness), and the
// option-parsing edge cases (no trailing hash, no options).
func TestActiveJobSet(t *testing.T) {
	got := eval(t, ajSrc(`
class SetJob < ActiveJob::Base
  def perform(x); puts "did #{x}"; end
end
j = SetJob.set(queue: "q1", priority: 3).perform_later(7)
puts j.queue_name
puts j.priority
SetJob.set(wait: 0.001).perform_now(8)
SetJob.set(wait: 0).perform_now(9)
SetJob.set("notahash").perform_now(10)
SetJob.set.perform_now(11)
p SetJob.set(priority: 2.5)
p SetJob.set(wait: 0).to_s
p(SetJob.set(wait: 0) ? :y : :n)
`))
	want := "did 7\nq1\n3\ndid 8\ndid 9\ndid 10\ndid 11\n" +
		"#<ActiveJob::ConfiguredJob SetJob>\n" +
		"\"#<ActiveJob::ConfiguredJob SetJob>\"\n" +
		":y\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestActiveJobRetryDiscard covers retry_on (matching retries up to the attempt
// count, then re-raises; with a wait delay; a non-matching error propagates) and
// discard_on (a matching error is swallowed), plus the option-parsing branches.
func TestActiveJobRetryDiscard(t *testing.T) {
	got := eval(t, ajSrc(`
class RetryJob < ActiveJob::Base
  retry_on RuntimeError, attempts: 3
  def perform; puts "try"; raise "boom"; end
end
begin
  RetryJob.perform_now
rescue RuntimeError
  puts "gaveup"
end

class WaitJob < ActiveJob::Base
  retry_on RuntimeError, wait: 0.001, attempts: 2
  def perform; puts "w"; raise "x"; end
end
begin
  WaitJob.perform_now
rescue RuntimeError
  puts "done"
end

class NoMatchJob < ActiveJob::Base
  retry_on ArgumentError, attempts: 3
  def perform; puts "n"; raise "boom"; end
end
begin
  NoMatchJob.perform_now
rescue RuntimeError
  puts "up"
end

class DiscardJob < ActiveJob::Base
  discard_on RuntimeError
  def perform; puts "d"; raise "boom"; end
end
DiscardJob.perform_now
puts "after"

class RegJob < ActiveJob::Base
  retry_on ArgumentError, wait: 0.001
  retry_on ArgumentError
  retry_on ArgumentError, 99
  discard_on ArgumentError
  def perform; puts "reg"; end
end
RegJob.perform_now
`))
	want := "try\ntry\ntry\ngaveup\n" +
		"w\nw\ndone\n" +
		"n\nup\n" +
		"d\nafter\n" +
		"reg\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestActiveJobCallbacks covers the before/after/around _perform and _enqueue
// callback chains (their ordering, around halting by not yielding, and an around
// block that yields to a raising #perform) and the missing-block guard.
func TestActiveJobCallbacks(t *testing.T) {
	got := eval(t, ajSrc(`
class CbJob < ActiveJob::Base
  before_perform { puts "before" }
  after_perform { puts "after" }
  around_perform { |job, blk| puts "around-in"; blk.call; puts "around-out" }
  before_enqueue { puts "b-enq" }
  after_enqueue { puts "a-enq" }
  around_enqueue { |job, blk| puts "aenq-in"; blk.call; puts "aenq-out" }
  def perform; puts "perform"; end
end
CbJob.perform_later

class HaltJob < ActiveJob::Base
  around_perform { |job, blk| puts "halt" }
  def perform; puts "should-not-run"; end
end
HaltJob.perform_now
puts "post"

class AroundRaise < ActiveJob::Base
  around_perform { |job, blk| puts "ar"; blk.call; puts "after-call" }
  def perform; raise "boom"; end
end
begin
  AroundRaise.perform_now
rescue RuntimeError
  puts "caught"
end
`))
	want := "b-enq\naenq-in\nbefore\naround-in\nperform\naround-out\nafter\naenq-out\na-enq\n" +
		"halt\npost\n" +
		"ar\nafter-call\ncaught\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestActiveJobTestAdapter covers the :test queue adapter: perform_later records
// jobs (enqueued_jobs), perform_enqueued_jobs drains and runs them, queue_adapter
// reports the selected adapter, and switching back to :inline drops the recorder.
func TestActiveJobTestAdapter(t *testing.T) {
	got := eval(t, ajSrc(`
class TestJob < ActiveJob::Base
  self.queue_adapter = :test
  def perform(x); puts "ran #{x}"; end
end
puts TestJob.queue_adapter
TestJob.perform_later("x")
puts TestJob.enqueued_jobs.length
puts TestJob.perform_enqueued_jobs
puts TestJob.enqueued_jobs.length

class Flip < ActiveJob::Base
  self.queue_adapter = :test
  self.queue_adapter = :inline
  def perform; end
end
puts Flip.queue_adapter
`))
	want := "test\n1\nran x\n1\n0\ninline\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestActiveJobPerformAllLater covers ActiveJob.perform_all_later enqueuing several
// instances at once and its guard against non-job arguments.
func TestActiveJobPerformAllLater(t *testing.T) {
	got := eval(t, ajSrc(`
class AllJob < ActiveJob::Base
  def perform(x); puts "all #{x}"; end
end
ActiveJob.perform_all_later(AllJob.new(1), AllJob.new(2))
begin
  ActiveJob.perform_all_later(5)
rescue ArgumentError
  puts "notjob"
end
begin
  ActiveJob.perform_all_later(Object.new)
rescue ArgumentError
  puts "notjob2"
end
`))
	want := "all 1\nall 2\nnotjob\nnotjob2\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestActiveJobArguments covers ActiveJob::Arguments.serialize / deserialize: the
// wire form (symbol-keys marker), the plain / string-keyed / indifferent hash and
// GlobalID round-trips, and the GlobalID serialization seam.
func TestActiveJobArguments(t *testing.T) {
	got := eval(t, ajSrc(`
ser = ActiveJob::Arguments.serialize([nil, true, 1, 1.5, [1], {name: "x"}, "s"])
p ser[0]
p ser[1]
puts ser[5]["name"]
p ser[5]["_aj_symbol_keys"]

r = ActiveJob::Arguments.deserialize(ActiveJob::Arguments.serialize([{name: "x"}]))
puts r[0][:name]
s = ActiveJob::Arguments.deserialize(ActiveJob::Arguments.serialize([{"str" => 1}]))
puts s[0]["str"]

ih = ActiveJob::Arguments.deserialize([{"a" => 1, "_aj_hash_with_indifferent_access" => true}])
puts ih[0]["a"]

gid = ActiveJob::Arguments.deserialize([{"_aj_globalid" => "gid://app/User/1"}])
puts gid[0]

prim = ActiveJob::Arguments.deserialize([nil, true, 2.5])
p prim

class Widget
  def to_global_id; "gid://app/Widget/9"; end
end
w = ActiveJob::Arguments.serialize([Widget.new])
puts w[0]["_aj_globalid"]
`))
	want := "nil\ntrue\nx\n[\"name\"]\nx\n1\n1\ngid://app/User/1\n[nil, true, 2.5]\ngid://app/Widget/9\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

// TestActiveJobArgumentErrors covers the serialize / deserialize error branches:
// an unsupported argument type, a non-string/symbol hash key, a rich serialized
// type not reconstructed at the Ruby boundary, a bad payload, and the argument
// guards on the module methods.
func TestActiveJobArgumentErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{ajSrc(`class Plain; end
begin; ActiveJob::Arguments.serialize([Plain.new]); rescue ActiveJob::SerializationError; puts "serr"; end`), "serr\n"},
		{ajSrc(`begin; ActiveJob::Arguments.serialize([{1 => 2}]); rescue ActiveJob::SerializationError; puts "kerr"; end`), "kerr\n"},
		{ajSrc(`begin
  ActiveJob::Arguments.deserialize([{"_aj_serialized" => "ActiveJob::Serializers::TimeSerializer", "value" => "2020-01-01T00:00:00.000000000Z"}])
rescue ActiveJob::DeserializationError; puts "derr"; end`), "derr\n"},
		{ajSrc(`begin
  ActiveJob::Arguments.deserialize([{"_aj_serialized" => "Unknown::Serializer", "value" => "x"}])
rescue ActiveJob::DeserializationError; puts "derr2"; end`), "derr2\n"},
		{ajSrc(`begin; ActiveJob::Arguments.serialize; rescue ArgumentError; puts "aa1"; end`), "aa1\n"},
		{ajSrc(`begin; ActiveJob::Arguments.serialize(5); rescue TypeError; puts "aa2"; end`), "aa2\n"},
		{ajSrc(`begin; ActiveJob::Arguments.deserialize; rescue ArgumentError; puts "aa3"; end`), "aa3\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveJobErrors covers the guards on the class DSL and adapter methods:
// queue_as / retry_on / discard_on / callback argument errors, an unknown queue
// adapter, and the :test-only inspection helpers on a non-test class, plus the
// serialize reader's serialization-error branch.
func TestActiveJobErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{ajSrc(`begin; class J < ActiveJob::Base; queue_as; end; rescue ArgumentError; puts "qerr"; end`), "qerr\n"},
		{ajSrc(`begin; class J < ActiveJob::Base; retry_on; end; rescue ArgumentError; puts "rerr"; end`), "rerr\n"},
		{ajSrc(`begin; class J < ActiveJob::Base; retry_on "nope"; end; rescue TypeError; puts "terr"; end`), "terr\n"},
		{ajSrc(`begin; class J < ActiveJob::Base; before_perform; end; rescue ArgumentError; puts "cberr"; end`), "cberr\n"},
		{ajSrc(`begin; class J < ActiveJob::Base; around_perform; end; rescue ArgumentError; puts "arerr"; end`), "arerr\n"},
		{ajSrc(`begin; class J < ActiveJob::Base; self.queue_adapter = :bogus; end; rescue ArgumentError; puts "aderr"; end`), "aderr\n"},
		{ajSrc(`class J < ActiveJob::Base; def perform; end; end
begin; J.enqueued_jobs; rescue ArgumentError; puts "notest"; end`), "notest\n"},
		{ajSrc(`class J < ActiveJob::Base; def perform; end; end
begin; J.send(:queue_adapter=); rescue ArgumentError; puts "noarg"; end`), "noarg\n"},
		{ajSrc(`class Plain2; end
class J < ActiveJob::Base; def perform(x); end; end
begin; J.new(Plain2.new).serialize; rescue ActiveJob::SerializationError; puts "sererr"; end`), "sererr\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActiveJobQueueName covers the class-level queue_name reader reporting the
// declared (or default) queue.
func TestActiveJobQueueName(t *testing.T) {
	cases := []struct{ src, want string }{
		{ajSrc(`class A < ActiveJob::Base; def perform; end; end
puts A.queue_name`), "default\n"},
		{ajSrc(`class B < ActiveJob::Base; queue_as :low; def perform; end; end
puts B.queue_name`), "low\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
