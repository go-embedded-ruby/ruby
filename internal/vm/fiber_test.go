package vm_test

import (
	"regexp"
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
		// A `break` in the fiber body has no enclosing iterator to unwind to, so it
		// is a break from a proc-closure — a rescue-able LocalJumpError, as in MRI.
		{`Fiber.new { break }.resume`, "break from proc-closure"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got %v want %q", c.src, err, c.want)
		}
	}
	// Inspect renders MRI-style: #<Fiber:0x... label (status)>, with the status
	// tracking the fiber's life-cycle.
	inspectRe := regexp.MustCompile(`\A"#<Fiber:0x[0-9a-f]+ .+ \(created\)>"\n\z`)
	if got := eval(t, `p Fiber.new { 1 }.inspect`); !inspectRe.MatchString(got) {
		t.Errorf("inspect created: got %q", got)
	}
	if got := eval(t, `p Fiber.new { Fiber.current.inspect }.resume`); !strings.Contains(got, "(resumed)") {
		t.Errorf("inspect resumed: got %q", got)
	}
	if got := eval(t, `f = Fiber.new { Fiber.yield }; f.resume; p f.inspect`); !strings.Contains(got, "(suspended)") {
		t.Errorf("inspect suspended: got %q", got)
	}
	if got := eval(t, `f = Fiber.new { 1 }; f.resume; p f.inspect`); !strings.Contains(got, "(terminated)") {
		t.Errorf("inspect terminated: got %q", got)
	}
}

// TestFiberTransfer covers Fiber#transfer (symmetric coroutine switching), the
// root fiber and Fiber.current, and the resume/transfer mixing rules, asserted
// against MRI Ruby 3.4.
func TestFiberTransfer(t *testing.T) {
	cases := []struct{ src, want string }{
		// A transfer chain: root -> f2 -> f1; f1 (reached by transfer) finishing
		// returns to the root, leaving f2 suspended until transferred to again.
		{`f1 = Fiber.new { :one }
		  f2 = Fiber.new { f1.transfer; :two }
		  p f2.transfer
		  p f2.transfer`, ":one\n:two\n"},
		// A resumed fiber that transfers out: the transferred-to fiber finishing
		// returns to the resumed fiber, which then runs to completion.
		{`f1 = Fiber.new { :inner }
		  f2 = Fiber.new { f1.transfer; :outer }
		  p f2.resume`, ":outer\n"},
		// Values pass across transfer in both directions.
		{`main = Fiber.current
		  worker = Fiber.new { |x| y = main.transfer(x + 1); main.transfer(y * 2) }
		  p worker.transfer(10)
		  p worker.transfer(100)`, "11\n200\n"},
		// Fiber.current is the root fiber at the top level, and transferring to it
		// is always allowed (it never dies).
		{`root = Fiber.current
		  p root.instance_of?(Fiber)
		  p root.transfer
		  p root.alive?`, "true\nnil\ntrue\n"},
		// Fiber-local storage (Thread#[]) is per-fiber, not shared across fibers.
		{`fib = Fiber.new { Thread.current[:v] = 1; Fiber.yield; p Thread.current[:v] }
		  fib.resume
		  p Thread.current[:v]
		  fib.resume`, "nil\n1\n"},
		// The root fiber's #inspect has no source label (it has no block).
		{`p Fiber.current.inspect =~ /\A#<Fiber:0x\h+ \(resumed\)>\z/ ? :ok : :no`, ":ok\n"},
		// #to_s renders like #inspect (string interpolation goes through to_s).
		{`f = Fiber.new { 1 }; p("#{f}".start_with?("#<Fiber:0x"))`, "true\n"},
		// Transferring to an already-transfer-suspended fiber resumes it in place.
		{`main = Fiber.current
		  f = Fiber.new { main.transfer(:a); main.transfer(:b) }
		  p f.transfer
		  p f.transfer`, ":a\n:b\n"},
		// An exception raised in a transferred-to fiber propagates out of transfer.
		{`begin; Fiber.new { raise "boom" }.transfer; rescue => e; p e.message; end`, "\"boom\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// Transfer to a fiber suspended by Fiber.yield is a FiberError.
		{`f = Fiber.new { Fiber.yield }; f.resume; f.transfer`, "suspended by Fiber.yield"},
		// Transfer to a dead fiber is a FiberError.
		{`f = Fiber.new { 1 }; f.transfer; f.transfer`, "dead fiber"},
		// Resuming a fiber that has been transferred to is a FiberError.
		{`main = Fiber.current
		  f = Fiber.new { main.transfer }
		  f.transfer
		  f.resume`, "transferred"},
		// Resuming a resuming fiber is a FiberError (root is resuming here).
		{`root = Fiber.current
		  f = Fiber.new { root.resume }
		  f.resume`, "attempt to resume a resuming fiber"},
		// Fiber.current called at the top level and yielded from raises.
		{`Fiber.yield`, "can't yield from root fiber"},
		// A fiber may not be resumed or transferred from another thread.
		{`f = Fiber.new { 1 }; Thread.new { f.transfer }.join`, "across threads"},
		// A fiber resuming itself (via Fiber.current) is a FiberError.
		{`f = Fiber.new { Fiber.current.resume }; f.resume`, "attempt to resume the current fiber"},
		// Transferring to a fiber that is resuming a child is a FiberError.
		{`a = nil
		  b = Fiber.new { a.transfer }
		  a = Fiber.new { b.resume }
		  a.resume`, "cannot transfer to a resuming fiber"},
		// A non-local return in a fiber body terminates it abnormally.
		{`Fiber.new { return }.resume`, "fiber terminated abnormally"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got %v want %q", c.src, err, c.want)
		}
	}
}
