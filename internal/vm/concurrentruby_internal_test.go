// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestConcurrentRubyAtomics covers Concurrent::AtomicReference / AtomicFixnum /
// AtomicBoolean: readers, writers, atomic swaps, compare-and-set (both branches)
// and the block-driven #update.
func TestConcurrentRubyAtomics(t *testing.T) {
	cases := []struct{ src, want string }{
		// AtomicReference
		{`require "concurrent"; r = Concurrent::AtomicReference.new; p r.value`, "nil"},
		{`require "concurrent"; r = Concurrent::AtomicReference.new(1); p r.get`, "1"},
		{`require "concurrent"; r = Concurrent::AtomicReference.new(1); p r.set(2); p r.value`, "2\n2"},
		{`require "concurrent"; r = Concurrent::AtomicReference.new(1); r.value = 9; p r.value`, "9"},
		{`require "concurrent"; r = Concurrent::AtomicReference.new(1); p r.get_and_set(5); p r.value`, "1\n5"},
		{`require "concurrent"; r = Concurrent::AtomicReference.new(1); p r.swap(5); p r.value`, "1\n5"},
		{`require "concurrent"; r = Concurrent::AtomicReference.new(1); p r.compare_and_set(1, 2); p r.value`, "true\n2"},
		{`require "concurrent"; r = Concurrent::AtomicReference.new(1); p r.compare_and_set(9, 2); p r.value`, "false\n1"},
		{`require "concurrent"; r = Concurrent::AtomicReference.new(1); p r.compare_and_swap(1, 2)`, "true"},
		{`require "concurrent"; r = Concurrent::AtomicReference.new(3); p r.update { |v| v + 1 }; p r.value`, "4\n4"},
		// AtomicFixnum
		{`require "concurrent"; f = Concurrent::AtomicFixnum.new; p f.value`, "0"},
		{`require "concurrent"; f = Concurrent::AtomicFixnum.new(10); p f.value`, "10"},
		{`require "concurrent"; f = Concurrent::AtomicFixnum.new(10); f.value = 3; p f.value`, "3"},
		{`require "concurrent"; f = Concurrent::AtomicFixnum.new(10); p f.increment; p f.increment(4)`, "11\n15"},
		{`require "concurrent"; f = Concurrent::AtomicFixnum.new(10); p f.up`, "11"},
		{`require "concurrent"; f = Concurrent::AtomicFixnum.new(10); p f.decrement; p f.decrement(4)`, "9\n5"},
		{`require "concurrent"; f = Concurrent::AtomicFixnum.new(10); p f.down`, "9"},
		{`require "concurrent"; f = Concurrent::AtomicFixnum.new(10); p f.compare_and_set(10, 20); p f.value`, "true\n20"},
		{`require "concurrent"; f = Concurrent::AtomicFixnum.new(10); p f.compare_and_set(0, 20)`, "false"},
		{`require "concurrent"; f = Concurrent::AtomicFixnum.new(3); p f.update { |v| v * 10 }; p f.value`, "30\n30"},
		// a Bignum initial exercises the *object.Bignum coercion branch (lossy).
		{`require "concurrent"; f = Concurrent::AtomicFixnum.new(2 ** 70); p f.value.class`, "Integer"},
		// AtomicBoolean
		{`require "concurrent"; b = Concurrent::AtomicBoolean.new; p b.value`, "false"},
		{`require "concurrent"; b = Concurrent::AtomicBoolean.new(true); p b.value`, "true"},
		{`require "concurrent"; b = Concurrent::AtomicBoolean.new; b.value = true; p b.value`, "true"},
		{`require "concurrent"; b = Concurrent::AtomicBoolean.new(true); p b.true?; p b.false?`, "true\nfalse"},
		{`require "concurrent"; b = Concurrent::AtomicBoolean.new; p b.make_true; p b.value`, "true\ntrue"},
		{`require "concurrent"; b = Concurrent::AtomicBoolean.new(true); p b.make_false; p b.value`, "true\nfalse"},
		{`require "concurrent"; b = Concurrent::AtomicBoolean.new(true); p b.compare_and_set(true, false); p b.value`, "true\nfalse"},
	}
	runConcurrentCases(t, cases)
}

