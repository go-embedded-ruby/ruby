package vm

// Ruby call-depth limit.
//
// Runaway recursion in a Ruby program must raise SystemStackError, which the
// program can rescue. Before this limit existed it instead exhausted the Go
// goroutine stack, and a Go stack overflow is a FATAL RUNTIME ERROR, not a
// panic: `runtime: goroutine stack exceeds 1000000000-byte limit` /
// `fatal error: stack overflow`, exit status 2, with a deferred recover()
// never running. There is therefore no mitigation available to an embedding
// host — ruby.Run is the documented embedding API, so three lines of untrusted
// Ruby could end the host process (issue #768). The limit has to live in the
// interpreter, which is also where MRI puts it.
//
// MRI's own limit is a budget of MACHINE-STACK BYTES plus a VM-stack size
// (rb_execution_context_t's machine.stack_maxsize and vm_stack_size,
// vm_core.h v3_4_1:1070), checked by CHECK_VM_STACK_OVERFLOW
// (vm_core.h v3_4_1:1912) and stack_check/ec_stack_overflow
// (vm_insnhelper.c v3_4_1:59). Expressed in depth, the MRI 4.0.5 on the
// development machine reaches 11913 frames of `def f(n); f(n + 1); end`
// before raising. rbgo counts FRAMES rather than bytes because it has no
// portable way to read the Go stack pointer, and because a frame count is the
// quantity it can restore exactly on every unwind path.
const (
	// defaultMaxCallDepth is the depth at which exec refuses to push frame
	// number defaultMaxCallDepth+1 and raises SystemStackError instead.
	//
	// TO BE CHOSEN BY MEASUREMENT (next commit in this branch), not by taste.
	// TestStackDepthLimitSitsBelowTheGoOverflow
	// is the standing check: it binary-searches, in subprocesses under a reduced
	// runtime/debug.SetMaxStack, the largest depth rbgo can actually reach before
	// the Go runtime kills the process, converts that to BYTES OF GO STACK PER
	// RUBY FRAME, and fails if this constant is not comfortably below the depth
	// that figure implies at Go's default 1 GB maximum.
	//
	// Bytes-per-frame is the portable quantity and the one worth quoting: it is a
	// property of rbgo's call path, whereas the depth it permits moves if Go's
	// default stack maximum ever changes. The worst (most Go-stack-hungry) path
	// measured is a block yielded through Integer#times, not a plain send, and
	// the figure below is that worst path's.
	//
	// The margin is deliberately large: the
	// measurement is per-platform and per-toolchain, a future Ruby-level frame
	// may cost more Go stack than today's, and overshooting the budget is fatal
	// and unrecoverable while undershooting it merely raises an exception the
	// program was already required to handle.
	defaultMaxCallDepth = 10000
)

// raiseDeep raises SystemStackError with MRI's exact message.
//
// It needs NO Ruby-level frame of its own, which is what makes it safe to call
// from the very place that has just refused to create one: exceptionObject
// builds the exception as a Go *RObject and never dispatches #initialize, and
// rbgo runs `rescue`/`ensure` bodies as instructions inside the frame that owns
// them rather than as new frames. MRI needs a preallocated instance
// (special_exceptions[ruby_error_sysstack], vm_insnhelper.c v3_4_1:61) for the
// same reason: at the moment of overflow there is no room to build one.
//
// The message is copied from MRI rather than invented:
// rb_vm_register_special_exception(ruby_error_sysstack, rb_eSysStackError,
// "stack level too deep") (proc.c v3_4_1:4425).
func raiseDeep() {
	raise("SystemStackError", "stack level too deep")
}
