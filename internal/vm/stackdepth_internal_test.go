package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// runAtDepth runs src on a VM whose call-depth limit is `limit`, and returns its
// stdout. The limit is lowered from defaultMaxCallDepth so a test can reach it in
// milliseconds: at the real limit every one of these programs would allocate
// hundreds of megabytes of Go stack before raising, which is a measurement
// (TestStackDepthLimitSitsBelowTheGoOverflow) and not something to pay for in
// every test that only wants to know WHAT is raised.
func runAtDepth(t *testing.T, limit int, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	vm := New(&buf)
	vm.maxCallDepth = limit
	if _, err := vm.Run(iseq); err != nil {
		t.Fatalf("run: %v\noutput so far:\n%s", err, buf.String())
	}
	return strings.TrimRight(buf.String(), "\n")
}

// TestSystemStackErrorSitsUnderExceptionNotStandardError pins the hierarchy
// against MRI's, which is what decides whether a bare `rescue` catches it.
//
// MRI: `rb_eSysStackError = rb_define_class("SystemStackError", rb_eException)`,
// proc.c v3_4_1:4424. Verified against the MRI 4.0.5 on the development machine:
// `SystemStackError.ancestors` is [SystemStackError, Exception, Object, Kernel,
// BasicObject] — no StandardError anywhere in it.
func TestSystemStackErrorSitsUnderExceptionNotStandardError(t *testing.T) {
	got := runSrc(t, `
p defined?(SystemStackError)
p SystemStackError.superclass
p SystemStackError.ancestors
p SystemStackError.ancestors.include?(StandardError)
`)
	want := strings.Join([]string{
		`"constant"`,
		`Exception`,
		`[SystemStackError, Exception, Object, Kernel, BasicObject]`,
		`false`,
	}, "\n")
	if got != want {
		t.Errorf("hierarchy:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestRunawayRecursionRaisesARescuableSystemStackError is the defect itself: this
// program used to print `runtime: goroutine stack exceeds 1000000000-byte limit`
// and a 50 KB goroutine dump, and exit 2 — a FATAL Go runtime error, which a host
// embedding rbgo through ruby.Run cannot recover() from (issue #768).
func TestRunawayRecursionRaisesARescuableSystemStackError(t *testing.T) {
	got := runAtDepth(t, 200, `
def f(n); f(n + 1); end
begin
  f(0)
rescue SystemStackError => e
  puts "SystemStackError rescued: #{e.class}"
  puts e.message
end
puts "still alive"
`)
	// "stack level too deep" is MRI's message, copied from
	// rb_vm_register_special_exception(..., "stack level too deep"),
	// proc.c v3_4_1:4425 — not invented here.
	want := "SystemStackError rescued: SystemStackError\nstack level too deep\nstill alive"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestBareRescueDoesNotCatchSystemStackError is the consequence of the hierarchy
// above, and the reason the hierarchy is worth a test of its own. Measured on the
// MRI 4.0.5 on this machine: the bare `rescue` does not fire.
func TestBareRescueDoesNotCatchSystemStackError(t *testing.T) {
	got := runAtDepth(t, 200, `
def f(n); f(n + 1); end
begin
  begin
    f(0)
  rescue => e
    puts "WRONG: bare rescue caught #{e.class}"
  end
rescue SystemStackError
  puts "not caught by bare rescue"
end
`)
	if got != "not caught by bare rescue" {
		t.Errorf("got %q, want %q", got, "not caught by bare rescue")
	}
}

// TestEveryKindOfRubyFrameIsCounted checks the claim that exec is the ONE place a
// Ruby-level frame is created, so one check covers every way to recurse. Each of
// these recurses through a different dispatch path and each must raise rather
// than reach the Go runtime.
func TestEveryKindOfRubyFrameIsCounted(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"method", `def f(n); f(n + 1); end`},
		{"kernel_send", `def f(n); send(:f, n + 1); end`},
		{"public_send", `def f(n); public_send(:f, n + 1); end`},
		{"block", `def f(n); 1.times { f(n + 1) }; end`},
		{"each_block", `def f(n); [1].each { f(n + 1) }; end`},
		{"yield", `def g; yield; end
def f(n); g { f(n + 1) }; end`},
		{"define_method", `define_method(:f) { |n| f(n + 1) }`},
		{"lambda", `F = ->(n) { F.call(n + 1) }
def f(n); F.call(n); end`},
		{"proc_call", `P = proc { |n| P.call(n + 1) }
def f(n); P.call(n); end`},
		{"method_object", `def f(n); method(:f).call(n + 1); end`},
		{"method_missing", `class Object
  def method_missing(m, *a); m == :zz ? f(a[0] + 1) : super; end
end
def f(n); zz(n); end`},
		{"instance_eval", `def f(n); 1.instance_eval { f(n + 1) }; end`},
		{"tap", `def f(n); 1.tap { f(n + 1) }; end`},
		{"super", `class A; def f(n); n; end; end
class B < A; def f(n); C.new.f(n + 1); end; end
class C < A; def f(n); B.new.f(n + 1); end; end
def f(n); B.new.f(n); end`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runAtDepth(t, 200, tc.body+`
begin
  f(0)
rescue SystemStackError
  puts "raised"
end
`)
			if got != "raised" {
				t.Errorf("%s: got %q, want %q — this path reached the Go stack instead", tc.name, got, "raised")
			}
		})
	}
}

// TestDepthDoesNotLeakOnAnyNonLocalExit is the test the design owes, because the
// depth is len(vm.frameNames) and exec has no defer that decrements it: the
// restoration is done by whoever CATCHES an unwind, truncating the stack back to
// its own saved depth.
//
// Each case drives a runaway recursion to the limit, rescues it, then does it
// AGAIN and reports the depth reached both times. Equal depths prove the counter
// came all the way back; a leak would show as a smaller second number (the second
// recursion starting pre-poisoned by the first), which is the exact failure a
// hand-rolled counter without a defer would produce.
func TestDepthDoesNotLeakOnAnyNonLocalExit(t *testing.T) {
	// The interleaved non-local exit, run between the two recursions: each is a
	// path that leaves exec WITHOUT running its tail.
	for _, tc := range []struct{ name, between string }{
		{"nothing", ``},
		{"break", `[1, 2, 3].each { |x| break if x == 2 }`},
		{"next", `[1, 2, 3].each { |x| next if x == 2 }`},
		{"return_from_block", `def r; [1, 2, 3].each { |x| return x if x == 2 }; end
r`},
		{"throw_catch", `catch(:t) { [1].each { throw :t, 9 } }`},
		{"ensure_through_raise", `begin
  (begin; raise "x"; ensure; nil; end)
rescue RuntimeError
  nil
end`},
		{"deep_return_from_block", `def r(n); [1].each { return n if n > 50; r(n + 1) }; nil; end
r(0)`},
		{"deep_break", `def r(n); [1].each { break if n > 50; r(n + 1) }; nil; end
r(0)`},
		{"deep_throw", `def r(n); throw :deep if n > 50; r(n + 1); end
catch(:deep) { r(0) }`},
		{"deep_ensure", `def r(n); begin; r(n + 1) if n < 50; ensure; nil; end; end
r(0)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runAtDepth(t, 200, `
$d = 0
def f(n); $d = n; f(n + 1); end
def probe
  $d = 0
  begin
    f(0)
  rescue SystemStackError
  end
  $d
end
first = probe
`+tc.between+`
second = probe
puts first
puts second == first ? "same" : "LEAKED: second=#{second} first=#{first}"
`)
			lines := strings.Split(got, "\n")
			if len(lines) != 2 {
				t.Fatalf("got %q, want two lines", got)
			}
			// The depth reached must be the limit, less the frames the probe
			// itself occupies (top level, probe, and the begin/rescue): a number
			// that is merely non-zero would pass even if the limit fired early.
			if lines[0] != "197" {
				t.Errorf("first recursion reached depth %s, want 197 (limit 200 less the probe's own frames)", lines[0])
			}
			if lines[1] != "same" {
				t.Errorf("after %s: %s", tc.name, lines[1])
			}
		})
	}
}

// TestASecondRunawayRecursionStillRaises states the leak consequence as the thing
// a user would notice, separately from the depth arithmetic above: a program that
// rescues one runaway recursion must still be able to run — and still be
// protected — afterwards.
func TestASecondRunawayRecursionStillRaises(t *testing.T) {
	got := runAtDepth(t, 200, `
def f(n); f(n + 1); end
3.times do |i|
  begin
    f(0)
    puts "WRONG: no raise on round #{i}"
  rescue SystemStackError
    puts "round #{i} raised"
  end
end
puts "still alive"
`)
	want := "round 0 raised\nround 1 raised\nround 2 raised\nstill alive"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestRescuingAtTheLimitTerminates covers the one shape that could in principle
// spin: a `rescue` written INSIDE the recursive method, so the handler runs at a
// depth that is itself at the limit and whatever it calls raises again. Each
// re-raise is caught one frame further out, so the cascade is bounded by the
// depth — but "bounded" is a claim worth a test, since the alternative is a hang.
func TestRescuingAtTheLimitTerminates(t *testing.T) {
	got := runAtDepth(t, 120, `
$n = 0
def f(n)
  begin
    f(n + 1)
  rescue SystemStackError
    $n += 1
    raise
  end
end
begin
  f(0)
rescue SystemStackError
  puts "terminated"
end
puts $n > 0
`)
	if got != "terminated\ntrue" {
		t.Errorf("got %q, want %q", got, "terminated\ntrue")
	}
}

// TestDeepRecursionBelowTheLimitStillWorks is the other half of the gate: the
// limit must not break legitimate recursion. 150 frames under a limit of 200
// must complete normally.
func TestDeepRecursionBelowTheLimitStillWorks(t *testing.T) {
	got := runAtDepth(t, 200, `
def f(n); n == 0 ? 0 : 1 + f(n - 1); end
puts f(150)
`)
	if got != "150" {
		t.Errorf("got %q, want %q", got, "150")
	}
}

// TestDefaultCallDepthLimitLeavesRoomForMRIProgrammes guards the conformance side
// of the constant from below. MRI's limit is a budget of machine-stack BYTES, so
// its depth depends on how big a frame is: the MRI 4.0.5 on the development
// machine reaches 10919 frames of `def f(n); $d = n; f(n + 1); end` and only 3853
// levels of the same recursion through a block. A limit below MRI's deepest would
// make rbgo raise where MRI does not, turning a security fix into a conformance
// regression.
func TestDefaultCallDepthLimitLeavesRoomForMRIProgrammes(t *testing.T) {
	const deepestObservedMRIDepth = 10919
	if defaultMaxCallDepth <= deepestObservedMRIDepth {
		t.Errorf("defaultMaxCallDepth = %d is not above the deepest depth MRI 4.0.5 was measured to reach (%d): a program that recurses that far works on MRI and would raise here",
			defaultMaxCallDepth, deepestObservedMRIDepth)
	}
}

// TestANewVMAlwaysHasALimit pins the thing that would silently restore the defect:
// a VM built by the public constructor must never have maxCallDepth 0, which the
// `>=` check would read as "refuse every frame" — and, were the check written the
// other way round, as "no limit at all".
func TestANewVMAlwaysHasALimit(t *testing.T) {
	if got := New(&bytes.Buffer{}).maxCallDepth; got != defaultMaxCallDepth {
		t.Errorf("New(...).maxCallDepth = %d, want defaultMaxCallDepth = %d", got, defaultMaxCallDepth)
	}
}

// TestTheLimitHoldsAcrossAThreadBoundary checks a shape the design could
// plausibly have got wrong. rbgo keeps ONE frame stack for the whole VM, shared
// by every Ruby thread under the GVL, and Run resets it to [:0] at its
// boundaries — so a thread body could have been handed a FRESH depth budget
// while its parent's Go frames were still live, letting a program amplify its
// reachable depth by one limit per thread it spawns.
//
// It does not: the shared stack keeps accumulating across the boundary, so the
// recursion below reaches HALF the limit in levels (each level costs two
// entries, the method frame and the block frame) rather than restarting. That is
// conservative — each Ruby thread is a goroutine with its own Go stack, so the
// budget is stricter than the stacks require — and conservative is the safe
// direction for a limit whose job is to stay below a fatal error.
//
// Measured with the real constant before it was written down: plain recursion
// reached 16382 and this shape reached 8191, exactly half.
func TestTheLimitHoldsAcrossAThreadBoundary(t *testing.T) {
	got := runAtDepth(t, 60, `
$max = 0
def g(n)
  $max = n if n > $max
  Thread.new { g(n + 1) }.join
end
begin
  g(0)
rescue SystemStackError
end
puts $max < 60
puts $max > 10
`)
	if got != "true\ntrue" {
		t.Errorf("got %q, want %q — a thread boundary either reset the depth budget or exhausted it too early", got, "true\ntrue")
	}
}
