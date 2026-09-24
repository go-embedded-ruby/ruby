package vm_test

import (
	"strings"
	"testing"
)

// Kernel#warn's uplevel: level names a caller frame whose "path:lineno: " is
// prepended to the message — error.c v3_4_0 rb_warn_m, which takes the location
// from rb_ec_backtrace_location_ary(ec, lev + 1, 1, TRUE). Level 0 is the frame
// that called warn, and each further level walks one frame out.
func TestWarnUplevelPrefix(t *testing.T) {
	// Level 0 names f's own frame: the line the warn call sits on (2).
	got := eval(t, "$VERBOSE = true\ndef f; warn(\"x\", uplevel: 0); end\nf\n")
	if !strings.HasSuffix(got, ":2: warning: x\n") {
		t.Errorf("uplevel 0: got %q, want a ':2: warning: x' suffix", got)
	}
	// Level 1 names f's caller — the top-level frame, on line 3.
	got = eval(t, "$VERBOSE = true\ndef f; warn(\"x\", uplevel: 1); end\nf\n")
	if !strings.HasSuffix(got, ":3: warning: x\n") {
		t.Errorf("uplevel 1: got %q, want a ':3: warning: x' suffix", got)
	}
	// One prefix for the whole call, not one per message: rb_io_puts appends every
	// argument into the ONE buffer the prefix was written into.
	got = eval(t, "$VERBOSE = true\ndef f; warn(\"a\", \"b\", uplevel: 0); end\nf\n")
	if !strings.HasSuffix(got, ":2: warning: a\nb\n") {
		t.Errorf("two messages: got %q", got)
	}
}

// A level past the bottom of the stack has no location, and rb_warn_m then
// writes the bare "warning: " — which is what keeps `warn "x", uplevel: 100`
// from naming a frame that is not there.
func TestWarnUplevelPastBottom(t *testing.T) {
	if got := eval(t, "$VERBOSE = true\nwarn(\"x\", uplevel: 100)\n"); got != "warning: x\n" {
		t.Errorf("uplevel 100: got %q, want %q", got, "warning: x\n")
	}
	// A level too large to hold in an int is past the bottom of any stack; it is
	// rejected before the conversion to int could wrap it into a valid index.
	if got := eval(t, "$VERBOSE = true\nwarn(\"x\", uplevel: 9223372036854775807)\n"); got != "warning: x\n" {
		t.Errorf("uplevel maxint: got %q, want %q", got, "warning: x\n")
	}
}

// The uplevel level is converted ONCE. A #to_int that counts its calls must not
// see two, which is what a second toIntCoerce on the raise path used to cost.
func TestWarnUplevelConvertsLevelOnce(t *testing.T) {
	src := "$VERBOSE = true\n$n = 0\no = Object.new\ndef o.to_int; $n += 1; 0; end\nwarn(\"x\", uplevel: o)\n$stdout.puts $n\n"
	if got := eval(t, src); !strings.HasSuffix(got, "1\n") {
		t.Errorf("to_int call count: got %q, want a trailing 1", got)
	}
}

// A negative level is rb_warn_m's ArgumentError, reported with the converted
// value rather than the object handed in.
func TestWarnUplevelNegative(t *testing.T) {
	class, msg := evalErr(t, "$VERBOSE = true\nwarn(\"x\", uplevel: -2)\n")
	if class != "ArgumentError" || msg != "negative level (-2)" {
		t.Errorf("got %s: %q", class, msg)
	}
}

// rb_warn_m runs category: through rb_to_symbol_type, which is
// rb_convert_type_with_id(val, T_SYMBOL, "Symbol", idTo_sym): anything that
// answers #to_sym converts, and only a value that does not — or whose #to_sym
// gives a non-Symbol — is the TypeError.
func TestWarnCategoryToSym(t *testing.T) {
	// :experimental is enabled by default, so the converted category reaches the
	// filter and the message is written: the conversion is observable.
	src := "$VERBOSE = true\no = Object.new\ndef o.to_sym; :experimental; end\nwarn(\"x\", category: o)\n"
	if got := eval(t, src); got != "x\n" {
		t.Errorf("to_sym category: got %q, want %q", got, "x\n")
	}
	// A String converts through its own String#to_sym — no case of its own.
	if got := eval(t, "$VERBOSE = true\nwarn(\"x\", category: \"experimental\")\n"); got != "x\n" {
		t.Errorf("string category: got %q, want %q", got, "x\n")
	}
	class, msg := evalErr(t, "$VERBOSE = true\nwarn(\"x\", category: Object.new)\n")
	if class != "TypeError" || msg != "no implicit conversion of Object into Symbol" {
		t.Errorf("no to_sym: got %s: %q", class, msg)
	}
	class, msg = evalErr(t, "$VERBOSE = true\no = Object.new\ndef o.to_sym; 5; end\nwarn(\"x\", category: o)\n")
	if class != "TypeError" || msg != "can't convert Object to Symbol (Object#to_sym gives Integer)" {
		t.Errorf("bad to_sym: got %s: %q", class, msg)
	}
}