// TestConcurrentRubyMap covers Concurrent::Map: element access, atomic
// read-modify-write (compute_if_absent / put_if_absent) on both the present and
// absent paths, deletion, membership and enumeration (keys recovered through the
// canonical-key mapping).
func TestConcurrentRubyMap(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "concurrent"; m = Concurrent::Map.new; p m[:x]`, "nil"},
		{`require "concurrent"; m = Concurrent::Map.new; m[:x] = 2; p m[:x]`, "2"},
		{`require "concurrent"; m = Concurrent::Map.new; m.store(:x, 7); p m[:x]`, "7"},
		// String keys address the same slot by content, not identity.
		{`require "concurrent"; m = Concurrent::Map.new; m["k"] = 1; p m["#{?k}"]`, "1"},
		{`require "concurrent"; m = Concurrent::Map.new; p m.compute_if_absent(:a) { 5 }; p m.compute_if_absent(:a) { 99 }`, "5\n5"},
		{`require "concurrent"; m = Concurrent::Map.new; p m.put_if_absent(:a, 1); p m.put_if_absent(:a, 2); p m[:a]`, "nil\n1\n1"},
		{`require "concurrent"; m = Concurrent::Map.new; m[:a] = 1; p m.delete(:a); p m[:a]`, "1\nnil"},
		{`require "concurrent"; m = Concurrent::Map.new; p m.delete(:missing)`, "nil"},
		{`require "concurrent"; m = Concurrent::Map.new; m[:a] = 1; p m.key?(:a); p m.has_key?(:b)`, "true\nfalse"},
		{`require "concurrent"; m = Concurrent::Map.new; m[:a] = 1; m[:b] = 2; p m.size; p m.length`, "2\n2"},
		{`require "concurrent"; m = Concurrent::Map.new; p m.empty?; m[:a] = 1; p m.empty?`, "true\nfalse"},
		{`require "concurrent"; m = Concurrent::Map.new; m[:a] = 1; m.clear; p m.size`, "0"},
		{`require "concurrent"; m = Concurrent::Map.new; m[:a] = 1; m[:b] = 2; p m.keys.sort; p m.values.sort`, "[:a, :b]\n[1, 2]"},
		{`require "concurrent"; m = Concurrent::Map.new; m[:a] = 1; acc = []; m.each_pair { |k, v| acc << [k, v] }; p acc`, "[[:a, 1]]"},
		{`require "concurrent"; m = Concurrent::Map.new; m[:a] = 1; acc = []; m.each { |k, v| acc << k }; p acc`, "[:a]"},
	}
	runConcurrentCases(t, cases)
}

// TestConcurrentRubyFuture covers Concurrent::Future.execute: a block posted to
// the vmExecutor runs inline, so the future is settled on return — value / state
// / predicate readers, the rejected path (value! re-raises, reason carries the
// exception) and #wait.
func TestConcurrentRubyFuture(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "concurrent"; f = Concurrent::Future.execute { 1 + 2 }; p f.value`, "3"},
		{`require "concurrent"; f = Concurrent::Future.execute { 1 + 2 }; p f.value(1)`, "3"},
		{`require "concurrent"; f = Concurrent::Future.execute { 1 + 2 }; p f.value!`, "3"},
		{`require "concurrent"; f = Concurrent::Future.execute { 1 + 2 }; p f.state`, ":fulfilled"},
		{`require "concurrent"; f = Concurrent::Future.execute { 1 + 2 }; p f.fulfilled?; p f.realized?; p f.rejected?; p f.pending?; p f.complete?`, "true\ntrue\nfalse\nfalse\ntrue"},
		{`require "concurrent"; f = Concurrent::Future.execute { 1 + 2 }; p f.reason`, "nil"},
		{`require "concurrent"; f = Concurrent::Future.execute { 1 }; p f.wait.value`, "1"},
		// rejection: the block raises.
		{`require "concurrent"; f = Concurrent::Future.execute { raise "boom" }; p f.state; p f.rejected?; p f.value`, ":rejected\ntrue\nnil"},
		{`require "concurrent"; f = Concurrent::Future.execute { raise "boom" }; p f.reason.message`, `"boom"`},
		{`require "concurrent"
f = Concurrent::Future.execute { raise ArgumentError, "bad" }
begin
  f.value!
rescue ArgumentError => e
  p e.message
end`, `"bad"`},
	}
	runConcurrentCases(t, cases)
}

