// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

// evalDeadline runs src and fails within d if it has not finished, naming the
// program that blocked instead of hanging the test binary.
//
// Every witness in this file is a program that DEADLOCKED before the fixes it
// covers, so every one of them needs its own bound. #695 is the cost of a
// concurrency test without one: a single hung test consumed 80 minutes of a
// 90-minute lane and reported as `panic: test timed out` with no failing
// assertion anywhere in the job, which is far more expensive to diagnose than
// "accept did not return within 5s".
//
// The bound belongs to the TEST and not to the VM: a deadline in a blocking
// primitive where MRI blocks indefinitely would be a divergence, not a fix. On a
// timeout the VM goroutine is left behind, because Go cannot interrupt a
// goroutine blocked in a syscall or on a mutex; each call builds a fresh VM, so a
// leaked one blocks nothing that follows.
func evalDeadline(t *testing.T, d time.Duration, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	type res struct {
		out string
		err error
	}
	done := make(chan res, 1)
	go func() {
		var buf bytes.Buffer
		_, runErr := vm.New(&buf).Run(iseq)
		done <- res{buf.String(), runErr}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("runtime error: %v", r.err)
		}
		return r.out
	case <-time.After(d):
		t.Fatalf("program did not finish within %s — a thread is blocked in a wait "+
			"nothing can end:\n%s", d, src)
		return ""
	}
}

