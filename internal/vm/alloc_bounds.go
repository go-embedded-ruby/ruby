package vm

import (
	"math"
	"runtime"
	"strings"
	"unsafe"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Bounds for an allocation whose size comes from a Ruby value.
//
// MRI guards such an allocation in two places, and the two give DIFFERENT
// exception classes. Reproducing that asymmetry is the point of this file;
// normalising it would be wrong.
//
//  1. A multiplying operator DIVIDES a ceiling by one factor and compares the
//     quotient with the other, so the product is never formed:
//
//     array.c  v4.0.5:5235   if (ARY_MAX_SIZE/len < RARRAY_LEN(ary))
//     rb_raise(rb_eArgError, "argument too big");
//     string.c v4.0.5:2593   if (len && LONG_MAX/len <  RSTRING_LEN(str))
//     rb_raise(rb_eArgError, "argument too big");
//
//     The two ceilings are not the same, and that is the whole asymmetry.
//     Array#* divides ARY_MAX_SIZE, which is LONG_MAX/sizeof(VALUE)
//     (array.c v4.0.5:73) — a real allocation ceiling. String#* divides bare
//     LONG_MAX — a pure arithmetic-overflow test, because a String's unit is one
//     byte and no product of a byte count can exceed a long without overflowing
//     it. So, measured against MRI 4.0.5:
//
//     [1]  * (2**62)  =>  ArgumentError: argument too big
//     "x"  * (2**62)  =>  NoMemoryError: failed to allocate memory
//     "ab" * (2**62)  =>  ArgumentError: argument too big
//
//     ARY_MAX_SIZE/2**62 is 0, which IS below RARRAY_LEN([1]) == 1, so Array#*
//     refuses. LONG_MAX/2**62 is 1, which is NOT below RSTRING_LEN("x") == 1, so
//     String#* proceeds to allocate and the ALLOCATION is what fails. Widen the
//     receiver to two bytes and LONG_MAX/2**62 == 1 < 2, so String#* refuses too
//     — which is why "ab" is an ArgumentError and "x" is not.
//
//  2. Whatever survives (1) is allocated, and a refused allocation is
//     NoMemoryError.
//
// rbgo formed the product FIRST — len(a.Elems)*int(n) — and tested the result,
// which is three defects in one (issue #777). The product can wrap NEGATIVE
// (make panics "cap out of range"), wrap to exactly ZERO (make SUCCEEDS with cap
// 0 and the fill loop then grows without bound: no panic, no exception, just
// memory), or stay positive and still land above what make accepts. Dividing
// rather than multiplying makes all three unrepresentable, which is why every
// guard below divides and none multiplies before it has checked.
const (
	// aryMaxSize is ARY_MAX_SIZE (array.c v4.0.5:73, LONG_MAX/(int)sizeof(VALUE)):
	// the largest element count whose byte size still fits a machine int.
	// unsafe.Sizeof derives the element width from object.Value's own
	// representation rather than hard-coding 16, so the bound follows the target's
	// word size — 32-bit wasm included — and a change to Value cannot silently
	// leave the bound behind.
	aryMaxSize = int64(math.MaxInt) / int64(unsafe.Sizeof(object.Value(nil)))

	// strMaxSize is the bare LONG_MAX of string.c v4.0.5:2593. A String's element
	// is one byte, so its sizeof is 1 and the ceiling IS the machine int maximum.
	strMaxSize = int64(math.MaxInt)
)

// repeatFitsArray reports whether repeating elems elements n times stays within
// ARY_MAX_SIZE, by MRI's division test (array.c v4.0.5:5235). n must not be
// negative; a zero n always fits (MRI returns an empty array before testing).
func repeatFitsArray(elems int, n int64) bool {
	return n == 0 || aryMaxSize/n >= int64(elems)
}

// repeatFitsString reports whether repeating srcLen bytes n times stays within a
// machine int, by MRI's division test (string.c v4.0.5:2593). n must not be
// negative; a zero n always fits.
func repeatFitsString(srcLen int, n int64) bool {
	return n == 0 || strMaxSize/n >= int64(srcLen)
}

// fitsArrayAlloc reports whether n elements stay within ARY_MAX_SIZE. It is the
// single-factor form of the test, for a size that is taken from a Ruby value
// without being multiplied (Array.new's capacity, Array#fill's new length).
func fitsArrayAlloc(n int64) bool { return n <= aryMaxSize }

// allocValues returns an empty []object.Value with capacity n, raising
// NoMemoryError if the runtime refuses the capacity. The caller must already
// have checked n against aryMaxSize; this converts the remaining regime, where a
// capacity below ARY_MAX_SIZE is still more than the Go runtime will hand out.
func allocValues(n int64) []object.Value {
	defer convertAllocPanic()
	return make([]object.Value, 0, n)
}

// allocBytes returns an empty []byte with capacity n, raising NoMemoryError if
// the runtime refuses the capacity. This is the regime MRI reaches for
// `"x" * (2**62)`: its own guard passes and malloc is what fails.
func allocBytes(n int64) []byte {
	defer convertAllocPanic()
	return make([]byte, 0, n)
}

// convertAllocPanic turns a Go allocation fault into NoMemoryError, which is
// MRI's class and message for a refused allocation. The message is MRI's own
// literal (gc.c v4.0.5:5545, rb_vm_register_special_exception(... rb_eNoMemError,
// "failed to allocate memory")), and it is what MRI 4.0.5 prints for
// `"x" * (2**62)` as measured.
//
// It must be deferred DIRECTLY — `defer convertAllocPanic()`, never wrapped in a
// closure — because recover() only takes effect when called by the deferred
// function itself.
//
// Only an allocation fault converts. Go names those in the panic text, and the
// prefix is the only handle a program has on them: "makeslice:", "makemap:" and
// "growslice:" are allocation refusals, while "index out of range" and "slice
// bounds out of range" are ordinary faults that must keep their existing
// meaning. Anything else — a RubyError, a control-flow signal, a broken VM
// invariant — is re-panicked untouched so real defects stay loud. If a future Go
// release renames these, the classification degrades to the pre-existing
// behaviour (callNative's ArgumentError) rather than breaking, and
// TestAllocPanicTextStillNamesMakeslice fails to say so.
//
// Go's own maxAlloc is not reachable from a program, so it cannot be tested for
// up front; recovering the panic it raises is the only way to name that bound.
// One regime stays out of reach: a capacity BELOW maxAlloc that the host cannot
// actually satisfy makes the Go runtime abort with a fatal "out of memory" that
// no recover can catch, where MRI would raise NoMemoryError. That residual is a
// property of the runtime, not of this guard.
func convertAllocPanic() {
	if r := recover(); r != nil {
		if re, ok := r.(runtime.Error); ok && isAllocFault(re.Error()) {
			raise("NoMemoryError", "failed to allocate memory")
		}
		panic(r)
	}
}

// isAllocFault reports whether a Go runtime panic text names an allocation
// refusal rather than an ordinary bounds fault. See convertAllocPanic.
func isAllocFault(msg string) bool {
	for _, p := range [...]string{"makeslice:", "makemap:", "growslice:"} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