// TestConcurrentRubyPromise covers Concurrent::Promise: explicit fulfil / reject
// (with a String and with an exception object), #then chaining on both the
// fulfilled and rejected paths, and the state readers.
func TestConcurrentRubyPromise(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "concurrent"; pr = Concurrent::Promise.new; pr.fulfill(5); p pr.value; p pr.state`, "5\n:fulfilled"},
		{`require "concurrent"; pr = Concurrent::Promise.new; pr.fulfill(5); p pr.fulfilled?; p pr.rejected?; p pr.pending?`, "true\nfalse\nfalse"},
		{`require "concurrent"; pr = Concurrent::Promise.new; pr.fulfill(5); p pr.then { |v| v * 2 }.value`, "10"},
		{`require "concurrent"; pr = Concurrent::Promise.new; p pr.pending?`, "true"},
		// fulfil with no argument settles to nil (concurrentPromiseVal empty path).
		{`require "concurrent"; pr = Concurrent::Promise.new; pr.fulfill; p pr.value; p pr.fulfilled?`, "nil\ntrue"},
		// reject with a String.
		{`require "concurrent"; pr = Concurrent::Promise.new; pr.reject("nope"); p pr.state; p pr.reason.message`, `:rejected` + "\n" + `"nope"`},
		// reject with an exception object; reason returns that object.
		{`require "concurrent"; e = ArgumentError.new("x"); pr = Concurrent::Promise.new; pr.reject(e); p pr.reason.equal?(e)`, "true"},
		// then on a rejected promise propagates the rejection (block not run).
		{`require "concurrent"; pr = Concurrent::Promise.new; pr.reject("nope"); c = pr.then { |v| v }; p c.rejected?`, "true"},
		// a raise inside then rejects the child.
		{`require "concurrent"; pr = Concurrent::Promise.new; pr.fulfill(1); c = pr.then { raise "inner" }; p c.rejected?; p c.reason.message`, `true` + "\n" + `"inner"`},
	}
	runConcurrentCases(t, cases)
}

// TestConcurrentRubyPromiseErrors covers the MultipleAssignmentError raised on a
// second settlement of a Promise.
func TestConcurrentRubyPromiseErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "concurrent"
pr = Concurrent::Promise.new
pr.fulfill(1)
begin
  pr.fulfill(2)
rescue Concurrent::MultipleAssignmentError => e
  p e.class.name
end`, `"Concurrent::MultipleAssignmentError"`},
		{`require "concurrent"
pr = Concurrent::Promise.new
pr.reject("a")
begin
  pr.reject("b")
rescue Concurrent::MultipleAssignmentError
  puts "caught"
end`, "caught"},
	}
	runConcurrentCases(t, cases)
}

