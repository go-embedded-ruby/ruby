// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// runKeepingError runs a program and returns the VM, its stdout and whatever
// came out of Run, so a test can assert on a program that ends by raising AND on
// the state the VM was left in. runSrcErr does the same but lives behind
// `//go:build !windows && !wasm`, and these tests must compile on every lane —
// CI vets the test binary for GOOS=wasip1 and GOOS=js.
func runKeepingError(t *testing.T, src string) (*VM, string, error) {
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
	machine := New(&buf)
	_, runErr := machine.Run(iseq)
	return machine, buf.String(), runErr
}

// TestRepeatBoundRegimes is the witness for issue #777. A size taken from a Ruby
// value and multiplied reaches `make` in three distinct regimes, and before the
// fix each failed differently and none failed correctly:
//
//	wrapped NEGATIVE    [1,2] * (2**63-1)    2*(2**63-1) is -2 as an int
//	over what make takes [1] * (2**62)       positive, above maxAlloc
//	wrapped to ZERO     [1,2,3,4] * (2**62)  4*2**62 is EXACTLY 0
//
// The last is the dangerous one: `make` SUCCEEDED with capacity 0, so there was
// no panic and no exception, and the fill loop then appended 2**62 slices of 4.
// Measured on the pre-fix binary it passed 1.1 GB of resident memory in 60 s and
// was still climbing when the harness killed it.
//
// Every guard divides rather than multiplies (MRI's own shape, array.c
// v4.0.5:5235), so all three regimes are refused by the same test and the
// product is never formed. Expectations measured byte-for-byte against the
// installed ruby 4.0.5.
func TestRepeatBoundRegimes(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// --- Array#*: MRI divides ARY_MAX_SIZE, so all of these are ArgumentError.
		{"ary_over_maxalloc", `begin; [1] * (2**62); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: argument too big\n"},
		{"ary_two_elem", `begin; [1,2] * (2**62); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: argument too big\n"},
		// The silent one: 4 * 2**62 == 2**64 == 0 in a machine int.
		{"ary_wrap_to_zero", `begin; [1,2,3,4] * (2**62); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: argument too big\n"},
		// 2 * (2**63-1) wraps to -2.
		{"ary_wrap_negative", `begin; [1,2] * (2**63-1); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: argument too big\n"},
		{"ary_wrap_negative_odd", `begin; [1,2,3] * (2**63-1); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: argument too big\n"},

		// --- String#*: MRI divides bare LONG_MAX, so the class depends on the
		// receiver's LENGTH. This asymmetry is MRI's and is reproduced, not
		// normalised -- see alloc_bounds.go.
		{"str_one_byte_is_nomemory", `begin; "x" * (2**62); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"NoMemoryError: failed to allocate memory\n"},
		{"str_two_byte_is_argerror", `begin; "ab" * (2**62); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: argument too big\n"},
		{"str_three_byte_is_argerror", `begin; "xyz" * (2**62); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: argument too big\n"},

		// --- Empty receiver: repeats to empty for ANY in-range count, without
		// looping. `[] * (2**62)` used to loop 2**62 times appending nothing.
		{"empty_ary_huge", `p([] * (2**62))`, "[]\n"},
		{"empty_str_huge", `p("" * (2**62))`, "\"\"\n"},
		// ...but a negative count still raises, because MRI tests the sign before
		// the empty case (array.c v4.0.5:5228-5233).
		{"empty_ary_negative", `begin; [] * -1; rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: negative argument\n"},
		{"empty_str_negative", `begin; "" * -1; rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: negative argument\n"},

		// --- Count coercion now goes through NUM2LONG for Array#* too, so an
		// out-of-range Bignum is RangeError (MRI) and not TypeError (rbgo before).
		{"ary_bignum_count", `begin; [1] * (10**40); rescue Exception=>e; puts e.class; end`,
			"RangeError\n"},
		{"str_bignum_count", `begin; "x" * (10**40); rescue Exception=>e; puts e.class; end`,
			"RangeError\n"},
		// A Float count truncates toward zero, as NUM2LONG does.
		{"ary_float_count", `p([1,2] * 2.9)`, "[1, 2, 1, 2]\n"},
		// A #to_int object converts; this used to be a separate inline coercion.
		{"ary_to_int_count", `o = Object.new; def o.to_int; 3; end; p([7] * o)`, "[7, 7, 7]\n"},
		// Nothing integer-ish at all is still a TypeError.
		{"ary_no_coercion", `begin; [1] * :sym; rescue Exception=>e; puts e.class; end`, "TypeError\n"},

		// --- Zero and ordinary counts keep working (the fast path).
		{"ary_zero", `p([1,2] * 0)`, "[]\n"},
		{"ary_small", `p([1,2] * 3)`, "[1, 2, 1, 2, 1, 2]\n"},
		{"str_small", `p("ab" * 3)`, "\"ababab\"\n"},

		// --- Array.new no longer leaks the Go text. Before: `ArgumentError: new:
		// runtime error: makeslice: cap out of range`.
		{"array_new_over_ceiling", `begin; Array.new(2**62); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: array size too big\n"},
		{"array_new_with_default", `begin; Array.new(2**62, 0); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: array size too big\n"},

		// --- Array#fill reports rb_ary_fill's own message (array.c v4.0.5:5087),
		// which is not ary_new's.
		{"fill_too_big", `begin; [1,2,3].fill(0, 0, 2**62); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"ArgumentError: argument too big\n"},

		// --- The callNative backstop: a size that reaches a `make` with no guard of
		// its own is now NoMemoryError with MRI's message, not an ArgumentError
		// carrying Go's internal text.
		{"ljust_backstop", `begin; "x".ljust(2**62); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"NoMemoryError: failed to allocate memory\n"},
		{"rjust_backstop", `begin; "x".rjust(2**62); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"NoMemoryError: failed to allocate memory\n"},
		{"center_backstop", `begin; "x".center(2**62); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"NoMemoryError: failed to allocate memory\n"},
		{"range_to_a_backstop", `begin; (1..2**62).to_a; rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"NoMemoryError: failed to allocate memory\n"},
		{"integer_shift_backstop", `begin; 1 << (2**62); rescue Exception=>e; puts "#{e.class}: #{e.message}"; end`,
			"NoMemoryError: failed to allocate memory\n"},
		// An ordinary bounds fault must NOT be reclassified: only an allocation
		// fault becomes NoMemoryError, so this stays the ArgumentError it was.
		{"index_fault_stays_argerror", `p("x".ljust(3))`, "\"x  \"\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}

// TestAllocPanicTextStillNamesMakeslice pins the one thing isAllocFault cannot
// control: the text the Go runtime puts in a makeslice panic. The classification
// in convertAllocPanic reads that text because a program has no other handle on
// maxAlloc, so if a Go release renames it this test says so rather than letting
// every refused allocation quietly go back to being an ArgumentError with Go's
// internals in the message.
//
// This is the known positive the classifier is validated on.
func TestAllocPanicTextStillNamesMakeslice(t *testing.T) {
	msg := func() (s string) {
		defer func() {
			r := recover()
			s = r.(error).Error()
		}()
		n := int64(math.MaxInt64 / 4) // above maxAlloc on every supported platform
		_ = make([]byte, 0, n)
		return ""
	}()
	if !strings.Contains(msg, "makeslice:") {
		t.Fatalf("Go's makeslice panic text no longer contains %q: %q\n"+
			"isAllocFault classifies on this prefix; update it (and this test) together.", "makeslice:", msg)
	}
	if !isAllocFault(msg) {
		t.Errorf("isAllocFault(%q) = false, want true", msg)
	}
	// The negative side: an ordinary bounds fault must not be classified as an
	// allocation refusal, or callNative would report every one of them as
	// NoMemoryError.
	for _, notAlloc := range []string{
		"runtime error: index out of range [5] with length 3",
		"runtime error: slice bounds out of range [:9] with capacity 4",
		"runtime error: invalid memory address or nil pointer dereference",
	} {
		if isAllocFault(notAlloc) {
			t.Errorf("isAllocFault(%q) = true, want false", notAlloc)
		}
	}
}

// TestRepeatBoundsAreDerivedNotGuessed checks the two ceilings against the
// definitions they are transcribed from, so a change to object.Value's width (or
// a 32-bit target) cannot leave them behind. ARY_MAX_SIZE is LONG_MAX divided by
// the element width (array.c v4.0.5:73); strMaxSize is bare LONG_MAX, because a
// String's element is one byte (string.c v4.0.5:2593).
func TestRepeatBoundsAreDerivedNotGuessed(t *testing.T) {
	if strMaxSize != int64(math.MaxInt) {
		t.Errorf("strMaxSize = %d, want math.MaxInt = %d", strMaxSize, int64(math.MaxInt))
	}
	if aryMaxSize >= strMaxSize {
		t.Errorf("aryMaxSize (%d) must be strictly below strMaxSize (%d): an element is wider than a byte",
			aryMaxSize, strMaxSize)
	}
	if aryMaxSize <= 0 {
		t.Fatalf("aryMaxSize = %d, want positive", aryMaxSize)
	}
	// The division guards must agree with the ceilings at the boundary, and must
	// never form the product.
	if !repeatFitsArray(1, aryMaxSize) {
		t.Errorf("repeatFitsArray(1, aryMaxSize) = false, want true at the boundary")
	}
	if repeatFitsArray(2, aryMaxSize) {
		t.Errorf("repeatFitsArray(2, aryMaxSize) = true, want false just past the boundary")
	}
	if !repeatFitsString(1, strMaxSize) {
		t.Errorf("repeatFitsString(1, strMaxSize) = false, want true at the boundary")
	}
	if repeatFitsString(2, strMaxSize) {
		t.Errorf("repeatFitsString(2, strMaxSize) = true, want false just past the boundary")
	}
	// A zero count always fits, whatever the other factor: MRI returns empty
	// before testing, and dividing by zero must never be reached.
	if !repeatFitsArray(math.MaxInt32, 0) || !repeatFitsString(math.MaxInt32, 0) {
		t.Error("a zero count must fit for any receiver length")
	}
	// fitsArrayAlloc is the single-factor form used by Array.new / Array#fill.
	if !fitsArrayAlloc(aryMaxSize) {
		t.Error("fitsArrayAlloc(aryMaxSize) = false, want true at the boundary")
	}
	if fitsArrayAlloc(aryMaxSize + 1) {
		t.Error("fitsArrayAlloc(aryMaxSize+1) = true, want false")
	}
}

// TestNonRubyErrorPanicKeepsItsCause is the witness for the second defect in
// #777, independent of the first. Run's deferred handler converted the recovered
// value with an UNCHECKED `r.(RubyError)` assertion, so any other panic value
// made the assertion panic INSIDE the handler. The host then saw
//
//	returned err  : <nil>
//	escaped panic : interface conversion: interface {} is runtime.errorString,
//	                not vm.RubyError
//
// — the cause gone, and none of the handler's cleanup run, so a host that
// recovers in a wrapper carried on with a VM holding a half-unwound frame stack.
//
// Fixing the guards removed #777's trigger; this test uses a native method that
// panics with a plain value so the handler's default arm is exercised directly,
// and would stay meaningful for any future non-RubyError panic.
func TestNonRubyErrorPanicKeepsItsCause(t *testing.T) {
	const cause = "a panic value that is not a RubyError"

	prog, err := parser.Parse(`def deep; boom; end
deep`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	machine := New(&buf)
	// callNative re-panics anything that is not a runtime.Error untouched, so a
	// plain string reaches Run's handler exactly as a broken VM invariant would.
	machine.cObject.defineArgc("boom", 0, func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		panic(cause)
	})

	// The whole point: Run must RETURN, not let a panic escape. Anything escaping
	// here is the original defect.
	var escaped any
	runErr := func() (e error) {
		defer func() { escaped = recover() }()
		_, e = machine.Run(iseq)
		return e
	}()
	if escaped != nil {
		t.Fatalf("a panic escaped Run: %v (the handler re-panicked, which is the defect)", escaped)
	}

	// (c): Run must not report success for a program that did not complete.
	if runErr == nil {
		t.Fatal("Run returned a nil error for a program that panicked (issue #773 from this side)")
	}
	// The cause must survive. Before the fix the only text naming it was gone.
	if !strings.Contains(runErr.Error(), cause) {
		t.Errorf("the cause did not survive: err = %q, want it to contain %q", runErr.Error(), cause)
	}

	// The handler's cleanup must have run: these are the frame stacks it resets,
	// and skipping them is what left a surviving host on an undefined VM.
	if n := len(machine.frameNames); n != 0 {
		t.Errorf("frameNames not reset: len = %d, want 0 (handler cleanup did not run)", n)
	}
	if n := len(machine.frameFiles); n != 0 {
		t.Errorf("frameFiles not reset: len = %d, want 0", n)
	}
	if n := len(machine.frameCrefs); n != 0 {
		t.Errorf("frameCrefs not reset: len = %d, want 0", n)
	}
	if n := len(machine.frameMethods); n != 0 {
		t.Errorf("frameMethods not reset: len = %d, want 0", n)
	}
	if n := len(machine.fileStack); n != 0 {
		t.Errorf("fileStack not reset: len = %d, want 0", n)
	}
}

// TestRunReportsAnErrorForARefusedAllocation closes the loop between the two
// defects on #777's own trigger: the expression that used to escape as
// `[recovered, repanicked]` with a nil error now comes back as an ordinary Ruby
// exception through Run's error return, with the frame stacks reset.
func TestRunReportsAnErrorForARefusedAllocation(t *testing.T) {
	machine, out, runErr := runKeepingError(t, `[1] * (2**62)`)
	if runErr == nil {
		t.Fatal("Run returned nil for a program that raised")
	}
	if got, want := runErr.Error(), "ArgumentError: argument too big"; got != want {
		t.Errorf("err = %q, want %q", got, want)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if n := len(machine.frameNames); n != 0 {
		t.Errorf("frameNames not reset: len = %d, want 0", n)
	}
}