// TestThreadKillInterruptsBlockingWait covers the interruptibility of the waits a
// Thread#kill or Thread#raise must reach. MRI's blocking primitives are
// interruptible by construction: threadptr_interrupt_locked (thread.c) sets the
// interrupt flag and calls a registered unblocking function, and each wait loops
// around RUBY_VM_CHECK_INTS_BLOCKING — do_mutex_lock and queue_sleep
// (thread_sync.c), thread_join_sleep (thread.c). Go's sync.Mutex and channel
// receives have no such hook, so each of these waits is a wait queue plus a
// select on the thread's interrupt channel.
//
// Every expectation here is MRI 4.0.5's own output for the same program.
func TestThreadKillInterruptsBlockingWait(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// A thread killed while blocked in Mutex#lock terminates, so the join returns.
		{"kill_in_mutex_lock", `m = Mutex.new
m.lock
t = Thread.new { m.lock }
sleep 0.05
t.kill
t.join
puts "joined"`, "joined\n"},
		// The same for Queue#pop, whose wait is a different queue in a different file.
		{"kill_in_queue_pop", `q = Queue.new
t = Thread.new { q.pop }
sleep 0.05
t.kill
t.join
puts "joined"`, "joined\n"},
		// A SizedQueue#push blocked on a full queue is equally interruptible.
		{"kill_in_sized_queue_push", `q = SizedQueue.new(1)
q.push(:full)
t = Thread.new { q.push(:blocked) }
sleep 0.05
t.kill
t.join
puts "joined"`, "joined\n"},
		// Thread#raise reaches a thread blocked in Mutex#lock, not only a sleeping one.
		{"raise_in_mutex_lock", `m = Mutex.new
m.lock
t = Thread.new { begin; m.lock; rescue => e; puts "rescued #{e.message}"; end }
sleep 0.05
t.raise("wake")
t.join`, "rescued wake\n"},
		{"raise_in_queue_pop", `q = Queue.new
t = Thread.new { begin; q.pop; rescue => e; puts "rescued #{e.message}"; end }
sleep 0.05
t.raise("wake")
t.join`, "rescued wake\n"},
		// A killed JOINER leaves its join; the target keeps running.
		{"kill_the_joiner", `a = Thread.new { sleep }
j = Thread.new { a.join; puts "joiner returned" }
sleep 0.05
j.kill
j.join
puts "joiner dead: #{j.status.inspect}"
a.kill`, "joiner dead: false\n"},
		// A Mutex held by a killed thread is released, as
		// rb_threadptr_unlock_all_locking_mutexes (thread.c) does on thread exit.
		{"killed_owner_releases_mutex", `m = Mutex.new
t = Thread.new { m.lock; sleep }
sleep 0.05
t.kill
t.join
m.lock
puts "relocked"`, "relocked\n"},
		// And by a thread that died on an unhandled exception.
		{"failed_owner_releases_mutex", `m = Mutex.new
t = Thread.new { m.lock; raise "boom" }
begin; t.join; rescue RuntimeError; end
m.lock
puts "relocked"`, "relocked\n"},
		// A waiter already queued behind the dead owner gets the mutex, rather than
		// the release stranding it.
		{"waiter_inherits_from_dead_owner", `m = Mutex.new
owner = Thread.new { m.lock; sleep }
sleep 0.05
w = Thread.new { m.lock; puts "waiter got it" }
sleep 0.05
owner.kill
owner.join
w.join`, "waiter got it\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := evalDeadline(t, 10*time.Second, c.src); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

// TestBlockingWaitNotInterruptedWhenItShouldNotBe is the other half of the
// proof: making a wait interruptible must not make it interruptED. A lock that
// succeeds immediately, a kill on a thread that is not blocked, an ordinary join
// and a Queue#pop satisfied by a push all behave exactly as before — and as MRI
// 4.0.5 does. Fixing a hang by letting a lock spin or time out would pass the
// tests above and fail these.
func TestBlockingWaitNotInterruptedWhenItShouldNotBe(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"lock_succeeds_immediately", `m = Mutex.new
p m.lock.equal?(m)
p m.locked?
p m.unlock.equal?(m)
p m.synchronize { :body }
t = Thread.new { m.synchronize { :inner } }
p t.value`, "true\ntrue\ntrue\n:body\n:inner\n"},
		{"kill_a_thread_that_is_not_blocked", `q = Queue.new
t = Thread.new { q.push(:go); loop { Thread.pass } }
q.pop
t.kill
t.join
p [t.status, t.alive?, t.value]`, "[false, false, nil]\n"},
		{"ordinary_join_and_pop", `t = Thread.new { 1 + 1 }
p t.join.equal?(t)
p t.value
q = Queue.new
t2 = Thread.new { q.pop }
q.push(:x)
p t2.value`, "true\n2\n:x\n"},
		// Mutex ownership still moves strictly in queue order under contention, so the
		// release-on-death bookkeeping has not turned the mutex into a free-for-all.
		{"mutual_exclusion_holds", `m = Mutex.new
n = 0
ts = 8.times.map { Thread.new { 200.times { m.synchronize { n += 1 } } } }
ts.each(&:join)
p n`, "1600\n"},
		// A join with a timeout still times out rather than being ended early, and
		// still returns nil for a thread that has not finished.
		{"timed_join_still_times_out", `t = Thread.new { sleep }
p t.join(0.05)
t.kill
p t.join(5).equal?(t)`, "nil\ntrue\n"},
		// An unhandled exception WITHOUT abort_on_exception does not reach the main
		// thread: its sleep runs to completion, as in MRI.
		{"no_abort_does_not_interrupt_main", `Thread.new { raise "boom" }
sleep 0.2
puts "main survived"`, "main survived\n"},
		// The relock at the end of ConditionVariable#wait / Mutex#sleep is
		// UNINTERRUPTIBLE, so a thread killed while blocked acquiring the mutex still
		// gets it before it unwinds -- MRI's rb_mutex_sleep uses
		// mutex_lock_uninterruptible as its ensure clause (thread_sync.c), and
		// core/conditionvariable/wait_spec.rb asserts exactly this.
		//
		// This one PASSES on unmodified main: it is a regression guard, not a witness
		// for a new defect. Making the relock interruptible along with every other
		// wait broke it, and the Ruby ensure clause's unlock then raised "Attempt to
		// unlock a mutex which is not locked" -- caught by the corpus, one example.
		{"relock_after_condvar_wait_is_uninterruptible", `m = Mutex.new
cv = ConditionVariable.new
in_sync = false
owned = nil
th = Thread.new do
  m.synchronize do
    in_sync = true
    begin
      cv.wait(m)
    ensure
      owned = m.owned?
    end
  end
end
Thread.pass until in_sync
Thread.pass until th.stop?
m.synchronize do
  cv.signal
  sleep 0.05
  th.kill
end
th.join
p owned
p m.locked?`, "true\nfalse\n"},
		// A raise masked by Thread.handle_interrupt(:never) does not end the wait it
		// arrives during — the wait goes back to sleep, as do_mutex_lock's loop does.
		{"masked_raise_does_not_end_the_wait", `q = Queue.new
t = Thread.new do
  Thread.handle_interrupt(RuntimeError => :never) do
    q.pop
    puts "pop returned normally"
  end
end
sleep 0.05
t.raise("masked")
sleep 0.05
q.push(:v)
begin; t.join; rescue RuntimeError => e; puts "delivered on exit: #{e.message}"; end`,
			"pop returned normally\ndelivered on exit: masked\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := evalDeadline(t, 15*time.Second, c.src); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

// TestAbortOnExceptionInterruptsMainThread covers Thread#abort_on_exception= and
// Thread.abort_on_exception=: an unhandled exception in such a thread is raised
// in the MAIN thread, so a bare `sleep` there ENDS. MRI does this at the tail of
// thread_start_func_2 (thread.c) with rb_threadptr_raise(ractor_main_th, ...).
//
// The flag was previously stored by its setter and read by its getter and by
// nothing else: a declared field equal to its own default, which no program could
// tell from the feature being absent.
func TestAbortOnExceptionInterruptsMainThread(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"class_level_flag", `Thread.abort_on_exception = true
begin
  Thread.new { raise "boom" }
  sleep 5
  puts "NOT INTERRUPTED"
rescue RuntimeError => e
  puts "interrupted: #{e.message}"
end`, "interrupted: boom\n"},
		{"per_thread_flag", `begin
  t = Thread.new { sleep 0.05; raise "later" }
  t.abort_on_exception = true
  sleep 5
  puts "NOT INTERRUPTED"
rescue RuntimeError => e
  puts "interrupted: #{e.message}"
end`, "interrupted: later\n"},
		// The exception the main thread sees is the very object the thread raised.
		{"same_exception_object", `exc = RuntimeError.new("shared")
Thread.abort_on_exception = true
begin
  Thread.new { raise exc }
  sleep 5
rescue RuntimeError => e
  p e.equal?(exc)
end`, "true\n"},
		// The accessors read back what was set — and a new thread does NOT inherit the
		// class-level flag, which is why the third line is false. Nothing in MRI's
		// thread_create_core assigns th->abort_on_exception (unlike
		// report_on_exception, which IS copied from the VM default), and termination
		// tests the two independently. rbgo seeded the per-thread flag from the
		// class-level one and answered true here.
		{"accessors", `p Thread.abort_on_exception
Thread.abort_on_exception = true
p Thread.abort_on_exception
t = Thread.new { sleep }
p t.abort_on_exception
p Thread.new { sleep 0.01 }.report_on_exception
t.abort_on_exception = false
p t.abort_on_exception
t.kill`, "false\ntrue\nfalse\ntrue\nfalse\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := evalDeadline(t, 15*time.Second, c.src); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}