// TestConcurrentRubyPools covers Concurrent::FixedThreadPool / ThreadPoolExecutor:
// posted blocks run inline (serialized on the VM goroutine, no worker leaked),
// completion accounting, graceful shutdown and the read surface.
func TestConcurrentRubyPools(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "concurrent"; pool = Concurrent::FixedThreadPool.new(2); acc = []; p pool.post { acc << 1 }; p acc`, "true\n[1]"},
		{`require "concurrent"; pool = Concurrent::FixedThreadPool.new(2); pool.post { 1 }; pool.post { 2 }; p pool.completed_task_count`, "2"},
		{`require "concurrent"; pool = Concurrent::FixedThreadPool.new(2); p pool.queue_length; p pool.running?; p pool.shutdown?`, "0\ntrue\nfalse"},
		{`require "concurrent"; pool = Concurrent::FixedThreadPool.new(3); p pool.length; p pool.max_length`, "3\n3"},
		{`require "concurrent"; pool = Concurrent::FixedThreadPool.new(2); p pool.wait_for_termination; pool.shutdown; p pool.shutdown?; p pool.running?; p pool.wait_for_termination`, "false\ntrue\nfalse\ntrue"},
		{`require "concurrent"; pool = Concurrent::FixedThreadPool.new(2); pool.shutdown; p pool.post { 1 }`, "false"},
		{`require "concurrent"; pool = Concurrent::FixedThreadPool.new(2); pool.kill; p pool.shutdown?`, "true"},
		// a task's raise is swallowed by the pool.
		{`require "concurrent"; pool = Concurrent::FixedThreadPool.new(2); p pool.post { raise "x" }; p pool.completed_task_count`, "true\n1"},
		// ThreadPoolExecutor with an options Hash.
		{`require "concurrent"; pool = Concurrent::ThreadPoolExecutor.new(min_threads: 2, max_threads: 5); p pool.length; p pool.max_length`, "2\n5"},
		{`require "concurrent"; pool = Concurrent::ThreadPoolExecutor.new; p pool.length`, "1"},
		{`require "concurrent"; pool = Concurrent::ThreadPoolExecutor.new(7); p pool.length`, "1"},
	}
	runConcurrentCases(t, cases)
}

// TestConcurrentRubySync covers CountDownLatch / Semaphore / CyclicBarrier. Every
// wait is deterministic: a latch is counted to zero before an untimed wait, and a
// barrier trips with a single party (its action running inline) — no time.Sleep,
// no blocking on an unmet condition.
func TestConcurrentRubySync(t *testing.T) {
	cases := []struct{ src, want string }{
		// CountDownLatch
		{`require "concurrent"; l = Concurrent::CountDownLatch.new; p l.count`, "1"},
		{`require "concurrent"; l = Concurrent::CountDownLatch.new(2); p l.count; l.count_down; p l.count`, "2\n1"},
		{`require "concurrent"; l = Concurrent::CountDownLatch.new(1); l.count_down; p l.wait`, "true"},
		{`require "concurrent"; l = Concurrent::CountDownLatch.new(1); p l.wait(0)`, "false"},
		// Semaphore
		{`require "concurrent"; s = Concurrent::Semaphore.new(2); p s.available_permits`, "2"},
		{`require "concurrent"; s = Concurrent::Semaphore.new(2); s.acquire; p s.available_permits; s.release; p s.available_permits`, "1\n2"},
		{`require "concurrent"; s = Concurrent::Semaphore.new(1); p s.try_acquire; p s.try_acquire`, "true\nfalse"},
		{`require "concurrent"; s = Concurrent::Semaphore.new(3); p s.drain_permits; p s.available_permits`, "3\n0"},
		{`require "concurrent"; s = Concurrent::Semaphore.new(3); s.reduce_permits(1); p s.available_permits`, "2"},
		{`require "concurrent"; s = Concurrent::Semaphore.new(2); s.acquire(2); p s.available_permits`, "0"},
		// CyclicBarrier
		{`require "concurrent"; b = Concurrent::CyclicBarrier.new(1); p b.wait`, "true"},
		{`require "concurrent"; b = Concurrent::CyclicBarrier.new(2); p b.parties; p b.number_waiting`, "2\n0"},
		{`require "concurrent"; ran = []; b = Concurrent::CyclicBarrier.new(1) { ran << :go }; b.wait; p ran`, "[:go]"},
		{`require "concurrent"; b = Concurrent::CyclicBarrier.new(2); p b.wait(0); p b.broken?`, "false\ntrue"},
		{`require "concurrent"; b = Concurrent::CyclicBarrier.new(2); b.wait(0); b.reset; p b.broken?`, "false"},
	}
	runConcurrentCases(t, cases)
}

// TestConcurrentRubyArgErrors covers the argument-validation branches: a missing
// block, a non-Integer count, and a non-numeric timeout.
func TestConcurrentRubyArgErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "concurrent"
begin; Concurrent::Future.execute; rescue ArgumentError => e; p e.message; end`, `"no block given"`},
		{`require "concurrent"
m = Concurrent::Map.new
begin; m.compute_if_absent(:a); rescue ArgumentError => e; p e.message; end`, `"no block given"`},
		{`require "concurrent"
r = Concurrent::AtomicReference.new(1)
begin; r.update; rescue ArgumentError => e; p e.message; end`, `"no block given"`},
		{`require "concurrent"
f = Concurrent::AtomicFixnum.new(1)
begin; f.update; rescue ArgumentError => e; p e.message; end`, `"no block given"`},
		{`require "concurrent"
m = Concurrent::Map.new
begin; m.each_pair; rescue ArgumentError => e; p e.message; end`, `"no block given"`},
		{`require "concurrent"
pool = Concurrent::FixedThreadPool.new(1)
begin; pool.post; rescue ArgumentError => e; p e.message; end`, `"no block given"`},
		{`require "concurrent"
begin; Concurrent::AtomicFixnum.new("x"); rescue TypeError => e; puts "typeerr"; end`, "typeerr"},
		{`require "concurrent"
l = Concurrent::CountDownLatch.new(1)
begin; l.wait("soon"); rescue TypeError => e; puts "typeerr"; end`, "typeerr"},
		{`require "concurrent"
l = Concurrent::CountDownLatch.new(1); l.count_down
p l.wait(0.01) == true`, "true"},
	}
	runConcurrentCases(t, cases)
}

