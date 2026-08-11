package vm

import (
	"io"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestNumericCtorReraisesNonRubyError: with exception: false, numericCtor
// swallows a RubyError (yielding nil) but must let a non-RubyError panic
// propagate unchanged. Only a direct call can inject such a panic, since Ruby
// code raises RubyErrors exclusively.
func TestNumericCtorReraisesNonRubyError(t *testing.T) {
	vm := New(io.Discard)

	// A RubyError under exception: false is swallowed to nil.
	if got := vm.numericCtor(false, func() object.Value {
		return raise("ArgumentError", "boom")
	}); got != object.NilV {
		t.Fatalf("numericCtor swallow: got %v want nil", got)
	}

	// A non-RubyError panic propagates.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected the non-RubyError panic to propagate")
		}
		if _, ok := r.(RubyError); ok {
			t.Fatalf("RubyError leaked where a plain panic was expected: %#v", r)
		}
	}()
	vm.numericCtor(false, func() object.Value {
		panic("not a ruby error")
	})
}
