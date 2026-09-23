// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestWave28KernelSleepScheduler pins rb_f_sleep (process.c) and the coercion it
// leans on, rb_time_interval (time.c time_timespec with interval=TRUE).
//
// rb_f_sleep asks rb_fiber_scheduler_current first — the thread's scheduler, but
// only while the running fiber is non-blocking (scheduler.c,
// rb_fiber_scheduler_current_for_threadptr reading th->blocking, which cont.c
// keeps at zero exactly for a non-blocking fiber) — and hands it the arguments
// verbatim through rb_fiber_scheduler_kernel_sleepv. So `sleep` with no argument
// reaches #kernel_sleep with no argument, and the duration is never inspected on
// that path.
//
// Every want string below is the raw stdout of the same source under MRI ruby
// 4.0.5, compared byte for byte.
func TestWave28KernelSleepScheduler(t *testing.T) {
	// sched builds the ruby/spec LoggingScheduler in miniature: it records the
	// hook it was called with and yields, which is what lets `resume` return.
	const sched = `
class S
  attr_reader :events
  def initialize; @events = []; end
  def block(*a); @events << [:block, a]; Fiber.yield; end
  def unblock(*a); @events << [:unblock, a]; Fiber.yield; end
  def io_wait(*a); @events << [:io_wait, a]; Fiber.yield; end
  def kernel_sleep(*a); @events << [:kernel_sleep, a]; Fiber.yield; end
end
Fiber.set_scheduler(S.new)
`
	cases := []struct{ src, want string }{
		// A non-blocking fiber routes sleep to the scheduler; with no argument the
		// hook is called with no argument (kernel_sleepv forwards argc verbatim),
		// and with one it carries that one.
		{sched + `f = Fiber.new(blocking: false) { sleep }; f.resume; p Fiber.scheduler.events`, "[[:kernel_sleep, []]]\n"},
		{sched + `f = Fiber.new(blocking: false) { sleep(0.01) }; f.resume; p Fiber.scheduler.events`, "[[:kernel_sleep, [0.01]]]\n"},
		{sched + `f = Fiber.new(blocking: false) { sleep(nil) }; f.resume; p Fiber.scheduler.events`, "[[:kernel_sleep, [nil]]]\n"},
		// A blocking fiber does not consult it: th->blocking is non-zero there.
		{sched + `f = Fiber.new(blocking: true) { sleep(0.001) }; f.resume; p Fiber.scheduler.events`, "[]\n"},
		// Neither does the root fiber, which is blocking too.
		{sched + `sleep(0.001); p Fiber.scheduler.events`, "[]\n"},
		// Fiber.scheduler is rb_fiber_scheduler_get: it answers from any fiber,
		// blocking or not, which is what the checks above rely on.
		{sched + `p Fiber.new(blocking: true) { Fiber.scheduler.class }.resume`, "S\n"},
		// The scheduler path skips rb_check_arity entirely — MRI never looks at
		// the arguments it forwards.
		{sched + `f = Fiber.new(blocking: false) { sleep(1, 2) }; f.resume; p Fiber.scheduler.events`, "[[:kernel_sleep, [1, 2]]]\n"},
		// ...and it never coerces them, so a String reaches the hook unrefused.
		{sched + `f = Fiber.new(blocking: false) { sleep("2") }; f.resume; p Fiber.scheduler.events`, "[[:kernel_sleep, [\"2\"]]]\n"},
		// Uninstalling the scheduler puts the ordinary path back.
		{sched + `Fiber.set_scheduler(nil); p Fiber.new(blocking: false) { sleep(0.001) }.resume`, "0\n"},

		// rb_time_interval, numeric cases.
		{`p sleep(0)`, "0\n"},
		{`p sleep(0.001)`, "0\n"},
		{`begin; sleep(-1); rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"time interval must not be negative\"]\n"},
		{`begin; sleep(-0.1); rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"time interval must not be negative\"]\n"},
		{`begin; sleep(-Float::INFINITY); rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"time interval must not be negative\"]\n"},
		// The `f != t.tv_sec` guard: a Float whose integral part cannot be held in
		// a time_t. MRI's own vsnprintf renders the non-finite cases "NaN"/"Inf".
		{`begin; sleep(Float::NAN); rescue => e; p [e.class, e.message]; end`, "[RangeError, \"NaN out of Time range\"]\n"},
		{`begin; sleep(Float::INFINITY); rescue => e; p [e.class, e.message]; end`, "[RangeError, \"Inf out of Time range\"]\n"},
		{`begin; sleep(1e20); rescue => e; p [e.class, e.message]; end`, "[RangeError, \"100000000000000000000.000000 out of Time range\"]\n"},
		// NUM2TIMET on a Bignum: rbgo only builds one for a value an int64 cannot
		// hold, so every Bignum overflows the time_t.
		{`begin; sleep(2**70); rescue => e; p [e.class, e.message]; end`, "[RangeError, \"bignum too big to convert into 'long'\"]\n"},
		{`begin; sleep(-(2**70)); rescue => e; p [e.class, e.message]; end`, "[RangeError, \"bignum too big to convert into 'long'\"]\n"},

		// rb_time_interval, the #divmod(1) fallback: [whole seconds, fraction].
		{`p sleep(Rational(1, 999))`, "0\n"},
		{`o = Object.new; def o.divmod(*); [0, 0.001]; end; p sleep(o)`, "0\n"},
		// NUM2TIMET truncates a Float quotient rather than refusing it.
		{`o = Object.new; def o.divmod(*); [0.5, 0]; end; p sleep(o)`, "0\n"},
		// arg_range_check applies to the whole-seconds half only.
		{`begin; sleep(Rational(-1, 2)); rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"time interval must not be negative\"]\n"},
		// The quotient goes through NUM2TIMET, which is rb_num2long: it names nil
		// differently from everything else it cannot convert.
		{`o = Object.new; def o.divmod(*); [nil, 0]; end; begin; sleep(o); rescue => e; p [e.class, e.message]; end`,
			"[TypeError, \"no implicit conversion from nil to integer\"]\n"},
		{`o = Object.new; def o.divmod(*); ["a", 0]; end; begin; sleep(o); rescue => e; p [e.class, e.message]; end`,
			"[TypeError, \"no implicit conversion of String into Integer\"]\n"},
		// A #divmod that does not answer with an Array is no conversion at all.
		{`o = Object.new; def o.divmod(*); 5; end; begin; sleep(o); rescue => e; p [e.class, e.message]; end`, "[TypeError, \"can't convert Object into time interval\"]\n"},
		// Nothing to send #divmod to: a String has none.
		{`begin; sleep('2'); rescue => e; p [e.class, e.message]; end`, "[TypeError, \"can't convert String into time interval\"]\n"},
		{`begin; sleep(:s); rescue => e; p [e.class, e.message]; end`, "[TypeError, \"can't convert Symbol into time interval\"]\n"},
		// A #divmod reached only through method_missing still converts, as
		// rb_check_funcall's respond_to_missing? probe allows.
		{`o = Object.new; def o.respond_to_missing?(n, p = false); n == :divmod; end; def o.method_missing(n, *a); n == :divmod ? [0, 0.001] : super; end; p sleep(o)`, "0\n"},

		// rb_check_arity(argc, 0, 1) guards the duration branch of rb_f_sleep.
		{`begin; sleep(1, 2); rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"wrong number of arguments (given 2, expected 0..1)\"]\n"},
		{`begin; sleep(nil, nil); rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"wrong number of arguments (given 2, expected 0..1)\"]\n"},

		// Mutex#sleep validates its timeout with the same rb_time_interval
		// (thread_sync.c rb_mutex_sleep), so the Rational reaches it too.
		{`m = Mutex.new; m.lock; m.sleep(Rational(1, 999)); p m.locked?`, "true\n"},
		{`m = Mutex.new; m.lock; begin; m.sleep('2'); rescue => e; p [e.class, e.message]; end`, "[TypeError, \"can't convert String into time interval\"]\n"},

		// IO.select's timeout reaches the coercion without a VM to send #divmod
		// with (spawn.go), so it stops at the numeric cases and refuses the rest.
		{`begin; IO.select([], [], [], Object.new); rescue => e; p [e.class, e.message]; end`, "[TypeError, \"can't convert Object into time interval\"]\n"},
		{`begin; IO.select([], [], [], -1); rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"time interval must not be negative\"]\n"},
		{`p IO.select([], [], [], 0)`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestWave28SleepReturnsWholeSeconds pins rb_f_sleep's result: it brackets the
// wait with time(0), a clock of whole seconds, so what comes back is the number
// of second boundaries crossed and never a rounding of the duration. A sub-second
// sleep can only answer 0 or 1, and a thread woken early reports far less than
// the duration it asked for.
func TestWave28SleepReturnsWholeSeconds(t *testing.T) {
	if got := eval(t, `p [0, 1].include?(sleep(0.2))`); got != "true\n" {
		t.Errorf("sub-second sleep result: got %q", got)
	}
	const woken = `r = nil; t = Thread.new { r = sleep(600) }; Thread.pass until t.status == "sleep"; t.wakeup; t.join; p r < 600`
	if got := eval(t, woken); got != "true\n" {
		t.Errorf("woken sleep result: got %q", got)
	}
}
