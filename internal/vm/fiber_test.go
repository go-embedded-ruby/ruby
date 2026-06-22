package vm_test

import (
	"strings"
	"testing"
)

// TestFiber covers Fiber: resume/yield handoff (with values passed both ways),
// alive?, the dead-fiber and root-yield FiberErrors, error propagation, and an
// infinite generator. Asserted against MRI Ruby 4.0.5.
func TestFiber(t *testing.T) {
	cases := []struct{ src, want string }{
		{`f = Fiber.new { 42 }; p f.resume`, "42\n"},
		{`f = Fiber.new { |x| x * 2 }; p f.resume(5)`, "10\n"},
		// Values pass in both directions across yield.
		{`f = Fiber.new { |x| a = Fiber.yield(x + 1); b = Fiber.yield(a * 2); "done #{b}" }
		  p f.resume(10); p f.resume(100); p f.resume(7)`, "11\n200\n\"done 7\"\n"},
		// alive? tracks the fiber's life.
		{`f = Fiber.new { Fiber.yield(1) }; p f.alive?; f.resume; p f.alive?; f.resume; p f.alive?`,
			"true\ntrue\nfalse\n"},
		// resume arguments reach the block (auto-splat for several).
		{`f = Fiber.new { |a, b| p [a, b]; Fiber.yield(0) }; f.resume(1, 2)`, "[1, 2]\n"},
		{`f = Fiber.new { Fiber.yield(1, 2) }; p f.resume`, "[1, 2]\n"}, // yield several -> array
		// A fiber as a generator over an each loop and as an infinite stream.
		{`f = Fiber.new { (1..3).each { |i| Fiber.yield(i) } }; p [f.resume, f.resume, f.resume]`,
			"[1, 2, 3]\n"},
		{`r = []; g = Fiber.new { i = 0; loop { Fiber.yield(i); i += 1 } }; 5.times { r << g.resume }; p r`,
			"[0, 1, 2, 3, 4]\n"},
		// A Ruby exception raised inside the fiber propagates out of resume.
		{`f = Fiber.new { raise "boom" }; begin; f.resume; rescue => e; p e.message; end`, "\"boom\"\n"},
		{`p(Fiber.new { 1 } ? "y" : "n")`, "\"y\"\n"}, // Truthy
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Resuming a finished fiber and yielding at the root both raise FiberError;
	// Fiber.new needs a block.
	errs := []struct{ src, want string }{
		{`f = Fiber.new { 1 }; f.resume; f.resume`, "FiberError"},
		{`Fiber.yield(1)`, "FiberError"},
		{`Fiber.new`, "ArgumentError"},
		// A non-local break out of the fiber body terminates it abnormally. (MRI
		// reports a LocalJumpError here; we surface a FiberError — a documented
		// edge difference for break/return escaping a fiber.)
		{`Fiber.new { break }.resume`, "FiberError"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got %v want %q", c.src, err, c.want)
		}
	}
	// Inspect (MRI shows an address; we don't).
	if got := eval(t, `p Fiber.new { 1 }`); got != "#<Fiber>\n" {
		t.Errorf("inspect: got %q", got)
	}
}
