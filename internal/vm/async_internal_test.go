// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	async "github.com/go-ruby-async/async"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// asyncRun runs src on a fresh VM and returns its trimmed stdout, failing on a
// parse/compile/run error.
func asyncRun(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	if _, err := New(&buf).Run(iseq); err != nil {
		t.Fatalf("run: %v", err)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// TestAsyncValueTypes covers the value wrappers' string/truthy surface and
// classOf routing.
func TestAsyncValueTypes(t *testing.T) {
	vm := New(io.Discard)
	vals := []struct {
		v interface {
			ToS() string
			Inspect() string
			Truthy() bool
		}
		cls *RClass
	}{
		{&AsyncTask{cls: vm.cAsyncTask}, vm.cAsyncTask},
		{&AsyncBarrier{cls: vm.consts["Async::Barrier"].(*RClass)}, vm.consts["Async::Barrier"].(*RClass)},
		{&AsyncSemaphore{cls: vm.consts["Async::Semaphore"].(*RClass)}, vm.consts["Async::Semaphore"].(*RClass)},
		{&AsyncCondition{cls: vm.consts["Async::Condition"].(*RClass)}, vm.consts["Async::Condition"].(*RClass)},
		{&AsyncNotification{cls: vm.consts["Async::Notification"].(*RClass)}, vm.consts["Async::Notification"].(*RClass)},
		{&AsyncQueue{cls: vm.consts["Async::Queue"].(*RClass)}, vm.consts["Async::Queue"].(*RClass)},
		{&AsyncLimitedQueue{cls: vm.consts["Async::LimitedQueue"].(*RClass)}, vm.consts["Async::LimitedQueue"].(*RClass)},
		{&AsyncWaiter{cls: vm.consts["Async::Waiter"].(*RClass)}, vm.consts["Async::Waiter"].(*RClass)},
	}
	for _, c := range vals {
		if c.v.ToS() != c.v.Inspect() || !c.v.Truthy() {
			t.Errorf("%T: ToS=%q Inspect=%q Truthy=%v", c.v, c.v.ToS(), c.v.Inspect(), c.v.Truthy())
		}
		if vm.classOf(c.v.(object.Value)) != c.cls {
			t.Errorf("%T: classOf mismatch", c.v)
		}
	}
	if (&asyncRubyError{e: RubyError{Class: "X", Message: "m"}}).Error() != "X: m" {
		t.Fatal("asyncRubyError.Error")
	}
}

// TestAsyncStructuredConcurrency exercises the task tree, waiting, state, the
// synchronization primitives and Async::Task.current end-to-end on the
// deterministic scheduler (no wall-clock sleeps).
func TestAsyncStructuredConcurrency(t *testing.T) {
	src := `
require "async"

r = Async() do |task|
  puts "current? #{Async::Task.current.equal?(task)}"
  child = task.async { |t| 41 + 1 }
  puts "child.wait=#{child.wait}"
  puts "child.state=#{child.state} complete=#{child.complete?} completed=#{child.completed?} running=#{child.running?} failed=#{child.failed?} stopped=#{child.stopped?}"
  puts "child.result=#{child.result}"
  puts "child.parent? #{child.parent.equal?(task)}"
  puts "root.parent=#{task.parent.inspect}"

  barrier = Async::Barrier.new
  puts "barrier.empty? #{barrier.empty?}"
  acc = []
  3.times { |i| barrier.async { |t| acc << i } }
  puts "barrier.size=#{barrier.size}"
  barrier.wait
  puts "barrier=#{acc.sort.inspect}"

  sem = Async::Semaphore.new(1)
  got = sem.acquire { 99 }
  puts "sem.acquire block=#{got} count=#{sem.count} limit=#{sem.limit} blocking?=#{sem.blocking?} waiting=#{sem.waiting}"
  sem.acquire
  sem.release
  sem.limit = 2
  puts "sem.limit now=#{sem.limit}"

  cond = Async::Condition.new
  waiter_task = task.async { |t| cond.wait }
  task.async { |t| cond.signal("hi") }.wait
  puts "cond.empty? #{cond.empty?} wait_count=#{cond.wait_count}"
  puts "cond value=#{waiter_task.wait}"

  note = Async::Notification.new
  nt = task.async { |t| note.wait }
  task.async { |t| note.signal }.wait
  nt.wait
  puts "note.empty? #{note.empty?} wait_count=#{note.wait_count}"

  q = Async::Queue.new
  puts "q.empty? #{q.empty?}"
  q.push("a"); q.enqueue("b"); q << "c"
  puts "q.size=#{q.size} pop=#{q.pop} dequeue=#{q.dequeue}"

  lq = Async::LimitedQueue.new(2)
  lq.enqueue(1); lq.push(2)
  puts "lq.limit=#{lq.limit} size=#{lq.size} limited?=#{lq.limited?} empty?=#{lq.empty?} deq=#{lq.dequeue}"

  waiter = Async::Waiter.new
  waiter.async { |t| 10 }
  waiter.async { |t| 20 }
  puts "waiter.size=#{waiter.size} results=#{waiter.wait.inspect}"

  children_before = task.children.length
  slow = task.async { |t| t.sleep(5); "slow" }
  puts "children includes slow? #{task.children.length > children_before}"
  puts "slow=#{slow.wait}"

  timed = task.with_timeout(10) { |t| "in-time" }
  puts "with_timeout ok=#{timed}"

  task.yield
  nap = task.async { |t| t.sleep; "napped" }
  puts "nap=#{nap.wait}"

  b2 = Async::Barrier.new
  b2.async { |t| t.sleep(50) }
  b2.stop
  puts "b2.empty? #{b2.empty?}"

  w2 = Async::Waiter.new(task)
  w2.async { |t| 7 }
  puts "w2=#{w2.wait.inspect}"

  w3 = Async::Waiter.new(42)
  w3.async { |t| 8 }
  puts "w3=#{w3.wait.inspect}"

  "root-done"
end
puts "result=#{r}"
puts "current outside=#{Async::Task.current.inspect}"
`
	got := asyncRun(t, src)
	want := strings.Join([]string{
		"current? true",
		"child.wait=42",
		"child.state=complete complete=true completed=true running=false failed=false stopped=false",
		"child.result=42",
		"child.parent? true",
		"root.parent=nil",
		"barrier.empty? true",
		"barrier.size=3",
		"barrier=[0, 1, 2]",
		"sem.acquire block=99 count=0 limit=1 blocking?=false waiting=0",
		"sem.limit now=2",
		"cond.empty? true wait_count=0",
		"cond value=hi",
		"note.empty? true wait_count=0",
		"q.empty? true",
		"q.size=3 pop=a dequeue=b",
		"lq.limit=2 size=2 limited?=true empty?=false deq=1",
		"waiter.size=2 results=[10, 20]",
		"children includes slow? true",
		"slow=slow",
		"with_timeout ok=in-time",
		"nap=napped",
		"b2.empty? true",
		"w2=[7]",
		"w3=[8]",
		"result=root-done",
		"current outside=nil",
	}, "\n")
	if got != want {
		t.Fatalf("async mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestAsyncCancellation covers stop (a parked child unwinds via the library's
// cancel panic, re-raised through runAsyncBody), stopping a task then waiting
// (Async::Stop), a task body raising (re-raised verbatim), and a timeout firing
// (Async::TimeoutError).
func TestAsyncCancellation(t *testing.T) {
	src := `
require "async"

# stop a parked child, then wait -> Async::Stop
Async() do |task|
  child = task.async { |t| t.sleep(100); "never" }
  child.stop
  begin
    child.wait
  rescue Async::Stop => e
    puts "stop rescued: #{e.class}"
  end
  puts "child.stopped? #{child.stopped?}"
end

# a task body raising is re-raised verbatim at the waiting boundary
Async() do |task|
  begin
    task.async { |t| raise ArgumentError, "boom" }.wait
  rescue => e
    puts "raise rescued: #{e.class}: #{e.message}"
  end
end

# with_timeout that overruns -> Async::TimeoutError
Async() do |task|
  begin
    task.with_timeout(0.001) { |t| t.sleep(100); "late" }
  rescue Async::TimeoutError => e
    puts "timeout rescued: #{e.class}"
  end
end

# the whole reactor failing propagates out of Kernel#Async
begin
  Async() { |task| raise "top-level" }
rescue => e
  puts "top rescued: #{e.message}"
end

# calling a blocking primitive with no running task raises
begin
  Async::Queue.new.dequeue
rescue => e
  puts "no-task rescued: #{e.class}"
end
`
	got := asyncRun(t, src)
	want := strings.Join([]string{
		"stop rescued: Async::Stop",
		"child.stopped? true",
		"raise rescued: ArgumentError: boom",
		"timeout rescued: Async::TimeoutError",
		"top rescued: top-level",
		"no-task rescued: RuntimeError",
	}, "\n")
	if got != want {
		t.Fatalf("async cancellation mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestAsyncArityErrors covers the ArgumentError guards on the block/argument
// requiring methods.
func TestAsyncArityErrors(t *testing.T) {
	cases := []string{
		`require "async"; Async()`,
		`require "async"; Async() { |task| task.async }`,
		`require "async"; Async() { |task| task.with_timeout(1) }`,
		`require "async"; Async() { |task| task.with_timeout { 1 } }`,
		`require "async"; Async() { |task| Async::Barrier.new.async }`,
		`require "async"; Async() { |task| Async::Waiter.new.async }`,
		`require "async"; Async() { |task| Async::Queue.new.enqueue }`,
		`require "async"; Async() { |task| Async::LimitedQueue.new(1).enqueue }`,
	}
	for _, src := range cases {
		prog, perr := parser.Parse(src)
		if perr != nil {
			t.Fatalf("parse %q: %v", src, perr)
		}
		iseq, cerr := compiler.Compile(prog)
		if cerr != nil {
			t.Fatalf("compile %q: %v", src, cerr)
		}
		_, rerr := New(io.Discard).Run(iseq)
		if rerr == nil || !strings.HasPrefix(rerr.Error(), "ArgumentError") {
			t.Errorf("%q: expected ArgumentError, got %v", src, rerr)
		}
	}
}

// TestAsyncHelpers exercises the pure Go helpers directly, including the
// fall-through branches the Ruby surface does not reach.
func TestAsyncHelpers(t *testing.T) {
	vm := New(io.Discard)

	// asyncResultValue: object.Value, nil, non-value.
	if asyncResultValue(object.IntValue(5)).(object.Integer) != 5 {
		t.Fatal("resultValue value")
	}
	if !object.IsNil(asyncResultValue(nil)) || !object.IsNil(asyncResultValue(123)) {
		t.Fatal("resultValue nil/non-value")
	}

	// asyncDuration: Integer, Float, other.
	if asyncDuration(object.IntValue(2)).Seconds() != 2 {
		t.Fatal("duration int")
	}
	if asyncDuration(object.Float(0.5)).Milliseconds() != 500 {
		t.Fatal("duration float")
	}
	if asyncDuration(object.NewString("x")) != 0 {
		t.Fatal("duration other")
	}

	// asyncInt: default, Integer, Float, other.
	if asyncInt(nil, 7) != 7 {
		t.Fatal("int default")
	}
	if asyncInt([]object.Value{object.IntValue(3)}, 0) != 3 || asyncInt([]object.Value{object.Float(4.9)}, 0) != 4 {
		t.Fatal("int number")
	}
	if asyncInt([]object.Value{object.NewString("x")}, 9) != 9 {
		t.Fatal("int other")
	}

	// asyncRaise: nil is a no-op; a wrapped Ruby error re-raises verbatim; the
	// sentinel errors map to Async exceptions; anything else is a RuntimeError.
	vm.asyncRaise(nil)
	assertRaise(t, "RuntimeError", func() { vm.asyncRaise(errors.New("plain")) })
	assertRaise(t, "Async::Stop", func() { vm.asyncRaise(async.ErrStop) })
	assertRaise(t, "Async::TimeoutError", func() { vm.asyncRaise(async.ErrTimeout) })
	assertRaise(t, "Boom", func() { vm.asyncRaise(&asyncRubyError{e: RubyError{Class: "Boom", Message: "x"}}) })

	// asyncCaller raises when no task is running.
	assertRaise(t, "RuntimeError", func() { vm.asyncCaller() })

	// Semaphore#limit= with no argument raises (the setter arity guard is not
	// reachable through Ruby setter syntax, which always supplies a value).
	semCls := vm.consts["Async::Semaphore"].(*RClass)
	sem := &AsyncSemaphore{s: async.NewSemaphore(1), cls: semCls}
	assertRaise(t, "ArgumentError", func() {
		semCls.methods["limit="].native(vm, sem, nil, nil)
	})
}

// assertRaise asserts fn panics a RubyError of the given class.
func assertRaise(t *testing.T, wantClass string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		re, ok := r.(RubyError)
		if !ok || re.Class != wantClass {
			t.Fatalf("expected raise %s, got %#v", wantClass, r)
		}
	}()
	fn()
}