// Thread.each_caller_location yields each frame of the current execution stack
// as a Thread::Backtrace::Location and answers nil — vm_backtrace.c v3_4_0:1401,
// whose ec_backtrace_range(ec, argc, argv, 1, 1, &n) call gives it exactly
// Kernel#caller_locations' argument handling.
func TestThreadEachCallerLocation(t *testing.T) {
	// Both calls are made from the SAME frame and both default to level 1, so the
	// two lists must agree entry for entry — which is the equality ruby/spec's
	// core/thread/each_caller_location_spec.rb pins down.
	src := `def probe
  a = []
  Thread.each_caller_location { |l| a << l }
  p a.map(&:to_s) == caller_locations.map(&:to_s)
  p a[0].class
end
def mid; probe; end
mid
`
	if got := eval(t, src); got != "true\nThread::Backtrace::Location\n" {
		t.Errorf("got %q", got)
	}
	if got := eval(t, "p Thread.each_caller_location {}"); got != "nil\n" {
		t.Errorf("return value: got %q, want %q", got, "nil\n")
	}
	// The start/length pair is caller_locations' too: (1, 1) is the caller's frame
	// alone, from either spelling.
	src = `def probe
  x = nil
  Thread.each_caller_location(1, 1) { |l| x = l }
  p [x.to_s == caller_locations(1, 1)[0].to_s, x.to_s]
end
def mid; probe; end
mid
`
	if got := eval(t, src); !strings.HasPrefix(got, "[true, ") {
		t.Errorf("start/length: got %q", got)
	}
	// The Range form reaches callerSlice's Array#[] path.
	src = `def probe
  a = []
  Thread.each_caller_location(1..1) { |l| a << l }
  p a.size
end
def mid; probe; end
mid
`
	if got := eval(t, src); got != "1\n" {
		t.Errorf("range form: got %q", got)
	}
}

// A start past the top of the stack selects nothing: no yield, no error, nil.
// That is also why a block-less call over an empty selection is NOT the
// LocalJumpError — the error comes from the rb_yield, which never happens.
func TestThreadEachCallerLocationOvershoot(t *testing.T) {
	if got := eval(t, "p Thread.each_caller_location(100) { |l| p l }"); got != "nil\n" {
		t.Errorf("overshoot with block: got %q", got)
	}
	if got := eval(t, "p Thread.each_caller_location(100)"); got != "nil\n" {
		t.Errorf("overshoot without block: got %q", got)
	}
}

// Without a block the first yield is the LocalJumpError, message "no block
// given" (not the "(yield)" form a bare yield reports).
func TestThreadEachCallerLocationNoBlock(t *testing.T) {
	class, msg := evalErr(t, "def top; Thread.each_caller_location; end\ntop\n")
	if class != "LocalJumpError" || msg != "no block given" {
		t.Errorf("got %s: %q", class, msg)
	}
}

// ec_backtrace_range's rb_scan_args(argc, argv, "02:") caps the positionals at
// two, and its rb_get_kwargs runs against an EMPTY keyword table — so every
// keyword is an unknown one.
func TestThreadEachCallerLocationArgErrors(t *testing.T) {
	class, msg := evalErr(t, "Thread.each_caller_location(12, foo: 10) {}")
	if class != "ArgumentError" || msg != "unknown keyword: :foo" {
		t.Errorf("one keyword: got %s: %q", class, msg)
	}
	class, msg = evalErr(t, "Thread.each_caller_location(foo: 1, bar: 2) {}")
	if class != "ArgumentError" || msg != "unknown keywords: :foo, :bar" {
		t.Errorf("two keywords: got %s: %q", class, msg)
	}
	class, msg = evalErr(t, "Thread.each_caller_location(1, 2, 3) {}")
	if class != "ArgumentError" || msg != "wrong number of arguments (given 3, expected 0..2)" {
		t.Errorf("arity: got %s: %q", class, msg)
	}
	class, msg = evalErr(t, "Thread.each_caller_location(-1) {}")
	if class != "ArgumentError" || msg != "negative level (-1)" {
		t.Errorf("negative level: got %s: %q", class, msg)
	}
}
