package vm

import (
	"io"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strconv"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// The worst (most Go-stack-hungry) of the 28 recursion paths measured when
// defaultMaxCallDepth was chosen: three nested native dispatch layers per Ruby
// frame, at 8132 bytes of Go stack per frame against 5619 for a plain `f(n+1)`.
// Measuring the margin on the cheapest path would overstate it by 45%.
const worstMeasuredRecursion = `
def f(n); send(:public_send, :send, :f, n + 1); end
begin
  f(0)
rescue SystemStackError
end
`

// measureStackChildEnv names the environment variable that turns this test
// binary into the child half of the measurement: a depth limit to run under.
const measureStackChildEnv = "RBGO_STACKDEPTH_CHILD"

// stackDepthChild is the child half. It runs the worst recursion path under the
// given call-depth limit and a REDUCED runtime/debug.SetMaxStack, and exits 0
// only if the program completed — i.e. if SystemStackError was raised before the
// Go runtime ran out of stack. Exceeding the stack is fatal and kills the
// process, which is precisely the signal the parent bisects on, and precisely
// why this cannot be done in-process.
//
// The stack limit is reduced so each probe costs milliseconds: at Go's real
// default the recursion would have to allocate half a gigabyte of stack to
// reach the overflow. What the parent recovers from the small-stack measurement
// is BYTES PER FRAME, which is stable across stack sizes (see stackdepth.go),
// and the depth at the real default follows from it.
func stackDepthChild() {
	limit, err := strconv.Atoi(os.Getenv(measureStackChildEnv))
	if err != nil {
		os.Exit(3)
	}
	maxStack, err := strconv.Atoi(os.Getenv("RBGO_STACKDEPTH_MAXSTACK"))
	if err != nil {
		os.Exit(3)
	}
	debug.SetMaxStack(maxStack)
	prog, err := parser.Parse(worstMeasuredRecursion)
	if err != nil {
		os.Exit(3)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		os.Exit(3)
	}
	// On its own goroutine, so the measured stack is the interpreter's and not
	// the testing framework's.
	status := make(chan int, 1)
	go func() {
		vm := New(io.Discard)
		vm.maxCallDepth = limit
		if _, err := vm.Run(iseq); err != nil {
			status <- 4
			return
		}
		status <- 0
	}()
	os.Exit(<-status)
}

// TestStackDepthLimitSitsBelowTheGoOverflow is the standing form of the
// measurement that chose defaultMaxCallDepth, and the guard that keeps it
// honest: it fails if a change to rbgo makes a Ruby frame expensive enough in Go
// stack that the limit stops being comfortably below the depth at which the Go
// runtime kills the process.
//
// It bisects, in subprocesses, the largest limit the worst recursion path can be
// given before the process dies, under a deliberately small SetMaxStack. That
// yields bytes of Go stack per Ruby frame, from which the depth at Go's real
// default follows — NOT by dividing the 1,000,000,000-byte default by it, but by
// dividing 2^29, because goroutine stacks are powers of two and 2^30 exceeds the
// default. Getting that wrong overstates the available depth by exactly 2x; see
// stackdepth.go, where the direct measurement at the real default (66051) is
// recorded against this model's prediction (66016).
func TestStackDepthLimitSitsBelowTheGoOverflow(t *testing.T) {
	if os.Getenv(measureStackChildEnv) != "" {
		stackDepthChild()
		return
	}
	if runtime.GOARCH == "wasm" {
		t.Skip("wasm cannot spawn the subprocess this measurement needs")
	}
	const maxStack = 8 << 20
	survives := func(limit int) bool {
		cmd := exec.Command(os.Args[0], "-test.run=TestStackDepthLimitSitsBelowTheGoOverflow", "-test.timeout=10m")
		cmd.Env = append(os.Environ(),
			measureStackChildEnv+"="+strconv.Itoa(limit),
			"RBGO_STACKDEPTH_MAXSTACK="+strconv.Itoa(maxStack))
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		return cmd.Run() == nil
	}
	// A limit of 1 must always be survivable; if even that kills the process the
	// measurement is measuring something other than what it thinks.
	if !survives(1) {
		t.Fatal("the child died at a call-depth limit of 1: the measurement is not measuring recursion")
	}
	lo, hi := 1, 2
	for hi < 1<<22 && survives(hi) {
		lo = hi
		hi *= 2
	}
	if hi >= 1<<22 {
		t.Fatalf("no limit below %d overflowed an %d-byte stack: either the limit is not being applied or SetMaxStack is not honoured", 1<<22, maxStack)
	}
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if survives(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	bytesPerFrame := float64(maxStack) / float64(lo)
	// 2^29 and not 2^30: see the comment above and stackdepth.go.
	const largestGoStackUnderTheDefault = 1 << 29
	overflowDepth := int(largestGoStackUnderTheDefault / bytesPerFrame)
	t.Logf("worst measured recursion path: reached depth %d under a %d-byte stack = %.0f bytes of Go stack per Ruby frame",
		lo, maxStack, bytesPerFrame)
	t.Logf("implied overflow depth at Go's default maximum (largest power-of-two stack under 1,000,000,000 = %d bytes): %d",
		largestGoStackUnderTheDefault, overflowDepth)
	t.Logf("defaultMaxCallDepth = %d, margin %.2fx", defaultMaxCallDepth, float64(overflowDepth)/float64(defaultMaxCallDepth))

	// The margin the constant was chosen with is 4.03x. Failing below 2x leaves
	// room for the platform-to-platform variation this single-platform
	// measurement cannot see, while still catching a real erosion: a change that
	// doubled the Go stack cost of a Ruby frame would trip it.
	const minMargin = 2.0
	if got := float64(overflowDepth) / float64(defaultMaxCallDepth); got < minMargin {
		t.Errorf("defaultMaxCallDepth = %d is only %.2fx below the measured Go overflow depth %d (%.0f bytes/frame); want at least %.1fx. A Ruby frame has become more expensive in Go stack — lower the constant rather than lowering this bound.",
			defaultMaxCallDepth, got, overflowDepth, bytesPerFrame, minMargin)
	}
}
