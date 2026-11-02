package vm

import "testing"

// evalInspect is evalString's sibling for cases whose answer is a structure
// rather than a message: `puts` on an Array prints one element per line, which
// would compare two different things.
func evalInspect(t *testing.T, src string) string {
	t.Helper()
	return runSrc(t, "__v = begin\n"+src+"\nend\nputs __v.inspect")
}

// Ruby 4.0 gives Thread#raise the cause: keyword — ruby/spec includes the
// shared `:kernel_raise_with_cause` examples into core/thread/raise_spec.rb
// from 4.0 and not before. rbgo's Fiber#raise already had it; Thread#raise
// built its exception by hand and dropped both cause: and the backtrace
// argument, so the two were the same rule in two places and only one of them
// was right.
//
// ⛔ These ran on the SLEEPING-thread shape rather than the current one,
// because Thread#raise on the current thread takes a different path (it panics
// here and now) and would have exercised none of the delivery.
func TestThreadRaiseCause(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{{
		name: "cause: sets the raised exception's cause",
		src: `c = StandardError.new("original")
t = Thread.new { Thread.current.report_on_exception = false; sleep }
Thread.pass while t.status != "sleep"
t.raise("new error", cause: c)
begin; t.value; rescue => e; [e.message, e.cause.message]; end`,
		want: `["new error", "original"]`,
	}, {
		name: "cause: nil prevents the automatic chaining",
		src: `t = Thread.new { Thread.current.report_on_exception = false; sleep }
Thread.pass while t.status != "sleep"
begin
  raise "first error"
rescue
  t.raise("second error", cause: nil)
end
begin; t.value; rescue => e; [e.message, e.cause]; end`,
		want: `["second error", nil]`,
	}, {
		// ⛔ The chaining reads the CALLER's $!, not the target thread's, which
		// is what :kernel_raise_across_contexts pins from 4.0. The target here
		// is asleep and has no current exception at all.
		name: "the automatic cause comes from the CALLING context",
		src: `t = Thread.new { Thread.current.report_on_exception = false; sleep }
Thread.pass while t.status != "sleep"
begin
  raise "caller's error"
rescue
  t.raise("delivered")
end
begin; t.value; rescue => e; [e.message, e.cause.message]; end`,
		want: `["delivered", "caller's error"]`,
	}, {
		name: "no previous exception leaves the cause nil",
		src: `t = Thread.new { Thread.current.report_on_exception = false; sleep }
Thread.pass while t.status != "sleep"
t.raise("error without a cause")
begin; t.value; rescue => e; [e.message, e.cause]; end`,
		want: `["error without a cause", nil]`,
	}, {
		name: "only cause: and nothing else is an ArgumentError",
		src: `t = Thread.new { Thread.current.report_on_exception = false; sleep }
Thread.pass while t.status != "sleep"
r = begin; t.raise(cause: StandardError.new("c")); rescue => e; [e.class, e.message]; end
t.kill; t.join; r`,
		want: `[ArgumentError, "only cause is given with no arguments"]`,
	}, {
		name: "a cause that is neither an Exception nor nil is a TypeError",
		src: `t = Thread.new { Thread.current.report_on_exception = false; sleep }
Thread.pass while t.status != "sleep"
r = begin; t.raise("message", cause: Object.new); rescue => e; [e.class, e.message]; end
t.kill; t.join; r`,
		want: `[TypeError, "exception object expected"]`,
	}, {
		name: "a cause that would close a cycle is an ArgumentError",
		src: `t = Thread.new { Thread.current.report_on_exception = false; sleep }
Thread.pass while t.status != "sleep"
r = begin
  begin
    raise "Error 1"
  rescue => e1
    begin
      raise "Error 2"
    rescue
      begin
        raise "Error 3"
      rescue => e3
        t.raise(e1, cause: e3)
      end
    end
  end
rescue => e
  [e.class, e.message]
end
t.kill; t.join; r`,
		want: `[ArgumentError, "circular causes"]`,
	}, {
		// The third positional argument was dropped outright before this change;
		// Kernel#raise has always taken it.
		name: "the backtrace argument is honoured",
		src: `t = Thread.new { Thread.current.report_on_exception = false; sleep }
Thread.pass while t.status != "sleep"
t.raise(ArgumentError, "with a backtrace", ["line1", "line2"])
begin; t.value; rescue => e; [e.class, e.message, e.backtrace]; end`,
		want: `[ArgumentError, "with a backtrace", ["line1", "line2"]]`,
	}, {
		// ⛔ THE CONTROL. A dead thread still ignores the raise and answers nil
		// — pinned by core/thread/raise_spec.rb outside any version guard, and
		// the easiest thing to break by moving the argument handling earlier.
		name: "a dead thread still ignores it and answers nil",
		src: `t = Thread.new { :dead }
Thread.pass while t.alive?
r = t.raise("Kill the thread")
t.join
r`,
		want: `nil`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalInspect(t, tc.src); got != tc.want {
				t.Errorf("\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}
