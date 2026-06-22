package vm_test

import (
	"strings"
	"testing"
)

// TestThread covers Thread, Mutex and Queue, asserted against MRI Ruby 4.0.5.
// Threads run on an emulated GVL with eager start (a new thread runs until its
// first block), which makes the interleavings here deterministic.
func TestThread(t *testing.T) {
	cases := []struct{ src, want string }{
		// Basic spawn / value / join.
		{`t = Thread.new { 1 + 2 }; p t.value`, "3\n"},
		{`p [1, 2].map { |i| Thread.new(i) { |x| x * 10 } }.map(&:value)`, "[10, 20]\n"},
		{`t = Thread.new { 5 }; t.join; p [t.alive?, t.status]`, "[false, false]\n"},
		{`t = Thread.new { 1 }; t.join; p [t.status, t.name]`, "[false, nil]\n"},
		{`p Thread.current.status`, "\"run\"\n"},
		{`p Thread.current == Thread.main`, "true\n"},
		{`p Thread.list.include?(Thread.current)`, "true\n"},
		{`t = Thread.new { 1 }; t.join; p Thread.list.include?(t)`, "false\n"},
		{`p Thread.new { Thread.current.name = "w"; Thread.current.name }.value`, "\"w\"\n"},
		{`t = Thread.current; t[:a] = 1; p [t[:a], t["a"], t[:z], t.key?(:a), t.key?(:z)]`, "[1, 1, nil, true, false]\n"},
		{`t = Thread.current; t.abort_on_exception = true; p t.abort_on_exception`, "true\n"},
		{`Thread.pass; p :passed`, ":passed\n"},

		// Exception in a thread is re-raised on join, and changes its status.
		{`t = Thread.new { raise "boom" }; begin; t.join; rescue => e; p e.message; end`, "\"boom\"\n"},
		{`t = Thread.new { raise "x" }; begin; t.join; rescue; end; p t.status`, "nil\n"},

		// Mutex.
		{`p [Mutex.equal?(Thread::Mutex), Queue.equal?(Thread::Queue)]`, "[true, true]\n"},
		{`m = Mutex.new; p [m.locked?, m.lock.locked?, m.unlock.locked?]`, "[false, true, false]\n"},
		{`m = Mutex.new; a = m.try_lock; b = m.try_lock; m.unlock; p [a, b]`, "[true, false]\n"},
		{`m = Mutex.new; r = [m.owned?]; m.lock; r << m.owned?; m.unlock; r << m.owned?; p r`, "[false, true, false]\n"},
		{`m = Mutex.new; m.lock; begin; m.lock; rescue ThreadError; p :recursive; end`, ":recursive\n"},
		{`m = Mutex.new; begin; m.unlock; rescue ThreadError; p :notowned; end`, ":notowned\n"},
		{`m = Mutex.new; n = 0; 3.times.map { Thread.new { m.synchronize { n += 1 } } }.each(&:join); p n`, "3\n"},
		// Real contention: a thread blocks acquiring a mutex the main thread holds,
		// then proceeds once main releases it (exercises the lock waitq + handoff).
		{`m = Mutex.new; m.lock; b = Thread.new { m.lock; m.unlock; :ok }; r = m.locked?; m.unlock; p [r, b.value]`, "[true, :ok]\n"},

		// Queue: FIFO, blocking pop woken by push, close semantics, aliases.
		{`q = Queue.new; q.push(1); p [q.size, q.empty?, q.pop, q.empty?]`, "[1, false, 1, true]\n"},
		{`q = Queue.new; q << 1; q.enq(2); q.push(3); p [q.deq, q.shift, q.pop, q.num_waiting, q.length]`, "[1, 2, 3, 0, 0]\n"},
		{`q = Queue.new; q.push(1); q.clear; p q.empty?`, "true\n"},
		{`q = Queue.new; r = [q.closed?]; q.close; r << q.closed?; p r`, "[false, true]\n"},
		{`q = Queue.new; t = Thread.new { q.pop }; q.push(42); p t.value`, "42\n"},
		{`q = Queue.new; t = Thread.new { q.pop + 1 }; q.push(10); p t.value`, "11\n"},
		{`q = Queue.new; q.close; p q.pop`, "nil\n"},
		{`q = Queue.new; t = Thread.new { q.pop }; q.close; p t.value`, "nil\n"},
		{`q = Queue.new; begin; q.pop(true); rescue ThreadError; p :empty; end`, ":empty\n"},
		{`q = Queue.new; q.close; begin; q.push(1); rescue ClosedQueueError; p :closed; end`, ":closed\n"},

		// sleep (GVL-aware) returns the (truncated) seconds slept.
		{`p [sleep(0), sleep(0.0)]`, "[0, 0]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// No-argument sleep is a deliberate simplification: it returns 0 immediately
	// rather than blocking forever as MRI does (covers the no-args branch).
	if got := eval(t, "p sleep"); got != "0\n" {
		t.Errorf("sleep (no arg) = %q, want 0", got)
	}

	errs := []struct{ src, want string }{
		{`Thread.new`, "block"},
		{`Mutex.new.synchronize`, "block"},
		{`sleep("x")`, "time interval"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
