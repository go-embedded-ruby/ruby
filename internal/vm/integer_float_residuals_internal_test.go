package vm

import (
	"io"
	"math"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestGuardArgcSkipsMissing exercises the branch of guardArgc that skips a name
// the class does not define (so a rename never silently drops an arity check
// against a phantom method).
func TestGuardArgcSkipsMissing(t *testing.T) {
	vm := New(io.Discard)
	cls := newClass("GuardProbe", vm.cObject)
	// "present" gets a guard; "absent" is never defined, so guardArgc must skip it
	// rather than panic on the nil lookup.
	cls.define("present", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	guardArgc(cls, 1, 1, "present", "absent")
	if _, ok := cls.methods["absent"]; ok {
		t.Fatalf("guardArgc defined a phantom method for a missing name")
	}
	if _, ok := cls.methods["present"]; !ok {
		t.Fatalf("guardArgc dropped the present method")
	}
}

// TestArgcMessage covers both the equal-bounds and the range rendering.
func TestArgcMessage(t *testing.T) {
	if got := argcMessage(2, 1, 1); got != "wrong number of arguments (given 2, expected 1)" {
		t.Errorf("equal bounds: %q", got)
	}
	if got := argcMessage(2, 0, 1); got != "wrong number of arguments (given 2, expected 0..1)" {
		t.Errorf("range bounds: %q", got)
	}
}

// TestRubyFloatModEdges pins the signed-zero and non-finite-divisor behaviour of
// ruby_float_mod directly (the operator and method paths both funnel through it).
func TestRubyFloatModEdges(t *testing.T) {
	if got := rubyFloatMod(4.2, math.Inf(1)); got != 4.2 {
		t.Errorf("4.2 %% +Inf = %v, want 4.2", got)
	}
	if got := rubyFloatMod(4.2, math.Inf(-1)); !math.IsInf(got, -1) {
		t.Errorf("4.2 %% -Inf = %v, want -Inf", got)
	}
	if got := rubyFloatMod(math.Inf(1), 42); !math.IsNaN(got) {
		t.Errorf("Inf %% 42 = %v, want NaN", got)
	}
	if got := rubyFloatMod(math.Copysign(0, -1), 42); got != 0 || !math.Signbit(got) {
		t.Errorf("-0.0 %% 42 = %v, want -0.0", got)
	}
	if got := rubyFloatMod(4.0, 2.0); got != 0 {
		t.Errorf("4.0 %% 2.0 = %v, want 0", got)
	}
}
