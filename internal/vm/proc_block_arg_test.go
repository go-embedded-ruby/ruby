package vm_test

import "testing"

// TestProcCallBlockArg covers a proc/lambda's own `&b` block parameter binding to
// the block passed to Proc#call / [] / .() — previously discarded, so `&b` was
// nil — while a bare `yield` inside the proc still reaches the block captured
// where the proc was defined. Verified against MRI Ruby 4.0.6.
func TestProcCallBlockArg(t *testing.T) {
	cases := []struct{ src, want string }{
		// The block passed to .call binds &b.
		{`f = ->(&b){ b.call(5) }; p f.call { |x| x * 2 }`, "10\n"},
		{`g = ->(a, &b){ b.call(a) }; p g.call(3) { |x| x + 1 }`, "4\n"},
		{`pr = proc { |&b| b.call(2) }; p pr.call { |x| x * 3 }`, "6\n"},
		// A splat plus &b, and the block forwarded on to another method.
		{`l = ->(*a, &b){ [a, b.call] }; p l.call(1, 2) { 99 }`, "[[1, 2], 99]\n"},
		{`st = ->(r, *a, &b){ r.step(*a, &b) }
res = []; st.call(1, 10, 3) { |x| res << x }; p res`, "[1, 4, 7, 10]\n"},
		// &b is nil when the proc is called without a block.
		{`f = ->(&b){ b.nil? }; p f.call`, "true\n"},
		// A bare `yield` inside a proc still reaches the block captured where the
		// proc was defined, NOT the block passed to .call (MRI semantics).
		{`def outer; pr = proc { yield }; pr.call { :passed }; end
p(outer { :captured })`, ":captured\n"},
		// Proc#[] and .() thread the block too.
		{`f = ->(&b){ b.call(4) }; p f[] { |x| x - 1 }`, "3\n"},
		// A method's &b (already working) is unaffected.
		{`def m(&b); b.call(7); end; p m { |x| x * 10 }`, "70\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestNumericStepInfiniteStart covers Numeric#step with a non-finite start and a
// finite step: MRI's step count is (stop-start)/step — NaN or negative here — so
// it yields nothing rather than looping forever on the constant term. An infinite
// step still yields the start once. Verified against MRI Ruby 4.0.6.
func TestNumericStepInfiniteStart(t *testing.T) {
	cases := []struct{ src, want string }{
		{`n = 0; Float::INFINITY.step(Float::INFINITY, 1) { n += 1 }; p n`, "0\n"},
		{`n = 0; Float::INFINITY.step(10, 1) { n += 1 }; p n`, "0\n"},
		{`n = 0; (-Float::INFINITY).step(-Float::INFINITY, -1) { n += 1 }; p n`, "0\n"},
		// A finite walk is unchanged.
		{`a = []; 1.0.step(10.0, 3.0) { |x| a << x }; p a`, "[1.0, 4.0, 7.0, 10.0]\n"},
		{`a = []; 0.0.step(1.0, 0.1) { |x| a << x }; p a.size`, "11\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
