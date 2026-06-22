package vm_test

import "testing"

// TestEnvPoolCaptureSurvival guards the frame-env recycling (getEnv/putEnv +
// markEnvCaptured): a closure or binding that OUTLIVES its defining method must
// keep working even though many intervening calls recycle frame envs. If capture
// marking were wrong, the defining frame's env would be pooled and reused by the
// noise calls, corrupting the closed-over locals. Asserted against MRI 4.0.5.
func TestEnvPoolCaptureSurvival(t *testing.T) {
	cases := []struct{ src, want string }{
		// Counter closure returned from a method, called long after it returned —
		// with 1000 intervening calls that recycle envs.
		{`def mk; n = 0; -> { n += 1 }; end
c = mk
def noise(x); x * 2; end
1000.times { |i| noise(i) }
p [c.call, c.call, c.call]`, "[1, 2, 3]\n"},
		// Two independent closures over distinct frames must not alias each other.
		{`def adder(x); -> (y) { x + y }; end
a = adder(10); b = adder(100)
500.times { |i| i + 1 }
p [a.call(5), b.call(5)]`, "[15, 105]\n"},
		// A Binding captured and read after its method returned (with noise between).
		{`def cap; v = 42; binding; end
bd = cap
300.times { |i| i }
p bd.local_variable_get(:v)`, "42\n"},
		// Nested closures: the inner closure pins the whole enclosing chain.
		{`def outer; a = 1; -> { b = 10; -> { a + b }.call }; end
f = outer
200.times { |i| i }
p f.call`, "11\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
