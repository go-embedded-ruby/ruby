package vm

// Ruby call-depth limit.
//
// Runaway recursion in a Ruby program must raise SystemStackError, which the
// program can rescue. Before this limit existed it instead exhausted the Go
// goroutine stack, and a Go stack overflow is a FATAL RUNTIME ERROR, not a
// panic: `runtime: goroutine stack exceeds 1000000000-byte limit` /
// `fatal error: stack overflow`, exit status 2, with a deferred recover() never
// running. There is therefore no mitigation available to an embedding host —
// ruby.Run is the documented embedding API, so three lines of untrusted Ruby
// could end the host process (issue #768, where the recover() and
// debug.SetMaxStack controls are written up). The limit has to live in the
// interpreter, which is also where MRI puts it.
//
// MRI's own limit is a budget of MACHINE-STACK BYTES plus a VM-stack size
// (rb_execution_context_t's machine.stack_maxsize, vm_core.h v3_4_1:1070),
// checked by CHECK_VM_STACK_OVERFLOW (vm_core.h v3_4_1:1912) and by
// stack_check/ec_stack_overflow (vm_insnhelper.c v3_4_1:59).
//
// rbgo counts FRAMES rather than bytes because it has no portable way to read
// the Go stack pointer: a stored stack address cannot be used as a baseline,
// since the Go runtime COPIES a goroutine's stack when it grows and the saved
// address is then stale by the relocation delta. A frame count is also the
// quantity rbgo can restore exactly on every unwind path.
//
// Because MRI's budget is in bytes, ITS depth depends on how big a frame is,
// and is therefore not one number. Measured against the MRI 4.0.5 on the
// development machine:
//
//	def f(n); $d = n; f(n + 1); end                     10919 frames
//	def f(n); $d = n; 1.times { f(n + 1) }; end          3853 levels
//
// 10919 is the figure that matters here: a limit below MRI's DEEPEST reachable
// depth would make rbgo raise where MRI does not, turning a security fix into a
// conformance regression. TestDefaultCallDepthLimitLeavesRoomForMRIProgrammes
// is the guard from that side.
const (
	// defaultMaxCallDepth is the depth at which exec refuses to push frame
	// number defaultMaxCallDepth+1 and raises SystemStackError instead.
	//
	// CHOSEN BY MEASUREMENT, not by taste. 28 recursion paths were bisected in
	// subprocesses — binary-searching the largest limit the path can be given
	// before the Go runtime kills the process — which gives BYTES OF GO STACK
	// PER RUBY FRAME, the portable quantity. Worst and best of the 28, on
	// darwin/arm64 with Go 1.27.1:
	//
	//	send(:public_send, :send, :f, n + 1)   8132 B/frame   WORST
	//	catch(:x) { f(n + 1) }                 6531 B/frame
	//	1.times { f(n + 1) }                   6228 B/frame
	//	[1].inject(0) { f(n + 1) }             5998 B/frame
	//	f(n + 1)                               5619 B/frame   best
	//
	// Bytes-per-frame is dead stable across stack sizes — 6452.8, 6450.3,
	// 6448.4 and 6448.1 B/frame at SetMaxStack of 8, 16, 64 and 256 MiB — so it
	// is a property of rbgo's call path and not of the measurement.
	//
	// THE DEPTH IT IMPLIES IS NOT 1 GB DIVIDED BY IT, and getting that wrong
	// would have been a factor of two. Go's default maximum is 1,000,000,000
	// bytes (decimal), and goroutine stacks are POWERS OF TWO, so the largest
	// stack actually permitted is 2^29 = 536,870,912 bytes — 512 MiB, since
	// 2^30 exceeds the default. The worst path's overflow depth is therefore
	// 512 MiB / 8132 = 66016, and bisecting it directly at Go's real default,
	// with no SetMaxStack at all, measured 66051. The prediction and the direct
	// measurement agree to 0.05%, which is what makes the model quotable.
	//
	// 16384 sits 4.03x below that measured 66051, and 1.50x above MRI's deepest
	// 10919. The margin is deliberately large and asymmetric on purpose:
	// overshooting the budget is fatal and unrecoverable, while undershooting
	// it merely raises an exception the program was already required to handle.
	// It also has to absorb a figure measured on ONE platform and toolchain,
	// and a future Ruby-level frame that costs more Go stack than today's.
	// At the limit the worst path holds about 133 MB of Go stack.
	//
	// TestStackDepthLimitSitsBelowTheGoOverflow re-derives all of this on every
	// run and fails if the margin erodes.
	//
	// KNOWN INCOMPLETE, and measured rather than assumed: a frame count cannot
	// bound the Go stack on its own, because a program can put an unbounded
	// number of NATIVE dispatch layers between two Ruby frames, and a native
	// layer costs Go stack while pushing no frame. Each extra Kernel#send layer
	// in `send(:send, ..., :f, n + 1)` costs 833 B:
	//
	//	sends per Ruby frame      1      2      4      8     16     32
	//	bytes per frame        7282   8117   9777  13107  19761  33091
	//	overflow depth        73728  66144  54912  40960  27168  16224
	//
	// so about 25 nested sends per frame bring the overflow depth below this
	// limit, and no limit above MRI's 10919 can cover an unbounded chain.
	// Closing that needs a count of native dispatch nesting as well, which is a
	// change to the hot dispatch path (vm.send) and so a scope decision of its
	// own rather than a line added here — see issue #768.
	defaultMaxCallDepth = 16384
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
