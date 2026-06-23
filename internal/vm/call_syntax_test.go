package vm_test

import "testing"

// TestCallSyntax covers the .() call shorthand and paren-less command arguments
// on a method-call receiver. Asserted against MRI Ruby 4.0.5.
func TestCallSyntax(t *testing.T) {
	cases := []struct{ src, want string }{
		// .() is sugar for .call.
		{`f = proc { |x| x * 2 }; p f.(5)`, "10\n"},
		{`add = ->(a, b) { a + b }; p add.(3, 4)`, "7\n"},
		{`g = ->(x) { ->(y) { x + y } }; p g.(1).(2)`, "3\n"}, // chained
		// Paren-less command argument on a method-call receiver.
		{`fib = Fiber.new { Fiber.yield 1; 2 }; p [fib.resume, fib.resume]`, "[1, 2]\n"},
		{`e = Enumerator.new { |y| y.yield 10; y.yield 20 }; p e.to_a`, "[10, 20]\n"},
		// Regressions: chains, operators, paren'd calls keep working.
		{`p [1, 2].map { |x| x }.first`, "1\n"},
		{`class C; def b; 5; end; end; p C.new.b + 1`, "6\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