// TestConcurrentRubyDefensiveBranches covers the defensive seams that Ruby code
// cannot reach: a block that raises a non-Ruby Go panic (re-propagated), and the
// reason/re-raise paths for a rejection that did not originate from a Ruby raise.
func TestConcurrentRubyDefensiveBranches(t *testing.T) {
	vm := New(&bytes.Buffer{})

	// runConcurrentBlock re-panics a non-RubyError Go panic unchanged.
	t.Run("non-ruby panic propagates", func(t *testing.T) {
		defer func() {
			r := recover()
			if r != "boom" {
				t.Fatalf("expected re-panic of \"boom\", got %v", r)
			}
		}()
		blk := &Proc{native: func(_ *VM, _ []object.Value) object.Value { panic("boom") }}
		vm.runConcurrentBlock(blk, nil)
	})

	// concurrentReason for a non-rubyRejection error builds a Concurrent::Error.
	if got := vm.concurrentReason(nil); !object.IsNil(got) {
		t.Fatalf("concurrentReason(nil) = %v, want nil", got)
	}
	obj := vm.concurrentReason(errors.New("plain"))
	if name := vm.classOf(obj).name; name != "Concurrent::Error" {
		t.Fatalf("concurrentReason class = %q, want Concurrent::Error", name)
	}

	// concurrentReRaise for a non-rubyRejection error raises Concurrent::Error.
	func() {
		defer func() {
			r := recover()
			re, ok := r.(RubyError)
			if !ok || re.Class != "Concurrent::Error" {
				t.Fatalf("concurrentReRaise recovered %v, want Concurrent::Error", r)
			}
		}()
		vm.concurrentReRaise(errors.New("plain"))
	}()
}

// TestConcurrentRubyWrapperText covers the ToS / Inspect / Truthy trio each Ruby
// value wrapper exposes (used for #inspect, string interpolation and truthiness)
// and the rubyRejection Error string.
func TestConcurrentRubyWrapperText(t *testing.T) {
	wrappers := []object.Value{
		&ConcurrentAtomicRef{}, &ConcurrentAtomicFixnum{}, &ConcurrentAtomicBool{},
		&ConcurrentMap{}, &ConcurrentFuture{}, &ConcurrentPromise{},
		&ConcurrentPool{}, &ConcurrentLatch{}, &ConcurrentSemaphore{}, &ConcurrentBarrier{},
	}
	for _, w := range wrappers {
		if w.ToS() == "" || w.Inspect() != w.ToS() || !w.Truthy() {
			t.Fatalf("wrapper %T: ToS=%q Inspect=%q Truthy=%v", w, w.ToS(), w.Inspect(), w.Truthy())
		}
	}
	if got := (&rubyRejection{obj: object.NewString("boom")}).Error(); got != "boom" {
		t.Fatalf("rubyRejection.Error() = %q, want boom", got)
	}
}

// runConcurrentCases runs a table of {src, want} through runSrc.
func runConcurrentCases(t *testing.T, cases []struct{ src, want string }) {
	t.Helper()
	for _, c := range cases {
		if got := runSrc(t, c.src); got != c.want {
			t.Fatalf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
